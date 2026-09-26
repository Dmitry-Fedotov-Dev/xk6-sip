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
	media    []engine.MediaEvent
	first    []engine.FirstResponseEvent
	retrans  []engine.RetransmissionEvent
}

func (r *recorder) FirstResponse(e engine.FirstResponseEvent) {
	r.mu.Lock()
	r.first = append(r.first, e)
	r.mu.Unlock()
}
func (r *recorder) Retransmission(e engine.RetransmissionEvent) {
	r.mu.Lock()
	r.retrans = append(r.retrans, e)
	r.mu.Unlock()
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
func (r *recorder) Media(e engine.MediaEvent) {
	r.mu.Lock()
	r.media = append(r.media, e)
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
	if !in.WaitHeard(wait) || !out.WaitHeard(wait) {
		t.Fatal("no audio in one direction")
	}
	if out.Codec() != "PCMU" || in.Codec() != "PCMU" {
		t.Errorf("codecs %q/%q", out.Codec(), in.Codec())
	}
	if err := out.SendDTMF("5#", 0); err != nil {
		t.Fatal(err)
	}
	if !in.WaitDigits("5#", wait) {
		t.Fatalf("callee got DTMF %q", in.Digits())
	}
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
	if len(rec.media) != 2 {
		t.Fatalf("want 2 media events, got %+v", rec.media)
	}
	for _, m := range rec.media {
		if m.Stats.PacketsReceived == 0 || m.Stats.PacketsLost != 0 {
			t.Errorf("%s leg media %+v", m.Direction, m.Stats)
		}
	}
	t.Logf("media: %+v", rec.media)
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

func TestCallWithoutMedia(t *testing.T) {
	_, _, rec, a, b := setup(t)
	b.Start()
	out, _ := a.Call(engine.CallOptions{Target: "702", Media: &engine.MediaOptions{Disabled: true}})
	in, _ := b.ExpectCall(engine.CallMatch{Caller: "701"}, wait)
	if in == nil {
		t.Fatal("no incoming call")
	}
	in.Accept()
	if !out.ExpectConnected(wait) {
		t.Fatal("not connected")
	}
	// B has media and sends towards A's placeholder port; A has none.
	if out.Codec() != "" || out.WaitHeard(200*time.Millisecond) {
		t.Error("caller without media reports audio")
	}
	out.Hangup()
	in.ExpectDisconnected(wait)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.media) != 1 || rec.media[0].Direction != engine.Incoming {
		t.Errorf("media events %+v", rec.media)
	}
}

// Every INVITE transaction reports its first response: the 407 challenge
// of the first INVITE and the 100 Trying of the authenticated one.
func TestFirstResponseTime(t *testing.T) {
	_, _, rec, a, b := setup(t)
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	out, err := a.Call(engine.CallOptions{Target: "702"})
	if err != nil {
		t.Fatal(err)
	}
	in, _ := b.ExpectCall(engine.CallMatch{}, wait)
	if in == nil {
		t.Fatal("no call")
	}
	in.Accept()
	if !out.ExpectConnected(wait) {
		t.Fatal("not connected")
	}
	out.Hangup()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.first) != 2 {
		t.Fatalf("want 2 first-response events (407, then 100), got %+v", rec.first)
	}
	for _, e := range rec.first {
		if e.Method != "INVITE" || e.Delay < 0 || e.Delay > wait { // 0 is possible: Windows clock steps are ~0.5 ms
			t.Fatalf("bad event %+v", e)
		}
	}
	if len(rec.retrans) != 0 {
		t.Fatalf("retransmissions on a healthy local PBX: %+v", rec.retrans)
	}
}
