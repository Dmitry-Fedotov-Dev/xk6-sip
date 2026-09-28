package media

import (
	"encoding/binary"
	"io"
	"time"
)

// maxRecordSamples caps one track of a recording: 15 minutes, 14 MB per
// track. A call that runs longer keeps only the beginning.
const maxRecordSamples = 15 * 60 * sampleRate

// recorder keeps what a call leg heard (rx) and what it sent (tx) as
// linear 8 kHz samples on a common wall-clock timeline that starts when the
// stream is created.
type recorder struct {
	start  time.Time
	rx, tx track
}

// track places frames by their RTP timestamp rather than in arrival order,
// so a lost packet becomes silence in its place and jitter or reordering do
// not shift the audio.
type track struct {
	init    bool
	ssrc    uint32
	ts0     uint32 // RTP timestamp of the first frame of this SSRC
	off0    int    // its sample position on the recording timeline
	samples []int16
}

func newRecorder() *recorder { return &recorder{start: time.Now()} }

// at is the sample position of now on the recording timeline.
func (r *recorder) at(now time.Time) int {
	return int(now.Sub(r.start).Seconds() * sampleRate)
}

// put decodes a G.711 payload with table and stores it at the position of
// its RTP timestamp. now is the timeline position of this frame's arrival;
// it anchors the first frame and the first frame after an SSRC change.
func (t *track) put(now int, ssrc, ts uint32, payload []byte, table *[256]int16) {
	if !t.init || ssrc != t.ssrc {
		t.init, t.ssrc, t.ts0, t.off0 = true, ssrc, ts, now
	}
	at := t.off0 + int(int32(ts-t.ts0)) // #nosec G115 -- wrap-around difference is intended
	if at < 0 || at+len(payload) > maxRecordSamples {
		return
	}
	if need := at + len(payload); need > len(t.samples) {
		t.samples = append(t.samples, make([]int16, need-len(t.samples))...)
	}
	for i, b := range payload {
		t.samples[at+i] = table[b]
	}
}

// Recording is a copy of what a call leg heard and sent.
type Recording struct {
	Heard []int16 // received audio, what the subscriber hears
	Sent  []int16 // our audio, what the subscriber says
}

// Recording returns a copy of the recorded audio so far; ok is false when
// the stream was created without Record.
func (s *Stream) Recording() (Recording, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rec == nil {
		return Recording{}, false
	}
	return Recording{
		Heard: append([]int16(nil), s.rec.rx.samples...),
		Sent:  append([]int16(nil), s.rec.tx.samples...),
	}, true
}

// WriteWAV writes the recording as a 16-bit PCM 8 kHz stereo WAV: the left
// channel is what the subscriber heard, the right what it sent. The shorter
// track is padded with silence.
func (r Recording) WriteWAV(w io.Writer) error {
	n := max(len(r.Heard), len(r.Sent))
	data := make([]byte, 44+4*n)
	copy(data[0:], "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(36+4*n)) // #nosec G115 -- bounded by maxRecordSamples
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)           // fmt chunk size
	binary.LittleEndian.PutUint16(data[20:], 1)            // PCM
	binary.LittleEndian.PutUint16(data[22:], 2)            // channels
	binary.LittleEndian.PutUint32(data[24:], sampleRate)   // sample rate
	binary.LittleEndian.PutUint32(data[28:], sampleRate*4) // byte rate
	binary.LittleEndian.PutUint16(data[32:], 4)            // block align
	binary.LittleEndian.PutUint16(data[34:], 16)           // bits per sample
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(4*n)) // #nosec G115 -- bounded by maxRecordSamples
	for i := range n {
		p := 44 + 4*i
		if i < len(r.Heard) {
			binary.LittleEndian.PutUint16(data[p:], uint16(r.Heard[i])) // #nosec G115 -- reinterpret signed PCM bits
		}
		if i < len(r.Sent) {
			binary.LittleEndian.PutUint16(data[p+2:], uint16(r.Sent[i])) // #nosec G115 -- reinterpret signed PCM bits
		}
	}
	_, err := w.Write(data)
	return err
}
