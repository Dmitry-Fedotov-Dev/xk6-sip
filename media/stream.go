// Package media is the RTP side of a call: G.711 audio sent from a shared
// scheduler, RFC 3550 receive statistics, RFC 4733 DTMF and detection of
// whether any audio arrives at all.
package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"time"
)

type Config struct {
	IP        string  // local address for the socket and the SDP
	Codecs    []Codec // preference order; default PCMU, PCMA
	Audio     *Audio  // what we send; default DefaultAudio()
	Scheduler *Scheduler
	// HeardLevel is the level in dBFS above which a received frame counts
	// as audio; default -45.
	HeardLevel float64
	// NoLatch disables symmetric RTP: by default we send to where the
	// peer's packets come from once the first one arrives (NAT, SBCs).
	NoLatch bool
}

// Stats summarise one stream. Loss and jitter follow RFC 3550.
type Stats struct {
	Codec           string
	PacketsSent     uint64
	PacketsReceived uint64
	PacketsExpected uint64
	PacketsLost     uint64
	Jitter          time.Duration
	Heard           time.Duration // time of received audio above HeardLevel
	DTMF            string        // RFC 4733 digits received
}

// heardFrames of audio make WaitHeard true.
const heardFrames = 5

type Stream struct {
	cfg    Config
	conn   *net.UDPConn
	port   int
	sessID uint64
	shard  *shard

	mu         sync.Mutex
	closed     bool
	version    uint64
	remote     *net.UDPAddr
	latched    bool
	codec      Codec
	hasCodec   bool
	remoteDTMF int
	dir        Direction // our direction
	started    bool

	// sender
	ssrc     uint32
	seq      uint16
	ts       uint32
	src      []byte
	pos      int
	marker   bool
	dtmfQ    []*dtmfJob
	sent     uint64
	buf      [12 + frameSamples]byte
	heardThr int64 // mean square threshold

	// receiver
	rx       rxStats
	rxStart  time.Time
	heard    int
	heardCh  chan struct{}
	digits   []byte
	lastEvTS uint32
	haveEv   bool
	digitSig chan struct{}
}

type rxStats struct {
	init        bool
	ssrc        uint32
	baseSeq     uint16
	maxSeq      uint16
	cycles      uint64
	received    uint64
	prevExp     uint64 // expected/received of earlier SSRCs
	prevRecv    uint64
	lastTransit uint32
	haveTransit bool
	jitter      float64 // in timestamp units
}

// New opens the RTP socket and starts receiving right away, so early media
// and the first packets after 200 OK are counted.
func New(cfg Config) (*Stream, error) {
	if len(cfg.Codecs) == 0 {
		cfg.Codecs = []Codec{PCMU, PCMA}
	}
	if cfg.Audio == nil {
		cfg.Audio = DefaultAudio()
	}
	if cfg.Scheduler == nil {
		cfg.Scheduler = defaultScheduler()
	}
	if cfg.HeardLevel == 0 {
		cfg.HeardLevel = -45
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP(cfg.IP)})
	if err != nil {
		return nil, fmt.Errorf("RTP socket: %w", err)
	}
	amp := math.Pow(10, cfg.HeardLevel/20) * math.MaxInt16
	s := &Stream{
		cfg:        cfg,
		conn:       conn,
		port:       conn.LocalAddr().(*net.UDPAddr).Port,
		sessID:     rand.Uint64() >> 1, // #nosec G404 -- SDP session id, not security sensitive
		remoteDTMF: -1,
		dir:        SendRecv,
		ssrc:       rand.Uint32(),         // #nosec G404 -- RTP SSRC, not security sensitive
		seq:        uint16(rand.Uint32()), // #nosec G404 G115 -- random initial sequence number
		ts:         rand.Uint32(),         // #nosec G404 -- random initial timestamp
		marker:     true,
		heardThr:   int64(amp * amp),
		heardCh:    make(chan struct{}),
		digitSig:   make(chan struct{}),
		rxStart:    time.Now(),
	}
	go s.readLoop()
	return s, nil
}

func (s *Stream) Port() int { return s.port }

// Offer is our SDP offer with all our codecs.
func (s *Stream) Offer(dir Direction) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dir = dir
	s.version++
	return buildSDP(s.cfg.IP, s.port, s.cfg.Codecs, defaultDTMFPT, dir, s.sessID, s.version)
}

// Answer negotiates against the peer's offer and returns our answer.
func (s *Stream) Answer(r Remote) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := choose(s.cfg.Codecs, r.Codecs, true)
	if !ok {
		return nil, errors.New("no common codec")
	}
	s.setRemoteLocked(r, c)
	s.dir = r.Dir.Reverse()
	return s.sdpLocked(), nil
}

// ApplyAnswer takes the peer's answer to our offer.
func (s *Stream) ApplyAnswer(r Remote) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := choose(s.cfg.Codecs, r.Codecs, false)
	if !ok {
		return errors.New("no common codec in answer")
	}
	s.setRemoteLocked(r, c)
	s.dir = r.Dir.Reverse()
	return nil
}

// Update applies an in-dialog offer (re-INVITE, e.g. hold) and returns our
// answer, keeping the negotiated codec.
func (s *Stream) Update(r Remote) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Addr != nil && !r.Addr.IP.IsUnspecified() {
		s.remote, s.latched = r.Addr, false
	}
	s.dir = r.Dir.Reverse()
	return s.sdpLocked()
}

func (s *Stream) setRemoteLocked(r Remote, c Codec) {
	if r.Addr != nil && !r.Addr.IP.IsUnspecified() {
		s.remote, s.latched = r.Addr, false
	}
	if !s.hasCodec || s.codec.Name != c.Name {
		s.src, s.pos = s.cfg.Audio.encoded(c), 0
	}
	s.codec, s.hasCodec = c, true
	s.remoteDTMF = r.DTMFPT
}

func (s *Stream) sdpLocked() []byte {
	s.version++
	dtmf := -1
	if s.remoteDTMF >= 0 {
		dtmf = s.remoteDTMF
	}
	return buildSDP(s.cfg.IP, s.port, []Codec{s.codec}, dtmf, s.dir, s.sessID, s.version)
}

// Start begins sending (when our direction allows it).
func (s *Stream) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.closed {
		return
	}
	s.started = true
	s.shard = s.cfg.Scheduler.add(s)
}

// SetDirection changes what we send/receive, e.g. sendonly for hold.
func (s *Stream) SetDirection(d Direction) {
	s.mu.Lock()
	s.dir = d
	s.mu.Unlock()
}

// Codec is the negotiated codec name, "" before negotiation.
func (s *Stream) Codec() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasCodec {
		return ""
	}
	return s.codec.Name
}

// Close stops sending and receiving and returns final statistics.
func (s *Stream) Close() Stats {
	s.mu.Lock()
	if s.closed {
		st := s.statsLocked()
		s.mu.Unlock()
		return st
	}
	s.closed = true
	sh := s.shard
	st := s.statsLocked()
	s.mu.Unlock()
	if sh != nil {
		sh.remove(s)
	}
	_ = s.conn.Close()
	return st
}

func (s *Stream) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statsLocked()
}

func (s *Stream) statsLocked() Stats {
	st := Stats{
		PacketsSent: s.sent,
		Jitter:      time.Duration(s.rx.jitter / sampleRate * float64(time.Second)),
		Heard:       time.Duration(s.heard) * frameDurationMs * time.Millisecond,
		DTMF:        string(s.digits),
	}
	if s.hasCodec {
		st.Codec = s.codec.Name
	}
	exp, recv := s.rx.prevExp, s.rx.prevRecv
	if s.rx.init {
		exp += s.rx.cycles + uint64(s.rx.maxSeq) - uint64(s.rx.baseSeq) + 1
		recv += s.rx.received
	}
	st.PacketsExpected, st.PacketsReceived = exp, recv
	if exp > recv {
		st.PacketsLost = exp - recv
	}
	return st
}

// WaitHeard waits until at least 100 ms of audio above HeardLevel arrived.
func (s *Stream) WaitHeard(timeout time.Duration) bool {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-s.heardCh:
		return true
	case <-t.C:
		return false
	}
}

// Digits returns the DTMF digits received so far.
func (s *Stream) Digits() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.digits)
}

// WaitDigits waits until the received digits contain want.
func (s *Stream) WaitDigits(want string, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		s.mu.Lock()
		got, sig := string(s.digits), s.digitSig
		s.mu.Unlock()
		if strings.Contains(got, want) {
			return true
		}
		select {
		case <-sig:
		case <-deadline.C:
			return false
		}
	}
}

// ---- sending

type dtmfJob struct {
	event   byte
	packets int // event duration in 20 ms packets
	gap     int // silence after the event, in packets
	sent    int
	ended   int
	ts      uint32
}

var dtmfEvents = map[rune]byte{
	'0': 0, '1': 1, '2': 2, '3': 3, '4': 4, '5': 5, '6': 6, '7': 7, '8': 8, '9': 9,
	'*': 10, '#': 11, 'A': 12, 'B': 13, 'C': 14, 'D': 15,
}

// SendDTMF queues RFC 4733 events; each digit lasts dur (default 100 ms)
// followed by a 60 ms pause. It returns once queued.
func (s *Stream) SendDTMF(digits string, dur time.Duration) error {
	if dur <= 0 {
		dur = 100 * time.Millisecond
	}
	packets := max(int(dur/(frameDurationMs*time.Millisecond)), 1)
	jobs := make([]*dtmfJob, 0, len(digits))
	for _, r := range strings.ToUpper(digits) {
		ev, ok := dtmfEvents[r]
		if !ok {
			return fmt.Errorf("invalid DTMF digit %q", r)
		}
		jobs = append(jobs, &dtmfJob{event: ev, packets: packets, gap: 3})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.remoteDTMF < 0 {
		return errors.New("peer did not negotiate telephone-event")
	}
	s.dtmfQ = append(s.dtmfQ, jobs...)
	return nil
}

func (s *Stream) tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := s.ts
	s.ts += frameSamples // the timeline advances even when we are silent
	if s.closed || s.remote == nil || !s.hasCodec || !s.dir.sends() {
		s.marker = true
		return
	}

	b := s.buf[:]
	var n int
	if job := s.nextDTMF(); job != nil {
		if job.sent == 0 {
			job.ts = ts
		}
		end := job.sent >= job.packets
		dur := uint16(min(job.sent+1, job.packets) * frameSamples) // #nosec G115 -- bounded by packets*160
		b[12] = job.event
		b[13] = 10 // volume -10 dBm0
		if end {
			b[13] |= 0x80
			job.ended++
		} else {
			job.sent++
		}
		binary.BigEndian.PutUint16(b[14:], dur)
		s.header(uint8(s.remoteDTMF), job.sent == 1 && !end, job.ts) // #nosec G115 -- PT is 0..127
		n = 16
	} else {
		copy(b[12:], s.src[s.pos:s.pos+frameSamples])
		s.pos = (s.pos + frameSamples) % len(s.src)
		s.header(s.codec.PT, s.marker, ts)
		s.marker = false
		n = 12 + frameSamples
	}
	if _, err := s.conn.WriteToUDP(b[:n], s.remote); err == nil {
		s.sent++
	}
}

// nextDTMF returns the job to send in this tick, or nil for audio. Pauses
// between digits are sent as audio.
func (s *Stream) nextDTMF() *dtmfJob {
	for len(s.dtmfQ) > 0 {
		j := s.dtmfQ[0]
		switch {
		case j.sent < j.packets || j.ended < 3: // RFC 4733: end packet sent 3 times
			return j
		case j.gap > 0:
			j.gap--
			return nil
		default:
			s.dtmfQ = s.dtmfQ[1:]
		}
	}
	return nil
}

func (s *Stream) header(pt uint8, marker bool, ts uint32) {
	b := s.buf[:]
	b[0] = 0x80 // version 2
	b[1] = pt & 0x7F
	if marker {
		b[1] |= 0x80
	}
	binary.BigEndian.PutUint16(b[2:], s.seq)
	binary.BigEndian.PutUint32(b[4:], ts)
	binary.BigEndian.PutUint32(b[8:], s.ssrc)
	s.seq++
}

// ---- receiving

func (s *Stream) readLoop() {
	buf := make([]byte, 1500)
	for {
		n, src, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return // closed
		}
		s.receive(buf[:n], src)
	}
}

func (s *Stream) receive(b []byte, src *net.UDPAddr) {
	if len(b) < 12 || b[0]>>6 != 2 {
		return // not RTP (e.g. RTCP or STUN on the same port)
	}
	pt := b[1] & 0x7F
	if pt >= 72 && pt <= 76 {
		return // RTCP
	}
	seq := binary.BigEndian.Uint16(b[2:])
	ts := binary.BigEndian.Uint32(b[4:])
	ssrc := binary.BigEndian.Uint32(b[8:])
	hl := 12 + 4*int(b[0]&0x0F)
	if b[0]&0x10 != 0 { // header extension
		if len(b) < hl+4 {
			return
		}
		hl += 4 + 4*int(binary.BigEndian.Uint16(b[hl+2:]))
	}
	end := len(b)
	if b[0]&0x20 != 0 && end > 0 { // padding
		end -= int(b[end-1])
	}
	if hl > end {
		return
	}
	payload := b[hl:end]
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if !s.cfg.NoLatch && !s.latched {
		s.remote, s.latched = src, true
	}
	s.countSeq(ssrc, seq)

	switch {
	case s.remoteDTMF >= 0 && int(pt) == s.remoteDTMF, pt == defaultDTMFPT && s.remoteDTMF < 0:
		s.receiveDTMF(payload, ts)
	case s.hasCodec && pt == s.codec.PT:
		s.jitter(now, ts)
		if s.loud(payload, s.codec.decode) {
			s.heard++
			if s.heard == heardFrames {
				close(s.heardCh)
			}
		}
	default:
		if c, ok := codecByPT(pt); ok { // before negotiation or a codec switch
			s.jitter(now, ts)
			if s.loud(payload, c.decode) {
				s.heard++
				if s.heard == heardFrames {
					close(s.heardCh)
				}
			}
		}
	}
}

// countSeq tracks sequence numbers per RFC 3550 A.1 (simplified: no
// probation), restarting on SSRC change.
func (s *Stream) countSeq(ssrc uint32, seq uint16) {
	r := &s.rx
	if r.init && ssrc != r.ssrc {
		r.prevExp += r.cycles + uint64(r.maxSeq) - uint64(r.baseSeq) + 1
		r.prevRecv += r.received
		r.init, r.haveTransit = false, false
	}
	if !r.init {
		*r = rxStats{init: true, ssrc: ssrc, baseSeq: seq, maxSeq: seq, prevExp: r.prevExp, prevRecv: r.prevRecv, jitter: r.jitter}
		r.received = 1
		return
	}
	r.received++
	if delta := seq - r.maxSeq; delta != 0 && delta < 0x8000 {
		if seq < r.maxSeq {
			r.cycles += 1 << 16
		}
		r.maxSeq = seq
	}
}

func (s *Stream) jitter(now time.Time, ts uint32) {
	arrival := uint32(now.Sub(s.rxStart).Seconds() * sampleRate)
	transit := arrival - ts
	r := &s.rx
	if r.haveTransit {
		d := float64(int32(transit - r.lastTransit)) // #nosec G115 -- wrap-around difference is intended
		r.jitter += (math.Abs(d) - r.jitter) / 16
	}
	r.lastTransit, r.haveTransit = transit, true
}

func (s *Stream) loud(payload []byte, table *[256]int16) bool {
	if len(payload) == 0 {
		return false
	}
	var sum int64
	for _, x := range payload {
		v := int64(table[x])
		sum += v * v
	}
	return sum/int64(len(payload)) > s.heardThr
}

func (s *Stream) receiveDTMF(p []byte, ts uint32) {
	if len(p) < 4 || p[0] > 15 {
		return
	}
	if s.haveEv && ts == s.lastEvTS {
		return // same event: continuation or retransmitted end
	}
	s.haveEv, s.lastEvTS = true, ts
	s.digits = append(s.digits, "0123456789*#ABCD"[p[0]])
	close(s.digitSig)
	s.digitSig = make(chan struct{})
}
