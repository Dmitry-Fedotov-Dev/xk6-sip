---
title: 'Call.reject( [code], [reason] )'
description: 'Call.reject declines a ringing incoming call.'
weight: 02
---

# Call.reject( [code], [reason] )

Declines a ringing incoming call with a final error response. Use it to test busy, do-not-disturb and forwarding scenarios on the PBX.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| code (optional) | number | `603` | Status code from 300 to 699, for example `486` for busy. Other values are replaced with `603`. |
| reason (optional) | string | `'Rejected'` | Reason phrase of the response, for example `'Busy Here'`. |

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the response was sent. `false` for an outgoing call, or a call that is already answered, rejected or ended. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
// B is busy: the PBX must forward the call to C
const out = A.call({ callee: B });
const busy = B.expectCall({ caller: A });
if (busy) busy.reject(486, 'Busy Here');
check(C.expectCall({ caller: A, timeout: '10s' }), { 'forwarded on busy': (c) => c !== false });
```

{{< /code >}}
