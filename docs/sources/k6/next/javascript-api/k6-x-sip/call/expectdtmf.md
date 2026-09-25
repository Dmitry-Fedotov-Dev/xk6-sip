---
title: 'Call.expectDTMF( digits, [timeout] )'
description: 'Call.expectDTMF waits until the given DTMF digits arrive.'
weight: 15
---

# Call.expectDTMF( digits, [timeout] )

Waits until the DTMF digits received on this leg contain `digits`. Use it to check that the PBX passes key presses from one party to the other.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| digits | string | | Expected sequence, for example `'123#'`. |
| timeout (optional) | duration | `expectTimeout`, `'30s'` | How long to wait. |

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the digits arrived, `false` on timeout or if the call has no media. |

### Example

<!-- md-k6:skip -->

```javascript
out.sendDTMF('123#');
check(inc.expectDTMF('123#', '5s'), { 'B got 123#': (ok) => ok });
```
