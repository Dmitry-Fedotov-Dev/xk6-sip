// Audio quality of a call A <-> B: each side says its own phrase, and each
// must hear the other's phrase clearly, without drop-outs or a clipped first
// word, and must not hear itself (echo, crossed media). A failed step keeps
// the WAV of the call in recordings/ for analysis.
//
// The references in refs/ are synthetic speech-like phrases; replace them
// with your own recordings (16-bit PCM, 8 kHz, mono). The score threshold
// depends on the path: set it from a clean baseline run, lower for
// transcoding (-e MIN_SCORE=0.8).
import sip from 'k6/x/sip';
import { sleep } from 'k6';
import { device, step, call } from './lib.js';
export { options, handleSummary, teardown } from './lib.js';

const phraseA = sip.audio(open('./refs/phrase-a.wav', 'b'));
const phraseB = sip.audio(open('./refs/phrase-b.wav', 'b'));
const minScore = Number(__ENV.MIN_SCORE || 0.9);
const recordDir = __ENV.RECORD_DIR || 'recordings';

// 'onFailure' saves calls whose built-in expectations (expect*, isHeard)
// fail; the audio checks below are script checks, so they save explicitly.
sip.options({ record: 'onFailure', recordDir });

const A = device('A', 1, { audio: phraseA });
const B = device('B', 2, { audio: phraseB });

// audioStep is step() that keeps the WAV of the leg when it fails.
function audioStep(leg, name, ok) {
  if (!ok) {
    leg.saveRecording(`${recordDir}/${leg.callId.replace(/[^\w.-]/g, '_')}.wav`);
  }
  return step(name, ok);
}

// hears checks that call leg `leg` heard `ref` clearly.
function hears(who, leg, ref, whose) {
  const q = step(`${who} audio compared`, leg.compareAudio(ref));
  console.log(`${who} vs ${whose}: ${JSON.stringify(q)}`);
  audioStep(leg, `${who} hears ${whose} clearly (score ${q.score} >= ${minScore})`, q.score >= minScore);
  audioStep(leg, `${who}: no drop-outs (${q.gaps} ms)`, q.gaps < 100);
  audioStep(leg, `${who}: first word not clipped (${q.clippedStart} ms)`, q.clippedStart < 60);
  return q;
}

export default function () {
  const [out, inc] = call(A, B);
  sleep(4); // a phrase is 3 s: let one full loop arrive after the answer

  hears('B', inc, phraseA, "A's phrase");
  hears('A', out, phraseB, "B's phrase");

  const self = step('B own phrase compared', inc.compareAudio(phraseB));
  audioStep(inc, `B does not hear itself (score ${self.score})`, self.score < 0.5);

  inc.hangup();
  step('A disconnected', out.expectDisconnected('5s'));
}
