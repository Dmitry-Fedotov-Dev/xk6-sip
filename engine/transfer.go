package engine

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
)

// Call transfer: REFER (RFC 3515) with the NOTIFY progress subscription,
// attended transfer with Replaces (RFC 3891, RFC 5589), and the transferee
// and transfer-target roles for endpoints behind a proxy that does not
// handle transfers itself.

// transferProgress follows the NOTIFYs for a REFER we sent.
type transferProgress struct {
	mu     sync.Mutex
	status int // last sipfrag status
	done   chan struct{}
	once   sync.Once
}

func (t *transferProgress) update(status int, final bool) {
	t.mu.Lock()
	t.status = status
	t.mu.Unlock()
	if final {
		t.once.Do(func() { close(t.done) })
	}
}

// tags returns our and the peer's dialog tags.
func (c *Call) tags() (local, remote string) {
	if c.uac != nil {
		c.uac.mu.Lock()
		defer c.uac.mu.Unlock()
		local, _ = c.uac.invite.From().Params.Get("tag")
		if c.uac.res != nil {
			remote, _ = c.uac.res.To().Params.Get("tag")
		}
		return local, remote
	}
	local, _ = c.uas.InviteRequest.To().Params.Get("tag")
	remote, _ = c.uas.InviteRequest.From().Params.Get("tag")
	return local, remote
}

// remoteURI is the other party as addressed in this dialog: the dialled URI
// for calls we made, the caller's From URI for calls we answered.
func (c *Call) remoteURI() sip.Uri {
	if c.uac != nil {
		return c.uac.invite.Recipient
	}
	return c.uas.InviteRequest.From().Address
}

// Transfer is a blind transfer: REFER the other party to target (a number
// or SIP URI). It returns once the REFER is accepted; ExpectTransferred
// follows the outcome.
func (c *Call) Transfer(target string) bool {
	uri, err := c.dev.targetURI(target)
	if err != nil {
		return false
	}
	return c.refer("<" + uri.String() + ">")
}

// AttendedTransfer connects the other party of c with the other party of
// consult (a call we have with the transfer target) using Replaces.
func (c *Call) AttendedTransfer(consult *Call) bool {
	if consult == nil || consult.State() != StateConnected {
		return false
	}
	local, remote := consult.tags()
	// Tags as seen by the UA receiving the INVITE with Replaces: the other
	// end of the consultation dialog.
	replaces := url.QueryEscape(fmt.Sprintf("%s;to-tag=%s;from-tag=%s", consult.callID, remote, local))
	target := consult.remoteURI()
	return c.refer(fmt.Sprintf("<%s?Replaces=%s>", target.String(), replaces))
}

func (c *Call) refer(referTo string) bool {
	if c.State() != StateConnected {
		return false
	}
	p := &transferProgress{done: make(chan struct{})}
	c.mu.Lock()
	c.xfer = p
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(c.dev.ctx, inDialogTimeout)
	defer cancel()
	by := sip.Uri{Scheme: "sip", User: c.dev.aor.User, Host: c.dev.aor.Host}
	res, err := c.request(ctx, sip.REFER, nil, "",
		sip.NewHeader("Refer-To", referTo),
		sip.NewHeader("Referred-By", "<"+by.String()+">"))
	if err != nil || !res.IsSuccess() {
		p.update(0, true)
		return false
	}
	return true
}

// ExpectTransferred waits for the final NOTIFY of our REFER and reports
// whether the transfer succeeded (sipfrag 2xx).
func (c *Call) ExpectTransferred(timeout time.Duration) bool {
	c.mu.Lock()
	p := c.xfer
	c.mu.Unlock()
	if p == nil {
		return false
	}
	if !c.wait(p.done, timeout) {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status >= 200 && p.status < 300
}

func (d *Device) onNotify(req *sip.Request, tx sip.ServerTransaction) {
	d.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)))
	c := d.findCall(req)
	if c == nil {
		return // out-of-dialog NOTIFY (e.g. MWI): acknowledged and ignored
	}
	c.trace.msg(false, req)
	ev := req.GetHeader("Event")
	if ev == nil || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ev.Value())), "refer") {
		return
	}
	c.mu.Lock()
	p := c.xfer
	c.mu.Unlock()
	if p == nil {
		return
	}
	status := sipfragStatus(req.Body())
	terminated := false
	if ss := req.GetHeader("Subscription-State"); ss != nil {
		terminated = strings.HasPrefix(strings.ToLower(strings.TrimSpace(ss.Value())), "terminated")
	}
	p.update(status, status >= 200 || terminated)
}

// sipfragStatus parses "SIP/2.0 200 OK" from a message/sipfrag body.
func sipfragStatus(body []byte) int {
	line, _, _ := strings.Cut(string(body), "\n")
	f := strings.Fields(line)
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(f[1])
	return n
}

// ---- transferee: we received REFER

func (d *Device) onRefer(req *sip.Request, tx sip.ServerTransaction) {
	c := d.findCall(req)
	if c == nil {
		d.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)))
		return
	}
	c.trace.msg(false, req)
	target, replaces, err := parseReferTo(req.GetHeader("Refer-To"))
	if err != nil {
		d.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 400, "Bad Refer-To", nil)))
		return
	}
	d.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 202, "Accepted", nil)))
	c.trace.note(true, "SIP/2.0 202 Accepted (REFER)")

	hdrs := map[string]string{}
	if rb := req.GetHeader("Referred-By"); rb != nil {
		hdrs["Referred-By"] = rb.Value()
	}
	if replaces != "" {
		hdrs["Replaces"] = replaces
	}
	go c.followRefer(target, hdrs)
}

func (c *Call) followRefer(target string, hdrs map[string]string) {
	c.notifyRefer("SIP/2.0 100 Trying", false)
	nc, err := c.dev.Call(CallOptions{Target: target, Headers: hdrs})
	if err != nil {
		c.notifyRefer("SIP/2.0 503 Service Unavailable", true)
		return
	}
	select {
	case c.referred <- nc:
	default:
	}
	switch {
	case nc.ExpectConnected(time.Minute):
		c.notifyRefer("SIP/2.0 200 OK", true)
	default:
		code := nc.Status()
		if code < 300 {
			code = 487
		}
		c.notifyRefer(fmt.Sprintf("SIP/2.0 %d Transfer failed", code), true)
	}
}

func (c *Call) notifyRefer(frag string, final bool) {
	state := "active;expires=60"
	if final {
		state = "terminated;reason=noresource"
	}
	ctx, cancel := context.WithTimeout(c.dev.ctx, inDialogTimeout)
	defer cancel()
	_, _ = c.request(ctx, sip.NOTIFY, []byte(frag+"\r\n"), "message/sipfrag;version=2.0",
		sip.NewHeader("Event", "refer"),
		sip.NewHeader("Subscription-State", state))
}

// ReferredCall waits for the call we placed because the other side
// transferred us (REFER we received).
func (c *Call) ReferredCall(timeout time.Duration) *Call {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case nc := <-c.referred:
		return nc
	case <-t.C:
		return nil
	}
}

// parseReferTo splits "<sip:703@pbx?Replaces=abc%3Bto-tag%3D1...>" into the
// target URI and the unescaped Replaces value.
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
	if uri == "" {
		return "", "", errors.New("empty Refer-To")
	}
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

// ---- transfer target: INVITE with Replaces

// replacedCall finds the call an incoming "Replaces: callid;to-tag=;from-tag="
// refers to (to-tag is ours).
func (d *Device) replacedCall(req *sip.Request) (*Call, bool) {
	h := req.GetHeader("Replaces")
	if h == nil {
		return nil, false
	}
	callID, params, _ := strings.Cut(strings.TrimSpace(h.Value()), ";")
	var toTag, fromTag string
	for _, p := range strings.Split(params, ";") {
		k, v, _ := strings.Cut(strings.TrimSpace(p), "=")
		switch strings.ToLower(k) {
		case "to-tag":
			toTag = v
		case "from-tag":
			fromTag = v
		}
	}
	d.callsMu.Lock()
	c := d.calls[callID]
	d.callsMu.Unlock()
	if c == nil {
		return nil, true
	}
	if local, remote := c.tags(); local != toTag || remote != fromTag {
		return nil, true
	}
	return c, true
}
