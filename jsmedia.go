package xk6sip

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/grafana/sobek"
	"go.k6.io/k6/v2/js/common"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/engine"
	"github.com/Dmitry-Fedotov-Dev/xk6-sip/media"
)

// jsAudio is an audio source handle returned by sip.audio() / sip.tone().
type jsAudio struct {
	a *media.Audio
}

// audioCache shares decoded sources between VUs: every VU runs the init
// code and would otherwise keep its own copy of the same WAV.
type audioCache struct {
	mu sync.Mutex
	m  map[string]*media.Audio
}

func (c *audioCache) get(key string, make func() (*media.Audio, error)) (*media.Audio, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if a, ok := c.m[key]; ok {
		return a, nil
	}
	a, err := make()
	if err != nil {
		return nil, err
	}
	if c.m == nil {
		c.m = map[string]*media.Audio{}
	}
	c.m[key] = a
	return a, nil
}

// audio loads a WAV file (16-bit PCM, 8 kHz, mono):
//
//	const greeting = sip.audio(open('./greeting.wav', 'b'));
func (mi *ModuleInstance) audio(v sobek.Value) *jsAudio {
	rt := mi.vu.Runtime()
	var data []byte
	switch x := v.Export().(type) {
	case sobek.ArrayBuffer:
		data = x.Bytes()
	case []byte:
		data = x
	default:
		common.Throw(rt, fmt.Errorf("sip.audio: expected the result of open(path, 'b'), got %s", v.String()))
	}
	sum := sha256.Sum256(data)
	a, err := mi.root.audio.get(fmt.Sprintf("wav:%x", sum), func() (*media.Audio, error) { return media.ParseWAV(data) })
	if err != nil {
		common.Throw(rt, fmt.Errorf("sip.audio: %w", err))
	}
	return &jsAudio{a: a}
}

// tone is a sine source: sip.tone(freq = 1000, dbfs = -20).
func (mi *ModuleInstance) tone(freq, dbfs sobek.Value) *jsAudio {
	f, l := 1000.0, -20.0
	if isSet(freq) {
		f = freq.ToFloat()
	}
	if isSet(dbfs) {
		l = dbfs.ToFloat()
	}
	a, _ := mi.root.audio.get(fmt.Sprintf("tone:%g:%g", f, l), func() (*media.Audio, error) {
		return media.Tone(f, l, time.Second), nil
	})
	return &jsAudio{a: a}
}

// mediaFields reads media settings from options, Device or call objects:
//
//	{ media: false }                   // signalling only, no RTP
//	{ codecs: 'PCMA,PCMU' }            // preference order
//	{ audio: sip.audio(...) | 'tone' | 'silence' }
//	{ heardLevel: -45 }                // dBFS threshold for isHeard
func mediaFields(rt *sobek.Runtime, obj *sobek.Object, base engine.MediaOptions) (engine.MediaOptions, bool) {
	mo, changed := base, false
	if v := obj.Get("media"); isSet(v) {
		mo.Disabled, changed = !v.ToBoolean(), true
	}
	if v := obj.Get("codecs"); isSet(v) {
		list := v.String()
		if arr, ok := v.Export().([]any); ok {
			list = ""
			for _, x := range arr {
				list += fmt.Sprint(x) + ","
			}
		}
		codecs, err := media.ParseCodecs(list)
		if err != nil {
			common.Throw(rt, err)
		}
		mo.Codecs, changed = codecs, true
	}
	if v := obj.Get("audio"); isSet(v) {
		switch x := v.Export().(type) {
		case *jsAudio:
			mo.Audio = x.a
		case string:
			switch x {
			case "tone":
				mo.Audio = media.DefaultAudio()
			case "silence":
				mo.Audio = silence()
			default:
				common.Throw(rt, fmt.Errorf("audio: expected sip.audio(...), 'tone' or 'silence', got %q", x))
			}
		default:
			common.Throw(rt, fmt.Errorf("audio: expected sip.audio(...), 'tone' or 'silence'"))
		}
		changed = true
	}
	if v := obj.Get("heardLevel"); isSet(v) {
		mo.HeardLevel, changed = v.ToFloat(), true
	}
	return mo, changed
}

var silence = sync.OnceValue(media.Silence)

func sameMedia(a, b engine.MediaOptions) bool {
	return a.Disabled == b.Disabled && a.Audio == b.Audio && a.HeardLevel == b.HeardLevel &&
		slices.EqualFunc(a.Codecs, b.Codecs, func(x, y media.Codec) bool { return x.Name == y.Name })
}

// ---- Call media methods

// Codec is the negotiated codec (PCMU/PCMA), or null without media.
func (c *jsCall) Codec() sobek.Value {
	if name := c.call.Codec(); name != "" {
		return c.dev.mi.vu.Runtime().ToValue(name)
	}
	return sobek.Null()
}

// IsHeard waits until audio from the other side arrives.
func (c *jsCall) IsHeard(timeout sobek.Value) bool {
	rt := c.dev.mi.vu.Runtime()
	return c.expect("heard", c.call.WaitHeard(durationArg(rt, timeout, c.dev.mi.root.opts.expectTimeout)))
}

// SendDTMF sends RFC 4733 digits; duration per digit in ms (default 100).
func (c *jsCall) SendDTMF(digits string, durMs sobek.Value) bool {
	dur := 100 * time.Millisecond
	if isSet(durMs) {
		dur = time.Duration(durMs.ToFloat() * float64(time.Millisecond))
	}
	if err := c.call.SendDTMF(digits, dur); err != nil {
		c.dev.warn("sendDTMF failed", err)
		return false
	}
	return true
}

// ExpectDTMF waits until the received digits contain want.
func (c *jsCall) ExpectDTMF(want string, timeout sobek.Value) bool {
	rt := c.dev.mi.vu.Runtime()
	return c.expect("dtmf", c.call.WaitDigits(want, durationArg(rt, timeout, c.dev.mi.root.opts.expectTimeout)))
}

// ReceivedDTMF returns the digits received so far.
func (c *jsCall) ReceivedDTMF() string { return c.call.Digits() }

// MediaStats returns {codec, sent, received, lost, jitter (ms), heard (ms),
// dtmf} or null without media.
func (c *jsCall) MediaStats() sobek.Value {
	st, ok := c.call.MediaStats()
	if !ok {
		return sobek.Null()
	}
	return c.dev.mi.vu.Runtime().ToValue(map[string]any{
		"codec":    st.Codec,
		"sent":     st.PacketsSent,
		"received": st.PacketsReceived,
		"expected": st.PacketsExpected,
		"lost":     st.PacketsLost,
		"jitter":   ms(st.Jitter),
		"heard":    ms(st.Heard),
		"dtmf":     st.DTMF,
	})
}
