package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

// Reliable provisional responses (RFC 3262).

// hasOption reports whether a Require/Supported-style header lists tag.
func hasOption(m interface{ GetHeaders(string) []sip.Header }, name, tag string) bool {
	for _, h := range m.GetHeaders(name) {
		for _, v := range strings.Split(h.Value(), ",") {
			if strings.EqualFold(strings.TrimSpace(v), tag) {
				return true
			}
		}
	}
	return false
}

// UAC side: PRACK every new reliable 18x.
func (u *uacDialog) maybePRACK(c *Call, res *sip.Response) {
	if !hasOption(res, "Require", "100rel") {
		return
	}
	h := res.GetHeader("RSeq")
	if h == nil {
		return
	}
	rseq, err := strconv.ParseUint(strings.TrimSpace(h.Value()), 10, 32)
	if err != nil {
		return
	}
	u.mu.Lock()
	if rseq <= u.lastRSeq { // retransmission of an already acknowledged 18x
		u.mu.Unlock()
		return
	}
	u.lastRSeq = rseq
	u.earlyLocked(res)
	u.mu.Unlock()

	go func() {
		req := u.inDialog(sip.PRACK)
		req.AppendHeader(sip.NewHeader("RAck", fmt.Sprintf("%d %d INVITE", rseq, u.invite.CSeq().SeqNo)))
		c.trace.msg(true, req)
		ctx, cancel := context.WithTimeout(u.d.ctx, inDialogTimeout)
		defer cancel()
		start := time.Now()
		pr, err := u.do(ctx, req)
		u.d.report("PRACK", pr, err, time.Since(start))
		if pr != nil {
			c.trace.msg(false, pr)
		}
	}()
}

// earlyLocked sets up the early dialog from a provisional response so that
// PRACK can be sent within it. Must hold u.mu.
func (u *uacDialog) earlyLocked(res *sip.Response) {
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

// UAS side: send 180 reliably and retransmit it until PRACK arrives.
func (c *Call) ringReliably(sess *sipgo.DialogServerSession) error {
	res := sip.NewResponseFromRequest(sess.InviteRequest, 180, "Ringing", nil)
	res.AppendHeader(sip.NewHeader("Require", "100rel"))
	res.AppendHeader(sip.NewHeader("RSeq", "1"))
	if err := sess.WriteResponse(res); err != nil {
		return err
	}
	c.trace.note(true, "SIP/2.0 180 Ringing (reliable)")
	go func() {
		interval := sip.T1
		deadline := time.After(64 * sip.T1)
		for {
			select {
			case <-c.prack:
				return
			case <-c.connected:
				return
			case <-c.done:
				return
			case <-deadline:
				return
			case <-time.After(interval):
				if c.State() >= StateConnected {
					return
				}
				c.dev.logErr("180 retransmission", sess.WriteResponse(res))
				interval = min(2*interval, sip.T2)
			}
		}
	}()
	return nil
}

func (d *Device) onPrack(req *sip.Request, tx sip.ServerTransaction) {
	c := d.findCall(req)
	if c == nil {
		d.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)))
		return
	}
	c.trace.msg(false, req)
	d.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)))
	c.trace.note(true, "SIP/2.0 200 OK (PRACK)")
	c.prackOnce.Do(func() { close(c.prack) })
}

// ring sends 180, reliably if we do PRACK and the caller supports it.
func (c *Call) ring(sess *sipgo.DialogServerSession, req *sip.Request) error {
	if hasOption(req, "Require", "100rel") || (c.dev.cfg.PRACK && hasOption(req, "Supported", "100rel")) {
		return c.ringReliably(sess)
	}
	if err := sess.Respond(180, "Ringing", nil); err != nil {
		return err
	}
	c.trace.note(true, "SIP/2.0 180 Ringing")
	return nil
}
