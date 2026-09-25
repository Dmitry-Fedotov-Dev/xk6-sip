---
title: 'audio( data )'
description: 'sip.audio loads a WAV file as an audio source for calls.'
weight: 03
---

# audio( data )

Loads a WAV file that calls play in a loop instead of the default tone. The file must be 16-bit PCM, 8 kHz, mono. It is decoded once and shared by all VUs, so loading the same file in every VU costs memory only once.

| Parameter | Type | Description |
| --- | --- | --- |
| data | ArrayBuffer | Contents of the WAV file: the result of `open(path, 'b')`. |

### Returns

| Type | Description |
| --- | --- |
| object | Audio source to pass as `audio` to [options()](../options/), a [Device](../device/) or [call()](../device/call/). |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
import sip from 'k6/x/sip';

const greeting = sip.audio(open('./greeting.wav', 'b'));

const A = new sip.Device({
  device: 'phone1', registrar: 'sip:pbx:5060', user: '701@pbx', pass: 'secret', ext: '701',
  audio: greeting,
});
```

{{< /code >}}

To convert any audio file with ffmpeg:

```bash
ffmpeg -i in.mp3 -ar 8000 -ac 1 -c:a pcm_s16le out.wav
```
