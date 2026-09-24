package media

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/pion/sdp/v3"
)

// Direction is the SDP media direction attribute.
type Direction string

const (
	SendRecv Direction = "sendrecv"
	SendOnly Direction = "sendonly"
	RecvOnly Direction = "recvonly"
	Inactive Direction = "inactive"
)

// Reverse is the direction the answerer uses for an offered direction.
func (d Direction) Reverse() Direction {
	switch d {
	case SendOnly:
		return RecvOnly
	case RecvOnly:
		return SendOnly
	}
	return d
}

func (d Direction) sends() bool    { return d == SendRecv || d == SendOnly }
func (d Direction) receives() bool { return d == SendRecv || d == RecvOnly }

// Remote is what we learnt from the peer's SDP.
type Remote struct {
	Addr   *net.UDPAddr // nil or unspecified IP means hold
	Codecs []Codec      // supported codecs in the peer's order
	DTMFPT int          // telephone-event payload type, -1 if not offered
	Dir    Direction
}

// ParseSDP extracts the first audio stream of an SDP body.
func ParseSDP(body []byte) (Remote, error) {
	r := Remote{DTMFPT: -1, Dir: SendRecv}
	var sd sdp.SessionDescription
	if err := sd.UnmarshalString(string(body)); err != nil {
		return r, fmt.Errorf("parse SDP: %w", err)
	}
	var md *sdp.MediaDescription
	for _, m := range sd.MediaDescriptions {
		if m.MediaName.Media == "audio" {
			md = m
			break
		}
	}
	if md == nil {
		return r, errors.New("SDP has no audio stream")
	}
	conn := md.ConnectionInformation
	if conn == nil {
		conn = sd.ConnectionInformation
	}
	port := md.MediaName.Port.Value
	if conn != nil && conn.Address != nil && port != 0 {
		ip := net.ParseIP(conn.Address.Address)
		if ip == nil {
			// Hostnames in c= are legal but rare; resolve once.
			addrs, err := net.LookupIP(conn.Address.Address)
			if err != nil || len(addrs) == 0 {
				return r, fmt.Errorf("SDP connection address %q: %v", conn.Address.Address, err)
			}
			ip = addrs[0]
		}
		r.Addr = &net.UDPAddr{IP: ip, Port: port}
	}

	names := map[string]string{} // PT -> encoding name
	for _, a := range md.Attributes {
		switch a.Key {
		case "rtpmap":
			pt, rest, _ := strings.Cut(a.Value, " ")
			name, _, _ := strings.Cut(rest, "/")
			names[pt] = name
		case string(SendRecv), string(SendOnly), string(RecvOnly), string(Inactive):
			r.Dir = Direction(a.Key)
		}
	}
	for _, f := range md.MediaName.Formats {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 || n > 127 {
			continue
		}
		pt := uint8(n)
		name := names[f]
		switch {
		case strings.EqualFold(name, telephoneEvent):
			r.DTMFPT = n
		case name != "":
			if c, err := CodecByName(name); err == nil {
				c.PT = pt
				r.Codecs = append(r.Codecs, c)
			}
		default: // static payload type without rtpmap
			if c, ok := codecByPT(pt); ok {
				r.Codecs = append(r.Codecs, c)
			}
		}
	}
	if r.Addr == nil || r.Addr.IP.IsUnspecified() {
		// c=0.0.0.0 is the RFC 2543 way to put a call on hold.
		r.Dir = Inactive
	}
	return r, nil
}

// choose picks the codec to use: the peer's first codec that we support
// when we are the offerer, our first codec that the peer offered when we
// answer. The result carries the peer's payload type.
func choose(ours []Codec, remote []Codec, answering bool) (Codec, bool) {
	if answering {
		for _, o := range ours {
			for _, r := range remote {
				if r.Name == o.Name {
					return r, true
				}
			}
		}
		return Codec{}, false
	}
	for _, r := range remote {
		for _, o := range ours {
			if r.Name == o.Name {
				return r, true
			}
		}
	}
	return Codec{}, false
}

// buildSDP renders our audio description.
func buildSDP(ip string, port int, codecs []Codec, dtmfPT int, dir Direction, sessID, version uint64) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "v=0\r\no=- %d %d IN IP4 %s\r\ns=-\r\nc=IN IP4 %s\r\nt=0 0\r\n", sessID, version, ip, ip)
	fmt.Fprintf(&b, "m=audio %d RTP/AVP", port)
	for _, c := range codecs {
		fmt.Fprintf(&b, " %d", c.PT)
	}
	if dtmfPT >= 0 {
		fmt.Fprintf(&b, " %d", dtmfPT)
	}
	b.WriteString("\r\n")
	for _, c := range codecs {
		fmt.Fprintf(&b, "a=rtpmap:%d %s/%d\r\n", c.PT, c.Name, sampleRate)
	}
	if dtmfPT >= 0 {
		fmt.Fprintf(&b, "a=rtpmap:%d %s/%d\r\na=fmtp:%d 0-16\r\n", dtmfPT, telephoneEvent, sampleRate, dtmfPT)
	}
	fmt.Fprintf(&b, "a=ptime:%d\r\na=%s\r\n", frameDurationMs, dir)
	return []byte(b.String())
}
