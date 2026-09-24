package engine_test

import (
	"sync"
	"testing"
	"time"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/engine"
	"github.com/Dmitry-Fedotov-Dev/xk6-sip/testpbx"
)

type recorder struct {
	mu       sync.Mutex
	requests []engine.RequestEvent
	setups   []engine.CallSetupEvent
	ends     []engine.CallEndEvent
	incoming []engine.IncomingCallEvent
}

func (r *recorder) Request(e engine.RequestEvent) {
	r.mu.Lock()
	r.requests = append(r.requests, e)
	r.mu.Unlock()
}
func (r *recorder) CallSetup(e engine.CallSetupEvent) {
	r.mu.Lock()
	r.setups = append(r.setups, e)
	r.mu.Unlock()
}
func (r *recorder) CallEnd(e engine.CallEndEvent) {
	r.mu.Lock()
	r.ends = append(r.ends, e)
	r.mu.Unlock()
}
func (r *recorder) IncomingCall(e engine.IncomingCallEvent) {
	r.mu.Lock()
	r.incoming = append(r.incoming, e)
	r.mu.Unlock()
}

const wait = 5 * time.Second

func setup(t *testing.T) (*testpbx.PBX, *engine.Engine, *recorder, *engine.Device, *engine.Device) {
	t.Helper()

	rec := &recorder{}
	eng := engine.New(engine.Options{Observer: rec, LocalIP: "127.0.0.1"})
	pbx, err := testpbx.Start(testpbx.Config{
		Domain:       "test.local",
		AuthRegister: true,
		AuthInvite:   true,
		Users: []testpbx.User{
			{Name: "alice", Password: "a-secret", Ext: "701"},
			{Name: "bob", Password: "b-secret", Ext: "702"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Devices unregister on Close, so the PBX must outlive the engine.
	t.Cleanup(func() {
		eng.Close()
		pbx.Close()
	})

	mk := func(id, user, pass, ext string) *engine.Device {
		d, err := eng.NewDevice(engine.DeviceConfig{
			ID: id, Registrar: "sip:" + pbx.Addr(), User: user + "@test.local", Password: pass,
			Expires: 60 * time.Second, Identities: map[string]string{"ext": ext},
		})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	return pbx, eng, rec, mk("phone1", "alice", "a-secret", "701"), mk("phone2", "bob", "b-secret", "702")
}

func TestRegisterAndDestroy(t *testing.T) {
	pbx, _, rec, a, _ := setup(t)
	if pbx.Registered("alice") {
		t.Fatal("registered before first use")
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	if !a.Registered() || !pbx.Registered("alice") {
		t.Fatal("not registered")
	}
	rec.mu.Lock()
	if len(rec.requests) != 2 || rec.requests[0].Status != 401 || rec.requests[1].Status != 200 {
		t.Errorf("want REGISTER 401 then 200, got %+v", rec.requests)
	}
	rec.mu.Unlock()

	a.Destroy()
	if pbx.Registered("alice") {
		t.Fatal("still registered after Destroy")
	}
}

func TestCallAnswerHangup(t *testing.T) {
	_, _, rec, a, b := setup(t)
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}

	out, err := a.Call(engine.CallOptions{Target: "702"})
	if err != nil {
		t.Fatal(err)
	}
	in, err := b.ExpectCall(engine.CallMatch{Caller: "701", CallerDevice: a}, wait)
	if err != nil || in == nil {
		t.Fatalf("no incoming call: %v", err)
	}
	if !out.ExpectRinging(wait) {
		t.Fatal("no ringing on caller side")
	}
	if !in.Accept() {
		t.Fatal("accept failed")
	}
	if !out.ExpectConnected(wait) || !in.ExpectConnected(wait) {
		t.Fatalf("not connected:\nout:\n%s\nin:\n%s", engine.FormatLadder(out.Trace()), engine.FormatLadder(in.Trace()))
	}
	time.Sleep(50 * time.Millisecond)
	if !in.Hangup() {
		t.Fatal("hangup failed")
	}
	if !out.ExpectDisconnected(wait) {
		t.Fatal("caller not disconnected")
	}
	comp, _ := out.Completion()
	if comp.EndedBy != engine.EndedByRemote || comp.Duration < 50*time.Millisecond {
		t.Errorf("caller completion %+v", comp)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.setups) != 1 || !rec.setups[0].Success || !rec.setups[0].HasPDD {
		t.Errorf("setup events %+v", rec.setups)
	}
	if len(rec.incoming) != 1 || !rec.incoming[0].HasDelivery {
		t.Errorf("incoming events %+v", rec.incoming)
	}
	if len(rec.ends) != 2 {
		t.Errorf("want 2 call end events, got %+v", rec.ends)
	}
	t.Logf("setup=%v pdd=%v delivery=%v", rec.setups[0].SetupTime, rec.setups[0].PDD, rec.incoming[0].Delivery)
	t.Logf("caller ladder:\n%s", engine.FormatLadder(out.Trace()))
}

func TestCancelBeforeAnswer(t *testing.T) {
	_, _, rec, a, b := setup(t)
	b.Start()
	out, _ := a.Call(engine.CallOptions{Target: "702"})
	in, _ := b.ExpectCall(engine.CallMatch{Caller: "701"}, wait)
	if in == nil {
		t.Fatal("no incoming call")
	}
	out.ExpectRinging(wait)
	if !out.Hangup() {
		t.Fatal("hangup failed")
	}
	if !in.ExpectDisconnected(wait) {
		t.Fatal("callee still ringing after CANCEL")
	}
	if out.Status() != 487 || out.ExpectConnected(0) {
		t.Errorf("status %d", out.Status())
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.setups) != 1 || rec.setups[0].Success || !rec.setups[0].Cancelled {
		t.Errorf("setup events %+v", rec.setups)
	}
}

func TestReject(t *testing.T) {
	_, _, _, a, b := setup(t)
	b.Start()
	out, _ := a.Call(engine.CallOptions{Target: "702"})
	in, _ := b.ExpectCall(engine.CallMatch{}, wait)
	if in == nil {
		t.Fatal("no incoming call")
	}
	in.Reject(486, "Busy Here")
	if out.ExpectConnected(wait) {
		t.Fatal("connected after reject")
	}
	if out.Status() != 486 {
		t.Errorf("status %d, want 486", out.Status())
	}
}

func TestExpectCallWrongCaller(t *testing.T) {
	_, _, _, a, b := setup(t)
	b.Start()
	out, _ := a.Call(engine.CallOptions{Target: "702"})
	defer out.Hangup()
	in, err := b.ExpectCall(engine.CallMatch{Caller: "+7999"}, 300*time.Millisecond)
	if err != nil || in != nil {
		t.Fatalf("matched wrong caller: %v %v", in, err)
	}
	// The call is still in the inbox for a correct expectation.
	in, _ = b.ExpectCall(engine.CallMatch{Caller: "701"}, wait)
	if in == nil {
		t.Fatal("call lost from inbox")
	}
}

func TestUnknownTarget(t *testing.T) {
	_, _, _, a, _ := setup(t)
	out, _ := a.Call(engine.CallOptions{Target: "999"})
	if out.ExpectConnected(wait) || out.Status() != 404 {
		t.Fatalf("status %d, want 404", out.Status())
	}
}
