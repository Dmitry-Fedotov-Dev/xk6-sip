---
title: 'Call.isRemoteHold()'
description: 'Call.isRemoteHold reports whether the other side put this side on hold.'
weight: 21
---

# Call.isRemoteHold()

Reports whether the other side, or the PBX on its behalf, put this leg on hold: the last re-INVITE received had `a=sendonly` or `a=inactive`.

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if this side is held. |

### Example

<!-- md-k6:skip -->

```javascript
inc.hold();
check(out.isRemoteHold(), { 'A is on hold': (held) => held });
```
