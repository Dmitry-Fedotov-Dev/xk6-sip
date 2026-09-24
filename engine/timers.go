package engine

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/media"
)

// Session timers (RFC 4028). The refresher re-INVITEs every Session-Expires/2;
// the other side hangs up if no refresh arrives before the session expires.

type sessionTimer struct {
	expires   time.Duration
	refresher bool // we refresh
	timer     *time.Timer
}

// parseSessionExpires reads "Session-Expires: 1800;refresher=uac".
func parseSessionExpires(m interface{ GetHeader(string) sip.Header }) (time.Duration, string, bool) {
	h := m.GetHeader("Session-Expires")
	if h == nil {
		h = m.GetHeader("x") // compact form
	}
	if h == nil {
		return 0, "", false
	}
	v, params, _ := strings.Cut(h.Value(), ";")
	secs, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || secs <= 0 {
		return 0, "", false
	}
	refresher := ""
	for _, p := range strings.Split(params, ";") {
		if k, val, ok := strings.Cut(strings.TrimSpace(p), "="); ok && strings.EqualFold(k, "refresher") {
			refresher = strings.ToLower(val)
		}
	}
	return time.Duration(secs) * time.Second, refresher, true
}

// startSession arms the session timer after the dialog is established.
func (c *Call) startSession(expires time.Duration, weRefresh bool) {
	if expires <= 0 {
		return
	}
	c.mu.Lock()
	c.sess = &sessionTimer{expires: expires, refresher: weRefresh}
	c.mu.Unlock()
	c.trace.note(false, "session timer %v, refresher=%s", expires, map[bool]string{true: "us", false: "peer"}[weRefresh])
	c.sessionRefreshed()
}

// sessionRefreshed restarts the timer after any successful refresh.
func (c *Call) sessionRefreshed() {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.sess
	if s == nil || c.state == StateEnded {
		return
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	if s.refresher {
		s.timer = time.AfterFunc(s.expires/2, c.refreshSession)
	} else {
		s.timer = time.AfterFunc(s.expires-min(32*time.Second, s.expires/3), c.sessionExpired)
	}
}

func (c *Call) stopSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess != nil && c.sess.timer != nil {
		c.sess.timer.Stop()
	}
}

func (c *Call) refreshSession() {
	if c.State() != StateConnected {
		return
	}
	c.reinviteMu.Lock()
	defer c.reinviteMu.Unlock()
	dir := media.SendRecv
	if c.media != nil {
		dir = c.media.Direction()
	}
	c.trace.note(true, "session refresh")
	if ok, _ := c.reinviteOnce(dir, c.sessionHeaders()...); !ok {
		c.trace.note(true, "session refresh failed, hanging up")
		c.byeWith(EndedByError, "session refresh failed")
	}
}

func (c *Call) sessionExpired() {
	if c.State() != StateConnected {
		return
	}
	c.trace.note(true, "session expired: no refresh from peer")
	c.byeWith(EndedByTimeout, "session expired")
}

// sessionHeaders go into our re-INVITE/UPDATE when a session timer runs.
func (c *Call) sessionHeaders() []sip.Header {
	c.mu.Lock()
	s := c.sess
	c.mu.Unlock()
	hdrs := []sip.Header{sip.NewHeader("Supported", "100rel, timer")}
	if s == nil {
		return hdrs
	}
	role := "uas"
	if s.refresher {
		role = "uac" // we send this request, so we are its UAC
	}
	return append(hdrs, sip.NewHeader("Session-Expires", fmt.Sprintf("%d;refresher=%s", int(s.expires/time.Second), role)))
}

// addSessionHeaders echoes the session timer in responses to refreshes.
func (c *Call) addSessionHeaders(req *sip.Request, res *sip.Response) {
	if exp, refresher, ok := parseSessionExpires(req); ok {
		if refresher == "" {
			refresher = "uac"
		}
		res.AppendHeader(sip.NewHeader("Session-Expires", fmt.Sprintf("%d;refresher=%s", int(exp/time.Second), refresher)))
		res.AppendHeader(sip.NewHeader("Require", "timer"))
	}
}

// sessionFromAnswer arms the UAC side from the 2xx of our INVITE.
func (c *Call) sessionFromAnswer(res *sip.Response) {
	exp, refresher, ok := parseSessionExpires(res)
	if !ok {
		return
	}
	// The response must name the refresher; if it doesn't, the UAC refreshes.
	c.startSession(exp, refresher != "uas")
}

// sessionForAnswer decides the UAS side from the INVITE and returns the
// headers for our 2xx.
func (c *Call) sessionForAnswer(req *sip.Request) []sip.Header {
	exp, refresher, ok := parseSessionExpires(req)
	if !ok {
		if c.dev.cfg.SessionExpires <= 0 || !hasOption(req, "Supported", "timer") {
			return nil
		}
		exp, refresher = c.dev.cfg.SessionExpires, "uac"
	}
	if refresher == "" {
		refresher = "uac"
	}
	c.startSessionLater = func() { c.startSession(exp, refresher == "uas") }
	hdrs := []sip.Header{sip.NewHeader("Session-Expires", fmt.Sprintf("%d;refresher=%s", int(exp/time.Second), refresher))}
	if hasOption(req, "Supported", "timer") {
		hdrs = append(hdrs, sip.NewHeader("Require", "timer"))
	}
	return hdrs
}

// byeWith hangs up an established call with a specific reason.
func (c *Call) byeWith(by EndedBy, reason string) {
	c.mu.Lock()
	c.byeBy, c.byeReason = by, reason
	c.mu.Unlock()
	c.bye()
}
