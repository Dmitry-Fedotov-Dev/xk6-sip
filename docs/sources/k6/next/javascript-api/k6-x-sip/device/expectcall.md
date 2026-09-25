---
title: 'Device.expectCall( [options] )'
description: 'Device.expectCall waits for a matching incoming call and claims it.'
weight: 02
---

# Device.expectCall( [options] )

Waits for an incoming call that matches the expected caller and number, and claims it. The call keeps ringing until the script [accepts](../call/accept.md) or [rejects](../call/reject.md) it.

Incoming calls that don't match stay in the device inbox, and a later `expectCall()` can still claim them. A call that nobody claims or answers within `ringTimeout` is rejected with `480`. If the call arrived with a wrong number, `expectCall()` returns `false`, and the `sip_expect_failed{expect:call}` metric counts it. Under load, that metric shows how many calls the PBX routed to the wrong place or presented with a wrong number.

The caller number is taken from the P-Asserted-Identity header when present, otherwise from the From header, the same way a phone displays it.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| options.caller | [Device](_index.md) or string | any caller | Expected caller. A Device, or a literal number to compare with the caller number. |
| options.aon | string | any number of the caller | With a caller device: the name of the caller number that must be presented, for example `'onk'`. Without a caller: the literal expected number. |
| options.timeout | duration | `expectTimeout`, `'30s'` | How long to wait. |

### Returns

| Type | Description |
| --- | --- |
| [Call](../call/_index.md) or `false` | The incoming call leg, or `false` if no matching call arrived in time. |

When `caller` is a device of the same test, the extension also measures how long the PBX took to deliver the INVITE from caller to callee and reports it as `sip_invite_delivery_time`.

### Example

<!-- md-k6:skip -->

```javascript
import { check } from 'k6';

export default function () {
  const out = A.call({ callee: B, aon: 'gw_num' });

  // B must see A's external number, not the extension
  const inc = B.expectCall({ caller: A, aon: 'onk', timeout: '10s' });
  if (!check(inc, { 'B got the call with A external number': (c) => c !== false })) {
    out.hangup();
    return;
  }
  console.log(`caller number: ${inc.remote()}`);
  inc.accept();
}
```
