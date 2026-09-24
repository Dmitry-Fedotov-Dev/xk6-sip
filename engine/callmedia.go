package engine

import (
	"errors"
	"time"

	"github.com/emiago/sipgo/sip"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/media"
)

// MediaOptions control the RTP side of calls. The zero value means media on
// with PCMU/PCMA and the default tone.
type MediaOptions struct {
	Disabled bool          // SDP only, no RTP socket (signalling-only load)
	Codecs   []media.Codec // preference order
	Audio    *media.Audio  // what we send
	// HeardLevel in dBFS above which received audio counts as heard.
	HeardLevel float64
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
	return media.New(media.Config{IP: d.ip, Codecs: mo.Codecs, Audio: mo.Audio, HeardLevel: mo.HeardLevel})
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
	c.media = st
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
			return c.media.Update(r)
		}
	}
	return c.media.Offer(media.SendRecv)
}

// MediaOptions returns the media settings calls of this device use.
func (d *Device) MediaOptions() MediaOptions { return d.mediaOptions(nil) }
