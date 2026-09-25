---
title: 'Call.receivedDTMF()'
description: 'Call.receivedDTMF returns the DTMF digits received so far.'
weight: 16
---

# Call.receivedDTMF()

Returns all DTMF digits received on this leg so far, without waiting.

### Returns

| Type | Description |
| --- | --- |
| string | Received digits, or an empty string. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
console.log(`B received: ${inc.receivedDTMF()}`);
```

{{< /code >}}
