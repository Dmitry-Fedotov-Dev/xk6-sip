---
title: 'Call.hangup()'
description: 'Call.hangup ends a call in any state.'
weight: 03
---

# Call.hangup()

Ends the call, whatever its state:

| State | What is sent |
| --- | --- |
| outgoing, not answered | CANCEL |
| incoming, not answered | `603 Decline` |
| connected | BYE |
| ended | nothing |

If the other side answers at the same moment as CANCEL is sent, the call is ended with BYE. `hangup()` returns after the call has ended, which usually takes one round trip to the PBX.

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the call is ended. |

### Example

<!-- md-k6:skip -->

```javascript
const out = A.call({ callee: B });
// ...
out.hangup();
check(B_leg.expectDisconnected('5s'), { 'B got BYE': (ok) => ok });
check(out.howCompleted(), { 'A hung up': (c) => c.endedBy === 'local' });
```
