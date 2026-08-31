#!/usr/bin/env bash
set -euo pipefail

base_url="${HOOKLINE_URL:-http://localhost:8080}"
app_id="${APP_ID:?APP_ID is required}"
count="${COUNT:-500}"
parallel="${PARALLEL:-25}"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

seq 1 "$count" | xargs -P "$parallel" -I{} sh -c '
  curl --silent --show-error --fail \
    --output /dev/null --write-out "%{time_total}\n" \
    -X POST "'$base_url'/ingest/'$app_id'" \
    -H "Content-Type: application/json" \
    -H "X-Event-Type: load.test" \
    -H "Idempotency-Key: load-{}-$(date +%s%N)" \
    --data "{\"sequence\":{}}" >> "'$work_dir'/times"
'

sort -n "$work_dir/times" > "$work_dir/sorted"
p95_line=$(( (count * 95 + 99) / 100 ))
p95="$(sed -n "${p95_line}p" "$work_dir/sorted")"
printf 'accepted=%s parallel=%s p95_seconds=%s\n' "$count" "$parallel" "$p95"
