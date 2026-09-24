package engine

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/media"
)

// In-dialog requests for both sides of a call: our own uacDialog for calls
// we made, sipgo's DialogServerSession (which builds From/To/CSeq/Route
// correctly) for calls we answered.

const inDialogTimeout = 32 * time.Second

func (c *Call) newInDialog(method sip.RequestMethod) *sip.Request {
	if c.uac != nil {
		return c.uac.inDialog(method)
	}
	req := sip.NewRequest(method, c.uas.InviteRequest.Contact().Address)
	req.AppendHeader(sip.HeaderClone(&c.dev.contact))
	req.Laddr = c.dev.laddr
	return req
}

// request sends an in-dialog request and waits for its final response.
func (c *Call) request(ctx context.Context, method sip.RequestMethod, body []byte, ctype string, hdrs ...sip.Header) (*sip.Response, error) {
	req := c.newInDialog(method)
	for _, h := range hdrs {
		req.AppendHeader(h)
	}
	if body != nil {
		req.AppendHeader(sip.NewHeader("Content-Type", ctype))
		req.SetBody(body)
	}
	c.trace.msg(true, req)
	start := time.Now()
	var res *sip.Response
	var err error
	if c.uac != nil {
		res, err = c.uac.do(ctx, req)
	} else {
		res, err = c.uas.Do(ctx, req)
	}
	c.dev.report(method.String(), res, err, time.Since(start))
	if res != nil {
		c.trace.msg(false, res)
	} else {
		c.trace.note(false, "%s failed: %v", method, err)
	}
	return res, err
}

// ackReinvite acknowledges the 2xx of an in-dialog INVITE we sent.
func (c *Call) ackReinvite(res *sip.Response) error {
	c.trace.note(true, "ACK")
	if c.uac != nil {
		return c.dev.client.WriteRequest(c.uac.inDialogAck(res.CSeq().SeqNo), sipgo.ClientRequestAddVia)
	}
	ack := sip.NewRequest(sip.ACK, c.uas.InviteRequest.Contact().Address)
	ack.Laddr = c.dev.laddr
	return c.uas.WriteRequest(ack)
}

// Hold puts the other side on hold with a re-INVITE (a=sendonly).
func (c *Call) Hold() bool { return c.reinvite(media.SendOnly) }

// Unhold resumes the call (a=sendrecv).
func (c *Call) Unhold() bool { return c.reinvite(media.SendRecv) }

// OnHold reports whether we put the call on hold.
func (c *Call) OnHold() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.localHold
}

// RemoteHold reports whether the other side put us on hold.
func (c *Call) RemoteHold() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.remoteHold
}

func (c *Call) reinvite(dir media.Direction) bool {
	if c.State() != StateConnected {
		return false
	}
	c.reinviteMu.Lock()
	defer c.reinviteMu.Unlock()
	for attempt := 0; ; attempt++ {
		ok, retry := c.reinviteOnce(dir, c.sessionHeaders()...)
		if !retry || attempt > 0 {
			return ok
		}
		// 491 Request Pending: both sides re-INVITEd at once (RFC 3261 14.1).
		time.Sleep(2100*time.Millisecond + rand.N(1900*time.Millisecond)) // #nosec G404 -- glare backoff
	}
}

// reinviteOnce returns (success, retry after 491).
func (c *Call) reinviteOnce(dir media.Direction, hdrs ...sip.Header) (bool, bool) {
	var body []byte
	var prev media.Direction
	if c.media != nil {
		prev = c.media.Direction()
		body = c.media.Offer(dir)
	} else {
		body = sdpAudioDir(c.dev.ip, c.dev.port+2, dir)
	}
	ctx, cancel := context.WithTimeout(c.dev.ctx, inDialogTimeout)
	defer cancel()
	res, err := c.request(ctx, sip.INVITE, body, "application/sdp", hdrs...)
	if err != nil || !res.IsSuccess() {
		if c.media != nil {
			c.media.SetDirection(prev)
		}
		return false, err == nil && res.StatusCode == 491
	}
	c.dev.logErr("ACK", c.ackReinvite(res))
	if c.media != nil && len(res.Body()) > 0 {
		if r, err := media.ParseSDP(res.Body()); err == nil {
			c.dev.logErr("re-INVITE answer", c.media.ApplyAnswer(r))
		}
	}
	c.mu.Lock()
	c.localHold = dir != media.SendRecv
	c.mu.Unlock()
	c.sessionRefreshed()
	return true, false
}

// onUpdate answers UPDATE (session refresh or media change).
func (d *Device) onUpdate(req *sip.Request, tx sip.ServerTransaction) {
	c := d.findCall(req)
	if c == nil {
		d.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)))
		return
	}
	c.trace.msg(false, req)
	var res *sip.Response
	if len(req.Body()) > 0 {
		res = sip.NewSDPResponseFromRequest(req, c.reofferAnswer(req.Body()))
	} else {
		res = sip.NewResponseFromRequest(req, 200, "OK", nil)
	}
	res.AppendHeader(sip.HeaderClone(&d.contact))
	c.addSessionHeaders(req, res)
	d.logErr("respond", tx.Respond(res))
	c.trace.msg(true, res)
	c.sessionRefreshed()
}

var errNotConnected = errors.New("call is not connected")
