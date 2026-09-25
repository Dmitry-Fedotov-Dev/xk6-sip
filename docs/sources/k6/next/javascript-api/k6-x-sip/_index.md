---
title: 'k6/x/sip'
description: 'k6/x/sip drives SIP subscribers that register, call each other through a PBX and check signalling, audio and DTMF.'
weight: 11
---

# k6/x/sip

The `k6/x/sip` module turns k6 virtual users into SIP subscribers. Subscribers register on a PBX or SIP server, call each other through it, answer, hold, transfer, send DTMF and check that audio really arrives on the other side. One script works both as a functional test (one call, every step checked) and as a load test (thousands of concurrent calls with RTP).

The module is a k6 extension, [xk6-sip](https://github.com/Dmitry-Fedotov-Dev/xk6-sip). It is not part of the standard k6 binary, so build k6 with it first:

```bash
xk6 build v2.3.0 --with github.com/Dmitry-Fedotov-Dev/xk6-sip@latest
```

## Key concepts

- **Device** is one SIP subscriber: a UDP socket, a registration that is refreshed automatically, and the calls it makes and receives. Refer to [Device](device/).
- **Call** is one call leg as seen by one device. The caller gets its leg from [`device.call()`](device/call/), the callee gets its own leg from [`device.expectCall()`](device/expectcall/). Refer to [Call](call/).
- **Numbers (identities).** A subscriber usually has several numbers: an extension, an external number, a gateway number. They are plain fields of the Device, for example `ext: '701', onk: '+79101110011'`. Scripts refer to them by name with the `aon` option, so a test says "call B on its external number" instead of hard-coding digits.
- **Actions never wait, only expectations wait.** `call()`, `accept()`, `hangup()`, `hold()`, `transfer()` and `sendDTMF()` return at once; the SIP exchange continues in the background. Methods whose name starts with `expect`, and `isHeard()`, wait for something to happen and return `false` on timeout instead of throwing. Because of this rule, one VU can play both ends of a call: `call()` returns before the callee answers, so the script can go on to `expectCall()` and `accept()` on the other device.
- **Media.** Every call sends real RTP (G.711), a 1 kHz tone unless told otherwise. The receiver checks the signal level, so `isHeard()` means "audio arrived", not "packets arrived". Set `media: false` for signalling-only load.

## API

| Export | Description |
| --- | --- |
| [Device](device/) | Class: a SIP subscriber that registers, calls and receives calls. |
| [options( options )](options/) | Sets module-wide options: local IP, REGISTER rate, timeouts, tracing, media defaults. |
| [audio( data )](audio/) | Loads a WAV file as an audio source for calls. |
| [tone( [freq], [dbfs] )](tone/) | Creates a sine tone audio source. |
| [shutdown()](shutdown/) | Hangs up all calls, unregisters and closes all devices. Call it in `teardown()`. |
| [Metrics](metrics/) | Built-in `sip_*` and `rtp_*` metrics. |

## Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
import sip from 'k6/x/sip';
import { check } from 'k6';

export const options = {
  vus: 1,
  iterations: 1,
  thresholds: { checks: ['rate==1'] },
};

const A = new sip.Device({ device: 'phone1', registrar: 'sip:pbx.example.com:5060', user: '701@pbx.example.com', pass: 'secret', ext: '701' });
const B = new sip.Device({ device: 'phone2', registrar: 'sip:pbx.example.com:5060', user: '702@pbx.example.com', pass: 'secret', ext: '702' });

export default function () {
  const out = A.call({ callee: B });                        // action: returns at once
  const inc = B.expectCall({ caller: A, timeout: '10s' });  // expectation: waits
  if (!check(inc, { 'B got the call': (c) => c !== false })) {
    out.hangup();
    return;
  }
  inc.accept();
  check(out, {
    'A connected': (c) => c.expectConnected('5s'),
    'A hears B': (c) => c.isHeard('3s'),
  });
  check(inc, { 'B hears A': (c) => c.isHeard('3s') });

  out.hangup();
  if (!check(inc, { 'B got BYE': (c) => c.expectDisconnected('5s') })) {
    console.warn(out.trace()); // SIP ladder of this call
  }
}

export function teardown() {
  sip.shutdown();
}
```

{{< /code >}}

## Lifecycle

1. Devices are created in the init context. Creating a device does no network I/O.
2. The first `call()`, `expectCall()` or `register()` inside a test function opens the device socket and sends REGISTER. `call()` with a callee device also registers the callee, so it can receive the call.
3. The registration is refreshed in the background until the end of the test.
4. [`sip.shutdown()`](shutdown/) in `teardown()` hangs up remaining calls and unregisters every device.

A device can't be used in the init context: calling its methods there throws an error.

## Functional and load tests

The same `default` function runs as a functional test or as a load test; only `options` change.

- **Functional test:** `vus: 1, iterations: 1` and the threshold `checks: ['rate==1']`. Any failed check makes k6 exit with a non-zero code. A `handleSummary()` with `jUnit()` from [k6-summary](https://jslib.k6.io/k6-summary/0.1.0/index.js) writes a JUnit report for CI and test management tools.
- **Load test:** an arrival-rate executor gives a constant number of new calls per second (CAPS). By Little's law, concurrent calls = CAPS × call duration: 20 calls per second of 60 seconds each keep 1200 calls up, so the scenario needs about 1200 VUs and 2400 subscribers.

{{< code >}}

<!-- md-k6:skip -->

```javascript
export const options = {
  scenarios: {
    caps: { executor: 'constant-arrival-rate', rate: 20, timeUnit: '1s', duration: '10m', preAllocatedVUs: 1300 },
  },
  thresholds: {
    sip_call_success: ['rate>0.99'],
    sip_call_setup_time: ['p(95)<300'],
    rtp_audio_heard: ['rate>0.99'],
  },
};
```

{{< /code >}}

## Subscribers from a CSV file

Device options are plain strings, so a CSV row can be passed to the constructor as is. Every column that is not a known option becomes a number of the subscriber.

```csv
device,registrar,user,pass,expires,ext,onk
phone1,sip:pbx:5060,701@pbx,secret,300,701,+79101110011
phone2,sip:pbx:5060,702@pbx,secret,300,702,+79101110012
```

{{< code >}}

<!-- md-k6:skip -->

```javascript
import sip from 'k6/x/sip';
import { SharedArray } from 'k6/data';
import papaparse from 'https://jslib.k6.io/papaparse/5.1.1/index.js';

const subs = new SharedArray('subscribers', () =>
  papaparse.parse(open('./subscribers.csv'), { header: true, skipEmptyLines: true }).data);

// Each VU owns two subscribers. __VU is 0 while k6 reads the options.
const pair = Math.max(__VU - 1, 0) * 2;
const A = new sip.Device(subs[pair]);
const B = new sip.Device(subs[pair + 1]);
```

{{< /code >}}

## Durations

Every timeout and duration option accepts a string such as `'500ms'`, `'10s'` or `'1m30s'`, or a number of seconds. A numeric string, as it comes from a CSV file, also means seconds: `'180'` is three minutes.

## Supported protocols

| Area | Support |
| --- | --- |
| Transport | SIP over UDP |
| Registration | REGISTER with digest authentication, automatic refresh, unregistration on shutdown |
| Calls | INVITE with digest authentication (401 and 407), CANCEL, BYE, early media |
| In-dialog | re-INVITE hold and resume, glare handling (491), OPTIONS, INFO, UPDATE |
| Reliability | PRACK and 100rel (RFC 3262) |
| Session timers | RFC 4028: refresh, 422 and Min-SE |
| Transfers | REFER with NOTIFY sipfrag (RFC 3515), Replaces (RFC 3891) |
| Media | RTP with G.711 μ-law and A-law, symmetric RTP, loss and jitter per RFC 3550, DTMF per RFC 4733 |
