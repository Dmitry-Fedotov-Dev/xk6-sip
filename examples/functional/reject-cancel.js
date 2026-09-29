// Calls that don't connect: the callee rejects (486 Busy Here), the caller
// hangs up while it rings (CANCEL, 487 on the callee) and the number doesn't
// exist (404). The PBX must pass each outcome to the other side.
import { device, step } from './lib.js';
export { options, handleSummary, teardown } from './lib.js';

const A = device('A', 1);
const B = device('B', 2);
const unknown = __ENV.UNKNOWN_NUMBER || '1999';

export default function () {
  // B is busy.
  let out = step('A calls B', A.call({ callee: B }));
  let inc = step('B gets the call', B.expectCall({ caller: A, timeout: '10s' }));
  inc.reject(486, 'Busy Here');
  step('A: busy call ended', out.expectDisconnected('10s'));
  step(`A gets 486 (got ${out.status()})`, out.status() === 486);

  // A hangs up before B answers.
  out = step('A calls B again', A.call({ callee: B }));
  inc = step('B gets the second call', B.expectCall({ caller: A, timeout: '10s' }));
  step('A hears ringing', out.expectRinging('10s'));
  out.hangup();
  step('B: call cancelled', inc.expectDisconnected('10s'));
  step(`B: ended with 487 (got ${inc.status()})`, inc.status() === 487);
  step('A: cancelled call ended', out.expectDisconnected('10s'));

  // The number doesn't exist.
  out = step(`A calls ${unknown}`, A.call({ callee: unknown }));
  step('A: call to unknown number ended', out.expectDisconnected('10s'));
  step(`A gets 404 (got ${out.status()})`, out.status() === 404);
}
