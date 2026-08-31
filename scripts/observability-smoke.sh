#!/usr/bin/env bash
set -euo pipefail

prometheus_url="${PROMETHEUS_URL:-http://localhost:9091}"
grafana_url="${GRAFANA_URL:-http://localhost:3000}"
grafana_user="${GRAFANA_USER:-admin}"
grafana_password="${GRAFANA_ADMIN_PASSWORD:-hookline}"
expected_workers="${EXPECTED_WORKERS:-3}"
attempts="${SMOKE_RETRIES:-30}"

for command in curl python3; do
	command -v "$command" >/dev/null 2>&1 || { printf 'missing command: %s\n' "$command" >&2; exit 1; }
done

retry() {
	local description="$1"
	shift
	for ((n=1; n<=attempts; n++)); do
		if "$@"; then
			return 0
		fi
		sleep 2
	done
	printf 'FAILED: %s\n' "$description" >&2
	return 1
}

prometheus_ready() { curl -fsS "$prometheus_url/-/ready" >/dev/null; }
grafana_ready() { curl -fsS "$grafana_url/api/health" >/dev/null; }

prometheus_api_up() {
	curl -fsS --get --data-urlencode 'query=count(up{job="hookline"} == 1)' "$prometheus_url/api/v1/query" |
		python3 -c 'import json,sys; r=json.load(sys.stdin)["data"]["result"]; raise SystemExit(0 if r and int(float(r[0]["value"][1])) == 1 else 1)'
}

prometheus_workers_up() {
	curl -fsS --get --data-urlencode 'query=count(up{job="hookline-workers"} == 1)' "$prometheus_url/api/v1/query" |
		python3 -c 'import json,sys; expected=int(sys.argv[1]); r=json.load(sys.stdin)["data"]["result"]; raise SystemExit(0 if r and int(float(r[0]["value"][1])) == expected else 1)' "$expected_workers"
}

grafana_dashboard_ready() {
	curl -fsS -u "$grafana_user:$grafana_password" "$grafana_url/api/search?query=Hookline" |
		python3 -c 'import json,sys; raise SystemExit(0 if any("hookline" in item.get("title", "").lower() for item in json.load(sys.stdin)) else 1)'
}

retry "Prometheus did not become ready" prometheus_ready
retry "Grafana did not become ready" grafana_ready
retry "Prometheus does not see the API target" prometheus_api_up
retry "Prometheus does not see exactly $expected_workers healthy worker targets" prometheus_workers_up
retry "Grafana did not provision the Hookline dashboard" grafana_dashboard_ready

printf 'PASS prometheus_api=1 prometheus_workers=%s grafana_dashboard=Hookline\n' "$expected_workers"
