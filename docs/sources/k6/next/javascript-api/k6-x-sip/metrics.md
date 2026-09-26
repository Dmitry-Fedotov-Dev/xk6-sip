---
title: 'Metrics'
description: 'Built-in SIP and RTP metrics of k6/x/sip.'
weight: 06
---

# Metrics

The module reports these metrics in addition to the [built-in k6 metrics](https://grafana.com/docs/k6/latest/using-k6/metrics/reference/). Like any k6 metric, they can be used in thresholds and sent to Prometheus, InfluxDB or Grafana Cloud with the standard outputs.

## SIP

| Metric | Type | Tags | Description |
| --- | --- | --- | --- |
| `sip_requests` | Counter | `method`, `status` | SIP requests sent, by method and final status. `status` is `timeout` when no response came. |
| `sip_request_duration` | Trend | `method`, `status` | Time from request to final response. |
| `sip_failed_requests` | Rate | `method` | Share of requests that failed. `401` and `407` authentication challenges are not failures. |
| `sip_registrations` | Counter | | Successful registrations. |
| `sip_call_setup_time` | Trend | | Answered outgoing calls: time from INVITE to `200 OK`. |
| `sip_post_dial_delay` | Trend | | Outgoing calls: time from INVITE to the first 18x response, which is when the caller hears ringback. |
| `sip_call_success` | Rate | `status` | Share of outgoing calls that were answered. `status` is the final INVITE status. Calls this side cancelled before an answer, with `hangup()` or the no-answer timeout, are not counted. |
| `sip_call_duration` | Trend | `ended_by` | Talk time of outgoing calls, from answer to end. `ended_by` is `local`, `remote`, `timeout` or `error`. |
| `sip_invite_delivery_time` | Trend | | Time from the caller device sending INVITE to the callee device receiving it: how long the PBX took to route the call. Reported when `expectCall()` names the caller device. |
| `sip_invite_first_response_time` | Trend | `method` | INVITE sent to its first response, usually `100 Trying`. Until it arrives the INVITE is retransmitted every T1 = 500 ms, doubling. |
| `sip_retransmissions` | Counter | `method`, `kind`, `status` | Messages sent again by the transaction layer because the peer did not answer or acknowledge them: requests (`kind=request`) and final responses (`kind=response`, with `status`). Growth under load is an early sign of overload. |
| `sip_expect_failed` | Counter | `expect` | Expectations that returned `false`. `expect` names which one: `call`, `ringing`, `connected`, `disconnected`, `heard`, `dtmf`, `hold`, `unhold`, `transfer`, `transferred`, `referred call`. |

## RTP

RTP metrics are reported once per call leg, when a connected call ends.

| Metric | Type | Tags | Description |
| --- | --- | --- | --- |
| `rtp_packets_sent` | Counter | `codec`, `direction` | RTP packets sent. |
| `rtp_packets_received` | Counter | `codec`, `direction` | RTP packets received. |
| `rtp_packets_lost` | Counter | `codec`, `direction` | RTP packets lost, from sequence numbers (RFC 3550). |
| `rtp_jitter` | Trend | `codec`, `direction` | Interarrival jitter (RFC 3550). |
| `rtp_audio_heard` | Rate | `codec`, `direction` | Share of legs that heard audio from the other side. Below 1 means one-way or no audio. |

`direction` is `out` for the caller's leg and `in` for the callee's leg.

## Process metrics

With `sip.options({ metricsAddr: '127.0.0.1:6566' })` the k6 process serves these metrics for Prometheus to scrape at `/metrics`, next to the standard `process_*` (CPU seconds, resident memory) and `go_*` (heap, goroutines) metrics. They describe the load generator, not the test, so they carry no k6 tags.

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `xk6sip_network_bytes_total` | Counter | `proto` (`sip`, `rtp`), `direction` (`in`, `out`) | UDP payload on all SIP and RTP sockets of the process, loopback included. |
| `xk6sip_network_packets_total` | Counter | `proto`, `direction` | UDP datagrams on the same sockets. |
| `xk6sip_devices` | Gauge | | Devices with an open socket. |
| `xk6sip_calls_in_progress` | Gauge | | Answered outgoing calls that have not ended. |

## Counters for dashboards

Rate metrics and trend percentiles are cumulative over the whole run in most outputs, so a degradation in the middle of a long test barely moves them. These counters can be turned into per-window rates in Prometheus or InfluxDB.

| Metric | Type | Tags | Description |
| --- | --- | --- | --- |
| `sip_calls` | Counter | `phase` | Answered outgoing calls (`answered`) and their end (`ended`). `answered − ended` is the number of calls in progress. |
| `sip_call_results` | Counter | `result`, `status` | Outgoing calls by outcome: `success`, `failure` or `cancelled` (this side hung up before an answer, by `hangup()` or the no-answer timeout), with the final INVITE status. |
| `rtp_legs` | Counter | `codec`, `direction`, `heard` | Call legs by whether they heard audio (`heard` is `true` or `false`). |

The repository has a ready Prometheus and Grafana stack with a dashboard built on these metrics: refer to [Monitoring](monitoring.md).

With `sip.options({ deviceTag: true })`, every metric also gets a `device` tag with the device name.

## Metrics as operational questions

| Question | Metric |
| --- | --- |
| Does the PBX connect calls, and with which codes does it refuse them? | `sip_call_success` by `status` |
| How fast does the caller hear ringback and the answer? | `sip_post_dial_delay`, `sip_call_setup_time` |
| How long does the PBX take to route a call? | `sip_invite_delivery_time` |
| How many calls went to the wrong place or with a wrong number? | `sip_expect_failed{expect:call}` |
| Do people hear each other? | `rtp_audio_heard` |
| What is the media quality? | `rtp_jitter`, `rtp_packets_lost` |
| Does the registrar keep up? | `sip_request_duration{method:REGISTER}`, `sip_registrations` |

## Example thresholds

<!-- md-k6:skip -->

```javascript
export const options = {
  thresholds: {
    sip_call_success: ['rate>0.99'],
    sip_call_setup_time: ['p(95)<300'],
    sip_post_dial_delay: ['p(95)<200'],
    rtp_audio_heard: ['rate>0.99'],
    rtp_jitter: ['p(95)<20'],
    'sip_request_duration{method:REGISTER}': ['p(95)<200'],
    'sip_expect_failed{expect:call}': ['count==0'],
  },
};
```
