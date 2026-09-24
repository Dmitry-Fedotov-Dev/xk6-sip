// One A→B call with the SIP ladder of both legs printed, for checking a
// PBX by hand before running load.
//
//   testpbx -addr 127.0.0.1:5070 -users 2
//   ./k6 run examples/one-call.js
//
// Override the defaults with -e, e.g. -e REGISTRAR=sip:10.0.0.1:5060
// -e A_USER=701@pbx.local -e A_PASS=... -e A_EXT=701 (same for B_).
import sip from 'k6/x/sip';
import { check, sleep } from 'k6';

export const options = { vus: 1, iterations: 1 };

const env = (k, d) => __ENV[k] || d;
const registrar = env('REGISTRAR', 'sip:127.0.0.1:5070');

const ua1 = new sip.Device({
  device: 'A', registrar,
  user: env('A_USER', 'user1@test.local'), pass: env('A_PASS', 'secret'), ext: env('A_EXT', '1001'),
});
const ua2 = new sip.Device({
  device: 'B', registrar,
  user: env('B_USER', 'user2@test.local'), pass: env('B_PASS', 'secret'), ext: env('B_EXT', '1002'),
});

export default function () {
  check(ua1.register(), { 'A registered': (ok) => ok });
  check(ua2.register(), { 'B registered': (ok) => ok });

  const out = ua1.call({ callee: ua2, aon: 'ext' });
  const inc = ua2.expectCall({ caller: ua1, aon: 'ext', timeout: '5s' });
  const gotCall = check(inc, { 'B got the call from A': (c) => c !== false });

  if (gotCall) {
    check(out.expectRinging('5s'), { 'A hears ringing': (ok) => ok });
    inc.accept();
    check(out.expectConnected('5s'), { 'A connected': (ok) => ok });
    check(inc.expectConnected('5s'), { 'B connected': (ok) => ok });

    check(inc.isHeard('3s'), { 'B hears A': (ok) => ok });
    check(out.isHeard('3s'), { 'A hears B': (ok) => ok });
    check(out.codec(), { 'codec negotiated': (c) => c !== null });
    out.sendDTMF('123#');
    check(inc.expectDTMF('123#', '3s'), { 'B got DTMF 123#': (ok) => ok });
    sleep(1);
    inc.hangup();
    check(out.expectDisconnected('5s'), { 'A disconnected': (ok) => ok });
  } else {
    out.hangup();
  }

  console.log(`A leg (${out.state()}, status ${out.status()}):\n${out.trace()}`);
  if (gotCall) {
    console.log(`B leg, caller ${inc.remote()}:\n${inc.trace()}`);
    console.log('how completed: ' + JSON.stringify(out.howCompleted()));
    console.log('A media: ' + JSON.stringify(out.mediaStats()));
    console.log('B media: ' + JSON.stringify(inc.mediaStats()));
  }
}

export function teardown() {
  sip.shutdown();
}
