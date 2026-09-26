package engine

import (
	"bytes"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// NetStats are traffic counters of all SIP sockets of the process.
type NetStats struct {
	PacketsIn, PacketsOut uint64
	BytesIn, BytesOut     uint64
}

var sipNet struct{ pin, pout, bin, bout atomic.Uint64 }

// SIPNetStats returns the SIP traffic of the process so far.
func SIPNetStats() NetStats {
	return NetStats{
		PacketsIn: sipNet.pin.Load(), PacketsOut: sipNet.pout.Load(),
		BytesIn: sipNet.bin.Load(), BytesOut: sipNet.bout.Load(),
	}
}

// retransmitMemory is how long a sent message is remembered: longer than
// the lifetime of a client transaction (64*T1 = 32s).
const retransmitMemory = 40 * time.Second

// countingConn is a device's SIP socket. It counts traffic and reports
// messages that sipgo's transaction layer sends again (RFC 3261 timers A,
// E and G): the same request or response, identified by Via branch, CSeq
// and status, written a second time.
type countingConn struct {
	net.PacketConn
	dev *Device

	mu    sync.Mutex
	seen  map[string]time.Time
	swept time.Time
}

func newCountingConn(pc net.PacketConn, d *Device) *countingConn {
	return &countingConn{PacketConn: pc, dev: d, seen: map[string]time.Time{}}
}

func (c *countingConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(b)
	if n > 0 {
		sipNet.pin.Add(1)
		sipNet.bin.Add(uint64(n)) // #nosec G115 -- n is a positive datagram size
	}
	return n, addr, err
}

func (c *countingConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(b, addr)
	if err == nil {
		sipNet.pout.Add(1)
		sipNet.bout.Add(uint64(n)) // #nosec G115 -- n is a positive datagram size
		c.checkRetransmission(b)
	}
	return n, err
}

func (c *countingConn) checkRetransmission(b []byte) {
	key, method, status, ok := messageKey(b)
	if !ok {
		return
	}
	now := time.Now()
	c.mu.Lock()
	_, again := c.seen[key]
	c.seen[key] = now
	if now.Sub(c.swept) > retransmitMemory {
		for k, t := range c.seen {
			if now.Sub(t) > retransmitMemory {
				delete(c.seen, k)
			}
		}
		c.swept = now
	}
	c.mu.Unlock()
	if again {
		c.dev.observer().Retransmission(RetransmissionEvent{Device: c.dev.cfg.ID, Method: method, Status: status})
	}
}

// messageKey identifies a SIP message for retransmission detection without
// a full parse: start line, top Via branch and CSeq.
func messageKey(b []byte) (key, method string, status int, ok bool) {
	end := bytes.Index(b, []byte("\r\n\r\n"))
	if end < 0 {
		end = len(b)
	}
	lines := bytes.Split(b[:end], []byte("\r\n"))
	if len(lines) < 2 {
		return "", "", 0, false
	}
	start := lines[0]
	var branch, cseq []byte
	for _, l := range lines[1:] {
		name, value, found := bytes.Cut(l, []byte(":"))
		if !found {
			continue
		}
		name = bytes.ToLower(bytes.TrimSpace(name))
		switch {
		case branch == nil && (bytes.Equal(name, []byte("via")) || bytes.Equal(name, []byte("v"))):
			if i := bytes.Index(value, []byte("branch=")); i >= 0 {
				branch = value[i+7:]
				if j := bytes.IndexAny(branch, ";, "); j >= 0 {
					branch = branch[:j]
				}
			}
		case cseq == nil && bytes.Equal(name, []byte("cseq")):
			cseq = bytes.TrimSpace(value)
		}
	}
	if branch == nil || cseq == nil {
		return "", "", 0, false
	}
	_, cseqMethod, _ := bytes.Cut(cseq, []byte(" "))
	method = string(bytes.TrimSpace(cseqMethod))
	if bytes.HasPrefix(start, []byte("SIP/2.0 ")) {
		if len(start) < 11 {
			return "", "", 0, false
		}
		for _, ch := range start[8:11] {
			status = status*10 + int(ch-'0')
		}
		if status < 200 {
			// Provisional responses are not retransmitted by the
			// transaction layer; a repeated 180 is a new message.
			return "", "", 0, false
		}
	}
	return string(start[:min(len(start), 16)]) + "|" + string(branch) + "|" + string(cseq), method, status, true
}
