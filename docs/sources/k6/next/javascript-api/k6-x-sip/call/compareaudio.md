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

### Reading the score

Values measured with the speech-like signals of the extension's tests and on calls through a test PBX:

| Situation | score | gaps |
| --- | --- | --- |
| Clean G.711 path, any delay | 0.99 | 0 |
| Same, 12 dB quieter | ≥ 0.93 | 0 |
| 20% of packets lost | ~0.75 | equal to the lost speech |
| Unrelated speech (wrong party, crossed media) | < 0.4 | — |
| Silence (one-way audio) | 0 | the whole reference |
| The subscriber's own phrase instead of the other party's | ~0.1 | — |

A threshold of 0.9 fits a clean G.711 path. Transcoding (G.729, Opus) changes the waveform and lowers the score, so measure a baseline first and lower the threshold accordingly.

| Parameter | Type | Description |
| --- | --- | --- |
| reference | [audio](../audio.md) | The expected audio: `sip.audio(open('ref.wav', 'b'))` or `sip.tone(...)`. |

### Performance

`compareAudio()` runs in the VU that calls it and takes CPU time proportional to the length of the recording times the length of the reference: about **15 ms** to find a 3-second reference in a 30-second recording on a desktop CPU, plus about 0.7 MB of short-lived memory. Recording itself is required, with its memory cost: refer to [saveRecording()](saverecording.md#performance).

In functional tests this is negligible. Under load, 1,000 comparisons a second would need about 15 CPU cores, so compare a sample of the calls, keep the reference short (3–5 s of speech is enough), and don't compare long recordings: the time grows with the whole recording, not with the part that matters. Take the comparison soon after the phrase has played rather than at the end of a long call.

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

Every call also adds its score to the `rtp_audio_score` metric, so `thresholds` can check the scores of the whole test. That includes deliberate negative checks such as "B does not hear itself", so leave them out of scripts whose thresholds use this metric.

Checks on the returned score are ordinary script checks: unlike failed `expect*()` methods, they don't make `record: 'onFailure'` save the call. Call [saveRecording()](saverecording.md) yourself when such a check fails, as the [audio quality functional test](https://github.com/Dmitry-Fedotov-Dev/xk6-sip/blob/main/examples/functional/audio-quality.js) does.

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
