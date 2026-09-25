---
title: 'Device.call( options )'
description: 'Device.call starts an outgoing call and returns without waiting for an answer.'
weight: 01
---

# Device.call( options )

Starts an outgoing call: sends INVITE and returns at once, without waiting for the other side. Authentication, provisional responses and the answer are handled in the background. Wait for them with [`expectRinging()`](../../call/expectringing/) and [`expectConnected()`](../../call/expectconnected/).

If `callee` is a device, `call()` first makes sure that the callee is registered, so it can receive the call.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| options.callee | [Device](../) or string | | Who to call. With a Device, the number to dial is taken from its numbers (refer to `aon`). With a string, it's dialled as is: a number such as `'702'` or a full SIP URI such as `'sip:702@pbx.example.com'`. |
| options.aon | string | `'ext'` | With a callee device: the name of the callee number to dial, for example `'onk'` or `'gw_num'`. Without a callee: a literal dial string, for example a service code `'*6701'`. |
| options.timeout | duration | `'60s'` | No-answer timeout. When it expires, the call is cancelled with CANCEL and [`howCompleted()`](../../call/howcompleted/) reports `endedBy: 'timeout'`. |
| options.id | string | | Label of the call, returned by `call.id`. Handy in logs. |
| options.headers | object | | Extra SIP headers for the INVITE, for example `{ 'X-Test-Case': 'TC-101' }`. |
| options.media, options.codecs, options.audio, options.heardLevel | | device settings | Media settings for this call only. Refer to [options()](../../options/). |

### Returns

| Type | Description |
| --- | --- |
| [Call](../../call/) or `false` | The outgoing call leg. `false` if the device or the callee couldn't register, or the INVITE couldn't be sent; the reason is logged as a warning. |

A call that the PBX rejects is still returned as a Call: it ends with the PBX status code, and `expectConnected()` returns `false`.

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
// Dial B's extension (aon defaults to 'ext')
const out1 = A.call({ callee: B });

// Dial B's external number
const out2 = A.call({ callee: B, aon: 'onk' });

// Dial a literal number or URI
const out3 = A.call({ callee: '8800' });
const out4 = A.call({ callee: 'sip:ivr@pbx.example.com' });

// Dial a service code: call pickup of B's line
const pick = C.call({ aon: '*6' + B.identity('ext') });

// Give up after 20 s of ringing, send a custom header, no RTP
const out5 = A.call({ callee: B, timeout: '20s', headers: { 'X-Test-Case': 'TC-101' }, media: false });
```

{{< /code >}}
