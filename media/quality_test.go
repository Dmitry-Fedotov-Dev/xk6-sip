package media

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand/v2"
	"testing"
	"time"
)

// speechLike is a deterministic speech-like signal: voiced "syllables" with
// a gliding pitch and harmonics up to 3.4 kHz, noisy consonants and pauses.
func speechLike(seed uint64, dur time.Duration) []int16 {
	r := rand.New(rand.NewPCG(seed, seed*7+1)) // #nosec G404 -- test signal
	n := int(dur.Seconds() * sampleRate)
	out := make([]int16, n)
	phase := 0.0
	for i := 0; i < n; {
		syl := int(sampleRate * (0.12 + 0.2*r.Float64())) // 120..320 ms
		gap := int(sampleRate * (0.03 + 0.12*r.Float64()))
		f0a, f0b := 90+140*r.Float64(), 90+140*r.Float64()
		noisy := r.Float64() < 0.3
		amp := 0.15 + 0.35*r.Float64()
		for j := 0; j < syl && i < n; j, i = j+1, i+1 {
			t := float64(j) / float64(syl)
			env := amp * math.Sin(math.Pi*t)
			var v float64
			if noisy {
				v = env * (r.Float64()*2 - 1) * 0.6
			} else {
				f0 := f0a + (f0b-f0a)*t
				phase += 2 * math.Pi * f0 / sampleRate
				for h := 1; float64(h)*f0 < 3400; h++ {
					v += math.Sin(float64(h)*phase) / float64(h)
				}
				v *= env * 0.5
			}
			out[i] = int16(max(-1, min(1, v)) * math.MaxInt16 * 0.9)
		}
		i += gap
	}
	return out
}

// g711 passes x through PCMU encode/decode like the network path.
func g711(x []int16) []int16 {
	out := make([]int16, len(x))
	for i, v := range x {
		out[i] = UlawDecode(UlawEncode(v))
	}
	return out
}

func scale(x []int16, db float64) []int16 {
	k := math.Pow(10, db/20)
	out := make([]int16, len(x))
	for i, v := range x {
		out[i] = int16(float64(v) * k)
	}
	return out
}

// heardAt builds a recording: silence, then x at offset samples, then tail.
func heardAt(x []int16, offset, tail int) []int16 {
	out := make([]int16, offset+len(x)+tail)
	copy(out[offset:], x)
	return out
}

func TestCompareClean(t *testing.T) {
	ref := speechLike(1, 4*time.Second)
	q, ok := Compare(ref, heardAt(g711(ref), 3371, 4000))
	if !ok {
		t.Fatal("not compared")
	}
	if q.Score < 0.95 {
		t.Errorf("score %.3f, want >= 0.95", q.Score)
	}
	if want := samplesDur(3371); q.Offset != want {
		t.Errorf("offset %v, want %v", q.Offset, want)
	}
	if q.Gaps != 0 || q.ClippedStart != 0 {
		t.Errorf("gaps %v clipped %v, want none", q.Gaps, q.ClippedStart)
	}
	if math.Abs(q.Gain) > 0.5 {
		t.Errorf("gain %.1f dB, want ~0", q.Gain)
	}
	if q.Compared != samplesDur(len(ref)) {
		t.Errorf("compared %v, want the whole reference", q.Compared)
	}
}

func TestCompareGainDoesNotMatter(t *testing.T) {
	ref := speechLike(2, 3*time.Second)
	q, _ := Compare(ref, heardAt(g711(scale(ref, -12)), 800, 800))
	if q.Score < 0.93 {
		t.Errorf("score %.3f at -12 dB, want >= 0.93", q.Score)
	}
	if math.Abs(q.Gain+12) > 1 {
		t.Errorf("gain %.1f dB, want ~-12", q.Gain)
	}
}

func TestCompareLoss(t *testing.T) {
	ref := speechLike(3, 4*time.Second)
	heard := g711(ref)
	r := rand.New(rand.NewPCG(9, 9)) // #nosec G404 -- test
	env := envelope(ref)
	speechBlock := map[int]bool{}
	for _, i := range speech(env) {
		speechBlock[i] = true
	}
	var lostSpeech time.Duration // speech the lost packets carried
	for f := 0; f+frameSamples <= len(heard); f += frameSamples {
		if r.Float64() < 0.2 { // 20% of packets lost
			clear(heard[f : f+frameSamples])
			for b := f / envBlock; b < (f+frameSamples)/envBlock; b++ {
				if speechBlock[b] {
					lostSpeech += 10 * time.Millisecond
				}
			}
		}
	}
	clean, _ := Compare(ref, heardAt(g711(ref), 500, 500))
	q, _ := Compare(ref, heardAt(heard, 500, 500))
	t.Logf("clean %.3f, 20%% loss %.3f, gaps %v of %v lost speech", clean.Score, q.Score, q.Gaps, lostSpeech)
	if q.Score > clean.Score-0.1 {
		t.Errorf("20%% loss scored %.3f, clean %.3f: loss must cost more", q.Score, clean.Score)
	}
	if d := q.Gaps - lostSpeech; d < -lostSpeech/5 || d > lostSpeech/5 {
		t.Errorf("gaps %v, lost speech %v: want within 20%%", q.Gaps, lostSpeech)
	}
}

func TestCompareWrongAudio(t *testing.T) {
	ref := speechLike(4, 4*time.Second)
	other := speechLike(5, 6*time.Second)
	q, _ := Compare(ref, g711(other))
	if q.Score > 0.4 {
		t.Errorf("unrelated speech scored %.3f, want <= 0.4", q.Score)
	}
	q, _ = Compare(ref, heardAt(nil, 0, 5*sampleRate)) // silence
	if q.Score > 0.1 || q.Gaps == 0 {
		t.Errorf("silence scored %.3f with gaps %v", q.Score, q.Gaps)
	}
}

func TestCompareClippedStart(t *testing.T) {
	ref := speechLike(6, 3*time.Second)
	heard := g711(ref)
	cut := 0
	for cut < len(ref) && ref[cut] == 0 { // start of the first syllable
		cut++
	}
	clear(heard[:cut+2400]) // 300 ms of the first word lost
	q, _ := Compare(ref, heardAt(heard, 1000, 1000))
	if q.ClippedStart < 200*time.Millisecond || q.ClippedStart > 400*time.Millisecond {
		t.Errorf("clipped start %v, want ~300ms", q.ClippedStart)
	}
}

func TestCompareTone(t *testing.T) {
	ref := DefaultAudio().PCM()
	loop := make([]int16, 0, 3*len(ref))
	for range 3 {
		loop = append(loop, ref...)
	}
	q, ok := Compare(ref, heardAt(g711(loop), 1234, 0))
	if !ok || q.Score < 0.9 {
		t.Errorf("tone scored %.3f (ok=%v), want >= 0.9", q.Score, ok)
	}
	q, _ = Compare(ref, heardAt(g711(Tone(440, -20, 2*time.Second).PCM()), 0, 0))
	if q.Score > 0.7 {
		t.Errorf("a 440 Hz tone against the 1 kHz reference scored %.3f", q.Score)
	}
}

func TestCompareTooShort(t *testing.T) {
	if _, ok := Compare(make([]int16, 100), make([]int16, 8000)); ok {
		t.Error("compared a 12 ms reference")
	}
}

func TestRecorderPlacesByTimestamp(t *testing.T) {
	var tr track
	frame := func(v byte) []byte { return bytes.Repeat([]byte{v}, frameSamples) }
	tr.put(800, 1, 1000, frame(0x10), &ulawTable)                 // anchor: ts 1000 at sample 800
	tr.put(9999, 1, 1000+2*frameSamples, frame(0x20), &ulawTable) // arrives late, placed by ts
	tr.put(9999, 1, 1000+1*frameSamples, frame(0x30), &ulawTable) // reordered
	tr.put(0, 1, 1000+4*frameSamples, frame(0x40), &ulawTable)    // one frame lost before it
	if len(tr.samples) != 800+5*frameSamples {
		t.Fatalf("length %d", len(tr.samples))
	}
	for i, want := range []byte{0x10, 0x30, 0x20, 0, 0x40} {
		got := tr.samples[800+i*frameSamples]
		exp := int16(0)
		if want != 0 {
			exp = ulawTable[want]
		}
		if got != exp {
			t.Errorf("frame %d: sample %d, want %d", i, got, exp)
		}
	}
}

func TestRecordingWAV(t *testing.T) {
	r := Recording{Heard: []int16{1, -2, 3}, Sent: []int16{7}}
	var b bytes.Buffer
	if err := r.WriteWAV(&b); err != nil {
		t.Fatal(err)
	}
	d := b.Bytes()
	if string(d[0:4]) != "RIFF" || binary.LittleEndian.Uint16(d[22:]) != 2 || binary.LittleEndian.Uint32(d[40:]) != 12 {
		t.Fatalf("bad header % x", d[:44])
	}
	got := []int16{}
	for p := 44; p < len(d); p += 2 {
		got = append(got, int16(binary.LittleEndian.Uint16(d[p:]))) // #nosec G115 -- test
	}
	want := []int16{1, 7, -2, 0, 3, 0} // interleaved heard/sent, sent padded
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("samples %v, want %v", got, want)
		}
	}
}

// BenchmarkCompare is a 3 s reference in a 30 s recording of one leg.
func BenchmarkCompare(b *testing.B) {
	ref := speechLike(7, 3*time.Second)
	heard := make([]int16, 0, 30*sampleRate)
	for len(heard)+len(ref) <= 30*sampleRate {
		heard = append(heard, g711(ref)...)
	}
	b.ResetTimer()
	for range b.N {
		Compare(ref, heard)
	}
}
