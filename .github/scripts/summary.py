#!/usr/bin/env python3
"""Markdown for the CI run summary page ($GITHUB_STEP_SUMMARY).

  summary.py functional <reports dir> <scenario>...   from the JUnit reports
  summary.py load <summary.json> <title>              from k6 --summary-export
"""
import json
import os
import sys
import xml.etree.ElementTree as ET


def cell(s):
    return str(s).replace("|", "\\|")


def functional(reports, scenarios):
    rows, details = [], []
    for s in scenarios:
        path = os.path.join(reports, f"{s}.xml")
        if not os.path.exists(path):
            rows.append(f"| {s} | ❌ | — | no report: k6 stopped before writing it |")
            continue
        cases = [(c.get("name"), c.find("failure") is None) for c in ET.parse(path).iter("testcase")]
        steps = [c for c in cases if not c[0].startswith("threshold ")]
        failed = [name for name, ok in cases if not ok]
        passed = sum(ok for _, ok in steps)
        ok = not failed
        # a failed step stops the scenario, so it is the first failure
        first = next((n for n, good in steps if not good), failed[0] if failed else "")
        count = f"{len(steps)}" if ok else f"{passed} of {len(steps)} passed"
        rows.append(f"| {s} | {'✅' if ok else '❌'} | {count} | {cell(first)} |")
        table = "\n".join(f"| {'✅' if good else '❌'} | {cell(n)} |" for n, good in cases)
        details.append(
            f"<details{'' if ok else ' open'}><summary>{'✅' if ok else '❌'} {s}</summary>\n\n"
            f"| | Step |\n|---|---|\n{table}\n\n</details>\n"
        )
    bad = sum("| ❌ |" in r for r in rows)
    head = (f"### ❌ Functional: {bad} of {len(rows)} scenarios failed" if bad
            else f"### ✅ Functional: {len(rows)} scenarios passed")
    print(f"{head}\n\n| Scenario | Result | Steps | Failed step |\n|---|---|---|---|")
    print("\n".join(rows) + "\n")
    print("\n".join(details))


def ms(v):
    return f"{v:.2f} ms" if v < 10 else f"{v:.0f} ms"


def pct(v):
    return f"{v * 100:.2f}".rstrip("0").rstrip(".") + "%"


def load(path, title):
    m = json.load(open(path, encoding="utf-8"))["metrics"]
    get = lambda name, key, default=None: m.get(name, {}).get(key, default)
    shown, rows = set(), []

    def row(label, value, metric=None):
        th = m.get(metric, {}).get("thresholds", {}) if metric else {}
        shown.add(metric)
        # summary-export marks a crossed threshold with true
        mark = ("❌" if any(th.values()) else "✅") if th else ""
        rows.append(f"| {label} | {value} | {cell(', '.join(th)) or '—'} | {mark} |")

    calls = get("sip_call_success", "passes", 0) + get("sip_call_success", "fails", 0)
    row("Calls", f"{calls}, {get('sip_call_results', 'rate', 0):.2f} CAPS")
    if "sip_call_success" in m:
        row("Successful calls", pct(get("sip_call_success", "value")), "sip_call_success")
    for metric, label in [("sip_call_setup_time", "Setup time p95"),
                          ("sip_post_dial_delay", "Post-dial delay p95"),
                          ("sip_invite_first_response_time", "First response to INVITE p95")]:
        if metric in m:
            row(label, ms(get(metric, "p(95)")), metric)
    if "rtp_audio_heard" in m:
        row("Legs that heard audio", pct(get("rtp_audio_heard", "value")), "rtp_audio_heard")
    if "rtp_audio_score" in m:
        row("Audio score p95", f"{get('rtp_audio_score', 'p(95)'):.3f}", "rtp_audio_score")
    if "rtp_jitter" in m:
        row("Jitter p95", ms(get("rtp_jitter", "p(95)")), "rtp_jitter")
    if "rtp_packets_received" in m:
        row("RTP packets received / lost",
            f"{get('rtp_packets_received', 'count', 0):,} / {get('rtp_packets_lost', 'count', 0):,}")
    if "sip_retransmissions" in m:
        row("SIP retransmissions", get("sip_retransmissions", "count"), "sip_retransmissions")
    if "dropped_iterations" in m:
        row("Dropped iterations", get("dropped_iterations", "count"), "dropped_iterations")
    if "checks" in m:
        row("Checks", f"{get('checks', 'passes', 0):,} of {get('checks', 'passes', 0) + get('checks', 'fails', 0):,}",
            "checks")
    for metric, v in m.items():  # thresholds on anything not listed above
        if v.get("thresholds") and metric not in shown:
            row(metric, "", metric)

    ths = [crossed for v in m.values() for crossed in v.get("thresholds", {}).values()]
    bad = sum(ths)
    head = (f"### ❌ Load: {bad} of {len(ths)} thresholds crossed" if bad
            else f"### ✅ Load: {len(ths)} thresholds passed")
    print(f"{head}\n\n{title}\n\n| Metric | Value | Threshold | |\n|---|---|---|---|")
    print("\n".join(rows))
    print("\nThe whole dashboard for the run is `dashboard.png` in the **load-report** artifact.\n")


if __name__ == "__main__":
    if len(sys.argv) >= 3 and sys.argv[1] == "functional":
        functional(sys.argv[2], sys.argv[3:])
    elif len(sys.argv) == 4 and sys.argv[1] == "load":
        load(sys.argv[2], sys.argv[3])
    else:
        sys.exit(__doc__)
