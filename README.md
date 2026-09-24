# xk6-sip

k6 extension `k6/x/sip` for load testing PBXs and SIP servers with scripted
subscribers: each VU owns real SIP devices that register, call each other
through the system under test and check what arrives on the other side.

Status: REGISTER, INVITE/CANCEL/BYE with digest auth; RTP with G.711
(PCMU/PCMA), RFC 3550 loss/jitter, RFC 4733 DTMF and audio detection.
Hold, transfer and PRACK are next.

## Compatibility

| xk6-sip | k6 | Go |
|---|---|---|
| v0.1.x – v0.2.x | v2.x (built and tested with v2.3.0) | 1.26+ |

## Build and run

```sh
xk6 build v2.3.0 --with github.com/Dmitry-Fedotov-Dev/xk6-sip=. --output bin/k6
go build -o bin/testpbx ./cmd/testpbx

bin/testpbx -addr 127.0.0.1:5070 -users 200 -csv examples/subscribers.csv
bin/k6 run examples/call.js
```

Timings below a millisecond are only reliable on Linux: Go's clock on
Windows ticks in ~0.5 ms steps.

## Script API

```js
import sip from 'k6/x/sip';

sip.options({ registerRate: 50, expectTimeout: '30s' }); // init context, optional

// No network in init; REGISTER is sent on first use and refreshed.
// Unknown string fields (ext, onk, gw_num, ...) are the device's numbers.
const ua1 = new sip.Device({ device: 'phone1', registrar: 'sip:pbx:5060',
  user: 'a@domain', pass: 'secret', expires: 180, ext: '701' });

export default function () {
  const out = ua1.call({ callee: ua2, aon: 'ext' });   // returns at once
  const inc = ua2.expectCall({ caller: ua1, aon: 'ext', timeout: '5s' }); // Call or false
  inc.accept();
  out.expectConnected();        // true/false
  inc.hangup();                 // CANCEL, reject or BYE depending on state
  out.expectDisconnected();
}

export function teardown() { sip.shutdown(); } // unregister everything
```

Device: `call`, `expectCall`, `register`, `isRegistered`, `identity(key)`, `destroy`, `id`.
Call: `accept`, `reject(code?, reason?)`, `hangup`, `expectRinging`,
`expectConnected`, `expectDisconnected` (optional timeout), `state`,
`status`, `remote`, `howCompleted`, `trace` (SIP ladder), `callId`,
`codec`, `isHeard`, `sendDTMF`, `expectDTMF`, `receivedDTMF`, `mediaStats`.

Only `expect*` methods wait, so one VU can drive both ends of a call.

### Media

Every call sends RTP by default: a 1 kHz tone, so the far end can check
that audio arrives. Streams are paced by a shared scheduler (500 VUs /
~1000 concurrent streams / 50k packets/s use ~0.6 CPU core on Linux).

```js
const hello = sip.audio(open('./hello.wav', 'b')); // 16-bit PCM, 8 kHz, mono
const ua = new sip.Device({ ..., codecs: 'PCMA,PCMU', audio: hello });

const out = ua1.call({ callee: ua2, media: false }); // signalling only
inc.isHeard('3s');                // audio from the other side arrived
out.codec();                       // 'PCMA'
out.sendDTMF('1#');                // RFC 4733
inc.expectDTMF('1#', '5s');
out.mediaStats();                  // {codec, sent, received, lost, jitter, heard, dtmf}
```

Media options (`media`, `codecs`, `audio: sip.audio(...) | sip.tone(freq, dbfs) | 'silence'`,
`heardLevel`) can be set in `sip.options()`, per Device and per call.

## Metrics

| metric | type | tags |
|---|---|---|
| `sip_requests` | counter | method, status |
| `sip_request_duration` | trend | method, status |
| `sip_failed_requests` | rate | method (401/407 challenges are not failures) |
| `sip_call_setup_time` | trend | INVITE → 200 |
| `sip_post_dial_delay` | trend | INVITE → first 18x |
| `sip_call_success` | rate | status |
| `sip_call_duration` | trend | ended_by |
| `sip_invite_delivery_time` | trend | caller's INVITE → callee receives it |
| `sip_registrations` | counter | |
| `sip_expect_failed` | counter | expect |
| `rtp_packets_sent` / `_received` / `_lost` | counter | codec, direction (per call leg) |
| `rtp_jitter` | trend | RFC 3550 interarrival jitter per leg |
| `rtp_audio_heard` | rate | legs that received audio; < 1 means one-way audio |

`sip.options({ deviceTag: true })` adds a `device` tag to all of them.

## Layout

- `engine/` – SIP core, independent of k6 (sipgo transactions + own dialog layer)
- `media/` – RTP: G.711, scheduler, statistics, DTMF, audio detection
- `testpbx/`, `cmd/testpbx` – minimal registrar/B2BUA for tests and examples
- repo root – the k6 adapter
