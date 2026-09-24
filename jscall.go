package xk6sip

import (
	"github.com/grafana/sobek"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/engine"
)

// jsCall is the script-facing Call. Actions never wait for the other side;
// expect* methods wait and return false on timeout or failure.
type jsCall struct {
	CallID string `js:"callId"`
	ID     string `js:"id"`

	dev  *jsDevice
	call *engine.Call
}

func newJSCall(d *jsDevice, c *engine.Call) *jsCall {
	return &jsCall{CallID: c.CallID(), ID: c.Label(), dev: d, call: c}
}

func (c *jsCall) expect(what string, ok bool) bool {
	if !ok {
		c.dev.obs.expectFailed(what)
	}
	return ok
}

// Accept answers a ringing incoming call.
func (c *jsCall) Accept() bool { return c.call.Accept() }

// Reject declines a ringing incoming call; default 603 Decline.
func (c *jsCall) Reject(code sobek.Value, reason sobek.Value) bool {
	n, r := 603, ""
	if isSet(code) {
		n = int(code.ToInteger())
	}
	if isSet(reason) {
		r = reason.String()
	}
	return c.call.Reject(n, r)
}

// Hangup ends the call in any state (CANCEL, reject or BYE).
func (c *jsCall) Hangup() bool { return c.call.Hangup() }

func (c *jsCall) ExpectRinging(timeout sobek.Value) bool {
	rt := c.dev.mi.vu.Runtime()
	return c.expect("ringing", c.call.ExpectRinging(durationArg(rt, timeout, c.dev.mi.root.opts.expectTimeout)))
}

func (c *jsCall) ExpectConnected(timeout sobek.Value) bool {
	rt := c.dev.mi.vu.Runtime()
	return c.expect("connected", c.call.ExpectConnected(durationArg(rt, timeout, c.dev.mi.root.opts.expectTimeout)))
}

func (c *jsCall) ExpectDisconnected(timeout sobek.Value) bool {
	rt := c.dev.mi.vu.Runtime()
	return c.expect("disconnected", c.call.ExpectDisconnected(durationArg(rt, timeout, c.dev.mi.root.opts.expectTimeout)))
}

// State is one of calling, ringing, connected, ended.
func (c *jsCall) State() string { return c.call.State().String() }

// Status is the final INVITE status (200, 486...) or 0 if not known yet.
func (c *jsCall) Status() int { return c.call.Status() }

// Remote is the dialled number (outgoing) or the caller number (incoming).
func (c *jsCall) Remote() string { return c.call.Remote() }

// HowCompleted returns {endedBy, status, reason, duration} once the call
// has ended, otherwise null. duration is talk time in ms.
func (c *jsCall) HowCompleted() sobek.Value {
	rt := c.dev.mi.vu.Runtime()
	comp, ok := c.call.Completion()
	if !ok {
		return sobek.Null()
	}
	return rt.ToValue(map[string]any{
		"endedBy":  string(comp.EndedBy),
		"status":   comp.Status,
		"reason":   comp.Reason,
		"duration": ms(comp.Duration),
	})
}

// Trace returns the SIP ladder of this call, for logging failures.
func (c *jsCall) Trace() string { return engine.FormatLadder(c.call.Trace()) }
