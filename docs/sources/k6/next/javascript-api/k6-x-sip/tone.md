---
title: 'tone( [freq], [dbfs] )'
description: 'sip.tone creates a sine tone audio source.'
weight: 04
---

# tone( [freq], [dbfs] )

Creates a sine tone audio source. The default audio of every call is `sip.tone(1000, -20)`.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| freq (optional) | number | `1000` | Frequency in Hz. |
| dbfs (optional) | number | `-20` | Level in dBFS; `0` is full scale. |

### Returns

| Type | Description |
| --- | --- |
| object | Audio source to pass as `audio` to [options()](options.md), a [Device](device/_index.md) or [call()](device/call.md). |

### Example

<!-- md-k6:skip -->

```javascript
// A quiet tone: check that the PBX passes low-level audio
const out = A.call({ callee: B, audio: sip.tone(440, -40), heardLevel: -50 });
```
