---
title: 'Call.remote()'
description: 'Call.remote returns the number of the other party.'
weight: 09
---

# Call.remote()

Returns the other party as a number. For an outgoing leg, it is the dialled number. For an incoming leg, it is the caller number as a phone would display it: the user part of P-Asserted-Identity when the PBX sends it, otherwise of the From header.

### Returns

| Type | Description |
| --- | --- |
| string | The number of the other party. |

### Example

<!-- md-k6:skip -->

```javascript
const inc = B.expectCall({ timeout: '10s' });
if (inc) console.log(`incoming call from ${inc.remote()}`);
```
