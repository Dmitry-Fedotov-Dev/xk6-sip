---
title: 'Call.accept()'
description: 'Call.accept answers a ringing incoming call.'
weight: 01
---

# Call.accept()

Answers a ringing incoming call with `200 OK` and starts media. It returns at once; use [`expectConnected()`](expectconnected.md) on either leg to wait until the caller has acknowledged the answer.

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the answer was sent. `false` for an outgoing call, or a call that is already answered, rejected or ended. |

### Example

<!-- md-k6:skip -->

```javascript
const inc = B.expectCall({ caller: A });
if (inc) {
  inc.accept();
  check(inc.expectConnected('5s'), { 'B connected': (ok) => ok });
}
```
