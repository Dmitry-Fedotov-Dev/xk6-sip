package engine

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

type Direction int

const (
	Outgoing Direction = iota
	Incoming
)

func (d Direction) String() string {
	if d == Incoming {
		return "in"
	}
	return "out"
}

type State int

const (
	StateCalling   State = iota // INVITE sent / received, no 18x yet
	StateRinging                // 18x received / sent
	StateConnected              // 200 OK + ACK
	StateEnded
)

func (s State) String() string {
	return [...]string{"calling", "ringing", "connected", "ended"}[s]
}

type EndedBy string

const (
	EndedByLocal   EndedBy = "local"
	EndedByRemote  EndedBy = "remote"
	EndedByTimeout EndedBy = "timeout"
	EndedByError   EndedBy = "error"
)

type decision struct {
	code   int
	reason string
}

// Call is one leg as seen by a device. Actions (Hangup, Accept, Reject)
// never wait for a script step on another device; only Expect* methods wait,
// so a single script can drive both ends of a call.
type Call struct {
	dev    *Device
	dir    Direction
	label  string
	callID string
	remote string // callee target (out) or caller number (in)
	trace  tracer

	uac        *uacDialog
	uas        *sipgo.DialogServerSession
	cancelReq  chan struct{} // closed to CANCEL an outgoing INVITE
	cancelOnce sync.Once
	decision   chan decision

	mu        sync.Mutex
	state     State
	status    int
	reason    string
	endedBy   EndedBy
	tStart    time.Time // INVITE sent or received
	tRing     time.Time
	tAnswer   time.Time
	tEnd      time.Time
	ringing   chan struct{}
	connected chan struct{}
	done      chan struct{}
}

func newCall(d *Device, dir Direction) *Call {
	return &Call{
		dev:       d,
		dir:       dir,
		trace:     tracer{full: d.eng.opts.TraceBodies},
		decision:  make(chan decision, 1),
		cancelReq: make(chan struct{}),
		ringing:   make(chan struct{}),
		connected: make(chan struct{}),
		done:      make(chan struct{}),
	}
}

func (c *Call) Direction() Direction { return c.dir }
func (c *Call) CallID() string       { return c.callID }
func (c *Call) Label() string        { return c.label }

// Remote is the callee target for outgoing calls, the caller number for
// incoming ones.
func (c *Call) Remote() string { return c.remote }

func (c *Call) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Status is the final INVITE status, or the rejecting code; 0 until known.
func (c *Call) Status() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Completion describes how the call ended.
type Completion struct {
	EndedBy  EndedBy
	Status   int
	Reason   string
	Duration time.Duration // talk time; 0 if never connected
}

func (c *Call) Completion() (Completion, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != StateEnded {
		return Completion{}, false
	}
	comp := Completion{EndedBy: c.endedBy, Status: c.status, Reason: c.reason}
	if !c.tAnswer.IsZero() {
		comp.Duration = c.tEnd.Sub(c.tAnswer)
	}
	return comp, true
}

func (c *Call) Trace() []TraceEntry { return c.trace.snapshot() }

func (c *Call) markRinging(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state >= StateRinging {
		return
	}
	c.state = StateRinging
	c.tRing = t
	close(c.ringing)
}

func (c *Call) markConnected(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state >= StateConnected {
		return
	}
	if c.state < StateRinging {
		close(c.ringing)
	}
	c.state = StateConnected
	c.status = 200
	c.tAnswer = t
	close(c.connected)
}

// finish ends the call once; later calls are ignored.
func (c *Call) finish(by EndedBy, status int, reason string) {
	c.mu.Lock()
	if c.state == StateEnded {
		c.mu.Unlock()
		return
	}
	wasConnected := c.state == StateConnected
	if c.state < StateRinging {
		close(c.ringing)
	}
	c.state = StateEnded
	c.endedBy = by
	if status != 0 && !wasConnected {
		c.status = status
	}
	c.reason = reason
	c.tEnd = time.Now()
	dur := c.tEnd.Sub(c.tAnswer)
	close(c.done)
	c.mu.Unlock()

	c.dev.removeCall(c)
	if wasConnected {
		c.dev.observer().CallEnd(CallEndEvent{
			Device: c.dev.cfg.ID, Direction: c.dir, Duration: dur, EndedBy: by,
		})
	}
}

// answer sends 200 OK and waits for ACK. Runs in the INVITE handler.
func (c *Call) answer(sess *sipgo.DialogServerSession) {
	d := c.dev
	now := time.Now()
	res := sip.NewSDPResponseFromRequest(sess.InviteRequest, sdpAudio(d.ip, d.mediaPort()))
	c.trace.msg(true, res)
	if err := sess.WriteResponse(res); err != nil {
		c.finish(EndedByError, 0, "answer: "+err.Error())
		return
	}
	c.markConnected(now)
}

// Accept answers a ringing incoming call. The 200 OK/ACK exchange happens in
// the background; use ExpectConnected to wait for it.
func (c *Call) Accept() bool {
	return c.decide(decision{200, "OK"})
}

// Reject declines a ringing incoming call with the given final status.
func (c *Call) Reject(code int, reason string) bool {
	if code < 300 || code > 699 {
		code, reason = 603, "Decline"
	}
	if reason == "" {
		reason = "Rejected"
	}
	return c.decide(decision{code, reason})
}

func (c *Call) decide(dec decision) bool {
	if c.dir != Incoming || c.State() >= StateConnected {
		return false
	}
	select {
	case c.decision <- dec:
		return true
	default:
		return false // already decided
	}
}

// Hangup ends the call in whatever state it is: CANCEL or BYE for outgoing
// calls, reject or BYE for incoming ones. It returns once the call has
// ended locally, or false if that did not happen within 32s.
func (c *Call) Hangup() bool {
	switch c.State() {
	case StateEnded:
		return true
	case StateConnected:
		return c.bye()
	}
	if c.dir == Incoming {
		c.Reject(603, "Decline")
	} else if c.cancelReq != nil {
		c.requestCancel(EndedByLocal)
	}
	// The INVITE may have been answered meanwhile.
	select {
	case <-c.done:
		return true
	case <-c.connected:
		return c.bye()
	case <-time.After(32 * time.Second):
		return false
	}
}

func (c *Call) bye() bool {
	d := c.dev
	ctx, cancel := context.WithTimeout(d.ctx, 32*time.Second)
	defer cancel()
	c.trace.note(true, "BYE")
	start := time.Now()
	var res *sip.Response
	var err error
	if c.uac != nil {
		res, err = c.uac.do(ctx, c.uac.inDialog(sip.BYE))
	} else {
		err = c.uas.Bye(ctx)
		var resErr sipgo.ErrDialogResponse
		if errors.As(err, &resErr) {
			res, err = resErr.Res, nil
		} else if err == nil {
			res = sip.NewResponse(200, "OK")
		}
	}
	d.report("BYE", res, err, time.Since(start))
	if res != nil {
		c.trace.note(false, "SIP/2.0 %d %s (BYE)", res.StatusCode, res.Reason)
	} else {
		c.trace.note(false, "BYE failed: %v", err)
	}
	// Even if BYE failed the call is over for us.
	c.finish(EndedByLocal, 0, "BYE")
	return err == nil && res.IsSuccess()
}

func (c *Call) wait(ch <-chan struct{}, timeout time.Duration) bool {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-ch:
		return true
	case <-t.C:
		return false
	}
}

// ExpectRinging waits for a provisional 18x response (outgoing) or reports
// whether the call rang (incoming). A call answered without 18x or ended
// before it returns false.
func (c *Call) ExpectRinging(timeout time.Duration) bool {
	// ringing is also closed on answer and end so that waiters wake up.
	if !c.wait(c.ringing, timeout) {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.tRing.IsZero()
}

// ExpectConnected waits until the call is answered and confirmed. It returns
// false if the call ends first or the timeout expires.
func (c *Call) ExpectConnected(timeout time.Duration) bool {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-c.connected:
		return true
	case <-c.done:
		// done and connected may both be closed; connected wins.
		select {
		case <-c.connected:
			return true
		default:
			return false
		}
	case <-t.C:
		return false
	}
}

// ExpectDisconnected waits until the call ends.
func (c *Call) ExpectDisconnected(timeout time.Duration) bool {
	return c.wait(c.done, timeout)
}

func (c *Call) runOutgoing(noAnswer time.Duration) {
	d := c.dev
	r := c.uac.run(c, c.cancelReq, noAnswer)
	now := time.Now()

	setup := CallSetupEvent{Device: d.cfg.ID, SetupTime: now.Sub(c.tStart), Cancelled: r.cancelled}
	c.mu.Lock()
	if !c.tRing.IsZero() {
		setup.PDD, setup.HasPDD = c.tRing.Sub(c.tStart), true
	}
	c.mu.Unlock()
	if r.res != nil {
		setup.Status = r.res.StatusCode
	}

	if r.res != nil && r.res.IsSuccess() {
		if err := c.uac.ack(); err != nil {
			d.observer().CallSetup(setup)
			c.finish(EndedByError, 0, "ACK: "+err.Error())
			return
		}
		c.trace.note(true, "ACK")
		setup.Success = !r.cancelled
		d.observer().CallSetup(setup)
		c.markConnected(now)
		if r.cancelled {
			// Answered while we were cancelling: the answer won the race,
			// so hang up the established call.
			c.bye()
		}
		return
	}

	d.observer().CallSetup(setup)
	switch {
	case r.cancelled:
		c.finish(c.cancelledBy(), 487, "cancelled")
	case r.res != nil:
		c.finish(EndedByRemote, r.res.StatusCode, r.res.Reason)
	default:
		c.finish(EndedByError, 0, r.err.Error())
	}
}

// requestCancel asks the INVITE loop to send CANCEL.
func (c *Call) requestCancel(by EndedBy) {
	c.cancelOnce.Do(func() {
		c.mu.Lock()
		c.endedBy = by
		c.mu.Unlock()
		close(c.cancelReq)
	})
}

func (c *Call) cancelledBy() EndedBy {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.endedBy == "" {
		return EndedByTimeout // cancelled by the no-answer timer
	}
	return c.endedBy
}
