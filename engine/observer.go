package engine

import "time"

// Observer receives measurement events from the engine. Adapters (k6,
// standalone runner) turn them into metrics. Methods are called from engine
// goroutines and must not block.
type Observer interface {
	Request(RequestEvent)
	CallSetup(CallSetupEvent)
	CallEnd(CallEndEvent)
	IncomingCall(IncomingCallEvent)
}

// RequestEvent is one client transaction that got a final response or failed.
type RequestEvent struct {
	Device   string
	Method   string
	Status   int // 0 when no final response arrived (timeout, transport error)
	Duration time.Duration
	Err      error
}

// Failed reports whether the transaction is a failure for metrics purposes.
// Auth challenges are the normal first step of digest auth, not failures.
func (e RequestEvent) Failed() bool {
	if e.Err != nil || e.Status == 0 {
		return true
	}
	return e.Status >= 300 && e.Status != 401 && e.Status != 407
}

// CallSetupEvent is emitted once per outgoing call when INVITE completes.
type CallSetupEvent struct {
	Device    string
	Success   bool
	Cancelled bool // hung up by the script before answer
	Status    int
	SetupTime time.Duration // INVITE sent -> final response
	PDD       time.Duration // INVITE sent -> first 18x
	HasPDD    bool
}

// CallEndEvent is emitted when an established call ends.
type CallEndEvent struct {
	Device    string
	Direction Direction
	Duration  time.Duration // answer -> end
	EndedBy   EndedBy
}

// IncomingCallEvent is emitted when ExpectCall claims an incoming call.
type IncomingCallEvent struct {
	Device string
	Caller string
	// Delivery is the time from the caller device sending INVITE to this
	// device receiving it, i.e. routing time through the system under test.
	// Only known when the caller is a device of the same engine.
	Delivery    time.Duration
	HasDelivery bool
}

// NopObserver discards all events.
type NopObserver struct{}

func (NopObserver) Request(RequestEvent)           {}
func (NopObserver) CallSetup(CallSetupEvent)       {}
func (NopObserver) CallEnd(CallEndEvent)           {}
func (NopObserver) IncomingCall(IncomingCallEvent) {}
