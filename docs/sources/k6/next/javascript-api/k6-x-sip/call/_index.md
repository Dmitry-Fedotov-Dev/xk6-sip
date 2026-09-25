---
title: 'Call'
description: 'Call is one call leg as seen by one device.'
weight: 02
---

# Call

`Call` is one call leg as seen by one device. A call between A and B has two legs: A gets the outgoing leg from [`A.call()`](../device/call/), and B gets the incoming leg from [`B.expectCall()`](../device/expectcall/). Each leg has its own state, SIP trace and media statistics.

Methods of a call are either actions or expectations:

- **Actions** (`accept`, `reject`, `hangup`, `hold`, `transfer`, `sendDTMF`...) send a request and return. They may wait for the response to their own request, which the other device sends automatically, but they never wait for a step of the script on another device.
- **Expectations** (`expect*` and `isHeard`) wait until something happens or the timeout expires. They return `false` on timeout instead of throwing, so they fit directly into `check()`. Every failed expectation also increments the `sip_expect_failed` metric with the `expect` tag.

When a timeout isn't passed, expectations use `expectTimeout` from [options()](../options/), 30 seconds by default.

## States

| State | Meaning |
| --- | --- |
| `calling` | INVITE sent or received, no ringing yet. |
| `ringing` | Outgoing: an 18x response arrived. Incoming: the device is ringing. |
| `connected` | The call was answered. |
| `ended` | The call is over: hung up, rejected, cancelled or failed. |

## Properties

| Property | Type | Description |
| --- | --- | --- |
| callId | string | SIP Call-ID of the leg. Search for it in PBX logs. |
| id | string | Label given with `call({ id })`, or an empty string. |

## Methods

### Call control

| Method | Waits | Description |
| --- | --- | --- |
| [accept()](accept/) | no | Answers a ringing incoming call with `200 OK`. |
| [reject( [code], [reason] )](reject/) | no | Rejects a ringing incoming call, `603 Decline` by default. |
| [hangup()](hangup/) | response | Ends the call in any state: CANCEL, reject or BYE. |
| [expectRinging( [timeout] )](expectringing/) | yes | Waits until the call rings. |
| [expectConnected( [timeout] )](expectconnected/) | yes | Waits until the call is answered. |
| [expectDisconnected( [timeout] )](expectdisconnected/) | yes | Waits until the call ends. |

### State and diagnostics

| Method | Waits | Description |
| --- | --- | --- |
| [state()](state/) | no | Current state: `calling`, `ringing`, `connected` or `ended`. |
| [status()](status/) | no | Final INVITE status code, for example `200` or `486`, or `0` while unknown. |
| [remote()](remote/) | no | The other party: the dialled number, or the caller number of an incoming call. |
| [howCompleted()](howcompleted/) | no | How the call ended: who hung up, status, reason, talk time. |
| [trace()](trace/) | no | SIP ladder of the leg: every message with time and direction. |

### Media

| Method | Waits | Description |
| --- | --- | --- |
| [codec()](codec/) | no | Negotiated codec, `PCMU` or `PCMA`. |
| [isHeard( [timeout] )](isheard/) | yes | Waits until audio from the other side arrives. |
| [sendDTMF( digits, [duration] )](senddtmf/) | no | Sends DTMF digits (RFC 4733). |
| [expectDTMF( digits, [timeout] )](expectdtmf/) | yes | Waits until the given DTMF digits arrive. |
| [receivedDTMF()](receiveddtmf/) | no | DTMF digits received so far. |
| [mediaStats()](mediastats/) | no | RTP statistics: packets, loss, jitter, heard time, DTMF. |

### Hold and transfer

| Method | Waits | Description |
| --- | --- | --- |
| [hold()](hold/) | response | Puts the other side on hold (re-INVITE with `a=sendonly`). |
| [unhold()](unhold/) | response | Resumes a held call. |
| [isOnHold()](isonhold/) | no | Whether this side put the call on hold. |
| [isRemoteHold()](isremotehold/) | no | Whether the other side put this side on hold. |
| [transfer( dest, [aon] )](transfer/) | response | Blind transfer of the other party (REFER). |
| [attendedTransfer( consult )](attendedtransfer/) | response | Attended transfer: connects the other party with the party of the consultation call (REFER with Replaces). |
| [expectTransferred( [timeout] )](expecttransferred/) | yes | Waits for the result of a transfer made from this leg. |
| [expectReferredCall( [timeout] )](expectreferredcall/) | yes | Returns the new call this device made because the other side transferred it. |

## Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
import { check } from 'k6';

export default function () {
  const out = A.call({ callee: B, id: 'basic' });
  const inc = B.expectCall({ caller: A });
  if (!inc) {
    out.hangup();
    return;
  }
  check(out.expectRinging('5s'), { 'A hears ringback': (ok) => ok });
  inc.accept();
  check(out.expectConnected('5s'), { 'connected': (ok) => ok });

  out.sendDTMF('123#');
  check(inc.expectDTMF('123#', '5s'), { 'B got DTMF': (ok) => ok });

  inc.hangup();
  out.expectDisconnected('5s');
  console.log(JSON.stringify(out.howCompleted())); // {"endedBy":"remote","status":200,...}
}
```

{{< /code >}}
