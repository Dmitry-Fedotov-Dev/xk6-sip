---
title: 'Call.expectConnected( [timeout] )'
description: 'Call.expectConnected waits until the call is answered.'
weight: 05
---

# Call.expectConnected( [timeout] )

Waits until the call is answered. For an outgoing leg, the time from INVITE to `200 OK` is reported as the `sip_call_setup_time` metric.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| timeout (optional) | duration | `expectTimeout`, `'30s'` | How long to wait: `'5s'`, `'500ms'` or a number of seconds. |

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the call is connected, `false` on timeout or if the call ended first, for example because the PBX rejected it. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
const out = A.call({ callee: B });
const inc = B.expectCall({ caller: A });
inc.accept();
if (!check(out.expectConnected('5s'), { 'A connected': (ok) => ok })) {
  console.warn(out.trace());
}
```

{{< /code >}}
