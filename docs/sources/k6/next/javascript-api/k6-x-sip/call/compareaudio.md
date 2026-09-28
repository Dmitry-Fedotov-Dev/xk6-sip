---
title: 'Call.compareAudio( reference )'
description: 'Call.compareAudio scores the audio this call leg heard against a reference.'
weight: 27
---

# Call.compareAudio( reference )

Compares what this leg heard with the audio that should have arrived and returns a score with diagnostics. Use it to check that a subscriber hears the right party without distortion, or that the PBX played the right prompt.

Recording must be on for the call (`record: true` or `'onFailure'`); refer to [saveRecording()](saverecording.md). The method doesn't wait: it compares what was heard so far, so call it after the reference had time to play.

**The reference.** When the other party is a device of the same script, the reference is simply what that device sends: its `audio` option. Only audio produced by the PBX (IVR prompts, announcements, music on hold) needs a recorded reference file. Use speech for meaningful scores; a pure tone only tells whether the tone frequency arrived.

### How the score is computed

1. **Alignment.** The reference is located in the recording by the correlation of 10 ms level envelopes, then refined on the waveform to one sample. Network delay and the point where a looping source happened to start don't matter.
2. **Spectrograms** of the reference and of the aligned recording, in the telephone band 300–3400 Hz, 32 ms frames every 16 ms.
3. **Score.** Only frames where the reference has speech are compared. Each frequency band has its average over time removed, and the two spectrograms are correlated: the score says how closely the heard audio follows the changes of the reference in time and frequency. It doesn't depend on the level. Drop-outs, noise, distortion and wrong audio lower it; unrelated speech scores low even with a similar voice.

The idea follows full-reference quality models such as ViSQOL in a much simpler form. The score is not a MOS: use it to compare runs, versions and calls with each other and set the threshold from a clean baseline run.

| Parameter | Type | Description |
| --- | --- | --- |
| reference | [audio](../audio.md) | The expected audio: `sip.audio(open('ref.wav', 'b'))` or `sip.tone(...)`. |

### Returns

An object, or `null` if there is too little audio to compare (under 32 ms, or the reference is silent). Without recording or media it returns `null` and logs a warning.

| Property | Type | Description |
| --- | --- | --- |
| score | number | Similarity 0..1. A clean G.711 path scores about 0.95–1. |
| offset | number | Where the reference starts in the recording, ms. |
| compared | number | Length of the reference that was compared, ms; shorter than the reference if the recording ends early. |
| gaps | number | Reference speech during which nothing was heard, ms: lost packets, one-way audio. |
| clippedStart | number | The part of the first word that was lost, ms: gaps from the start of the reference speech until audio is first heard. |
| gain | number | Level of the heard audio relative to the reference, dB. |

Every call also adds its score to the `rtp_audio_score` metric, so `thresholds` can check the scores of the whole test.

### Example

<!-- md-k6:skip -->

```javascript
import sip from 'k6/x/sip';
import { check, sleep } from 'k6';

const hello = sip.audio(open('./refs/hello.wav', 'b'));
const A = new sip.Device({ /* ... */ audio: hello });
const B = new sip.Device({ /* ... */ record: true });

export const options = { thresholds: { rtp_audio_score: ['p(95)>0.9'] } };

export default function () {
  const out = A.call({ callee: B });
  const inc = B.expectCall({ caller: A });
  inc.accept();
  sleep(4); // let the phrase play

  const q = inc.compareAudio(hello);
  check(q, {
    'B hears A clearly': (q) => q && q.score >= 0.9,
    'no drop-outs': (q) => q && q.gaps < 100,
  });
  inc.hangup();
}
```
