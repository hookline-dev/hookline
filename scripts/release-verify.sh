#!/usr/bin/env bash
set -euo pipefail

[[ -f .env ]] || { printf 'missing .env; run cp .env.example .env first\n' >&2; exit 1; }
for command in curl docker make python3; do
	command -v "$command" >/dev/null 2>&1 || { printf 'missing command: %s\n' "$command" >&2; exit 1; }
done

set -a
# shellcheck disable=SC1091
. ./.env
set +a
export COMPOSE_PROJECT_NAME="${HOOKLINE_RELEASE_PROJECT_NAME:-hookline-release-$$}"
compose=(docker compose --env-file .env -f deploy/docker-compose.yml)
keep_stack="${HOOKLINE_RELEASE_KEEP_STACK:-0}"
cleanup() {
	if [[ "$keep_stack" != "1" ]]; then
		"${compose[@]}" down -v >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

printf '[1/6] lint and unit tests\n'
make lint test
printf '[2/6] integration tests and coverage\n'
make test-integration cover
printf '[3/6] compose stack\n'
"${compose[@]}" config --quiet
"${compose[@]}" up -d --build --wait --scale hookline-worker=3 postgres hookline-api hookline-worker sink prometheus grafana
printf '[4/6] observability\n'
EXPECTED_WORKERS=3 ./scripts/observability-smoke.sh
printf '[5/6] worker crash recovery\n'
EXPECTED_WORKERS=3 ./scripts/worker-recovery.sh
printf '[6/6] performance acceptance\n'

field() { python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
run_id="$(date +%s)-$$"
app="$(curl -fsS -X POST http://localhost:8080/api/v1/apps -H "Authorization: Bearer $ADMIN_API_KEY" -H 'Content-Type: application/json' -d "{\"name\":\"release-load-$run_id\",\"githubWebhookSecret\":\"release-github-secret-$run_id\"}")"
app_id="$(printf '%s' "$app" | field id)"
app_key="$(printf '%s' "$app" | field apiKey)"
endpoint="$(curl -fsS -X POST "http://localhost:8080/api/v1/apps/$app_id/endpoints" -H "Authorization: Bearer $app_key" -H 'Content-Type: application/json' -d '{"url":"http://sink:9090/hook","secret":"release-load-secret","rateLimitRps":10000}')"
endpoint_id="$(printf '%s' "$endpoint" | field id)"
curl -fsS -X POST "http://localhost:8080/api/v1/endpoints/$endpoint_id/subscriptions" -H "Authorization: Bearer $app_key" -H 'Content-Type: application/json' -d '{"eventType":"load.*"}' >/dev/null
APP_ID="$app_id" P95_MAX_MS=50 DRAIN_TIMEOUT_SECONDS=60 ./scripts/load.sh

printf 'PASS: automated v1 release verification completed successfully\n'
