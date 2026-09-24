package engine_test

import (
	"testing"
	"time"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/engine"
	"github.com/Dmitry-Fedotov-Dev/xk6-sip/testpbx"
)

// Scenarios through the B2BUA test PBX, which relays re-INVITEs and
// performs transfers itself (the way hosted PBXs such as a Centrex do).

func pbx3(t *testing.T) (a, b, c *engine.Device) {
	t.Helper()
	eng := engine.New(engine.Options{LocalIP: "127.0.0.1"})
	pbx, err := testpbx.Start(testpbx.Config{
		Domain: "test.local", AuthRegister: true, AuthInvite: true,
		Users: []testpbx.User{
			{Name: "alice", Password: "a", Ext: "701"},
			{Name: "bob", Password: "b", Ext: "702"},
			{Name: "carol", Password: "c", Ext: "703"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		eng.Close()
		pbx.Close()
	})
	mk := func(user, pass string) *engine.Device {
		d, err := eng.NewDevice(engine.DeviceConfig{ID: user, Registrar: "sip:" + pbx.Addr(), User: user + "@test.local", Password: pass})
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Start(); err != nil {
			t.Fatal(err)
		}
		return d
	}
	return mk("alice", "a"), mk("bob", "b"), mk("carol", "c")
}

func pbxConnect(t *testing.T, from, to *engine.Device, number, caller string) (*engine.Call, *engine.Call) {
	t.Helper()
	out, err := from.Call(engine.CallOptions{Target: number})
	if err != nil {
		t.Fatal(err)
	}
	in, _ := to.ExpectCall(engine.CallMatch{Caller: caller}, wait)
	if in == nil {
		t.Fatalf("%s got no call from %s:\n%s", to.ID(), caller, engine.FormatLadder(out.Trace()))
	}
	in.Accept()
	if !out.ExpectConnected(wait) || !in.ExpectConnected(wait) {
		t.Fatalf("not connected:\n%s", engine.FormatLadder(out.Trace()))
	}
	return out, in
}

func TestPBXHold(t *testing.T) {
	alice, bob, _ := pbx3(t)
	aOut, bIn := pbxConnect(t, alice, bob, "702", "701")
	if !bIn.WaitHeard(wait) {
		t.Fatal("no audio")
	}
	if !bIn.Hold() || !aOut.RemoteHold() {
		t.Fatalf("hold through PBX failed:\n%s", engine.FormatLadder(bIn.Trace()))
	}
	if !bIn.Unhold() || aOut.RemoteHold() {
		t.Fatal("unhold through PBX failed")
	}
	aOut.Hangup()
	if !bIn.ExpectDisconnected(wait) {
		t.Fatal("not disconnected")
	}
}

func TestPBXBlindTransfer(t *testing.T) {
	alice, bob, carol := pbx3(t)
	aOut, bIn := pbxConnect(t, alice, bob, "702", "701")

	if !bIn.Transfer("703") {
		t.Fatalf("REFER rejected:\n%s", engine.FormatLadder(bIn.Trace()))
	}
	// carol sees the transferee (alice), not bob.
	cIn, _ := carol.ExpectCall(engine.CallMatch{Caller: "701"}, wait)
	if cIn == nil {
		t.Fatal("carol got no call from alice")
	}
	cIn.Accept()
	if !bIn.ExpectTransferred(wait) {
		t.Fatalf("transfer not confirmed:\n%s", engine.FormatLadder(bIn.Trace()))
	}
	bIn.Hangup()
	// alice keeps her original call, now with carol's media.
	time.Sleep(100 * time.Millisecond)
	flowing(t, aOut, cIn)
	if aOut.State() != engine.StateConnected {
		t.Fatal("alice lost the call when bob hung up")
	}
	aOut.Hangup()
	if !cIn.ExpectDisconnected(wait) {
		t.Fatal("carol not disconnected")
	}
}

func TestPBXAttendedTransfer(t *testing.T) {
	alice, bob, carol := pbx3(t)
	aOut, bIn := pbxConnect(t, alice, bob, "702", "701")
	bIn.Hold()
	bOut, cIn := pbxConnect(t, bob, carol, "703", "702") // consultation

	if !bIn.AttendedTransfer(bOut) {
		t.Fatalf("REFER rejected:\n%s", engine.FormatLadder(bIn.Trace()))
	}
	if !bIn.ExpectTransferred(wait) {
		t.Fatalf("transfer not confirmed:\n%s", engine.FormatLadder(bIn.Trace()))
	}
	// The PBX drops bob's consultation leg; bob hangs up the original.
	if !bOut.ExpectDisconnected(wait) {
		t.Fatal("consultation call still up")
	}
	bIn.Hangup()
	time.Sleep(100 * time.Millisecond)
	if aOut.State() != engine.StateConnected || cIn.State() != engine.StateConnected {
		t.Fatalf("alice/carol lost the call: %s/%s", aOut.State(), cIn.State())
	}
	flowing(t, aOut, cIn)
	cIn.Hangup()
	if !aOut.ExpectDisconnected(wait) {
		t.Fatal("alice not disconnected")
	}
}

// flowing checks that RTP keeps arriving on both calls.
func flowing(t *testing.T, calls ...*engine.Call) {
	t.Helper()
	before := make([]uint64, len(calls))
	for i, c := range calls {
		s, _ := c.MediaStats()
		before[i] = s.PacketsReceived
	}
	time.Sleep(300 * time.Millisecond)
	for i, c := range calls {
		if s, _ := c.MediaStats(); s.PacketsReceived < before[i]+5 {
			t.Errorf("%s: no RTP after transfer (%d -> %d)", c.Remote(), before[i], s.PacketsReceived)
		}
	}
}
