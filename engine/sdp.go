package engine

import (
	"fmt"
	"time"
)

// sdpAudio builds the audio SDP used for both offer and answer.
// TODO(media): real RTP socket and codec negotiation; the port is a
// placeholder until the media layer exists, so nothing listens on it.
func sdpAudio(ip string, port int) []byte {
	id := time.Now().UnixNano() / 1000
	return fmt.Appendf(nil, "v=0\r\n"+
		"o=- %d %d IN IP4 %s\r\n"+
		"s=-\r\n"+
		"c=IN IP4 %s\r\n"+
		"t=0 0\r\n"+
		"m=audio %d RTP/AVP 0 8 101\r\n"+
		"a=rtpmap:0 PCMU/8000\r\n"+
		"a=rtpmap:8 PCMA/8000\r\n"+
		"a=rtpmap:101 telephone-event/8000\r\n"+
		"a=fmtp:101 0-16\r\n"+
		"a=ptime:20\r\n"+
		"a=sendrecv\r\n", id, id, ip, ip, port)
}
