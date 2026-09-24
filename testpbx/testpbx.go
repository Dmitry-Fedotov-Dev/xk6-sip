// Package testpbx is a minimal registrar + B2BUA for tests and examples.
// It routes INVITEs by extension, presents the caller's extension as caller
// ID, and optionally challenges REGISTER and INVITE with digest auth. It is
// not meant to be a real PBX.
package testpbx

import (
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
)

type User struct {
	Name     string // SIP user part, e.g. "alice"
	Password string
	Ext      string // extension used for routing and as caller ID
}

type Config struct {
	Addr         string // UDP listen address; default "127.0.0.1:0"
	Domain       string // default: listen host
	Users        []User
	AuthRegister bool
	AuthInvite   bool
	Logger       *slog.Logger
}

type Stats struct {
	Registers, Invites, Answered atomic.Int64
}

type PBX struct {
	cfg      Config
	log      *slog.Logger
	addr     string
	laddr    sip.Addr
	pc       net.PacketConn
	ua       *sipgo.UserAgent
	client   *sipgo.Client
	dialogUA sipgo.DialogUA
	Stats    Stats

	users map[string]*User // by name
	exts  map[string]*User // by extension

	mu      sync.Mutex
	regs    map[string]sip.Uri // user name -> contact
	legs    map[string]*bridge // Call-ID of either leg -> bridge
	orphans map[string]leg     // transferor legs left after a transfer
	nonce   atomic.Int64
}

func Start(cfg Config) (*PBX, error) {
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:0"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	pc, err := net.ListenPacket("udp", cfg.Addr)
	if err != nil {
		return nil, err
	}
	la := pc.LocalAddr().(*net.UDPAddr)
	host := la.IP.String()
	if cfg.Domain == "" {
		cfg.Domain = host
	}
	addr := net.JoinHostPort(host, strconv.Itoa(la.Port))

	ua, err := sipgo.NewUA(sipgo.WithUserAgent("testpbx"), sipgo.WithUserAgentHostname(host),
		sipgo.WithUserAgentTransactionLayerOptions(sip.WithTransactionLayerLogger(cfg.Logger)),
		sipgo.WithUserAgentTransportLayerOptions(sip.WithTransportLayerLogger(cfg.Logger)))
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	srv, err := sipgo.NewServer(ua, sipgo.WithServerLogger(cfg.Logger))
	if err != nil {
		_ = ua.Close()
		_ = pc.Close()
		return nil, err
	}
	cl, err := sipgo.NewClient(ua,
		sipgo.WithClientLogger(cfg.Logger),
		sipgo.WithClientHostname(host),
		sipgo.WithClientPort(la.Port),
		sipgo.WithClientConnectionAddr(addr))
	if err != nil {
		_ = ua.Close()
		_ = pc.Close()
		return nil, err
	}

	p := &PBX{
		cfg:    cfg,
		log:    cfg.Logger,
		addr:   addr,
		laddr:  sip.Addr{IP: la.IP, Port: la.Port},
		pc:     pc,
		ua:     ua,
		client: cl,
		dialogUA: sipgo.DialogUA{
			Client:     cl,
			ContactHDR: sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "pbx", Host: host, Port: la.Port}},
		},
		users:   make(map[string]*User),
		exts:    make(map[string]*User),
		regs:    make(map[string]sip.Uri),
		legs:    make(map[string]*bridge),
		orphans: make(map[string]leg),
	}
	for i := range cfg.Users {
		u := &cfg.Users[i]
		p.users[u.Name] = u
		if u.Ext != "" {
			p.exts[u.Ext] = u
		}
	}

	srv.OnRegister(p.onRegister)
	srv.OnInvite(p.onInvite)
	srv.OnAck(p.onAck)
	srv.OnBye(p.onBye)
	srv.OnRefer(p.onRefer)
	srv.OnNotify(func(req *sip.Request, tx sip.ServerTransaction) {
		p.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)))
	})
	srv.OnUpdate(func(req *sip.Request, tx sip.ServerTransaction) {
		p.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)))
	})
	srv.OnOptions(func(req *sip.Request, tx sip.ServerTransaction) {
		p.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil)))
	})
	go srv.ServeUDP(pc)
	return p, nil
}

// Addr is the UDP host:port the PBX listens on.
func (p *PBX) Addr() string   { return p.addr }
func (p *PBX) Domain() string { return p.cfg.Domain }

func (p *PBX) Close() {
	_ = p.ua.Close()
	_ = p.pc.Close()
}

// Registered reports whether user currently has a binding.
func (p *PBX) Registered(user string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.regs[user]
	return ok
}

func (p *PBX) onRegister(req *sip.Request, tx sip.ServerTransaction) {
	p.Stats.Registers.Add(1)
	name := req.To().Address.User
	u := p.users[name]
	if u == nil {
		p.logErr("respond", tx.Respond(sip.NewResponseFromRequest(req, 404, "Not Found", nil)))
		return
	}
	if p.cfg.AuthRegister && !p.authorized(req, tx, u, "Authorization", 401) {
		return
	}

	expires := 3600
	if h := req.GetHeader("Expires"); h != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(h.Value())); err == nil {
			expires = n
		}
	}
	contact := req.Contact()
	if contact != nil {
		if v, ok := contact.Params.Get("expires"); ok {
			if n, err := strconv.Atoi(v); err == nil {
				expires = n
			}
		}
	}

	p.mu.Lock()
	if expires == 0 || contact == nil {
		delete(p.regs, name)
	} else {
		p.regs[name] = contact.Address
	}
	p.mu.Unlock()

	res := sip.NewResponseFromRequest(req, 200, "OK", nil)
	if contact != nil && expires > 0 {
		c := sip.ContactHeader{Address: contact.Address}
		c.Params.Add("expires", strconv.Itoa(expires))
		res.AppendHeader(&c)
	}
	p.logErr("respond", tx.Respond(res))
}

// authorized checks digest credentials in hdr, challenging with code if they
// are missing or wrong.
func (p *PBX) authorized(req *sip.Request, tx sip.ServerTransaction, u *User, hdr string, code int) bool {
	if h := req.GetHeader(hdr); h != nil {
		cred, err := digest.ParseCredentials(h.Value())
		if err == nil && cred.Username == u.Name {
			want, err := digest.Digest(&digest.Challenge{
				Realm: cred.Realm, Nonce: cred.Nonce, Opaque: cred.Opaque,
				Algorithm: cred.Algorithm, QOP: qopList(cred.QOP),
			}, digest.Options{
				Method: req.Method.String(), URI: cred.URI,
				Username: u.Name, Password: u.Password,
				Cnonce: cred.Cnonce, Count: cred.Nc,
			})
			if err == nil && want.Response == cred.Response {
				return true
			}
		}
	}
	chal := digest.Challenge{Realm: p.cfg.Domain, Nonce: strconv.FormatInt(time.Now().UnixNano()+p.nonce.Add(1), 36), Algorithm: "MD5"}
	name, reason := "WWW-Authenticate", "Unauthorized"
	if code == 407 {
		name, reason = "Proxy-Authenticate", "Proxy Authentication Required"
	}
	res := sip.NewResponseFromRequest(req, code, reason, nil)
	res.AppendHeader(sip.NewHeader(name, chal.String()))
	p.logErr("respond", tx.Respond(res))
	return false
}

func qopList(q string) []string {
	if q == "" {
		return nil
	}
	return []string{q}
}

func (p *PBX) logErr(what string, err error) {
	if err != nil {
		p.log.Debug("testpbx: "+what+" failed", "error", err)
	}
}
