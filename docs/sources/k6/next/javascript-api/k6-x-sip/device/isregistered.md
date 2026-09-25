---
title: 'Device.isRegistered()'
description: 'Device.isRegistered reports whether the last REGISTER succeeded.'
weight: 04
---

# Device.isRegistered()

Reports whether the last REGISTER of the device succeeded. It doesn't send anything.

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the device is registered. |

### Example

<!-- md-k6:skip -->

```javascript
if (!A.isRegistered()) {
  console.warn(`${A.id} lost its registration`);
}
```
