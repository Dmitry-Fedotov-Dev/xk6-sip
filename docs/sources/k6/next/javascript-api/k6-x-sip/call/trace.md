---
title: 'Call.trace()'
description: 'Call.trace returns the SIP ladder of the call leg.'
weight: 11
---

# Call.trace()

Returns the SIP ladder of the call leg as text: every SIP message the device sent or received for this call, with the time since the first message and the direction. `->` is sent, `<-` is received. The ladder also shows events of the extension, such as authentication retries, hold and session refreshes.

Print it when a check fails: it shows exactly where the call went wrong, for this call only, without a traffic capture.

```text
+0.000s  -> INVITE sip:1999@test.local SIP/2.0
+0.001s  <- SIP/2.0 407 Proxy Authentication Required
+0.002s  -> INVITE (with credentials)
+0.003s  <- SIP/2.0 404 Not Found
```

A connected call where the other side put this side on hold and resumed:

```text
+0.000s  -> INVITE sip:1002@test.local SIP/2.0
+0.000s  <- SIP/2.0 407 Proxy Authentication Required
+0.000s  -> INVITE (with credentials)
+0.001s  <- SIP/2.0 100 Trying
+0.001s  <- SIP/2.0 180 Ringing
+0.002s  <- SIP/2.0 200 OK
+0.002s  -> ACK
+0.002s  <- INVITE sip:user1@127.0.0.1:59161 SIP/2.0
+0.002s  <- re-INVITE: remote sendonly
+0.002s  -> SIP/2.0 200 OK
+0.002s  <- ACK sip:user1@127.0.0.1:59161 SIP/2.0
+0.002s  <- INVITE sip:user1@127.0.0.1:59161 SIP/2.0
+0.003s  <- re-INVITE: remote sendrecv
+0.003s  -> SIP/2.0 200 OK
+0.003s  <- ACK sip:user1@127.0.0.1:59161 SIP/2.0
+0.003s  -> BYE
+0.003s  <- SIP/2.0 200 OK (BYE)
```

By default the trace keeps only the first line of each message, so it costs almost nothing even under load. To see full messages with headers and SDP, set `trace: true` in [options()](../../options/).

On Windows, the clock has a resolution of about 0.5 ms, so short intervals may show as `0.000s`. Use Linux for precise timings.

### Returns

| Type | Description |
| --- | --- |
| string | The ladder, one message per line. Empty if nothing was sent yet. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
import sip from 'k6/x/sip';
import { check } from 'k6';

sip.options({ trace: __ENV.SIP_TRACE === '1' }); // full messages on demand

export default function () {
  const out = A.call({ callee: B });
  const inc = B.expectCall({ caller: A, timeout: '5s' });
  if (inc) inc.accept();
  if (!check(out, { 'A connected': (c) => c.expectConnected('5s') })) {
    console.warn(out.trace());
    if (inc) console.warn(inc.trace()); // the other leg, as B saw it
  }
}
```

{{< /code >}}
