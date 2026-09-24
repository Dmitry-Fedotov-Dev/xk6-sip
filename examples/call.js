// A→B call through the PBX, both legs driven by the same VU.
// Each VU owns two subscribers from the CSV for the whole test.
//
//   testpbx -users 200 -csv examples/subscribers.csv
//   ./k6 run examples/call.js
//   ./k6 run -e VUS=500 -e HOLD=10 -e DURATION=1m examples/call.js  (needs -users >= 2*VUS)
import sip from 'k6/x/sip';
import { SharedArray } from 'k6/data';
import { check, sleep } from 'k6';
import papaparse from 'https://jslib.k6.io/papaparse/5.1.1/index.js';

export const options = {
  scenarios: {
    calls: {
      executor: 'constant-vus',
      vus: Number(__ENV.VUS || 10),
      duration: __ENV.DURATION || '30s',
    },
  },
  thresholds: {
    sip_call_success: ['rate>0.99'],
    sip_call_setup_time: ['p(95)<500'],
    rtp_audio_heard: ['rate>0.99'], // no one-way audio
  },
};

sip.options({ registerRate: 50, deviceTag: __ENV.DEVICE_TAG === '1' });

const subs = new SharedArray('subscribers', () =>
  papaparse.parse(open('./subscribers.csv'), { header: true, skipEmptyLines: true }).data);

// Declared in init: no network yet. REGISTER goes out on first use.
// __VU is 0 while k6 reads options, hence the max().
const pair = Math.max(__VU - 1, 0) * 2;
const ua1 = new sip.Device(subs[pair]);
const ua2 = new sip.Device(subs[pair + 1]);

export default function () {
  const out = ua1.call({ callee: ua2, aon: 'ext' });
  if (!out) return;

  const inc = ua2.expectCall({ caller: ua1, aon: 'ext', timeout: '5s' });
  if (!check(inc, { 'B got the call from A': (c) => c !== false })) {
    out.hangup();
    return;
  }

  inc.accept();
  if (!check(out.expectConnected('5s'), { 'A connected': (ok) => ok })) {
    console.warn(out.trace());
    out.hangup();
    return;
  }

  check(inc.isHeard('2s'), { 'B hears A': (ok) => ok });
  sleep(Number(__ENV.HOLD || 1) + Math.random()); // talk time, s
  inc.hangup();
  check(out.expectDisconnected('5s'), { 'A disconnected': (ok) => ok });
  sleep(0.5);
}

// Unregister all subscribers once the test is over.
export function teardown() {
  sip.shutdown();
}
