---
title: 'Call.state()'
description: 'Call.state returns the current state of the call.'
weight: 07
---

# Call.state()

Returns the current state of the call without waiting.

| Value | Meaning |
| --- | --- |
| `calling` | INVITE sent or received, no ringing yet. |
| `ringing` | Outgoing: an 18x response arrived. Incoming: the device is ringing. |
| `connected` | The call was answered. |
| `ended` | The call is over. |

### Returns

| Type | Description |
| --- | --- |
| string | One of `calling`, `ringing`, `connected`, `ended`. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
if (out.state() === 'connected') {
  out.hangup();
}
```

{{< /code >}}
