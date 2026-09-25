package engine

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"github.com/Dmitry-Fedotov-Dev/xk6-sip/media"
)

// DeviceConfig describes one subscriber.
type DeviceConfig struct {
	ID        string // name used in metrics and logs
	Registrar string // "sip:host:port" or "host:port"
	Proxy     string // outbound proxy "host:port"; defaults to the registrar
	User      string // "alice@domain" or "alice" (domain = registrar host)
	AuthUser  string // digest username; defaults to the user part of User
	Password  string
	Expires   time.Duration // registration lifetime; default 300s
	// NoRegister skips REGISTER, e.g. for trunks authorised by IP.
	NoRegister  bool
	DisplayName string
	// Identities are the numbers this subscriber is known by, e.g.
	// {"ext": "701", "onk": "+79101110011"}. Scripts refer to them by key.
	Identities map[string]string
	// SessionExpires requests RFC 4028 session timers on calls we make
	// (and offers them on calls we answer); 0 leaves it to the peer.
	SessionExpires time.Duration
	// PRACK makes us send 180 reliably (RFC 3262) when the caller supports it.
	PRACK bool
	// Media overrides the engine's media defaults for this device.
	Media *MediaOptions
	// Observer overrides the engine's observer for this device's events,
	// e.g. to route metrics to the k6 VU that owns the device.
	Observer Observer
}

// Device is a SIP subscriber: one UDP socket used for both client and server
// transactions, an optional registration that is kept refreshed, and the
// calls it makes and receives. Creating a Device does no network I/O; the
// socket is opened and REGISTER sent on first use.
type Device struct {
	eng *Engine
	cfg DeviceConfig

	aor          sip.Uri // sip:user@domain
	registrarURI sip.Uri
	proxyAddr    string
	authUser     string

	startMu sync.Mutex
	started bool

	ctx    context.Context
	cancel context.CancelFunc

	ip       string
	port     int
	laddr    sip.Addr
	pc       net.PacketConn
	ua       *sipgo.UserAgent
	client   *sipgo.Client
	server   *sipgo.Server
	contact  sip.ContactHeader
	dialogUA sipgo.DialogUA

	regMu      sync.Mutex
	registered bool
	regCallID  string
	regFromTag string
	regCSeq    uint32
	regTimer   *time.Timer

	callsMu sync.Mutex
	calls   map[string]*Call // by Call-ID

	inMu     sync.Mutex
	inbox    []*Call
	inSignal chan struct{}

	lastOut atomic.Pointer[Call]
}

func (e *Engine) NewDevice(cfg DeviceConfig) (*Device, error) {
	if cfg.Registrar == "" {
		return nil, errors.New("registrar is required")
	}
	if cfg.User == "" {
		return nil, errors.New("user is required")
	}
	if cfg.Expires == 0 {
		cfg.Expires = 300 * time.Second
	}

	var reg sip.Uri
	regStr := cfg.Registrar
	if !strings.HasPrefix(regStr, "sip:") && !strings.HasPrefix(regStr, "sips:") {
		regStr = "sip:" + regStr
	}
	if err := sip.ParseUri(regStr, &reg); err != nil {
		return nil, fmt.Errorf("registrar %q: %w", cfg.Registrar, err)
	}
	reg.User = ""
	if reg.Port == 0 {
		reg.Port = 5060
	}

	user, domain, _ := strings.Cut(cfg.User, "@")
	if domain == "" {
		domain = reg.Host
	}
	proxy := cfg.Proxy
	if proxy == "" {
		proxy = reg.HostPort()
	}
	authUser := cfg.AuthUser
	if authUser == "" {
		authUser = user
	}
	if cfg.ID == "" {
		cfg.ID = user + "@" + domain
	}

	d := &Device{
		eng:          e,
		cfg:          cfg,
		aor:          sip.Uri{Scheme: "sip", User: user, Host: domain},
		registrarURI: reg,
		proxyAddr:    proxy,
		authUser:     authUser,
		calls:        make(map[string]*Call),
		inSignal:     make(chan struct{}),
	}
	e.mu.Lock()
	e.devices[d] = struct{}{}
	e.mu.Unlock()
	return d, nil
}

func (d *Device) ID() string { return d.cfg.ID }

func (d *Device) observer() Observer {
	if d.cfg.Observer != nil {
		return d.cfg.Observer
	}
	return d.eng.opts.Observer
}

// Identity returns the number registered under key, e.g. "ext".
func (d *Device) Identity(key string) (string, bool) {
	v, ok := d.cfg.Identities[key]
	return v, ok
}

// Registered reports whether the last REGISTER succeeded.
// Started reports whether the socket is open (and REGISTER was sent).
func (d *Device) Started() bool {
	d.startMu.Lock()
	defer d.startMu.Unlock()
	return d.started
}

func (d *Device) Registered() bool {
	d.regMu.Lock()
	defer d.regMu.Unlock()
	return d.registered
}

// Start opens the socket and registers. It is called implicitly by Call and
// ExpectCall; calling it again after success is a no-op. A failed start is
// retried on the next call.
func (d *Device) Start() error {
	d.startMu.Lock()
	defer d.startMu.Unlock()
	if d.started {
		return nil
	}
	if err := d.open(); err != nil {
		return err
	}
	if !d.cfg.NoRegister {
		if err := d.Register(); err != nil {
			d.close()
			return err
		}
	}
	d.started = true
	return nil
}

func (d *Device) open() error {
	ip := d.eng.opts.LocalIP
	if ip == "" {
		var err error
		if ip, err = localIPFor(d.proxyAddr); err != nil {
			return err
		}
	}
	pc, err := net.ListenPacket("udp", net.JoinHostPort(ip, "0"))
	if err != nil {
		return err
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	hostPort := net.JoinHostPort(ip, strconv.Itoa(port))

	ua, err := sipgo.NewUA(sipgo.WithUserAgent(d.aor.User), sipgo.WithUserAgentHostname(ip),
		sipgo.WithUserAgentTransactionLayerOptions(sip.WithTransactionLayerLogger(d.eng.log)),
		sipgo.WithUserAgentTransportLayerOptions(sip.WithTransportLayerLogger(d.eng.log)))
	if err != nil {
		_ = pc.Close()
		return err
	}
	srv, err := sipgo.NewServer(ua, sipgo.WithServerLogger(d.eng.log))
	if err != nil {
		_ = ua.Close()
		_ = pc.Close()
		return err
	}
	cl, err := sipgo.NewClient(ua,
		sipgo.WithClientLogger(d.eng.log),
		sipgo.WithClientHostname(ip),
		sipgo.WithClientPort(port),
		// Send from the listening socket; WithClientPort alone only sets the
		// Via port and the client would open a socket per destination.
		sipgo.WithClientConnectionAddr(hostPort))
	if err != nil {
		_ = ua.Close()
		_ = pc.Close()
		return err
	}

	d.ctx, d.cancel = context.WithCancel(context.Background())
	d.ip, d.port, d.pc = ip, port, pc
	d.laddr = sip.Addr{IP: net.ParseIP(ip), Port: port}
	d.ua, d.client, d.server = ua, cl, srv
	d.contact = sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: d.aor.User, Host: ip, Port: port}}
	d.dialogUA = sipgo.DialogUA{Client: cl, ContactHDR: d.contact}
	d.regCallID = sip.GenerateTagN(24)
	d.regFromTag = sip.GenerateTagN(16)
	d.regCSeq = 0

	srv.OnInvite(d.onInvite)
	srv.OnAck(d.onAck)
	srv.OnBye(d.onBye)
	ok := func(req *sip.Request, tx sip.ServerTransaction) {
		d.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)))
	}
	srv.OnOptions(ok)
	srv.OnInfo(ok)
	srv.OnNotify(d.onNotify)
	srv.OnUpdate(d.onUpdate)
	srv.OnPrack(d.onPrack)
	srv.OnRefer(d.onRefer)
	go srv.ServeUDP(pc)
	if err := waitListening(ua, hostPort); err != nil {
		d.close()
		return err
	}
	return nil
}

// waitListening waits until sipgo has put the listener into its connection
// pool. ServeUDP does that in its own goroutine; a request sent before it
// makes the client try to bind the same address again and fail.
func waitListening(ua *sipgo.UserAgent, hostPort string) error {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if c, err := ua.TransportLayer().GetConnection("udp", hostPort); err == nil && c != nil {
			_, _ = c.TryClose() // GetConnection took a reference
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("SIP listener on %s did not start", hostPort)
		}
		time.Sleep(time.Millisecond)
	}
}

func (d *Device) close() {
	if d.cancel != nil {
		d.cancel()
	}
	if d.ua != nil {
		_ = d.ua.Close()
	}
	if d.pc != nil {
		_ = d.pc.Close()
	}
	d.ua, d.pc = nil, nil
}

// Destroy hangs up active calls, unregisters and closes the socket. The
// device stays known to the engine: Start reopens it and Engine.Close
// still cleans it up.
func (d *Device) Destroy() {
	d.startMu.Lock()
	defer d.startMu.Unlock()
	if !d.started {
		return
	}
	d.started = false

	d.callsMu.Lock()
	active := make([]*Call, 0, len(d.calls))
	for _, c := range d.calls {
		active = append(active, c)
	}
	d.callsMu.Unlock()
	for _, c := range active {
		c.Hangup()
	}

	d.regMu.Lock()
	if d.regTimer != nil {
		d.regTimer.Stop()
		d.regTimer = nil
	}
	wasRegistered := d.registered
	d.regMu.Unlock()
	if wasRegistered {
		ctx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
		d.logErr("unregister", d.register(ctx, 0))
		cancel()
	}
	d.close()
}

// Register sends REGISTER now and schedules refreshes.
func (d *Device) Register() error {
	return d.register(d.ctx, int(d.cfg.Expires/time.Second))
}

func (d *Device) register(ctx context.Context, expires int) error {
	if lim := d.eng.limiter; lim != nil {
		if err := lim.Wait(ctx); err != nil {
			return err
		}
	}

	d.regMu.Lock()
	d.regCSeq++
	cseq := d.regCSeq
	d.regMu.Unlock()

	req := sip.NewRequest(sip.REGISTER, d.registrarURI)
	from := &sip.FromHeader{DisplayName: d.cfg.DisplayName, Address: d.aor}
	from.Params.Add("tag", d.regFromTag)
	callID := sip.CallIDHeader(d.regCallID)
	req.AppendHeader(from)
	req.AppendHeader(&sip.ToHeader{Address: d.aor})
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: cseq, MethodName: sip.REGISTER})
	req.AppendHeader(sip.HeaderClone(&d.contact))
	req.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	req.SetDestination(d.proxyAddr)

	res, err := d.do(ctx, req)
	if err == nil && (res.StatusCode == 401 || res.StatusCode == 407) {
		res, err = d.doAuth(ctx, req, res)
		d.regMu.Lock()
		d.regCSeq = req.CSeq().SeqNo
		d.regMu.Unlock()
	}
	if err == nil && res.StatusCode != 200 {
		err = fmt.Errorf("REGISTER: %d %s", res.StatusCode, res.Reason)
	}

	d.regMu.Lock()
	defer d.regMu.Unlock()
	if expires == 0 {
		d.registered = false
		return err
	}
	if err != nil {
		d.registered = false
		// #nosec G404 -- retry jitter, not security sensitive
		d.scheduleRefresh(time.Duration(5+rand.IntN(25)) * time.Second)
		return err
	}
	d.registered = true
	granted := grantedExpires(res, &d.contact, expires)
	// Refresh at 80-95% of the lifetime so subscribers created together do
	// not refresh together.
	// #nosec G404 -- refresh jitter, not security sensitive
	d.scheduleRefresh(time.Duration(float64(granted) * (0.80 + 0.15*rand.Float64()) * float64(time.Second)))
	return nil
}

// scheduleRefresh must be called with regMu held.
func (d *Device) scheduleRefresh(after time.Duration) {
	if after < time.Second {
		after = time.Second
	}
	if d.regTimer != nil {
		d.regTimer.Stop()
	}
	ctx := d.ctx
	d.regTimer = time.AfterFunc(after, func() {
		if ctx.Err() != nil {
			return
		}
		if err := d.Register(); err != nil {
			d.eng.log.Warn("re-register failed", "device", d.cfg.ID, "error", err)
		}
	})
}

func grantedExpires(res *sip.Response, ours *sip.ContactHeader, requested int) int {
	for _, h := range res.GetHeaders("Contact") {
		c, ok := h.(*sip.ContactHeader)
		if !ok || c.Address.Host != ours.Address.Host || c.Address.Port != ours.Address.Port {
			continue
		}
		if v, ok := c.Params.Get("expires"); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return n
			}
		}
	}
	if h := res.GetHeader("Expires"); h != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(h.Value())); err == nil && n > 0 {
			return n
		}
	}
	return requested
}

// do runs a non-INVITE client transaction and reports it.
func (d *Device) do(ctx context.Context, req *sip.Request) (*sip.Response, error) {
	start := time.Now()
	res, err := d.client.Do(ctx, req)
	d.report(req.Method.String(), res, err, time.Since(start))
	return res, err
}

func (d *Device) doAuth(ctx context.Context, req *sip.Request, chal *sip.Response) (*sip.Response, error) {
	start := time.Now()
	res, err := d.client.DoDigestAuth(ctx, req, chal, sipgo.DigestAuth{Username: d.authUser, Password: d.cfg.Password})
	d.report(req.Method.String(), res, err, time.Since(start))
	return res, err
}

func (d *Device) report(method string, res *sip.Response, err error, dur time.Duration) {
	ev := RequestEvent{Device: d.cfg.ID, Method: method, Duration: dur, Err: err}
	if res != nil {
		ev.Status = res.StatusCode
	}
	d.observer().Request(ev)
}

func (d *Device) addCall(c *Call) {
	d.callsMu.Lock()
	d.calls[c.callID] = c
	d.callsMu.Unlock()
}

func (d *Device) removeCall(c *Call) {
	d.callsMu.Lock()
	if d.calls[c.callID] == c {
		delete(d.calls, c.callID)
	}
	d.callsMu.Unlock()
}

func (d *Device) findCall(req *sip.Request) *Call {
	id := req.CallID()
	if id == nil {
		return nil
	}
	d.callsMu.Lock()
	defer d.callsMu.Unlock()
	return d.calls[id.Value()]
}

func (d *Device) onAck(req *sip.Request, tx sip.ServerTransaction) {
	if c := d.findCall(req); c != nil {
		c.trace.msg(false, req)
		if c.uas != nil {
			d.logErr("ACK", c.uas.ReadAck(req, tx))
		}
		if c.ackSDP && len(req.Body()) > 0 {
			c.ackAnswer(req.Body())
		}
	}
}

func (d *Device) onBye(req *sip.Request, tx sip.ServerTransaction) {
	c := d.findCall(req)
	if c == nil {
		d.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)))
		return
	}
	c.trace.msg(false, req)
	var err error
	if c.uac != nil {
		err = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	} else if c.uas != nil {
		err = c.uas.ReadBye(req, tx)
	}
	if err != nil {
		d.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)))
		return
	}
	c.trace.note(true, "SIP/2.0 200 OK (BYE)")
	c.finish(EndedByRemote, 0, "BYE")
}

func (d *Device) onInvite(req *sip.Request, tx sip.ServerTransaction) {
	if to := req.To(); to != nil && to.Params.Has("tag") {
		d.onReInvite(req, tx)
		return
	}
	sess, err := d.dialogUA.ReadInvite(req, tx)
	if err != nil {
		d.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 400, "Bad Request", nil)))
		return
	}
	c := newCall(d, Incoming)
	c.callID = req.CallID().Value()
	c.uas = sess
	c.remote = callerNumber(req)
	c.tStart = time.Now()
	c.trace.msg(false, req)
	old, hasReplaces := d.replacedCall(req)
	if hasReplaces && old == nil {
		d.logErr("respond", sess.Respond(481, "Call/Transaction Does Not Exist", nil))
		c.finish(EndedByLocal, 481, "Replaces: no such dialog")
		return
	}
	c.replaces = old
	c.answerHdr = c.sessionForAnswer(req)
	if mo := d.mediaOptions(nil); !mo.Disabled {
		if err := c.setupIncomingMedia(req.Body(), mo); err != nil {
			d.logErr("respond", sess.Respond(488, "Not Acceptable Here", nil))
			c.trace.note(true, "SIP/2.0 488 Not Acceptable Here (%v)", err)
			c.finish(EndedByLocal, 488, err.Error())
			return
		}
	}
	d.addCall(c)

	if old != nil {
		// Transfer target: the new call takes over an existing one
		// (attended transfer). Answer at once and hang up the old call.
		c.trace.note(false, "replaces %s", old.callID)
		c.Accept()
		go func() {
			if c.ExpectConnected(inDialogTimeout) {
				old.byeWith(EndedByRemote, "replaced")
			}
		}()
	} else if err := c.ring(sess, req); err != nil {
		c.finish(EndedByError, 0, err.Error())
		return
	}
	c.markRinging(time.Now())
	d.deliver(c)

	// The server transaction lives as long as this handler; wait for the
	// script to decide.
	timeout := time.NewTimer(d.eng.opts.RingTimeout)
	defer timeout.Stop()
	select {
	case dec := <-c.decision:
		if dec.code == 200 {
			c.answer(sess)
			return
		}
		d.logErr("respond", sess.Respond(dec.code, dec.reason, nil))
		c.trace.note(true, "SIP/2.0 %d %s", dec.code, dec.reason)
		c.finish(EndedByLocal, dec.code, dec.reason)
	case <-sess.Context().Done():
		c.trace.note(false, "CANCEL")
		c.finish(EndedByRemote, 487, "CANCEL")
	case <-timeout.C:
		d.logErr("respond", sess.Respond(480, "Temporarily Unavailable", nil))
		c.finish(EndedByTimeout, 480, "ring timeout")
	case <-d.ctx.Done():
		d.logErr("respond", sess.Respond(480, "Temporarily Unavailable", nil))
		c.finish(EndedByLocal, 480, "device destroyed")
	}
}

// onReInvite answers in-dialog INVITEs (hold, session refresh) and applies
// the new offer to the RTP stream.
func (d *Device) onReInvite(req *sip.Request, tx sip.ServerTransaction) {
	c := d.findCall(req)
	if c == nil {
		d.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)))
		return
	}
	c.trace.msg(false, req)
	// Our own re-INVITE is in progress: glare (RFC 3261 14.2).
	if !c.reinviteMu.TryLock() {
		d.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 491, "Request Pending", nil)))
		c.trace.note(true, "SIP/2.0 491 Request Pending")
		return
	}
	defer c.reinviteMu.Unlock()
	res := sip.NewSDPResponseFromRequest(req, c.reofferAnswer(req.Body()))
	res.AppendHeader(sip.HeaderClone(&d.contact))
	c.addSessionHeaders(req, res)
	d.logErr("respond", tx.Respond(res))
	c.trace.msg(true, res)
	c.sessionRefreshed()
}

// deliver puts an incoming call into the inbox and wakes ExpectCall waiters.
func (d *Device) deliver(c *Call) {
	d.inMu.Lock()
	d.inbox = append(d.inbox, c)
	close(d.inSignal)
	d.inSignal = make(chan struct{})
	d.inMu.Unlock()
}

// CallMatch selects which incoming call ExpectCall claims.
type CallMatch struct {
	// Caller is the expected caller number (From/P-Asserted-Identity user
	// part). Empty matches any caller.
	Caller string
	// CallerDevice, if set, is used to measure routing time.
	CallerDevice *Device
}

// ExpectCall waits for an incoming call that matches m and claims it.
// Calls that don't match stay in the inbox for later ExpectCall calls. It
// returns nil on timeout.
func (d *Device) ExpectCall(m CallMatch, timeout time.Duration) (*Call, error) {
	if err := d.Start(); err != nil {
		return nil, err
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		d.inMu.Lock()
		var found *Call
		kept := d.inbox[:0]
		for _, c := range d.inbox {
			switch {
			case c.State() == StateEnded:
			case found == nil && numbersMatch(m.Caller, c.remote):
				found = c
			default:
				kept = append(kept, c)
			}
		}
		clear(d.inbox[len(kept):])
		d.inbox = kept
		sig := d.inSignal
		d.inMu.Unlock()

		if found != nil {
			ev := IncomingCallEvent{Device: d.cfg.ID, Caller: found.remote}
			if m.CallerDevice != nil {
				if out := m.CallerDevice.lastOut.Load(); out != nil && !out.tStart.After(found.tStart) {
					if delay := found.tStart.Sub(out.tStart); delay < d.eng.opts.RingTimeout {
						ev.Delivery, ev.HasDelivery = delay, true
					}
				}
			}
			d.observer().IncomingCall(ev)
			return found, nil
		}
		select {
		case <-sig:
		case <-deadline.C:
			return nil, nil
		case <-d.ctx.Done():
			return nil, errors.New("device destroyed")
		}
	}
}

// CallOptions describes an outgoing call.
type CallOptions struct {
	// Target is a number/user (sent to the proxy's domain) or a full SIP URI.
	Target  string
	Headers map[string]string
	// Timeout is the no-answer timeout after which the call is cancelled.
	Timeout time.Duration
	Label   string
	// Media overrides the device's media options for this call.
	Media *MediaOptions
}

// Call sends INVITE and returns immediately; the call progresses in the
// background. Use ExpectConnected to wait for the answer.
func (d *Device) Call(opts CallOptions) (*Call, error) {
	if err := d.Start(); err != nil {
		return nil, err
	}
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}
	target, err := d.targetURI(opts.Target)
	if err != nil {
		return nil, err
	}

	req := sip.NewRequest(sip.INVITE, target)
	from := &sip.FromHeader{DisplayName: d.cfg.DisplayName, Address: d.aor}
	from.Params.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(from)
	req.AppendHeader(&sip.ToHeader{Address: target})
	req.AppendHeader(sip.HeaderClone(&d.contact))
	req.AppendHeader(sip.NewHeader("Supported", "100rel, timer"))
	if se := d.cfg.SessionExpires; se > 0 {
		req.AppendHeader(sip.NewHeader("Session-Expires", strconv.Itoa(int(se/time.Second))))
		req.AppendHeader(sip.NewHeader("Min-SE", "90"))
	}
	for k, v := range opts.Headers {
		req.AppendHeader(sip.NewHeader(k, v))
	}
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	req.SetDestination(d.destinationFor(target))

	c := newCall(d, Outgoing)
	if mo := d.mediaOptions(opts.Media); mo.Disabled {
		req.SetBody(d.placeholderSDP())
	} else {
		st, err := d.newStream(mo)
		if err != nil {
			return nil, err
		}
		c.media = st
		req.SetBody(st.Offer(media.SendRecv))
	}
	c.label = opts.Label
	c.remote = opts.Target
	c.uac = newUAC(d, req)
	c.tStart = time.Now()
	// The transaction lives as long as the device; hangup and the no-answer
	// timeout go through CANCEL, not context cancellation.
	if err := c.uac.send(d.ctx); err != nil {
		d.report("INVITE", nil, err, time.Since(c.tStart))
		c.finish(EndedByError, 0, err.Error())
		return c, nil
	}
	c.callID = req.CallID().Value()
	c.trace.msg(true, req)
	d.addCall(c)
	d.lastOut.Store(c)
	go c.runOutgoing(opts.Timeout)
	return c, nil
}

func (d *Device) targetURI(target string) (sip.Uri, error) {
	var u sip.Uri
	if target == "" {
		return u, errors.New("call target is empty")
	}
	if strings.HasPrefix(target, "sip:") || strings.HasPrefix(target, "sips:") {
		err := sip.ParseUri(target, &u)
		return u, err
	}
	user, host, _ := strings.Cut(target, "@")
	if host == "" {
		host = d.aor.Host
	}
	return sip.Uri{Scheme: "sip", User: user, Host: host}, nil
}

func localIPFor(hostPort string) (string, error) {
	conn, err := net.Dial("udp", hostPort)
	if err != nil {
		return "", fmt.Errorf("detect local IP for %s: %w", hostPort, err)
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}

// callerNumber extracts the caller as the system under test presents it:
// P-Asserted-Identity if present, else From.
func callerNumber(req *sip.Request) string {
	if h := req.GetHeader("P-Asserted-Identity"); h != nil {
		var u sip.Uri
		var p sip.HeaderParams
		if _, err := sip.ParseAddressValue(h.Value(), &u, &p); err == nil && u.User != "" {
			return u.User
		}
	}
	if f := req.From(); f != nil {
		return f.Address.User
	}
	return ""
}

// numbersMatch compares phone numbers ignoring a leading '+' and visual
// separators. Empty expected matches anything.
func numbersMatch(expected, got string) bool {
	if expected == "" {
		return true
	}
	return normalizeNumber(expected) == normalizeNumber(got)
}

func normalizeNumber(s string) string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "+")
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '(', ')', '.':
			return -1
		}
		return r
	}, s)
}

// logErr logs failures of best-effort sends (responses, ACKs) that have no
// caller to report to.
func (d *Device) logErr(what string, err error) {
	if err != nil {
		d.eng.log.Debug("sip: "+what+" failed", "device", d.cfg.ID, "error", err)
	}
}

// destinationFor picks where an out-of-dialog request goes: the configured
// proxy, else the registrar for targets in our domain, else the target's
// own host (e.g. a Refer-To pointing at another server).
func (d *Device) destinationFor(target sip.Uri) string {
	if d.cfg.Proxy != "" || target.Host == d.aor.Host || target.Host == d.registrarURI.Host {
		return d.proxyAddr
	}
	port := target.Port
	if port == 0 {
		port = 5060
	}
	return net.JoinHostPort(target.Host, strconv.Itoa(port))
}

// ContactURI is where this device receives requests, e.g. for calling it
// directly without a proxy. Empty before Start.
func (d *Device) ContactURI() string {
	if d.ip == "" {
		return ""
	}
	return d.contact.Address.String()
}
