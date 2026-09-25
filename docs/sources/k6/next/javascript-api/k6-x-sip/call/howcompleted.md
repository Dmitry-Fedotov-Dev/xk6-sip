---
title: 'Call.howCompleted()'
description: 'Call.howCompleted describes how the call ended.'
weight: 10
---

# Call.howCompleted()

Describes how the call ended. Call it after the call has ended, for example after [`expectDisconnected()`](../expectdisconnected/).

| Field | Type | Description |
| --- | --- | --- |
| endedBy | string | Who ended the call: `local` (this side), `remote` (the other side or the PBX), `timeout` (no-answer or ring timeout), `error` (a network or protocol error). |
| status | number | Final INVITE status, for example `200`, `486`, `487`. |
| reason | string | Reason phrase or error text. |
| duration | number | Talk time in milliseconds, from answer to end. `0` if the call was never answered. |

### Returns

| Type | Description |
| --- | --- |
| object or `null` | The description, or `null` while the call is not over. |

### Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
out.hangup();
const how = out.howCompleted();
check(how, {
  'A hung up': (c) => c.endedBy === 'local',
  'talked at least 5 s': (c) => c.duration >= 5000,
});
```

{{< /code >}}
