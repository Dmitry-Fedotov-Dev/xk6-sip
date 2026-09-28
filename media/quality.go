package media

import (
	"math"
	"math/cmplx"
	"slices"
	"time"
)

// Quality compares what a call leg heard with the reference audio that
// should have arrived.
type Quality struct {
	// Score is the similarity of the heard audio to the reference, 0..1:
	// the correlation of their telephone-band spectrograms after alignment.
	// It ignores the level; drop-outs, noise, distortion and wrong audio
	// lower it.
	Score float64
	// Offset is where the reference starts in the recording.
	Offset time.Duration
	// Compared is the length of the reference that was compared: shorter
	// than the reference when the recording ends early.
	Compared time.Duration
	// Gaps is the reference speech during which nothing was heard.
	Gaps time.Duration
	// ClippedStart is the part of the first word that was lost: gaps from
	// the start of the reference speech until audio is first heard.
	ClippedStart time.Duration
	// Gain is the level of the heard audio relative to the reference, dB.
	Gain float64
}

const (
	envBlock  = 80  // 10 ms blocks for coarse alignment
	specFrame = 256 // 32 ms spectrogram frames
	specHop   = 128 // 16 ms
	// Telephone band 300..3400 Hz in bins of a 256-point FFT at 8 kHz.
	bandLo = 10
	bandHi = 108
	// dB floor for silence: digital silence would otherwise be -Inf.
	floorDB = -100
	// A reference frame is speech if it is at most activeRange below the
	// loudest frame and above activeMin dBFS.
	activeRange = 40
	activeMin   = -55
	// A speech frame is a gap if the heard frame is gapDrop dB quieter
	// than the reference frame scaled by Gain.
	gapDrop = 20
)

// Compare aligns heard with ref and scores it. ok is false when either
// signal is too short to compare (under one spectrogram frame) or ref has no
// audio at all.
func Compare(ref, heard []int16) (Quality, bool) {
	if len(ref) < specFrame || len(heard) < specFrame {
		return Quality{}, false
	}
	lag := align(ref, heard)
	seg := make([]int16, len(ref)) // heard audio under the reference; zeros past the end
	if lag < len(heard) {
		copy(seg, heard[lag:])
	}
	compared := min(len(ref), max(len(heard)-lag, 0))

	rs, rl := spectrogram(ref)
	hs, _ := spectrogram(seg)
	active := speech(rl)
	if len(active) == 0 {
		return Quality{}, false
	}

	// Gaps are counted on 10 ms blocks: a lost 20 ms packet is two blocks,
	// while a 32 ms spectrogram frame would mostly hide it.
	re, he := envelope(ref), envelope(seg)
	blocks := speech(re)
	var diffs []float64
	for _, i := range blocks {
		if he[i] > floorDB+10 {
			diffs = append(diffs, he[i]-re[i])
		}
	}
	gain := 0.0 // nothing heard at all: every speech block is a gap
	if len(diffs) > 0 {
		slices.Sort(diffs)
		gain = diffs[len(diffs)/2]
	}
	gaps, clipped, leading := 0, 0, true
	for _, i := range blocks {
		if he[i] < re[i]+gain-gapDrop {
			gaps++
			if leading {
				clipped++
			}
		} else {
			leading = false
		}
	}
	if len(diffs) == 0 {
		gain = floorDB
	}

	block := samplesDur(envBlock)
	return Quality{
		Score:        spectralScore(rs, hs, active),
		Offset:       samplesDur(lag),
		Compared:     samplesDur(compared),
		Gaps:         time.Duration(gaps) * block,
		ClippedStart: time.Duration(clipped) * block,
		Gain:         math.Round(gain*10) / 10,
	}, true
}

// speech returns the indexes of frames with speech: at most activeRange dB
// below the loudest frame and above activeMin dBFS.
func speech(levels []float64) []int {
	if len(levels) == 0 {
		return nil
	}
	peak := slices.Max(levels)
	var out []int
	for i, e := range levels {
		if e >= activeMin && e >= peak-activeRange {
			out = append(out, i)
		}
	}
	return out
}

func samplesDur(n int) time.Duration { return time.Duration(n) * time.Second / sampleRate }

// align returns the sample offset in heard where ref fits best: first by
// the correlation of 10 ms level envelopes, then by the waveform within
// ±10 ms around it.
func align(ref, heard []int16) int {
	re, he := envelope(ref), envelope(heard)
	best, bestScore := 0, math.Inf(-1)
	last := max(len(he)-len(re), 0)
	stationary := variance(re) < 1 // a tone: the envelope has no shape to match
	for l := 0; l <= last; l++ {
		var s float64
		if stationary {
			s = -meanAbsDiff(re, he[l:])
		} else {
			s = pearson(re, he[l:])
		}
		if s > bestScore {
			best, bestScore = l, s
		}
	}

	// Refine on the waveform over the first second of the reference.
	n := min(len(ref), sampleRate)
	lag, lagScore := best*envBlock, math.Inf(-1)
	for d := -envBlock; d <= envBlock; d++ {
		l := best*envBlock + d
		if l < 0 || l+n > len(heard) {
			continue
		}
		var dot, e1, e2 float64
		for i := range n {
			a, b := float64(ref[i]), float64(heard[l+i])
			dot, e1, e2 = dot+a*b, e1+a*a, e2+b*b
		}
		if e1 == 0 || e2 == 0 {
			continue
		}
		if c := dot / math.Sqrt(e1*e2); c > lagScore {
			lag, lagScore = l, c
		}
	}
	return lag
}

// envelope is the level of each 10 ms block in dB.
func envelope(x []int16) []float64 {
	out := make([]float64, len(x)/envBlock)
	for i := range out {
		var sum float64
		for _, v := range x[i*envBlock : (i+1)*envBlock] {
			f := float64(v) / math.MaxInt16
			sum += f * f
		}
		out[i] = toDB(sum / envBlock)
	}
	return out
}

func toDB(power float64) float64 {
	if power <= 0 {
		return floorDB
	}
	return max(10*math.Log10(power), floorDB)
}

// spectrogram returns the telephone-band power spectrum of each frame in dB
// and the frame levels in dBFS.
func spectrogram(x []int16) ([][]float64, []float64) {
	frames := (len(x)-specFrame)/specHop + 1
	spec, level := make([][]float64, frames), make([]float64, frames)
	win := hann()
	buf := make([]complex128, specFrame)
	for f := range frames {
		var energy float64
		for i := range specFrame {
			v := float64(x[f*specHop+i]) / math.MaxInt16
			energy += v * v
			buf[i] = complex(v*win[i], 0)
		}
		fft(buf)
		row := make([]float64, bandHi-bandLo+1)
		for k := bandLo; k <= bandHi; k++ {
			a := cmplx.Abs(buf[k])
			row[k-bandLo] = toDB(a * a / specFrame)
		}
		spec[f], level[f] = row, toDB(energy/specFrame)
	}
	return spec, level
}

// spectralScore correlates the two spectrograms over the speech frames.
// Each frequency band has its average over time removed first, so what is
// compared is how the spectrum changes with time: unrelated speech with a
// similar average timbre scores low. A stationary reference (a tone) has
// nothing left after that, so for it the spectral shape is compared.
func spectralScore(ref, heard [][]float64, active []int) float64 {
	bins := len(ref[0])
	centre := func(s [][]float64) [][]float64 {
		mean := make([]float64, bins)
		for _, i := range active {
			for k, v := range s[i] {
				mean[k] += v / float64(len(active))
			}
		}
		out := make([][]float64, len(active))
		for j, i := range active {
			out[j] = make([]float64, bins)
			for k, v := range s[i] {
				out[j][k] = v - mean[k]
			}
		}
		return out
	}
	rc, hc := centre(ref), centre(heard)
	var dyn float64
	for _, row := range rc {
		for _, v := range row {
			dyn += v * v
		}
	}
	if dyn/float64(len(active)*bins) < 4 { // under 2 dB of change: stationary
		var sum float64
		for _, i := range active {
			sum += max(pearson(ref[i], heard[i]), 0)
		}
		return math.Round(sum/float64(len(active))*1000) / 1000
	}
	var dot, e1, e2 float64
	for j := range rc {
		for k := range rc[j] {
			a, b := rc[j][k], hc[j][k]
			dot, e1, e2 = dot+a*b, e1+a*a, e2+b*b
		}
	}
	if e1 == 0 || e2 == 0 {
		return 0
	}
	return math.Round(max(dot/math.Sqrt(e1*e2), 0)*1000) / 1000
}

// pearson is the correlation of a with the first len(a) values of b.
func pearson(a, b []float64) float64 {
	n := min(len(a), len(b))
	if n < 2 {
		return 0
	}
	var ma, mb float64
	for i := range n {
		ma, mb = ma+a[i], mb+b[i]
	}
	ma, mb = ma/float64(n), mb/float64(n)
	var dot, ea, eb float64
	for i := range n {
		x, y := a[i]-ma, b[i]-mb
		dot, ea, eb = dot+x*y, ea+x*x, eb+y*y
	}
	if ea == 0 || eb == 0 {
		return 0
	}
	return dot / math.Sqrt(ea*eb)
}

func variance(a []float64) float64 {
	if len(a) == 0 {
		return 0
	}
	var m, v float64
	for _, x := range a {
		m += x
	}
	m /= float64(len(a))
	for _, x := range a {
		v += (x - m) * (x - m)
	}
	return v / float64(len(a))
}

func meanAbsDiff(a, b []float64) float64 {
	n := min(len(a), len(b))
	if n == 0 {
		return math.Inf(1)
	}
	var s float64
	for i := range n {
		s += math.Abs(a[i] - b[i])
	}
	return s / float64(n)
}

func hann() []float64 {
	w := make([]float64, specFrame)
	for i := range w {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(specFrame-1))
	}
	return w
}

// fft is an in-place iterative radix-2 FFT; len(x) must be a power of two.
func fft(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wk := complex(1, 0)
			for k := range size / 2 {
				a, b := x[start+k], x[start+k+size/2]*wk
				x[start+k], x[start+k+size/2] = a+b, a-b
				wk *= w
			}
		}
	}
}

// PCM returns the samples of the source.
func (a *Audio) PCM() []int16 { return a.pcm }
