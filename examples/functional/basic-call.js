// Internal call A -> B by extension: ringing, answer, two-way audio, DTMF,
// hangup by B.
import { sleep } from 'k6';
import { device, step, call } from './lib.js';
export { options, handleSummary, teardown } from './lib.js';

const A = device('A', 1);
const B = device('B', 2);

export default function () {
  const [out, inc] = call(A, B);
  step('codec negotiated', out.codec());
  out.sendDTMF('123#');
  step('B receives DTMF 123#', inc.expectDTMF('123#', '5s'));
  sleep(1);
  inc.hangup();
  step('A disconnected', out.expectDisconnected('5s'));
  step('ended by B', out.howCompleted().endedBy === 'remote');
}
