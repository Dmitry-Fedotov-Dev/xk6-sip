---
title: 'Device.destroy()'
description: 'Device.destroy hangs up, unregisters and closes one device.'
weight: 06
---

# Device.destroy()

Hangs up the active calls of the device, unregisters it and closes its socket. The next `call()`, `expectCall()` or `register()` starts the device again with a new socket and a new registration.

Use it to test re-registration or to free a subscriber in the middle of a test. To clean up at the end of a test, use [`sip.shutdown()`](../../shutdown/) instead.

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
import { check } from 'k6';

export default function () {
  A.destroy(); // phone reboot
  check(A.register(), { 'A registered again': (ok) => ok });
}
```

{{< /code >}}
