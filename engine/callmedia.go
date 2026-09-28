package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/media"
)

// RecordMode says which calls keep their audio.
type RecordMode int

const (
	RecordOff       RecordMode = iota
	RecordAlways               // every call; saved to RecordDir when it is set
	RecordOnFailure            // every call, saved to RecordDir only if it failed
)

// DefaultRecordDir is where RecordOnFailure saves when RecordDir is empty.
const DefaultRecordDir = "recordings"

// MediaOptions control the RTP side of calls. The zero value means media on
// with PCMU/PCMA and the default tone.
type MediaOptions struct {
	Disabled bool          // SDP only, no RTP socket (signalling-only load)
	Codecs   []media.Codec // preference order
	Audio    *media.Audio  // what we send
	// HeardLevel in dBFS above which received audio counts as heard.
	HeardLevel float64
	// Record keeps what the call heard and sent in memory, about 32 KB per
	// second of call; RecordDir is where finished calls are saved as WAV.
	Record    RecordMode
	RecordDir string
}

// MediaEvent is emitted when a call that had media ends.
type MediaEvent struct {
	Device    string
	Direction Direction
	Stats     media.Stats
}

var errNoMedia = errors.New("call has no media")

func (d *Device) mediaOptions(override *MediaOptions) MediaOptions {
	switch {
	case override != nil:
		return *override
	case d.cfg.Media != nil:
		return *d.cfg.Media
	}
	return d.eng.opts.Media
}

func (d *Device) newStream(mo MediaOptions) (*media.Stream, error) {
	return media.New(media.Config{
		IP: d.ip, Codecs: mo.Codecs, Audio: mo.Audio, HeardLevel: mo.HeardLevel,
		Record: mo.Record != RecordOff,
	})
}

// useMedia attaches the call's RTP stream and remembers how to record it.
func (c *Call) useMedia(st *media.Stream, mo MediaOptions) {
	c.media = st
	c.recMode, c.recDir = mo.Record, mo.RecordDir
	if c.recMode == RecordOnFailure && c.recDir == "" {
		c.recDir = DefaultRecordDir
	}
}

// placeholderSDP is sent when media is disabled: a valid offer/answer whose
// port nothing listens on.
func (d *Device) placeholderSDP() []byte { return sdpAudio(d.ip, d.port+2) }

// remoteSDP applies SDP from a provisional or final response to our offer.
func (c *Call) remoteSDP(res *sip.Response) {
	if c.media == nil || len(res.Body()) == 0 {
		return
	}
	r, err := media.ParseSDP(res.Body())
	if err == nil {
		err = c.media.ApplyAnswer(r)
	}
	if err != nil {
		c.trace.note(false, "bad SDP answer: %v", err)
	}
}

// startMedia begins sending once the call is confirmed.
func (c *Call) startMedia() {
	if c.media != nil {
		c.media.Start()
	}
}

// closeMedia stops RTP and reports its statistics.
func (c *Call) closeMedia(connected bool) {
	if c.media == nil {
		return
	}
	st := c.media.Close()
	if connected {
		c.dev.observer().Media(MediaEvent{Device: c.dev.cfg.ID, Direction: c.dir, Stats: st})
	}
	c.autoSave()
}

// MarkFailed records that a check on this call failed, so RecordOnFailure
// saves its audio: when the call ends, or right away if it already has.
func (c *Call) MarkFailed() {
	c.mu.Lock()
	c.failed = true
	c.mu.Unlock()
	if c.State() == StateEnded {
		c.autoSave()
	}
}

// autoSave writes the recording of an ended call to RecordDir once, if the
// record mode asks for it.
func (c *Call) autoSave() {
	if c.media == nil || c.recDir == "" {
		return
	}
	c.mu.Lock()
	failed := c.failed || (c.endedBy == EndedByError && !c.tAnswer.IsZero())
	want := c.recMode == RecordAlways || (c.recMode == RecordOnFailure && failed)
	if !want || c.saved || c.state != StateEnded {
		c.mu.Unlock()
		return
	}
	c.saved = true
	c.mu.Unlock()
	name := fmt.Sprintf("%s_%s_%s.wav", fileSafe(c.dev.cfg.ID), c.dir, fileSafe(c.callID))
	if err := c.SaveRecording(filepath.Join(c.recDir, name)); err != nil {
		c.dev.eng.log.Warn("sip: saving recording failed", "device", c.dev.cfg.ID, "error", err)
	}
}

func fileSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			return r
		}
		return '_'
	}, s)
}

var errNotRecorded = errors.New("call is not recorded: set record: true or 'onFailure'")

// Recording is what the call heard and sent so far.
func (c *Call) Recording() (media.Recording, error) {
	if c.media == nil {
		return media.Recording{}, errNoMedia
	}
	r, ok := c.media.Recording()
	if !ok {
		return media.Recording{}, errNotRecorded
	}
	return r, nil
}

// SaveRecording writes the call's audio to path as a stereo WAV (left: what
// this side heard, right: what it sent), creating missing directories.
func (c *Call) SaveRecording(path string) error {
	r, err := c.Recording()
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	f, err := os.Create(path) // #nosec G304 -- the path comes from the test script author
	if err != nil {
		return err
	}
	if err := r.WriteWAV(f); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// CompareAudio scores what this side heard against ref. ok is false when
// there is too little audio to compare.
func (c *Call) CompareAudio(ref *media.Audio) (media.Quality, bool, error) {
	r, err := c.Recording()
	if err != nil {
		return media.Quality{}, false, err
	}
	q, ok := media.Compare(ref.PCM(), r.Heard)
	return q, ok, nil
}

// Codec is the negotiated audio codec, "" without media or before SDP.
func (c *Call) Codec() string {
	if c.media == nil {
		return ""
	}
	return c.media.Codec()
}

// WaitHeard waits until audio from the other side arrives (at least 100 ms
// above the heard level). False without media.
func (c *Call) WaitHeard(timeout time.Duration) bool {
	return c.media != nil && c.media.WaitHeard(timeout)
}

// SendDTMF sends RFC 4733 digits; dur is per digit (default 100 ms).
func (c *Call) SendDTMF(digits string, dur time.Duration) error {
	if c.media == nil {
		return errNoMedia
	}
	if c.State() != StateConnected {
		return errors.New("call is not connected")
	}
	c.trace.note(true, "DTMF %s", digits)
	return c.media.SendDTMF(digits, dur)
}

// WaitDigits waits until the received DTMF digits contain want.
func (c *Call) WaitDigits(want string, timeout time.Duration) bool {
	return c.media != nil && c.media.WaitDigits(want, timeout)
}

// Digits returns the DTMF digits received so far.
func (c *Call) Digits() string {
	if c.media == nil {
		return ""
	}
	return c.media.Digits()
}

// MediaStats returns current RTP statistics, false without media.
func (c *Call) MediaStats() (media.Stats, bool) {
	if c.media == nil {
		return media.Stats{}, false
	}
	return c.media.Stats(), true
}

// setupIncomingMedia prepares our answer to the INVITE's offer. An INVITE
// without SDP gets our offer in the 200 OK and the answer comes in the ACK.
func (c *Call) setupIncomingMedia(offer []byte, mo MediaOptions) error {
	st, err := c.dev.newStream(mo)
	if err != nil {
		return err
	}
	c.useMedia(st, mo)
	if len(offer) == 0 {
		c.answerSDP, c.ackSDP = st.Offer(media.SendRecv), true
		return nil
	}
	r, err := media.ParseSDP(offer)
	if err != nil {
		return err
	}
	c.answerSDP, err = st.Answer(r)
	return err
}

func (c *Call) ackAnswer(body []byte) {
	r, err := media.ParseSDP(body)
	if err == nil {
		err = c.media.ApplyAnswer(r)
	}
	if err != nil {
		c.trace.note(false, "bad SDP in ACK: %v", err)
	}
}

// reofferAnswer applies an in-dialog offer and returns our answer.
func (c *Call) reofferAnswer(offer []byte) []byte {
	if c.media == nil {
		return c.dev.placeholderSDP()
	}
	if len(offer) > 0 {
		if r, err := media.ParseSDP(offer); err == nil {
			c.trace.note(false, "re-INVITE: remote %s", r.Dir)
			c.mu.Lock()
			c.remoteHold = r.Dir == media.SendOnly || r.Dir == media.Inactive
			c.mu.Unlock()
			return c.media.Update(r)
		}
	}
	return c.media.Offer(media.SendRecv)
}

// MediaOptions returns the media settings calls of this device use.
func (d *Device) MediaOptions() MediaOptions { return d.mediaOptions(nil) }
