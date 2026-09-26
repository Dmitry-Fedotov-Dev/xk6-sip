package media

import "sync/atomic"

// NetStats are traffic counters of all RTP sockets of the process.
type NetStats struct {
	PacketsIn, PacketsOut uint64
	BytesIn, BytesOut     uint64
}

var rtpNet struct{ pin, pout, bin, bout atomic.Uint64 }

// RTPNetStats returns the RTP traffic of the process so far, including
// packets that were not RTP (RTCP, STUN) on the media sockets.
func RTPNetStats() NetStats {
	return NetStats{
		PacketsIn: rtpNet.pin.Load(), PacketsOut: rtpNet.pout.Load(),
		BytesIn: rtpNet.bin.Load(), BytesOut: rtpNet.bout.Load(),
	}
}
