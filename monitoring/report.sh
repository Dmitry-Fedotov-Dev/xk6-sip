#!/usr/bin/env bash
# Saves the whole xk6-sip Grafana dashboard for one run as a single tall PNG:
# a report to attach to a CI run, a ticket or an article.
#
#   monitoring/report.sh <testid> <from> <to> <out.png> [grafana-url]
#
# from and to are Unix seconds; take a margin of ~30 s around the run.
# Needs Chrome or Chromium (set CHROME to its path if it is not on PATH).
set -euo pipefail

testid=$1 from=$2 to=$3 out=$4 grafana=${5:-http://localhost:3001}
chrome=${CHROME:-$(command -v google-chrome || command -v chromium || command -v chromium-browser || true)}
if [ -z "$chrome" ]; then
  echo "Chrome or Chromium not found; set CHROME" >&2
  exit 1
fi

# Page height from the dashboard layout: a grid unit is 30 px plus an 8 px
# margin; headless Chrome only renders what fits in the window.
dashboard="$(dirname "$0")/grafana/dashboards/xk6-sip.json"
py=$(command -v python3 >/dev/null && python3 -c "" 2>/dev/null && echo python3 || echo python)
height=$("$py" -c 'import json,sys
d = json.load(open(sys.argv[1], encoding="utf-8"))
print(max(p["gridPos"]["y"] + p["gridPos"]["h"] for p in d["panels"]) * 38 + 80)' "$dashboard")

url="$grafana/d/xk6-sip/xk6-sip?orgId=1&kiosk&theme=dark&var-testid=$testid&var-window=30s&from=${from}000&to=${to}000"
"$chrome" --headless=new --disable-gpu --hide-scrollbars --window-size="1600,$height" \
  --run-all-compositor-stages-before-draw --virtual-time-budget=60000 \
  --screenshot="$out" "$url" 2>/dev/null
echo "dashboard for $testid ($(date -u -d "@$from" +%H:%M:%S 2>/dev/null || echo "$from")–$(date -u -d "@$to" +%H:%M:%S 2>/dev/null || echo "$to") UTC) saved to $out"
