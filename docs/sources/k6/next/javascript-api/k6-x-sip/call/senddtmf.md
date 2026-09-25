---
title: 'Call.sendDTMF( digits, [duration] )'
description: 'Call.sendDTMF sends DTMF digits in RTP.'
weight: 14
---

# Call.sendDTMF( digits, [duration] )

Sends DTMF digits as RTP events (RFC 4733), the way phones send key presses to voice menus.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| digits | string | | Digits to send: `0`–`9`, `*`, `#`, `A`–`D`. |
| duration (optional) | number | `100` | Duration of each digit in milliseconds. |

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the digits were sent, `false` if the call is not connected or has no media. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
// IVR: press 1 to reach the sales department
const out = A.call({ callee: '8800' });
out.expectConnected('10s');
out.sendDTMF('1');
check(B.expectCall({ caller: A, timeout: '15s' }), { 'IVR routed to B': (c) => c !== false });
```

{{< /code >}}
