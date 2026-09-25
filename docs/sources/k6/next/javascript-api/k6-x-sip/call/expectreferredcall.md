---
title: 'Call.expectReferredCall( [timeout] )'
description: 'Call.expectReferredCall returns the call a device made because it was transferred.'
weight: 25
---

# Call.expectReferredCall( [timeout] )

When the PBX passes REFER to the device instead of handling it, the device of the transferred party places the new call itself, like a real phone. `expectReferredCall()` on the transferred party's original leg returns that new call.

With a PBX that handles transfers itself, no new call is made on this side and the method returns `false`.

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| timeout (optional) | duration | `expectTimeout`, `'30s'` | How long to wait: `'5s'`, `'500ms'` or a number of seconds. |

### Returns

| Type | Description |
| --- | --- |
| [Call](../) or `false` | The new outgoing call, or `false` on timeout. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
inc.transfer(C);                        // B transfers A to C
const aNew = out.expectReferredCall('10s');
if (aNew) {
  const toC = C.expectCall({ caller: A });
  if (toC) toC.accept();
  check(aNew.expectConnected('10s'), { 'A connected to C': (ok) => ok });
}
```

{{< /code >}}
