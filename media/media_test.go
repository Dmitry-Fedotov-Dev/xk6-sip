package media

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestG711RoundTrip(t *testing.T) {
	for _, c := range []Codec{PCMU, PCMA} {
		for _, s := range []int16{0, 1, -1, 100, -100, 1000, -1000, 12345, -12345, 32767, -32768} {
			got := c.decode[c.encode(s)]
			diff := int(got) - int(s)
			if diff < 0 {
				diff = -diff
			}
			// G.711 quantisation error grows with amplitude: ~1/16 of the value.
			if limit := max(abs(int(s))/16, 16); diff > limit {
				t.Errorf("%s: %d -> %d (diff %d > %d)", c.Name, s, got, diff, limit)
			}
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func wav(samples []int16, rate uint32, channels uint16) []byte {
	var b bytes.Buffer
	data := len(samples) * 2
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+data))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), channels, rate, rate * 2, uint16(2), uint16(16)} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(data))
	_ = binary.Write(&b, binary.LittleEndian, samples)
	return b.Bytes()
}

func TestParseWAV(t *testing.T) {
	a, err := ParseWAV(wav(make([]int16, 8000), 8000, 1))
	if err != nil {
		t.Fatal(err)
	}
	if a.Duration() != time.Second {
		t.Errorf("duration %v", a.Duration())
	}
	if _, err := ParseWAV(wav(make([]int16, 100), 16000, 1)); err == nil {
		t.Error("16 kHz accepted")
	}
	if _, err := ParseWAV([]byte("junk")); err == nil {
		t.Error("junk accepted")
	}
}

func TestParseSDP(t *testing.T) {
	body := "v=0\r\no=- 1 1 IN IP4 10.0.0.1\r\ns=-\r\nc=IN IP4 10.0.0.1\r\nt=0 0\r\n" +
		"m=audio 4000 RTP/AVP 8 0 96\r\na=rtpmap:96 telephone-event/8000\r\na=sendonly\r\n"
	r, err := ParseSDP([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if r.Addr.String() != "10.0.0.1:4000" || r.DTMFPT != 96 || r.Dir != SendOnly || len(r.Codecs) != 2 || r.Codecs[0].Name != "PCMA" {
		t.Fatalf("%+v", r)
	}
	// We prefer PCMU; as answerer we pick our preference among offered codecs.
	if c, _ := choose([]Codec{PCMU, PCMA}, r.Codecs, true); c.Name != "PCMU" {
		t.Errorf("answerer chose %s", c.Name)
	}
	// As offerer we follow the answer's order.
	if c, _ := choose([]Codec{PCMU, PCMA}, r.Codecs, false); c.Name != "PCMA" {
		t.Errorf("offerer chose %s", c.Name)
	}

	if held, _ := ParseSDP([]byte(bytes.Replace([]byte(body), []byte("c=IN IP4 10.0.0.1"), []byte("c=IN IP4 0.0.0.0"), 1))); held.Dir != Inactive {
		t.Errorf("c=0.0.0.0 not treated as hold: %s", held.Dir)
	}
}

// pair negotiates a call between a (offerer) and b (answerer) on loopback.
func pair(t *testing.T, sched *Scheduler, aCodecs, bCodecs []Codec, bAudio *Audio) (*Stream, *Stream) {
	t.Helper()
	a, err := New(Config{IP: "127.0.0.1", Codecs: aCodecs, Scheduler: sched})
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(Config{IP: "127.0.0.1", Codecs: bCodecs, Scheduler: sched, Audio: bAudio})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); b.Close() })

	offer, err := ParseSDP(a.Offer(SendRecv))
	if err != nil {
		t.Fatal(err)
	}
	ans, err := b.Answer(offer)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseSDP(ans)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ApplyAnswer(r); err != nil {
		t.Fatal(err)
	}
	a.Start()
	b.Start()
	return a, b
}

func TestStreamAudioDTMFStats(t *testing.T) {
	sched := NewScheduler(2)
	defer sched.Close()
	a, b := pair(t, sched, []Codec{PCMU, PCMA}, []Codec{PCMA, PCMU}, Silence())

	if a.Codec() != "PCMA" || b.Codec() != "PCMA" {
		t.Fatalf("codecs %s/%s, want PCMA (answerer's preference)", a.Codec(), b.Codec())
	}
	if !b.WaitHeard(2 * time.Second) {
		t.Fatal("B did not hear A's tone")
	}
	if a.WaitHeard(300 * time.Millisecond) {
		t.Fatal("A heard B although B sends silence")
	}
	if err := a.SendDTMF("12#", 0); err != nil {
		t.Fatal(err)
	}
	if !b.WaitDigits("12#", 3*time.Second) {
		t.Fatalf("B got digits %q", b.Digits())
	}
	time.Sleep(200 * time.Millisecond)

	sa, sb := a.Close(), b.Close()
	t.Logf("A: %+v", sa)
	t.Logf("B: %+v", sb)
	if sb.PacketsReceived < 20 || sb.PacketsLost != 0 {
		t.Errorf("B receive stats %+v", sb)
	}
	if sb.Jitter > 20*time.Millisecond {
		t.Errorf("jitter %v on loopback", sb.Jitter)
	}
	if sb.DTMF != "12#" {
		t.Errorf("digits %q", sb.DTMF)
	}
	if sa.PacketsSent < sb.PacketsReceived {
		t.Errorf("sent %d < received %d", sa.PacketsSent, sb.PacketsReceived)
	}
}

func TestHoldStopsSending(t *testing.T) {
	sched := NewScheduler(1)
	defer sched.Close()
	a, b := pair(t, sched, nil, nil, nil)
	if !b.WaitHeard(2 * time.Second) {
		t.Fatal("no audio before hold")
	}
	a.SetDirection(Inactive)
	time.Sleep(100 * time.Millisecond) // let in-flight packets land
	before := b.Stats().PacketsReceived
	time.Sleep(300 * time.Millisecond)
	if after := b.Stats().PacketsReceived; after != before {
		t.Errorf("B kept receiving on hold: %d -> %d", before, after)
	}
}

func TestLossCounting(t *testing.T) {
	s := &Stream{}
	for _, seq := range []uint16{65533, 65534, 65535, 0, 2, 3} { // 1 lost, with wrap
		s.countSeq(7, seq)
	}
	st := s.statsLocked()
	if st.PacketsExpected != 7 || st.PacketsLost != 1 {
		t.Errorf("expected=%d lost=%d, want 7/1", st.PacketsExpected, st.PacketsLost)
	}
}
