---
title: 'Call.status()'
description: 'Call.status returns the final INVITE status code.'
weight: 08
---

# Call.status()

Returns the final status code of the INVITE: `200` for an answered call, the error code of a rejected call (`404`, `486`, `480`...), or `487` for a cancelled one. While the call has no final response, it returns `0`.

### Returns

| Type | Description |
| --- | --- |
| number | Final status code, or `0` while unknown. |

### Example

<!-- md-k6:skip -->

```javascript
const out = A.call({ callee: '1999' });
out.expectDisconnected('5s');
check(out.status(), { 'unknown number gives 404': (s) => s === 404 });
```
