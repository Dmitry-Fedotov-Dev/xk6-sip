---
title: 'Call.expectTransferred( [timeout] )'
description: 'Call.expectTransferred waits for the result of a transfer.'
weight: 24
---

# Call.expectTransferred( [timeout] )

Waits for the result of a [`transfer()`](transfer.md) or [`attendedTransfer()`](attendedtransfer.md) made from this leg. The result comes in the final NOTIFY from the other side (RFC 3515): a `2xx` status means the transferred party is connected to the target.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| timeout (optional) | duration | `expectTimeout`, `'30s'` | How long to wait: `'5s'`, `'500ms'` or a number of seconds. |

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the transfer succeeded. `false` if it failed, timed out or no transfer was made. |

### Example

<!-- md-k6:skip -->

```javascript
inc.transfer(C);
const toC = C.expectCall({ caller: A });
if (toC) toC.accept();
check(inc.expectTransferred('10s'), { 'transfer succeeded': (ok) => ok });
```
