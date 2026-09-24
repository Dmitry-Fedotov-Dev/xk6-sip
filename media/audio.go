package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

// Audio is an 8 kHz mono source that streams loop over. It is encoded once
// per codec and shared read-only by all streams, so thousands of calls cost
// no encoding CPU.
type Audio struct {
	pcm []int16

	mu  sync.Mutex
	enc map[string][]byte
}

func newAudio(pcm []int16) *Audio {
	// Whole frames only, so looping never produces a short packet.
	if n := len(pcm) / frameSamples * frameSamples; n > 0 {
		pcm = pcm[:n]
	} else {
		pcm = make([]int16, frameSamples)
	}
	return &Audio{pcm: pcm, enc: map[string][]byte{}}
}

// Tone is a sine wave at freq Hz and level dBFS (e.g. -20).
func Tone(freq, dbfs float64, dur time.Duration) *Audio {
	n := int(dur.Seconds() * sampleRate)
	amp := math.Pow(10, dbfs/20) * math.MaxInt16
	pcm := make([]int16, n)
	for i := range pcm {
		pcm[i] = int16(amp * math.Sin(2*math.Pi*freq*float64(i)/sampleRate))
	}
	return newAudio(pcm)
}

// Silence is digital silence.
func Silence() *Audio { return newAudio(make([]int16, frameSamples)) }

// DefaultAudio is what calls send unless told otherwise: a 1 kHz tone at
// -20 dBFS, so the far end can tell that audio arrives.
var DefaultAudio = sync.OnceValue(func() *Audio { return Tone(1000, -20, time.Second) })

// ParseWAV reads a RIFF WAV file with 16-bit PCM, 8 kHz, mono samples.
func ParseWAV(data []byte) (*Audio, error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errors.New("not a RIFF/WAVE file")
	}
	var fmtFound bool
	var channels, bits uint16
	var rate uint32
	var format uint16
	for p := 12; p+8 <= len(data); {
		id := string(data[p : p+4])
		size := int(binary.LittleEndian.Uint32(data[p+4 : p+8]))
		body := data[p+8:]
		if size > len(body) {
			size = len(body)
		}
		body = body[:size]
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, errors.New("short fmt chunk")
			}
			format = binary.LittleEndian.Uint16(body[0:2])
			channels = binary.LittleEndian.Uint16(body[2:4])
			rate = binary.LittleEndian.Uint32(body[4:8])
			bits = binary.LittleEndian.Uint16(body[14:16])
			fmtFound = true
		case "data":
			if !fmtFound {
				return nil, errors.New("data chunk before fmt chunk")
			}
			if format != 1 || bits != 16 || channels != 1 || rate != sampleRate {
				return nil, fmt.Errorf("need 16-bit PCM mono 8000 Hz WAV, got format=%d bits=%d channels=%d rate=%d "+
					"(convert with: ffmpeg -i in.wav -ar 8000 -ac 1 -c:a pcm_s16le out.wav)", format, bits, channels, rate)
			}
			pcm := make([]int16, size/2)
			for i := range pcm {
				pcm[i] = int16(binary.LittleEndian.Uint16(body[2*i:])) // #nosec G115 -- reinterpret little-endian bits as signed PCM
			}
			return newAudio(pcm), nil
		}
		p += 8 + size + size&1 // chunks are word aligned
	}
	return nil, errors.New("no data chunk")
}

// Duration of one loop of the source.
func (a *Audio) Duration() time.Duration {
	return time.Duration(len(a.pcm)) * time.Second / sampleRate
}

// encoded returns the whole source encoded with c.
func (a *Audio) encoded(c Codec) []byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	if b, ok := a.enc[c.Name]; ok {
		return b
	}
	b := make([]byte, len(a.pcm))
	for i, s := range a.pcm {
		b[i] = c.encode(s)
	}
	a.enc[c.Name] = b
	return b
}
