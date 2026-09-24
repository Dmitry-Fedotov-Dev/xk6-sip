// Shared setup for functional scenarios: one VU, one iteration, every
// check must pass (non-zero exit code otherwise) and a JUnit report for CI.
//
// Subscribers default to the test PBX (testpbx -users 3); point them at a
// real PBX with -e REGISTRAR=... -e A_USER=... -e A_PASS=... -e A_EXT=...
// (same for B_ and C_).
import sip from 'k6/x/sip';
import { check, fail } from 'k6';
import { jUnit, textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

export const options = {
  vus: 1,
  iterations: 1,
  thresholds: { checks: ['rate==1'] },
};

const env = (k, d) => __ENV[k] || d;
const registrar = env('REGISTRAR', 'sip:127.0.0.1:5070');

// device('A', 1) -> user1@test.local, ext 1001 unless overridden by A_*.
export function device(name, n) {
  return new sip.Device({
    device: name,
    registrar,
    user: env(`${name}_USER`, `user${n}@test.local`),
    pass: env(`${name}_PASS`, 'secret'),
    ext: env(`${name}_EXT`, String(1000 + n)),
  });
}

// step records a check and stops the scenario at the first failure.
export function step(name, value) {
  if (!check(value, { [name]: (v) => v !== false && v !== null && v !== undefined })) {
    fail(name);
  }
  return value;
}

// call places a call from a to b and answers it on b.
export function call(a, b, aon = 'ext') {
  const out = step(`${a.id} calls ${b.id}`, a.call({ callee: b, aon }));
  const inc = step(`${b.id} gets the call from ${a.id}`, b.expectCall({ caller: a, aon, timeout: '10s' }));
  inc.accept();
  step(`${a.id} connected`, out.expectConnected('10s'));
  step(`${b.id} hears ${a.id}`, inc.isHeard('5s'));
  step(`${a.id} hears ${b.id}`, out.isHeard('5s'));
  return [out, inc];
}

export function handleSummary(data) {
  return {
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
    [env('JUNIT', 'junit.xml')]: jUnit(data),
  };
}

// Hang up whatever a failed scenario left behind and unregister.
export function teardown() {
  sip.shutdown();
}
