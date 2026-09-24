// A calls B; B holds A, consults C, then transfers A to C (REFER with
// Replaces). A and C end up talking, B drops out.
import { sleep } from 'k6';
import { device, step, call } from './lib.js';
export { options, handleSummary, teardown } from './lib.js';

const A = device('A', 1);
const B = device('B', 2);
const C = device('C', 3);

export default function () {
  const [aToB, bFromA] = call(A, B);
  step('B holds A', bFromA.hold());

  const [bToC, cFromB] = call(B, C); // consultation
  step('B transfers A to C', bFromA.attendedTransfer(bToC));
  step('transfer confirmed to B', bFromA.expectTransferred('10s'));
  step('consultation leg dropped', bToC.expectDisconnected('5s'));
  bFromA.hangup();

  sleep(0.5);
  step('A still connected', aToB.state() === 'connected');
  step('C still connected', cFromB.state() === 'connected');
  const before = cFromB.mediaStats().received;
  sleep(0.5);
  step('C receives audio from A', cFromB.mediaStats().received > before);

  cFromB.hangup();
  step('A disconnected', aToB.expectDisconnected('5s'));
}
