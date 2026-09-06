#!/usr/bin/env bash
set -euo pipefail

base_url="${HOOKLINE_URL:-http://localhost:8080}"
admin_key="${ADMIN_API_KEY:-hk_dev_admin_change_me}"
compose_file="${HOOKLINE_COMPOSE_FILE:-deploy/docker-compose.yml}"
expected_workers="${EXPECTED_WORKERS:-3}"
wait_seconds="${RECOVERY_TIMEOUT_SECONDS:-90}"
compose=(docker compose --env-file .env -f "$compose_file")

for command in curl docker python3; do
	command -v "$command" >/dev/null 2>&1 || { printf 'missing command: %s\n' "$command" >&2; exit 1; }
done
[[ -f .env ]] || { printf 'missing .env; copy .env.example first\n' >&2; exit 1; }

field() { python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
workers_killed=0
restore_workers() {
	if (( workers_killed )); then
		"${compose[@]}" up -d --scale "hookline-worker=$expected_workers" hookline-worker >/dev/null 2>&1 || true
	fi
}
trap restore_workers EXIT

run_id="$(date +%s)-$$"
app="$(curl -fsS -X POST "$base_url/api/v1/apps" -H "Authorization: Bearer $admin_key" -H 'Content-Type: application/json' -d "{\"name\":\"worker-recovery-$run_id\",\"githubWebhookSecret\":\"recovery-github-secret-$run_id\"}")"
app_id="$(printf '%s' "$app" | field id)"
app_key="$(printf '%s' "$app" | field apiKey)"
endpoint="$(curl -fsS -X POST "$base_url/api/v1/apps/$app_id/endpoints" -H "Authorization: Bearer $app_key" -H 'Content-Type: application/json' -d '{"url":"http://sink:9090/hook?delay=8s","secret":"worker-recovery-secret","rateLimitRps":100}')"
endpoint_id="$(printf '%s' "$endpoint" | field id)"
curl -fsS -X POST "$base_url/api/v1/endpoints/$endpoint_id/subscriptions" -H "Authorization: Bearer $app_key" -H 'Content-Type: application/json' -d '{"eventType":"recovery.*"}' >/dev/null
curl -fsS -X POST http://localhost:9090/reset >/dev/null
curl -fsS -X POST "$base_url/ingest/$app_id" -H 'Content-Type: application/json' -H 'X-Event-Type: recovery.crash' -H "Idempotency-Key: recovery-$run_id" -d '{"crash":true}' >/dev/null

message_id="$(curl -fsS "$base_url/api/v1/messages?limit=1" -H "Authorization: Bearer $app_key" | python3 -c 'import json,sys; items=json.load(sys.stdin)["items"]; print(items[0]["id"] if items else "")')"
[[ -n "$message_id" ]] || { printf 'FAILED: recovery message was not created\n' >&2; exit 1; }

claimed=0
for _ in $(seq 1 75); do
	status="$(curl -fsS "$base_url/api/v1/messages/$message_id" -H "Authorization: Bearer $app_key" | field status)"
	if [[ "$status" == "in_flight" ]]; then
		claimed=1
		break
	fi
	sleep 0.2
done
(( claimed )) || { printf 'FAILED: worker did not claim the recovery message\n' >&2; exit 1; }

"${compose[@]}" kill -s SIGKILL hookline-worker >/dev/null
workers_killed=1
"${compose[@]}" up -d --scale "hookline-worker=$expected_workers" hookline-worker >/dev/null
workers_killed=0

deadline=$(( $(date +%s) + wait_seconds ))
status=""
while (( $(date +%s) <= deadline )); do
	status="$(curl -fsS "$base_url/api/v1/messages/$message_id" -H "Authorization: Bearer $app_key" | field status)"
	if [[ "$status" == "delivered" ]]; then
		break
	fi
	sleep 1
done
[[ "$status" == "delivered" ]] || { printf 'FAILED: message status after worker crash is %s\n' "$status" >&2; exit 1; }

receipts="$(curl -fsS http://localhost:9090/received | python3 -c 'import json,sys; print(len(json.load(sys.stdin)))')"
(( receipts >= 1 )) || { printf 'FAILED: sink did not receive the recovered message\n' >&2; exit 1; }
printf 'PASS message=%s status=delivered sink_receipts=%s\n' "$message_id" "$receipts"
