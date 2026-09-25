---
title: 'options( options )'
description: 'sip.options sets module-wide options of k6/x/sip.'
weight: 02
---

# options( options )

Sets options that apply to all devices of the k6 process. Call it in the init context, before the first `new sip.Device()`. Every VU runs the init code, so every VU calls `sip.options()` with the same values, and that is expected. Changing SIP or media options after the first device has been used throws an error.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| options.localIP | string | chosen by the OS | Local IP address that device sockets bind to. By default it is the address the OS would use to reach each device's proxy. Set it on hosts with several network interfaces. |
| options.registerRate | number | `0` (unlimited) | Maximum REGISTER requests per second across all devices. Use it to avoid a registration storm when thousands of VUs start at once. |
| options.ringTimeout | duration | `'3m'` | How long an incoming call may ring before it is claimed with `expectCall()` and answered. After that the device rejects it with `480 Temporarily Unavailable`. |
| options.expectTimeout | duration | `'30s'` | Default timeout of `expectCall()`, every `expect*()` method and `isHeard()`, used when the call doesn't pass its own. |
| options.trace | boolean | `false` | Keep full SIP messages, with headers and SDP, in [`call.trace()`](../call/trace/). By default only the first line of each message is kept. |
| options.deviceTag | boolean | `false` | Add a `device` tag with the device name to every SIP and RTP metric. Useful for debugging; it multiplies the number of time series by the number of devices. |
| options.media | boolean | `true` | `false` disables RTP for all calls, for signalling-only load. |
| options.codecs | string or array | `'PCMU,PCMA'` | Offered codecs in order of preference, for example `'PCMA,PCMU'` or `['PCMA']`. The aliases `ulaw`, `alaw`, `G711U` and `G711A` are accepted. |
| options.audio | [audio](../audio/), `'tone'` or `'silence'` | `'tone'` | What calls send: a WAV file, a 1 kHz tone at −20 dBFS, or silence. |
| options.heardLevel | number | `-45` | Level in dBFS above which received audio counts as heard by [`isHeard()`](../call/isheard/). |

The media options `media`, `codecs`, `audio` and `heardLevel` are defaults. A [Device](../device/) can override them for its calls, and a single [`call()`](../device/call/) can override them again.

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
import sip from 'k6/x/sip';

sip.options({
  registerRate: 50, // at most 50 REGISTER per second
  expectTimeout: '10s',
  codecs: 'PCMA',
  trace: __ENV.SIP_TRACE === '1',
});
```

{{< /code >}}
