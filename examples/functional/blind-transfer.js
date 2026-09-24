// A calls B, B blind-transfers A to C. C must see A as the caller and the
// PBX must keep A's call when B hangs up.
import { sleep } from 'k6';
import { device, step, call } from './lib.js';
export { options, handleSummary, teardown } from './lib.js';

const A = device('A', 1);
const B = device('B', 2);
const C = device('C', 3);

export default function () {
  C.register();
  const [out, inc] = call(A, B);

  step('B transfers A to C', inc.transfer(C));
  const toC = step('C gets the call from A', C.expectCall({ caller: A, aon: 'ext', timeout: '10s' }));
  toC.accept();
  step('transfer confirmed to B', inc.expectTransferred('10s'));
  inc.hangup();

  sleep(0.5);
  step('A still connected', out.state() === 'connected');
  step('C hears A', toC.isHeard('5s'));
  toC.hangup();
  step('A disconnected', out.expectDisconnected('5s'));
}
