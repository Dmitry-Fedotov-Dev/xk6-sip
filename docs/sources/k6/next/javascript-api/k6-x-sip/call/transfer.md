---
title: 'Call.transfer( dest, [aon] )'
description: 'Call.transfer makes a blind transfer of the other party.'
weight: 22
---

# Call.transfer( dest, [aon] )

Blind transfer: sends REFER that asks the other party to call `dest`. Call it on the leg with the party to transfer. The method returns when the REFER is accepted; wait for the result with [`expectTransferred()`](expecttransferred.md).

PBXs work in one of two ways, and both are supported:

- A B2BUA or hosted PBX handles the REFER itself: it calls the target and reconnects the transferred party's media.
- A proxy passes the REFER to the device of the transferred party. The device then places the new call itself; get it with [`expectReferredCall()`](expectreferredcall.md) on that party's leg.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| dest | [Device](../device/_index.md) or string | | Transfer target: a device, a number or a SIP URI. |
| aon (optional) | string | `'ext'` | With a device: the name of its number to transfer to. |

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the REFER was accepted, `false` if it was refused or the call is not connected. |

### Example

<!-- md-k6:skip -->

```javascript
// B transfers A to C; C must see A as the caller, not B
inc.transfer(C);
const toC = C.expectCall({ caller: A, timeout: '10s' });
if (toC) toC.accept();
check(inc.expectTransferred('10s'), { 'transfer succeeded': (ok) => ok });
inc.hangup();
```
