// B puts A on hold and resumes; audio stops and comes back.
import { sleep } from 'k6';
import { device, step, call } from './lib.js';
export { options, handleSummary, teardown } from './lib.js';

const A = device('A', 1);
const B = device('B', 2);

const received = (c) => c.mediaStats().received;

export default function () {
  const [out, inc] = call(A, B);

  step('B holds', inc.hold());
  step('A sees remote hold', out.isRemoteHold());
  sleep(0.2);
  const onHold = received(inc);
  sleep(0.5);
  step('B gets no audio while holding', received(inc) === onHold);

  step('B resumes', inc.unhold());
  sleep(0.5);
  step('audio back after resume', received(inc) > onHold);

  out.hangup();
  step('B disconnected', inc.expectDisconnected('5s'));
}
