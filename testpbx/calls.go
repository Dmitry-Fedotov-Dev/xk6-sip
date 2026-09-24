package testpbx

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// The PBX is a B2BUA: every call is two legs, one per endpoint, and the PBX
// relays between them. Media flows directly between the endpoints (SDP is
// passed through). Transfers are done by the PBX itself, like hosted PBXs
// do: the transferor's REFER never reaches the other party.

// leg is one dialog between the PBX and an endpoint.
type leg interface {
	callID() string
	user() *User
	// do sends an in-dialog request and waits for the final response.
	do(ctx context.Context, method sip.RequestMethod, body []byte, hdrs ...sip.Header) (*sip.Response, error)
	ack() error // ACK for the 2xx of our last in-dialog INVITE
	readBye(req *sip.Request, tx sip.ServerTransaction) error
	bye(ctx context.Context) error
	sdp() []byte // the endpoint's latest SDP
	setSDP([]byte)
}

type legBase struct {
	p  *PBX
	u  *User
	mu sync.Mutex
	sd []byte
}

func (l *legBase) user() *User { return l.u }
func (l *legBase) sdp() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sd
}
func (l *legBase) setSDP(b []byte) {
	if len(b) == 0 {
		return
	}
	l.mu.Lock()
	l.sd = b
	l.mu.Unlock()
}

func (l *legBase) newReq(method sip.RequestMethod, target sip.Uri, body []byte, hdrs []sip.Header) *sip.Request {
	req := sip.NewRequest(method, target)
	for _, h := range hdrs {
		req.AppendHeader(h)
	}
	if body != nil {
		if req.GetHeader("Content-Type") == nil {
			req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
		}
		req.SetBody(body)
	}
	req.Laddr = l.p.laddr
	return req
}

// uasLeg: the endpoint called the PBX.
type uasLeg struct {
	legBase
	s *sipgo.DialogServerSession
}

func (l *uasLeg) callID() string  { return l.s.InviteRequest.CallID().Value() }
func (l *uasLeg) target() sip.Uri { return l.s.InviteRequest.Contact().Address }
func (l *uasLeg) do(ctx context.Context, m sip.RequestMethod, body []byte, hdrs ...sip.Header) (*sip.Response, error) {
	return l.s.Do(ctx, l.newReq(m, l.target(), body, hdrs))
}
func (l *uasLeg) ack() error { return l.s.WriteRequest(l.newReq(sip.ACK, l.target(), nil, nil)) }
func (l *uasLeg) readBye(req *sip.Request, tx sip.ServerTransaction) error {
	return l.s.ReadBye(req, tx)
}
func (l *uasLeg) bye(ctx context.Context) error { return l.s.Bye(ctx) }

// uacLeg: the PBX called the endpoint.
type uacLeg struct {
	legBase
	s *sipgo.DialogClientSession
}

func (l *uacLeg) callID() string { return l.s.InviteRequest.CallID().Value() }
func (l *uacLeg) target() sip.Uri {
	if c := l.s.InviteResponse.Contact(); c != nil {
		return c.Address
	}
	return l.s.InviteRequest.Recipient
}
func (l *uacLeg) do(ctx context.Context, m sip.RequestMethod, body []byte, hdrs ...sip.Header) (*sip.Response, error) {
	return l.s.Do(ctx, l.newReq(m, l.target(), body, hdrs))
}
func (l *uacLeg) ack() error { return l.s.WriteRequest(l.newReq(sip.ACK, l.target(), nil, nil)) }
func (l *uacLeg) readBye(req *sip.Request, tx sip.ServerTransaction) error {
	return l.s.ReadBye(req, tx)
}
func (l *uacLeg) bye(ctx context.Context) error { return l.s.Bye(ctx) }

type bridge struct {
	x, y leg
}

func (b *bridge) other(l leg) leg {
	if l == b.x {
		return b.y
	}
	return b.x
}

func (p *PBX) link(b *bridge) {
	p.mu.Lock()
	p.legs[b.x.callID()], p.legs[b.y.callID()] = b, b
	p.mu.Unlock()
}

// find returns the bridge and leg a request belongs to.
func (p *PBX) find(req *sip.Request) (*bridge, leg) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := req.CallID().Value()
	if b := p.legs[id]; b != nil {
		if b.x.callID() == id {
			return b, b.x
		}
		return b, b.y
	}
	return nil, p.orphans[id]
}

func callerID(u *User) string {
	if u.Ext != "" {
		return u.Ext
	}
	return u.Name
}

func (p *PBX) contactOf(u *User) (sip.Uri, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.regs[u.Name]
	return c, ok
}

func (p *PBX) lookup(number string) *User {
	if u := p.exts[number]; u != nil {
		return u
	}
	return p.users[number]
}

// dial calls user u presenting caller ID from, with SDP offer, and waits
// for the answer. onRinging, if set, sees 180/183.
func (p *PBX) dial(ctx context.Context, u *User, from *User, offer []byte, onRinging func(*sip.Response)) (*uacLeg, error) {
	target, ok := p.contactOf(u)
	if !ok {
		return nil, &sipgo.ErrDialogResponse{Res: sip.NewResponse(480, "Temporarily Unavailable")}
	}
	req := sip.NewRequest(sip.INVITE, target)
	f := &sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: callerID(from), Host: p.cfg.Domain}}
	f.Params.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(f)
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: callerID(u), Host: p.cfg.Domain}})
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	req.SetBody(offer)
	s, err := p.dialogUA.WriteInvite(ctx, req)
	if err != nil {
		return nil, err
	}
	err = s.WaitAnswer(ctx, sipgo.AnswerOptions{OnResponse: func(res *sip.Response) error {
		if onRinging != nil && (res.StatusCode == 180 || res.StatusCode == 183) {
			onRinging(res)
		}
		return nil
	}})
	if err != nil {
		return nil, err
	}
	if err := s.Ack(context.Background()); err != nil {
		return nil, err
	}
	l := &uacLeg{legBase: legBase{p: p, u: u}, s: s}
	l.setSDP(s.InviteResponse.Body())
	return l, nil
}

func (p *PBX) onInvite(req *sip.Request, tx sip.ServerTransaction) {
	if to := req.To(); to != nil && to.Params.Has("tag") {
		p.onReInvite(req, tx)
		return
	}
	p.Stats.Invites.Add(1)

	caller := p.users[req.From().Address.User]
	if caller == nil {
		p.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 403, "Forbidden", nil)))
		return
	}
	if p.cfg.AuthInvite && !p.authorized(req, tx, caller, "Proxy-Authorization", 407) {
		return
	}
	callee := p.lookup(req.Recipient.User)
	if callee == nil {
		p.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 404, "Not Found", nil)))
		return
	}

	s, err := p.dialogUA.ReadInvite(req, tx)
	if err != nil {
		p.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 400, "Bad Request", nil)))
		return
	}
	defer func() { p.logErr("close", s.Close()) }()
	p.logErr("respond", s.Respond(100, "Trying", nil))
	a := &uasLeg{legBase: legBase{p: p, u: caller}, s: s}
	a.setSDP(req.Body())

	// The B leg is cancelled when A cancels.
	ctx, cancel := context.WithCancel(s.Context())
	defer cancel()
	b, err := p.dial(ctx, callee, caller, req.Body(), func(res *sip.Response) {
		p.logErr("respond", s.Respond(res.StatusCode, res.Reason, nil))
	})
	if err != nil {
		var re *sipgo.ErrDialogResponse
		switch {
		case errors.As(err, &re):
			p.logErr("respond", s.Respond(re.Res.StatusCode, re.Res.Reason, nil))
		case s.Context().Err() != nil:
			// A cancelled; the transaction layer already sent 487.
		default:
			p.logErr("respond", s.Respond(408, "Request Timeout", nil))
		}
		return
	}
	p.Stats.Answered.Add(1)
	p.link(&bridge{x: a, y: b})
	if err := s.WriteResponse(sip.NewSDPResponseFromRequest(s.InviteRequest, b.sdp())); err != nil {
		p.log.Warn("answer A leg", "error", err)
		p.unlink(a, b)
		p.logErr("dialog", b.bye(context.Background()))
	}
}

func (p *PBX) unlink(ls ...leg) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, l := range ls {
		delete(p.legs, l.callID())
		delete(p.orphans, l.callID())
	}
}

// onReInvite relays an in-dialog INVITE (hold, refresh) to the other leg.
func (p *PBX) onReInvite(req *sip.Request, tx sip.ServerTransaction) {
	br, l := p.find(req)
	if br == nil {
		p.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)))
		return
	}
	l.setSDP(req.Body())
	o := br.other(l)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := o.do(ctx, sip.INVITE, req.Body())
	if err != nil {
		p.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 500, "Server Internal Error", nil)))
		return
	}
	if res.IsSuccess() {
		p.logErr("ACK", o.ack())
		o.setSDP(res.Body())
	}
	fwd := sip.NewResponseFromRequest(req, res.StatusCode, res.Reason, res.Body())
	if len(res.Body()) > 0 {
		fwd.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	}
	fwd.AppendHeader(sip.HeaderClone(&p.dialogUA.ContactHDR))
	p.logErr("respond", tx.Respond(fwd))
}

func (p *PBX) onAck(req *sip.Request, tx sip.ServerTransaction) {
	if _, l := p.find(req); l != nil {
		if a, ok := l.(*uasLeg); ok {
			_ = a.s.ReadAck(req, tx) // fails harmlessly for ACKs of relayed re-INVITEs
		}
	}
}

func (p *PBX) onBye(req *sip.Request, tx sip.ServerTransaction) {
	br, l := p.find(req)
	if l == nil {
		p.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)))
		return
	}
	p.logErr("dialog", l.readBye(req, tx))
	if br == nil {
		p.unlink(l) // orphaned transferor leg
		return
	}
	o := br.other(l)
	p.unlink(l, o)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p.logErr("dialog", o.bye(ctx))
}

// ---- transfers

func (p *PBX) onRefer(req *sip.Request, tx sip.ServerTransaction) {
	br, transferor := p.find(req)
	if br == nil {
		p.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)))
		return
	}
	target, replaces, err := parseReferTo(req.GetHeader("Refer-To"))
	if err != nil {
		p.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 400, "Bad Refer-To", nil)))
		return
	}
	p.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 202, "Accepted", nil)))
	go func() {
		notify(transferor, "SIP/2.0 100 Trying", false)
		var status int
		if replaces != "" {
			status = p.attended(br, transferor, replaces)
		} else {
			status = p.blind(br, transferor, target)
		}
		notify(transferor, fmt.Sprintf("SIP/2.0 %d %s", status, reasonFor(status)), true)
	}()
}

func reasonFor(status int) string {
	if status == 200 {
		return "OK"
	}
	return "Transfer Failed"
}

// blind calls the target and connects it with the transferee; the
// transferor's leg is left for the transferor to hang up.
func (p *PBX) blind(br *bridge, transferor leg, target string) int {
	keep := br.other(transferor)
	user := target
	if u, err := url.Parse(target); err == nil && u.Opaque != "" {
		user, _, _ = strings.Cut(u.Opaque, "@")
	}
	c := p.lookup(user)
	if c == nil {
		return 404
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	nl, err := p.dial(ctx, c, keep.user(), keep.sdp(), nil)
	if err != nil {
		var re *sipgo.ErrDialogResponse
		if errors.As(err, &re) {
			return re.Res.StatusCode
		}
		return 500
	}
	// Point the transferee's media at the target.
	if !reinvite(keep, nl.sdp()) {
		p.logErr("dialog", nl.bye(ctx))
		return 500
	}
	p.mu.Lock()
	delete(p.legs, transferor.callID())
	p.orphans[transferor.callID()] = transferor
	p.mu.Unlock()
	p.link(&bridge{x: keep, y: nl})
	return 200
}

// attended joins the transferee with the far end of the transferor's
// consultation call named by Replaces, then drops the consultation leg.
func (p *PBX) attended(br *bridge, transferor leg, replaces string) int {
	callID, _, _ := strings.Cut(replaces, ";")
	p.mu.Lock()
	cb := p.legs[callID]
	p.mu.Unlock()
	if cb == nil || cb == br {
		return 481
	}
	var consult leg // transferor's side of the consultation call
	if cb.x.callID() == callID {
		consult = cb.x
	} else {
		consult = cb.y
	}
	keep, target := br.other(transferor), cb.other(consult)
	if !reinvite(keep, target.sdp()) || !reinvite(target, keep.sdp()) {
		return 500
	}
	p.mu.Lock()
	delete(p.legs, transferor.callID())
	delete(p.legs, consult.callID())
	p.orphans[transferor.callID()] = transferor
	p.mu.Unlock()
	p.link(&bridge{x: keep, y: target})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p.logErr("dialog", consult.bye(ctx))
	return 200
}

// reinvite sends l a re-INVITE with offer and ACKs the answer.
func reinvite(l leg, offer []byte) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := l.do(ctx, sip.INVITE, offer)
	if err != nil || !res.IsSuccess() {
		return false
	}
	l.setSDP(res.Body())
	return l.ack() == nil
}

func notify(l leg, frag string, final bool) {
	state := "active;expires=60"
	if final {
		state = "terminated;reason=noresource"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = l.do(ctx, sip.NOTIFY, []byte(frag+"\r\n"),
		sip.NewHeader("Content-Type", "message/sipfrag;version=2.0"),
		sip.NewHeader("Event", "refer"),
		sip.NewHeader("Subscription-State", state))
}

func parseReferTo(h sip.Header) (target, replaces string, err error) {
	if h == nil {
		return "", "", errors.New("no Refer-To")
	}
	v := strings.TrimSpace(h.Value())
	if i := strings.IndexByte(v, '<'); i >= 0 {
		if j := strings.IndexByte(v[i:], '>'); j > 0 {
			v = v[i+1 : i+j]
		}
	}
	uri, headers, _ := strings.Cut(v, "?")
	for _, kv := range strings.Split(headers, "&") {
		k, val, _ := strings.Cut(kv, "=")
		if strings.EqualFold(k, "Replaces") {
			if replaces, err = url.QueryUnescape(val); err != nil {
				return "", "", err
			}
		}
	}
	return uri, replaces, nil
}
