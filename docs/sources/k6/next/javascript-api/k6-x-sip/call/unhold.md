---
title: 'Call.unhold()'
description: 'Call.unhold resumes a held call.'
weight: 19
---

# Call.unhold()

Resumes a call that this side put on hold: sends re-INVITE with `a=sendrecv` and waits for the response.

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the other side accepted the re-INVITE. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
inc.hold();
sleep(2);
check(inc.unhold(), { 'call resumed': (ok) => ok });
check(out.isHeard('3s'), { 'A hears B again': (ok) => ok });
```

{{< /code >}}
