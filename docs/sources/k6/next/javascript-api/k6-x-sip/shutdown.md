---
title: 'shutdown()'
description: 'sip.shutdown hangs up, unregisters and closes all devices.'
weight: 05
---

# shutdown()

Hangs up every active call, unregisters every device (REGISTER with `Expires: 0`) and closes the sockets of the k6 process. Call it in `teardown()`, which runs once after all VUs have finished.

Without `shutdown()` the devices stay registered on the PBX until their registration expires.

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
import sip from 'k6/x/sip';

export function teardown() {
  sip.shutdown();
}
```

{{< /code >}}
