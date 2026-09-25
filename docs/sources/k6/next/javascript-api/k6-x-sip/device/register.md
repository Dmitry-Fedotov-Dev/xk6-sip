---
title: 'Device.register()'
description: 'Device.register sends REGISTER now.'
weight: 03
---

# Device.register()

Sends REGISTER now and waits for the result. Calling it is optional: the first `call()` or `expectCall()` registers the device automatically. Call it explicitly to check registration as a test step, or to measure registration separately from calls.

When `registerRate` is set in [options()](../../options/), `register()` waits for its turn.

### Returns

| Type | Description |
| --- | --- |
| boolean | `true` if the registrar accepted the registration. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
import { check } from 'k6';

export default function () {
  check(A.register(), { 'A registered': (ok) => ok });
}
```

{{< /code >}}
