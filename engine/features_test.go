package engine_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/engine"
)

// Direct device-to-device calls: no PBX, so the endpoints themselves do
// PRACK, hold, session timers and transfers.

func direct(t *testing.T, names ...string) (*recorder, []*engine.Device) {
	t.Helper()
	rec := &recorder{}
	eng := engine.New(engine.Options{Observer: rec, LocalIP: "127.0.0.1"})
	t.Cleanup(eng.Close)
	var devs []*engine.Device
	for _, n := range names {
		d, err := eng.NewDevice(engine.DeviceConfig{
			ID: n, Registrar: "sip:192.0.2.1", User: n + "@test.local", NoRegister: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Start(); err != nil {
			t.Fatal(err)
		}
		devs = append(devs, d)
	}
	return rec, devs
}

func withConfig(t *testing.T, cfg engine.DeviceConfig) *engine.Device {
	t.Helper()
	eng := engine.New(engine.Options{LocalIP: "127.0.0.1"})
	t.Cleanup(eng.Close)
	cfg.Registrar, cfg.NoRegister = "sip:192.0.2.1", true
	d, err := eng.NewDevice(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	return d
}

// connect makes a call from a to b and answers it.
func connect(t *testing.T, a, b *engine.Device) (*engine.Call, *engine.Call) {
	t.Helper()
	out, err := a.Call(engine.CallOptions{Target: b.ContactURI()})
	if err != nil {
		t.Fatal(err)
	}
	in, _ := b.ExpectCall(engine.CallMatch{}, wait)
	if in == nil {
		t.Fatalf("no incoming call:\n%s", engine.FormatLadder(out.Trace()))
	}
	in.Accept()
	if !out.ExpectConnected(wait) || !in.ExpectConnected(wait) {
		t.Fatalf("not connected:\n%s", engine.FormatLadder(out.Trace()))
	}
	return out, in
}

func ladderHas(c *engine.Call, s string) bool {
	return strings.Contains(engine.FormatLadder(c.Trace()), s)
}

func TestPRACK(t *testing.T) {
	_, d := direct(t, "alice")
	bob := withConfig(t, engine.DeviceConfig{ID: "bob", User: "bob@test.local", PRACK: true})
	out, _ := d[0].Call(engine.CallOptions{Target: bob.ContactURI()})
	in, _ := bob.ExpectCall(engine.CallMatch{}, wait)
	if in == nil {
		t.Fatal("no call")
	}
	ringing := out.ExpectRinging(wait)
	time.Sleep(100 * time.Millisecond) // let PRACK complete
	in.Accept()
	if !ringing || !out.ExpectConnected(wait) {
		t.Fatalf("ringing=%v\n%s\n%s", ringing, engine.FormatLadder(out.Trace()), engine.FormatLadder(in.Trace()))
	}
	if !ladderHas(out, "PRACK") || !ladderHas(in, "200 OK (PRACK)") {
		t.Fatalf("no PRACK exchange:\n%s\n%s", engine.FormatLadder(out.Trace()), engine.FormatLadder(in.Trace()))
	}
	out.Hangup()
}

func TestHoldUnhold(t *testing.T) {
	_, d := direct(t, "alice", "bob")
	out, in := connect(t, d[0], d[1])
	if !out.WaitHeard(wait) {
		t.Fatal("no audio before hold")
	}

	// Caller holds: callee stops sending (recvonly), caller keeps sending.
	if !out.Hold() {
		t.Fatalf("hold failed:\n%s", engine.FormatLadder(out.Trace()))
	}
	if !in.RemoteHold() || !out.OnHold() {
		t.Fatal("hold state not reflected")
	}
	time.Sleep(100 * time.Millisecond)
	a1, _ := out.MediaStats()
	time.Sleep(300 * time.Millisecond)
	a2, _ := out.MediaStats()
	if a2.PacketsReceived != a1.PacketsReceived {
		t.Errorf("caller still receives on hold: %d -> %d", a1.PacketsReceived, a2.PacketsReceived)
	}

	if !out.Unhold() {
		t.Fatal("unhold failed")
	}
	time.Sleep(300 * time.Millisecond)
	if a3, _ := out.MediaStats(); a3.PacketsReceived <= a2.PacketsReceived {
		t.Errorf("no audio after unhold")
	}

	// The callee can hold too (UAS side of the dialog).
	if !in.Hold() || !out.RemoteHold() {
		t.Fatalf("callee hold failed:\n%s", engine.FormatLadder(in.Trace()))
	}
	in.Hangup()
	if !out.ExpectDisconnected(wait) {
		t.Fatal("not disconnected")
	}
}

func TestSessionTimerRefresh(t *testing.T) {
	_, d := direct(t, "bob")
	alice := withConfig(t, engine.DeviceConfig{ID: "alice", User: "alice@test.local", SessionExpires: 2 * time.Second})
	out, in := connect(t, alice, d[0])
	time.Sleep(3500 * time.Millisecond) // refreshes at 1s, 2s, 3s
	if out.State() != engine.StateConnected || in.State() != engine.StateConnected {
		t.Fatalf("call dropped:\n%s", engine.FormatLadder(out.Trace()))
	}
	if n := strings.Count(engine.FormatLadder(out.Trace()), "session refresh"); n < 2 {
		t.Fatalf("%d refreshes:\n%s", n, engine.FormatLadder(out.Trace()))
	}
	out.Hangup()
}

func TestBlindTransferByEndpoints(t *testing.T) {
	_, d := direct(t, "alice", "bob", "carol")
	alice, bob, carol := d[0], d[1], d[2]
	aOut, bIn := connect(t, alice, bob)

	if !bIn.Transfer(carol.ContactURI()) {
		t.Fatalf("REFER not accepted:\n%s", engine.FormatLadder(bIn.Trace()))
	}
	cIn, _ := carol.ExpectCall(engine.CallMatch{}, wait)
	if cIn == nil {
		t.Fatal("carol got no call")
	}
	cIn.Accept()
	if !bIn.ExpectTransferred(wait) {
		t.Fatalf("transfer not reported:\n%s", engine.FormatLadder(bIn.Trace()))
	}
	bIn.Hangup()
	aNew := aOut.ReferredCall(wait)
	if aNew == nil || !aNew.ExpectConnected(wait) || !aNew.WaitHeard(wait) {
		t.Fatal("alice not connected to carol")
	}
	if !aOut.ExpectDisconnected(wait) {
		t.Fatal("original call still up")
	}
	aNew.Hangup()
}

func TestAttendedTransferByEndpoints(t *testing.T) {
	_, d := direct(t, "alice", "bob", "carol")
	alice, bob, carol := d[0], d[1], d[2]
	aOut, bIn := connect(t, alice, bob) // alice -> bob
	bOut, cIn := connect(t, bob, carol) // bob consults carol

	if !bIn.AttendedTransfer(bOut) {
		t.Fatalf("REFER not accepted:\n%s", engine.FormatLadder(bIn.Trace()))
	}
	// carol receives INVITE with Replaces, answers it and drops bob's call.
	cNew, _ := carol.ExpectCall(engine.CallMatch{}, wait)
	if cNew == nil || !cNew.ExpectConnected(wait) {
		t.Fatalf("carol did not take the replacing call:\n%s", engine.FormatLadder(aOut.Trace()))
	}
	if !cIn.ExpectDisconnected(wait) || !bOut.ExpectDisconnected(wait) {
		t.Fatal("replaced consultation call still up")
	}
	if !bIn.ExpectTransferred(wait) {
		t.Fatal("transfer not reported to bob")
	}
	bIn.Hangup()
	aNew := aOut.ReferredCall(wait)
	if aNew == nil || !aNew.WaitHeard(wait) || !cNew.WaitHeard(wait) {
		t.Fatal("alice and carol do not hear each other")
	}
	aNew.Hangup()
	if !cNew.ExpectDisconnected(wait) {
		t.Fatal("carol not disconnected")
	}
}
