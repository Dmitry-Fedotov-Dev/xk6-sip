---
title: 'Call.mediaStats()'
description: 'Call.mediaStats returns RTP statistics of the call leg.'
weight: 17
---

# Call.mediaStats()

Returns the RTP statistics of this leg at the moment of the call. Loss and jitter are computed as in RFC 3550.

| Field | Type | Description |
| --- | --- | --- |
| codec | string | Negotiated codec. |
| sent | number | RTP packets sent. |
| received | number | RTP packets received. |
| expected | number | Packets expected from the sequence numbers. |
| lost | number | Packets lost: expected minus received. |
| jitter | number | Interarrival jitter in milliseconds. |
| heard | number | Time of received audio above `heardLevel`, in milliseconds. |
| dtmf | string | DTMF digits received. |

When a connected call ends, the same values go to the `rtp_*` metrics.

### Returns

| Type | Description |
| --- | --- |
| object or `null` | The statistics, or `null` if the call has no media. |

### Example

<!-- md-k6:skip -->

```javascript
// Music on hold: A must keep receiving audio while B holds the call
inc.hold();
const before = out.mediaStats().received;
sleep(1);
check(out.mediaStats(), { 'A hears music on hold': (s) => s.received > before });
```
