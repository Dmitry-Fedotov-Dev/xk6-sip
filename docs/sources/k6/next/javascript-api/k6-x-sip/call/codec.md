---
title: 'Call.codec()'
description: 'Call.codec returns the negotiated codec.'
weight: 12
---

# Call.codec()

Returns the codec agreed in the SDP offer and answer.

### Returns

| Type | Description |
| --- | --- |
| string or `null` | `'PCMU'` or `'PCMA'`, or `null` if the call has no media or no answer yet. |

### Example

<!-- md-k6:skip -->

```javascript
const A = new sip.Device({ ...subs[0], codecs: 'PCMA' });
// ...
check(out.codec(), { 'PBX accepted PCMA': (c) => c === 'PCMA' });
```
