---
title: 'Call.saveRecording( path )'
description: 'Call.saveRecording writes the audio of the call leg to a WAV file.'
weight: 26
---

# Call.saveRecording( path )

Writes what this leg heard and sent to a WAV file: 16-bit PCM, 8 kHz, stereo. The **left channel is what the subscriber heard**, the **right channel is what it sent**. Missing directories are created. Works during the call and after it ended.

Recording must be on for the call: set `record: true` or `record: 'onFailure'` in [options()](../options.md), in the [Device](../device/_index.md) or in [`call()`](../device/call.md).

Audio is placed by RTP timestamps, not in arrival order: a lost packet becomes 20 ms of silence where it belongs, and jitter doesn't shift the audio. So drop-outs sound in the file exactly where they happened.

| Parameter | Type | Description |
| --- | --- | --- |
| path | string | File to write, relative to the directory k6 runs in. |

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the file was written; `false` with a warning in the log if the call is not recorded, has no media or the file can't be written. |

### Recording modes

| `record` | What is kept | What is saved automatically |
| --- | --- | --- |
| `false` (default) | nothing | nothing |
| `true` | every call | every call, if `recordDir` is set |
| `'onFailure'` | every call | calls with a failed expectation (`expect*`, `isHeard`, `hold`...) or dropped by an error, to `recordDir` (default `recordings`) |

Automatic files are named `<device>_<in|out>_<Call-ID>.wav`.

### Performance

Recording is off by default and costs nothing then: a stream without `record` keeps no audio, and the only extra work per RTP packet is one check.

With `record` on, the cost is **memory**, not CPU. Each recorded call leg keeps what it heard and sent as 16-bit samples: **32 KB per second of call**, about 1.9 MB per minute. The audio stays in memory from the start of the call until the call object is released after the iteration, whether or not it is saved.

| Recorded legs at once | Call length | Memory |
| --- | --- | --- |
| 1 | 30 s | ~1 MB |
| 100 | 30 s | ~94 MB |
| 1,000 | 30 s | **~1 GB** |
| 1,000 | 3 min | ~5.6 GB |

A call counts once per recorded side: when both A and B belong to the script and both record, 1,000 calls are 2,000 legs, about 2 GB. One leg is capped at 15 minutes (about 29 MB); a longer call keeps only its beginning.

Files are written only by `saveRecording()` or the automatic saving: the same 32 KB per second, so a 30-second leg is about 0.94 MB on disk. Saving everything from 100 such calls with both sides takes about 190 MB; `'onFailure'` saves only the failed ones.

Under load:

- record a sample of the calls, for example one device in ten, rather than all of them;
- keep recorded calls short;
- use `'onFailure'` so that disk use follows the number of failures;
- watch the memory panel of the load generator (refer to [Monitoring](../monitoring.md)): a recording load looks like a steady rise in resident memory that falls back when calls end.

### Example

<!-- md-k6:skip -->

```javascript
import sip from 'k6/x/sip';

sip.options({ record: 'onFailure', recordDir: 'records' }); // keep failed calls for analysis

export default function () {
  const out = ua1.call({ callee: '1003', record: true });
  out.expectConnected('30s');
  out.isHeard('3s');
  out.hangup();
  out.saveRecording(`records/manual-${__VU}-${__ITER}.wav`);
}
```
