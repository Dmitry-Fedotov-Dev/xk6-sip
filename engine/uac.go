package engine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
)

// uacDialog is the client side of an INVITE dialog. We don't use sipgo's
// DialogClientSession because it mutates the INVITE on auth retry (the ACK
// for the 401/407 then carries the wrong branch) and builds CANCEL towards
// the Request-URI host instead of the outbound proxy.
type uacDialog struct {
	d      *Device
	invite *sip.Request // last INVITE sent
	tx     sip.ClientTransaction

	mu           sync.Mutex
	res          *sip.Response // 2xx that established the dialog
	remoteTarget sip.Uri
	routes       []string // route set, from Record-Route in reverse order
	cseq         uint32
	lastRSeq     uint64 // highest RSeq we PRACKed
}

const maxAuthAttempts = 2

var errCancelled = errors.New("cancelled")

// inviteResult is the outcome of the INVITE transaction(s).
type inviteResult struct {
	res       *sip.Response // final response, nil on transport error/timeout
	err       error
	cancelled bool // CANCEL was sent (script hangup or no-answer timeout)
}

func newUAC(d *Device, invite *sip.Request) *uacDialog {
	return &uacDialog{d: d, invite: invite}
}

func (u *uacDialog) send(ctx context.Context) error {
	tx, err := u.d.client.TransactionRequest(ctx, u.invite)
	if err != nil {
		return err
	}
	u.tx = tx
	u.cseq = u.invite.CSeq().SeqNo
	return nil
}

// run drives the INVITE until a final response. onResponse sees every
// response; cancel is closed to request CANCEL.
func (u *uacDialog) run(c *Call, cancel <-chan struct{}, noAnswer time.Duration) inviteResult {
	d := u.d
	sent := c.tStart
	var provisional, cancelSent bool
	authAttempts := 0
	retried422 := false
	timer := time.NewTimer(noAnswer)
	defer timer.Stop()
	var cancelWait <-chan time.Time

	sendCancel := func() {
		if cancelSent {
			return
		}
		cancelSent = true
		c.trace.note(true, "CANCEL")
		go u.cancel()
		// A UAS may never answer CANCEL with 487 (RFC 3261 9.1).
		cancelWait = time.After(64 * sip.T1)
	}
	requestCancel := func() {
		cancel = nil
		timer.Stop()
		// CANCEL is only allowed after a provisional response.
		if provisional {
			sendCancel()
		}
	}

	for {
		select {
		case res := <-u.tx.Responses():
			now := time.Now()
			c.trace.msg(false, res)
			c.remoteSDP(res)
			switch {
			case res.IsProvisional():
				provisional = true
				u.maybePRACK(c, res)
				if res.StatusCode > 100 {
					c.markRinging(now)
				}
				if cancel == nil && !cancelSent {
					sendCancel()
				}
				continue
			case res.IsSuccess():
				d.report("INVITE", res, nil, now.Sub(sent))
				u.established(res)
				return inviteResult{res: res, cancelled: cancelSent}
			case (res.StatusCode == 401 || res.StatusCode == 407) && authAttempts < maxAuthAttempts && d.cfg.Password != "" && !cancelSent:
				d.report("INVITE", res, nil, now.Sub(sent))
				authAttempts++
				if err := u.retryWithAuth(res); err != nil {
					return inviteResult{res: res, err: err}
				}
				sent = time.Now()
				provisional = false
				c.trace.note(true, "INVITE (with credentials)")
				continue
			case res.StatusCode == 422 && !retried422 && !cancelSent:
				d.report("INVITE", res, nil, now.Sub(sent))
				retried422 = true
				if err := u.retryWithMinSE(res); err != nil {
					return inviteResult{res: res, err: err}
				}
				sent = time.Now()
				provisional = false
				c.trace.note(true, "INVITE (Session-Expires raised to Min-SE)")
				continue
			default:
				if !(cancelSent && res.StatusCode == 487) {
					d.report("INVITE", res, nil, now.Sub(sent))
				}
				return inviteResult{res: res, cancelled: cancelSent}
			}
		case <-u.tx.Done():
			err := u.tx.Err()
			if err == nil {
				err = errors.New("INVITE transaction terminated")
			}
			if !cancelSent {
				d.report("INVITE", nil, err, time.Since(sent))
			}
			return inviteResult{err: err, cancelled: cancelSent}
		case <-cancel:
			requestCancel()
		case <-timer.C:
			requestCancel()
			c.trace.note(true, "no answer in %v", noAnswer)
		case <-cancelWait:
			return inviteResult{err: errCancelled, cancelled: true}
		}
	}
}

func (u *uacDialog) retryWithAuth(chal *sip.Response) error {
	d := u.d
	hdrName, credName := "WWW-Authenticate", "Authorization"
	if chal.StatusCode == 407 {
		hdrName, credName = "Proxy-Authenticate", "Proxy-Authorization"
	}
	h := chal.GetHeader(hdrName)
	if h == nil {
		return fmt.Errorf("%d without %s", chal.StatusCode, hdrName)
	}
	ch, err := digest.ParseChallenge(h.Value())
	if err != nil {
		return fmt.Errorf("parse %s: %w", hdrName, err)
	}
	// A fresh request: the old one is still the origin of the challenged
	// transaction, which ACKs it with the original Via.
	req := u.invite.Clone()
	req.RemoveHeader("Via")
	req.RemoveHeader(credName)
	req.CSeq().SeqNo++
	cred, err := digest.Digest(ch, digest.Options{
		Method:   sip.INVITE.String(),
		URI:      req.Recipient.Addr(),
		Username: d.authUser,
		Password: d.cfg.Password,
	})
	if err != nil {
		return err
	}
	req.AppendHeader(sip.NewHeader(credName, cred.String()))

	tx, err := d.client.TransactionRequest(d.ctx, req, sipgo.ClientRequestAddVia)
	if err != nil {
		return err
	}
	u.invite, u.tx = req, tx
	u.mu.Lock()
	u.cseq = req.CSeq().SeqNo
	u.mu.Unlock()
	return nil
}

func (u *uacDialog) cancel() {
	d, inv := u.d, u.invite
	req := sip.NewRequest(sip.CANCEL, inv.Recipient)
	req.AppendHeader(sip.HeaderClone(inv.Via())) // must match the INVITE's top Via
	req.AppendHeader(sip.HeaderClone(inv.From()))
	req.AppendHeader(sip.HeaderClone(inv.To()))
	req.AppendHeader(sip.HeaderClone(inv.CallID()))
	req.AppendHeader(&sip.CSeqHeader{SeqNo: inv.CSeq().SeqNo, MethodName: sip.CANCEL})
	maxFwd := sip.MaxForwardsHeader(70)
	req.AppendHeader(&maxFwd)
	sip.CopyHeaders("Route", inv, req)
	req.SetBody(nil)
	req.SetDestination(inv.Destination())
	req.Laddr = inv.Laddr

	ctx, cancel := context.WithTimeout(d.ctx, 32*time.Second)
	defer cancel()
	start := time.Now()
	res, err := d.client.Do(ctx, req, func(*sipgo.Client, *sip.Request) error { return nil })
	d.report("CANCEL", res, err, time.Since(start))
}

func (u *uacDialog) established(res *sip.Response) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.res = res
	u.remoteTarget = u.invite.Recipient
	if c := res.Contact(); c != nil {
		u.remoteTarget = c.Address
	}
	rr := res.GetHeaders("Record-Route")
	u.routes = u.routes[:0]
	for i := len(rr) - 1; i >= 0; i-- {
		u.routes = append(u.routes, rr[i].Value())
	}
}

// inDialog builds a request within the dialog (RFC 3261 12.2.1.1).
// ACK reuses the INVITE's CSeq number; other methods take the next one.
func (u *uacDialog) inDialog(method sip.RequestMethod) *sip.Request {
	u.mu.Lock()
	defer u.mu.Unlock()
	seq := u.invite.CSeq().SeqNo
	if method != sip.ACK {
		u.cseq++
		seq = u.cseq
	}
	req := sip.NewRequest(method, u.remoteTarget)
	req.AppendHeader(sip.HeaderClone(u.invite.From()))
	req.AppendHeader(sip.HeaderClone(u.res.To()))
	req.AppendHeader(sip.HeaderClone(u.invite.CallID()))
	req.AppendHeader(&sip.CSeqHeader{SeqNo: seq, MethodName: method})
	maxFwd := sip.MaxForwardsHeader(70)
	req.AppendHeader(&maxFwd)
	for _, r := range u.routes {
		req.AppendHeader(sip.NewHeader("Route", r))
	}
	req.AppendHeader(sip.HeaderClone(&u.d.contact))
	req.SetBody(nil)
	req.Laddr = u.invite.Laddr
	return req
}

// inDialogAck builds the ACK for the 2xx of an in-dialog INVITE with CSeq seq.
func (u *uacDialog) inDialogAck(seq uint32) *sip.Request {
	ack := u.inDialog(sip.ACK)
	ack.CSeq().SeqNo = seq
	return ack
}

// ack sends ACK for the 2xx and re-sends it on 2xx retransmissions.
func (u *uacDialog) ack() error {
	ack := u.inDialog(sip.ACK)
	if err := u.d.client.WriteRequest(ack, sipgo.ClientRequestAddVia); err != nil {
		return err
	}
	retrans := ack.Clone()
	u.tx.OnRetransmission(func(r *sip.Response) {
		if r.IsSuccess() {
			u.d.logErr("ACK retransmission", u.d.client.WriteRequest(retrans))
		}
	})
	return nil
}

// do sends an in-dialog request and waits for its final response.
func (u *uacDialog) do(ctx context.Context, req *sip.Request) (*sip.Response, error) {
	tx, err := u.d.client.TransactionRequest(ctx, req, sipgo.ClientRequestAddVia)
	if err != nil {
		return nil, err
	}
	defer tx.Terminate()
	for {
		select {
		case res := <-tx.Responses():
			if res.IsProvisional() {
				continue
			}
			return res, nil
		case <-tx.Done():
			if err := tx.Err(); err != nil {
				return nil, err
			}
			return nil, errors.New("transaction terminated")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// retryWithMinSE re-sends the INVITE after 422 Session Interval Too Small
// with the interval the peer demands (RFC 4028 section 6).
func (u *uacDialog) retryWithMinSE(res *sip.Response) error {
	h := res.GetHeader("Min-SE")
	if h == nil {
		return errors.New("422 without Min-SE")
	}
	minSE, _, _ := strings.Cut(strings.TrimSpace(h.Value()), ";")
	if _, err := strconv.Atoi(minSE); err != nil {
		return fmt.Errorf("bad Min-SE %q", h.Value())
	}
	req := u.invite.Clone()
	req.RemoveHeader("Via")
	req.RemoveHeader("Session-Expires")
	req.RemoveHeader("Min-SE")
	req.CSeq().SeqNo++
	req.AppendHeader(sip.NewHeader("Session-Expires", minSE))
	req.AppendHeader(sip.NewHeader("Min-SE", minSE))
	tx, err := u.d.client.TransactionRequest(u.d.ctx, req, sipgo.ClientRequestAddVia)
	if err != nil {
		return err
	}
	u.invite, u.tx = req, tx
	u.mu.Lock()
	u.cseq = req.CSeq().SeqNo
	u.mu.Unlock()
	return nil
}
