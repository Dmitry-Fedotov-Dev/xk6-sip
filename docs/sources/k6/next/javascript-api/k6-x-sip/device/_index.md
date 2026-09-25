---
title: 'Device'
description: 'Device is a SIP subscriber that registers, makes and receives calls.'
weight: 01
---

# Device

`Device` is one SIP subscriber, like a desk phone or a softphone. It owns one UDP socket that it uses both to send requests and to receive them. It keeps its registration refreshed and makes and receives [calls](../call/).

Create devices in the init context. The constructor does no network I/O: the socket is opened and REGISTER is sent on first use inside a test function. A device lives until [`sip.shutdown()`](../shutdown/) or the end of the test, not only for one iteration, so a VU reuses its devices across iterations.

## Constructor

```javascript
new sip.Device(options)
```

| Parameter | Type | Default | Description |
| --- | --- | --- | --- |
| options.device | string | | Device name used in logs and in the `device` metric tag. |
| options.registrar | string | | Registrar address: `'sip:pbx.example.com:5060'` or `'pbx.example.com:5060'`. Required. Its host is also the domain for numbers that aren't full SIP URIs. |
| options.proxy | string | registrar | Outbound proxy `'host:port'` that all requests go to. |
| options.user | string | | Address of record: `'701@pbx.example.com'`, or `'701'` to use the registrar host as the domain. |
| options.authUser | string | user part of `user` | Username for digest authentication, when it differs from the user part. |
| options.pass | string | | Password for digest authentication. `password` is accepted as an alias. |
| options.expires | duration | `300` | Registration lifetime. The device refreshes the registration before it expires. |
| options.register | boolean | `true` | `false` skips REGISTER, for trunks authorized by IP address or for direct device-to-device calls. |
| options.displayName | string | | Display name in the From header. |
| options.sessionExpires | duration | `0` (off) | Request session timers (RFC 4028) on calls this device makes, and offer them on calls it answers. The device refreshes the session, handles `422 Session Interval Too Small` and hangs up a call whose peer stops refreshing it. |
| options.prack | boolean | `false` | Send `180 Ringing` reliably (RFC 3262) when the caller supports 100rel. Reliable 18x responses from the PBX are always acknowledged with PRACK, whatever this option says. |
| options.media, options.codecs, options.audio, options.heardLevel | | module defaults | Media settings for calls of this device. Refer to [options()](../options/). |
| any other field | string | | A number of the subscriber, for example `ext: '701'`, `onk: '+79101110011'`, `gw_num: '84951234567'`. The field name is up to you; scripts refer to it with `aon` and [`identity()`](identity/). |

## Properties

| Property | Type | Description |
| --- | --- | --- |
| id | string | The device name, `options.device`. |

## Methods

| Method | Waits | Description |
| --- | --- | --- |
| [call( options )](call/) | no | Starts an outgoing call and returns its [Call](../call/), or `false`. |
| [expectCall( [options] )](expectcall/) | yes | Waits for an incoming call that matches the caller and number, and returns it, or `false`. |
| [register()](register/) | yes | Sends REGISTER now. Returns `true` on success. |
| [isRegistered()](isregistered/) | no | Whether the last REGISTER succeeded. |
| [identity( key )](identity/) | no | Returns one of the device numbers, for example `identity('ext')`. |
| [destroy()](destroy/) | yes | Hangs up the device calls, unregisters it and closes its socket. |

## Behavior of a device as a phone

A device answers the network like a real phone would, without script code:

- It answers OPTIONS, INFO and UPDATE with `200 OK`.
- It accepts re-INVITE from the other side, for example hold and resume, and tracks it in [`isRemoteHold()`](../call/isremotehold/).
- When both sides send re-INVITE at once, it retries after `491 Request Pending` as RFC 3261 requires.
- It acknowledges reliable provisional responses with PRACK.
- When it receives REFER, it places the new call itself. The script gets that call with [`expectReferredCall()`](../call/expectreferredcall/).
- It answers an INVITE with Replaces automatically and hangs up the replaced call.
- It rejects an incoming call with `480 Temporarily Unavailable` if the script doesn't claim it within `ringTimeout` (3 minutes by default).

## Example

{{< code >}}

<!-- md-k6:skip -->

```javascript
import sip from 'k6/x/sip';

// Subscriber with three numbers
const A = new sip.Device({
  device: 'phone1',
  registrar: 'sip:pbx.example.com:5060',
  user: '701@pbx.example.com',
  pass: 'secret',
  ext: '701',
  onk: '+79101110011',
  gw_num: '84951110011',
});

// SIP trunk authorized by IP: no REGISTER
const trunk = new sip.Device({ device: 'trunk', registrar: 'sip:sbc.example.com:5060', user: 'trunk', register: false });
```

{{< /code >}}
