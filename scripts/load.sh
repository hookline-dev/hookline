#!/usr/bin/env bash
set -euo pipefail

base_url="${HOOKLINE_URL:-http://localhost:8080}"
metrics_url="${METRICS_URL:-$base_url/metrics}"
app_id="${APP_ID:?APP_ID is required}"
count="${COUNT:-500}"
parallel="${PARALLEL:-25}"
p95_max_ms="${P95_MAX_MS:-50}"
drain_timeout="${DRAIN_TIMEOUT_SECONDS:-60}"
drain_poll="${DRAIN_POLL_SECONDS:-1}"

for command in curl python3; do
	command -v "$command" >/dev/null 2>&1 || { printf 'missing command: %s\n' "$command" >&2; exit 1; }
done
for value in "$count" "$parallel" "$p95_max_ms" "$drain_timeout"; do
	[[ "$value" =~ ^[1-9][0-9]*$ ]] || { printf 'expected a positive integer, got %s\n' "$value" >&2; exit 1; }
done

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT
run_id="$(date +%s)-$$"
started="$(date +%s)"
export base_url app_id work_dir run_id

seq 1 "$count" | xargs -P "$parallel" -I{} sh -c '
	n="$1"
	curl --silent --show-error \
		--output "$work_dir/body-$n" --write-out "%{http_code} %{time_total}\n" \
		-X POST "$base_url/ingest/$app_id" \
		-H "Content-Type: application/json" \
		-H "X-Event-Type: load.test" \
		-H "Idempotency-Key: load-$run_id-$n" \
		--data "{\"sequence\":$n}" > "$work_dir/meta-$n"
' _ {}

read -r accepted messages duplicates p95_seconds < <(python3 - "$work_dir" "$count" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
expected = int(sys.argv[2])
accepted = messages = duplicates = 0
latencies = []
for number in range(1, expected + 1):
    code, elapsed = (root / f"meta-{number}").read_text().split()
    if code == "202":
        accepted += 1
    latencies.append(float(elapsed))
    body = json.loads((root / f"body-{number}").read_text())
    messages += int(body.get("messagesCreated", 0))
    duplicates += int(bool(body.get("duplicate", False)))
latencies.sort()
p95 = latencies[(len(latencies) * 95 + 99) // 100 - 1]
print(accepted, messages, duplicates, f"{p95:.6f}")
PY
)

if (( accepted != count )); then
	printf 'FAILED: accepted=%d, expected=%d\n' "$accepted" "$count" >&2
	exit 1
fi
if (( messages < count )); then
	printf 'FAILED: created_messages=%d; configure at least one matching active endpoint before the run\n' "$messages" >&2
	exit 1
fi
if (( duplicates != 0 )); then
	printf 'FAILED: duplicate responses=%d\n' "$duplicates" >&2
	exit 1
fi
if ! awk -v actual="$p95_seconds" -v maximum="$p95_max_ms" 'BEGIN { exit !(actual * 1000 <= maximum) }'; then
	printf 'FAILED: p95=%ss exceeds %sms\n' "$p95_seconds" "$p95_max_ms" >&2
	exit 1
fi

queue_counts() {
	curl -fsS "$metrics_url" | awk '
		$1 == "hookline_messages_pending" { pending=$2 }
		$1 == "hookline_messages_in_flight" { in_flight=$2 }
		END {
			if (pending == "" || in_flight == "") exit 2
			printf "%.0f %.0f\n", pending, in_flight
		}'
}

deadline=$((started + drain_timeout))
pending=-1
in_flight=-1
while (( $(date +%s) <= deadline )); do
	if read -r pending in_flight < <(queue_counts) && (( pending == 0 && in_flight == 0 )); then
		break
	fi
	sleep "$drain_poll"
done
completed="$(date +%s)"
elapsed=$((completed - started))
if (( pending != 0 || in_flight != 0 )); then
	printf 'FAILED: queue did not drain in %ss (pending=%s in_flight=%s)\n' "$drain_timeout" "$pending" "$in_flight" >&2
	exit 1
fi
if (( elapsed > drain_timeout )); then
	printf 'FAILED: acceptance and delivery took %ss, limit=%ss\n' "$elapsed" "$drain_timeout" >&2
	exit 1
fi

printf 'PASS accepted=%d created_messages=%d duplicates=%d p95_ms=%.3f pending=%d in_flight=%d total_seconds=%d\n' \
	"$accepted" "$messages" "$duplicates" "$(awk -v value="$p95_seconds" 'BEGIN { print value * 1000 }')" "$pending" "$in_flight" "$elapsed"
