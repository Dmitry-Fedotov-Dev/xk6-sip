---
title: 'Monitoring'
description: 'Prometheus and Grafana stack with a ready dashboard for xk6-sip runs.'
weight: 07
---

# Monitoring

The repository has a Prometheus and Grafana stack in `monitoring/` with a ready dashboard for xk6-sip runs. It shows, while the test is running, whether the PBX connects calls and how fast, whether people hear each other, whether the registrar keeps up, and whether the load generator itself is the limit.

k6 pushes metrics to Prometheus with remote write every 5 seconds; Grafana reads them with PromQL. Nothing needs to scrape the load generator.

## Quick start

```bash
# 1. Start Prometheus and Grafana
docker compose -f monitoring/docker-compose.yml up -d

# 2. Run a test that sends metrics to Prometheus
K6_PROMETHEUS_RW_SERVER_URL=http://localhost:9091/api/v1/write \
K6_PROMETHEUS_RW_TREND_AS_NATIVE_HISTOGRAM=true \
./k6 run -o experimental-prometheus-rw \
  --tag testid=run-1 --tag pbx_version=4.2.1 examples/call.js

# 3. Stop the stack; data stays in the volumes
docker compose -f monitoring/docker-compose.yml down
```

| Service | Address | Access |
| --- | --- | --- |
| Grafana | http://localhost:3001 | No login (anonymous admin); the dashboard is the home page. |
| Prometheus | http://localhost:9091 | No auth; k6 pushes metrics here. |

Both ports are bound to 127.0.0.1. The stack is meant for a workstation: on a shared server, disable anonymous access and set an admin password. The compose project is named `xk6-sip-monitoring` and uses ports 3001 and 9091, so it can run next to another Prometheus and Grafana on the same machine.

## Tags and variables

| Tag or variable | Purpose |
| --- | --- |
| `--tag testid=...` | Name of the run. Pick one or several runs in the **Test run** list. |
| `--tag pbx_version=...` | Version of the PBX under test. Pick versions in the **PBX version** list to compare them. |
| **Window** (30s, 1m, 5m) | Window for rates, shares and percentiles. A short window shows spikes sooner, a long one smooths noise. |

## Why native histograms and counters

When k6 sends metrics to Prometheus, trend percentiles and Rate metrics are cumulative since the start of the test: ten bad minutes after an hour of good calls barely move them. The dashboard therefore uses:

- **native histograms** for timings (`K6_PROMETHEUS_RW_TREND_AS_NATIVE_HISTOGRAM=true`), so Prometheus computes p50, p95 and p99 per window;
- **counters** for shares: `sip_calls`, `sip_call_results` and `rtp_legs`. Refer to [Metrics](metrics.md).

## Dashboard

22 panels in six rows. Every panel has a description behind the ⓘ icon next to its title.

| Row | Question | Panels |
| --- | --- | --- |
| Overview | The state of the run in five seconds | CAPS, calls in progress, call success, one-way audio, setup time p95, dropped iterations |
| Signalling | Does the PBX connect calls, and how fast? | Calls per second by final status, call setup time p50/p95/p99, post-dial delay, INVITE routing time, failed calls by status |
| Media | Do people hear each other? | One-way audio, jitter p95 by leg, RTP packet loss, RTP packets per second |
| Registrations and requests | Does the registrar keep up? | Registrations by status with cumulative p95, failed requests by method |
| Scenario | What went wrong from the test's point of view? | Failed expectations by type, calls ending per second by who hung up |
| Generator health | Is it the PBX or the test rig? | VUs and calls in progress, dropped iterations, iteration duration |

## Reading the dashboard

| Situation | What you see | What to do |
| --- | --- | --- |
| The PBX is overloaded | Setup time p95/p99 and post-dial delay grow; 503 or 480 appear by final status; call success drops | The capacity is the CAPS at which setup time stopped being flat. Record it for this PBX version. |
| One-way audio | One-way audio is red while call success is green; `heard` grows in failed expectations | Check jitter by leg and the `direction` tag to see which way audio is missing; check NAT and the media server. |
| Calls go to the wrong place | `call` grows in failed expectations, status codes may still be 200 | Check the dial plan and caller ID rules; print `call.trace()` in the script to see where the INVITE went. |
| The PBX drops calls | The `remote` or `error` share grows in calls ending by who hung up, although the script hangs up itself | Look at session timers and channel limits on the PBX. |
| The registrar can't keep up | Codes other than 200 and 401 appear in registrations; p95 grows; REGISTER shows up in failed requests | Lower `registerRate` in [options()](options.md) or record the registrar's limit. |
| The generator is the limit | Dropped iterations above 0; CAPS below the target; VUs at the maximum | Raise `preAllocatedVUs` (CAPS × call duration). Don't trust this run's results. |
| A new PBX version degraded | At the same load profile, setup time p95 or the failure share is higher than for the previous version | Select both versions in **PBX version** and compare. |

The dashboard shows the whole picture. For a single call, print [`call.trace()`](call/trace.md) when a check fails.

## Limitations

- Per-second rates are averages over the window and are fractional: 206 calls a minute is 3.4 per second. Graphs aren't rounded, so rare failures such as 0.06 calls per second stay visible.
- k6 sends a counter only when it changes. REGISTER comes in a burst at start and on refresh, so registrations are shown cumulatively.
- RTP counters are reported when a call ends, so packet rates and loss lag behind by the call duration.
- Calls in progress is `answered − ended`. If an iteration ends before its call does, the end event can be lost and the number drifts up. The examples always wait for `expectDisconnected()`.
- On Windows, Go's clock ticks in about 0.5 ms steps; measure precise timings on Linux.
- CPU and memory of the k6 process are not collected; add node-exporter or a similar agent on the load generator host.
