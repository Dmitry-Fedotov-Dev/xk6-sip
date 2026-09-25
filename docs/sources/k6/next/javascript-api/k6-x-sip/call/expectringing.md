---
title: 'Call.expectRinging( [timeout] )'
description: 'Call.expectRinging waits until the call rings.'
weight: 04
---

# Call.expectRinging( [timeout] )

Waits until the call rings. For an outgoing leg that means a provisional response such as `180 Ringing` has arrived: the caller would hear the ringback tone. The time from INVITE to the first 18x is reported as the `sip_post_dial_delay` metric.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| timeout (optional) | duration | `expectTimeout`, `'30s'` | How long to wait: `'5s'`, `'500ms'` or a number of seconds. |

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the call rings, `false` on timeout or if the call ended first. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
const out = A.call({ callee: B });
B.expectCall({ caller: A });
check(out.expectRinging('3s'), { 'A hears ringback within 3 s': (ok) => ok });
```

{{< /code >}}
