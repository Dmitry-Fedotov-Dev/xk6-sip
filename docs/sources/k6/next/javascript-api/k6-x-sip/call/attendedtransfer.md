---
title: 'Call.attendedTransfer( consult )'
description: 'Call.attendedTransfer connects the other party with the party of a consultation call.'
weight: 23
---

# Call.attendedTransfer( consult )

Attended (consultative) transfer. B has a call with A and a second, consultation call with C. `attendedTransfer()` on B's leg with A connects A with C and drops both of B's calls. It sends REFER with a Replaces parameter (RFC 3891) that points to the consultation call.

The method returns when the REFER is accepted; wait for the result with [`expectTransferred()`](../expecttransferred/).

| Parameter | Type | Description |
| --- | --- | --- |
| consult | [Call](../) | The connected consultation call with the transfer target. |

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the REFER was accepted. `false` if it was refused, or if this call or the consultation call is not connected. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
// B holds A, consults C and connects them
inc.hold();
const consult = B.call({ callee: C });
const atC = C.expectCall({ caller: B });
if (atC) atC.accept();
consult.expectConnected('10s');

check(inc.attendedTransfer(consult), { 'REFER accepted': (ok) => ok });
check(inc.expectTransferred('10s'), { 'A connected to C': (ok) => ok });
```

{{< /code >}}
