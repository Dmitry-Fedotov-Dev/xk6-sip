---
title: 'Call.hold()'
description: 'Call.hold puts the other side on hold.'
weight: 18
---

# Call.hold()

Puts the other side on hold: sends re-INVITE with `a=sendonly` in the SDP and waits for the response. Either side of a call can hold it. If both sides send re-INVITE at the same moment, the call retries after `491 Request Pending`, as RFC 3261 requires.

The other leg sees the hold with [`isRemoteHold()`](../isremotehold/).

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the other side accepted the re-INVITE, `false` if it refused or the call is not connected. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
check(inc.hold(), { 'B put A on hold': (ok) => ok });
check(out.isRemoteHold(), { 'A sees the hold': (held) => held });
check(inc.unhold(), { 'call resumed': (ok) => ok });
```

{{< /code >}}
