package engine_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/engine"
	"github.com/Dmitry-Fedotov-Dev/xk6-sip/media"
)

func TestRecordAndCompare(t *testing.T) {
	dir := t.TempDir()
	alice := withConfig(t, engine.DeviceConfig{ID: "alice", User: "alice@test.local",
		Media: &engine.MediaOptions{Record: engine.RecordAlways}})
	bob := withConfig(t, engine.DeviceConfig{ID: "bob", User: "bob@test.local",
		Media: &engine.MediaOptions{Record: engine.RecordOnFailure, RecordDir: dir}})

	out, _ := alice.Call(engine.CallOptions{Target: bob.ContactURI()})
	in, _ := bob.ExpectCall(engine.CallMatch{}, wait)
	if in == nil {
		t.Fatal("no call")
	}
	in.Accept()
	if !out.ExpectConnected(wait) || !out.WaitHeard(wait) {
		t.Fatal("no connected call with audio")
	}
	time.Sleep(1500 * time.Millisecond)

	q, ok, err := out.CompareAudio(media.DefaultAudio())
	if err != nil || !ok {
		t.Fatalf("compare: ok=%v err=%v", ok, err)
	}
	if q.Score < 0.9 || q.Gaps > 100*time.Millisecond {
		t.Errorf("heard the 1 kHz tone with %+v", q)
	}
	if q, _, _ := out.CompareAudio(media.Tone(440, -20, time.Second)); q.Score > 0.7 {
		t.Errorf("a 440 Hz reference scored %.3f against the 1 kHz tone", q.Score)
	}

	in.MarkFailed() // a failed check on bob's side: RecordOnFailure saves it
	in.Hangup()
	if !out.ExpectDisconnected(wait) {
		t.Fatal("not disconnected")
	}

	path := filepath.Join(t.TempDir(), "sub", "alice.wav")
	if err := out.SaveRecording(path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	// 1.5 s and more of stereo 16-bit 8 kHz audio: at least 48 KB.
	if err != nil || fi.Size() < 44+int64(1.5*8000*4) {
		t.Fatalf("recording %v: %v", fi, err)
	}

	saved, _ := filepath.Glob(filepath.Join(dir, "bob_in_*.wav"))
	if len(saved) != 1 {
		t.Fatalf("failed call not auto-saved: %v", saved)
	}
}

func TestRecordOffByDefault(t *testing.T) {
	_, d := direct(t, "alice", "bob")
	out, in := connect(t, d[0], d[1])
	defer in.Hangup()
	if err := out.SaveRecording(filepath.Join(t.TempDir(), "x.wav")); err == nil {
		t.Fatal("saved a recording of a call that was not recorded")
	}
	if _, _, err := out.CompareAudio(media.DefaultAudio()); err == nil {
		t.Fatal("compared audio of a call that was not recorded")
	}
}
