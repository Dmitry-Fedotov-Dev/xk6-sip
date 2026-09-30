// Shared setup for functional scenarios: one VU, one iteration, every
// check must pass (non-zero exit code otherwise) and a JUnit report for CI
// (-e JUNIT=report.xml) with one test case per step and per threshold.
//
// Subscribers default to the test PBX (testpbx -users 3); point them at a
// real PBX with -e REGISTRAR=... -e A_USER=... -e A_PASS=... -e A_EXT=...
// (same for B_ and C_).
import sip from 'k6/x/sip';
import { check, fail } from 'k6';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

export const options = {
  vus: 1,
  iterations: 1,
  thresholds: { checks: ['rate==1'] },
};

const env = (k, d) => __ENV[k] || d;
const registrar = env('REGISTRAR', 'sip:127.0.0.1:5070');

// device('A', 1) -> user1@test.local, ext 1001 unless overridden by A_*.
// extra adds Device options such as audio or record.
export function device(name, n, extra = {}) {
  return new sip.Device({
    device: name,
    registrar,
    user: env(`${name}_USER`, `user${n}@test.local`),
    pass: env(`${name}_PASS`, 'secret'),
    ext: env(`${name}_EXT`, String(1000 + n)),
    ...extra,
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

// results lists the steps in the order they ran, then the thresholds:
// { name, ok, detail }.
function results(data) {
  const out = [];
  const walk = (g) => {
    for (const c of g.checks || []) {
      out.push({ name: c.name, ok: c.fails === 0, detail: `${c.fails} of ${c.passes + c.fails} failed` });
    }
    (g.groups || []).forEach(walk);
  };
  walk(data.root_group);
  for (const [metric, m] of Object.entries(data.metrics)) {
    for (const [expr, t] of Object.entries(m.thresholds || {})) {
      out.push({ name: `threshold ${metric}: ${expr}`, ok: t.ok, detail: 'threshold crossed' });
    }
  }
  return out;
}

const xml = (s) => String(s).replace(/[<>&"]/g, (c) => ({ '<': '&lt;', '>': '&gt;', '&': '&amp;', '"': '&quot;' })[c]);

// junit is one test suite per scenario with a test case per step: step names
// carry the actual values ("A gets 404 (got 480)"), so the report explains
// a failure without the log. k6-summary's jUnit() only lists thresholds.
function junit(suite, rs) {
  const failures = rs.filter((r) => !r.ok).length;
  const cases = rs.map((r) => {
    const tc = `    <testcase name="${xml(r.name)}" classname="${xml(suite)}"`;
    return r.ok ? `${tc}/>` : `${tc}>\n      <failure message="${xml(r.name)}">${xml(r.detail)}</failure>\n    </testcase>`;
  });
  return `<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="${rs.length}" failures="${failures}">
  <testsuite name="${xml(suite)}" tests="${rs.length}" failures="${failures}">
${cases.join('\n')}
  </testsuite>
</testsuites>
`;
}

export function handleSummary(data) {
  const junitPath = env('JUNIT', 'junit.xml');
  const suite = env('SUITE', junitPath.replace(/^.*[\\/]/, '').replace(/\.xml$/, ''));
  return {
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
    [junitPath]: junit(suite, results(data)),
  };
}

// Hang up whatever a failed scenario left behind and unregister.
export function teardown() {
  sip.shutdown();
}
