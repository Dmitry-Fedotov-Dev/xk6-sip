---
title: 'Device.identity( key )'
description: 'Device.identity returns one of the numbers of a device.'
weight: 05
---

# Device.identity( key )

Returns one of the numbers given to the device constructor, by field name.

| Parameter | Type | Description |
| --- | --- | --- |
| key | string | Field name of the number, for example `'ext'` or `'onk'`. |

### Returns

| Type | Description |
| --- | --- |
| string or `undefined` | The number, or `undefined` if the device has no such field. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
// Call forwarding: *21*<number># sends B's calls to C
B.call({ aon: '*21*' + C.identity('ext') + '#' });
```

{{< /code >}}
