---
title: 'Call.isOnHold()'
description: 'Call.isOnHold reports whether this side put the call on hold.'
weight: 20
---

# Call.isOnHold()

Reports whether this side put the call on hold with [`hold()`](../hold/) and has not resumed it.

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if this side holds the call. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
inc.hold();
check(inc.isOnHold(), { 'B holds the call': (held) => held });
```

{{< /code >}}
