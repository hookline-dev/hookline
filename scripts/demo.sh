#!/usr/bin/env bash
set -euo pipefail
base=${HOOKLINE_BASE_URL:-http://localhost:8080}
key=${ADMIN_API_KEY:-hk_dev_admin_change_me}
field(){ python3 -c 'import json,sys;print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
app=$(curl -fsS -X POST "$base/api/v1/apps" -H "Authorization: Bearer $key" -H 'Content-Type: application/json' -d '{"name":"demo"}')
id=$(printf '%s' "$app"|field id); api=$(printf '%s' "$app"|field apiKey)
ep=$(curl -fsS -X POST "$base/api/v1/apps/$id/endpoints" -H "Authorization: Bearer $api" -H 'Content-Type: application/json' -d '{"url":"http://sink:9090/hook","secret":"whsec_demo","rateLimitRps":5}')
eid=$(printf '%s' "$ep"|field id)
curl -fsS -X POST "$base/api/v1/endpoints/$eid/subscriptions" -H "Authorization: Bearer $api" -H 'Content-Type: application/json' -d '{"eventType":"demo.*"}' >/dev/null
curl -fsS -X POST "$base/ingest/$id" -H 'Content-Type: application/json' -H 'X-Event-Type: demo.created' -H 'Idempotency-Key: demo-1' -d '{"hello":"Hookline"}'
printf '\nDashboard: %s\nAPI key: %s\n' "$base" "$api"
