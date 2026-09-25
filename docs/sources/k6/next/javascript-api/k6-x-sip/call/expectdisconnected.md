---
title: 'Call.expectDisconnected( [timeout] )'
description: 'Call.expectDisconnected waits until the call ends.'
weight: 06
---

# Call.expectDisconnected( [timeout] )

Waits until the call ends for any reason: the other side hung up, the PBX dropped it, the call was cancelled or rejected. Use [`howCompleted()`](howcompleted.md) afterwards to check how it ended.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| timeout (optional) | duration | `expectTimeout`, `'30s'` | How long to wait: `'5s'`, `'500ms'` or a number of seconds. |

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the call has ended, `false` on timeout. |

### Example

<!-- md-k6:skip -->

```javascript
// B doesn't answer: A cancels after 20 s of ringing
const out = A.call({ callee: B, timeout: '20s' });
B.expectCall({ caller: A });
check(out.expectDisconnected('25s'), { 'call ended': (ok) => ok });
check(out.howCompleted(), { 'cancelled on no answer': (c) => c.endedBy === 'timeout' && c.status === 487 });
```
