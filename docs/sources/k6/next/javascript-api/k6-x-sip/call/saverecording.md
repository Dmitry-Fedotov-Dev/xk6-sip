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

A recording takes about 32 KB of memory per second of call until the call object is released: 1 MB for a 30-second call. Under load prefer `'onFailure'` and short calls, or record only some of the calls.

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
