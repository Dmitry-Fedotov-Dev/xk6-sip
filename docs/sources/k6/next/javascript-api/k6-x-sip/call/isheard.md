---
title: 'Call.isHeard( [timeout] )'
description: 'Call.isHeard waits until audio from the other side arrives.'
weight: 13
---

# Call.isHeard( [timeout] )

Waits until this leg receives audio from the other side. Audio counts as heard when the level of received frames stays above `heardLevel` (−45 dBFS by default) for about 100 ms. Packets with silence or comfort noise don't count, so `isHeard()` catches one-way audio even when RTP packets flow.

Under load, the `rtp_audio_heard` metric reports the share of call legs that heard audio. A value below 1 means one-way or no audio.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| timeout (optional) | duration | `expectTimeout`, `'30s'` | How long to wait: `'5s'`, `'500ms'` or a number of seconds. |

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if audio was heard, `false` on timeout or if the call has no media. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
inc.accept();
check(inc.isHeard('3s'), { 'B hears A': (ok) => ok });
check(out.isHeard('3s'), { 'A hears B': (ok) => ok });
```

{{< /code >}}
