#!/usr/bin/env bash
set -euo pipefail

if [[ -z "${TEST_DATABASE_URL:-}" ]]; then
  echo "TEST_DATABASE_URL is required" >&2
  exit 2
fi

coverage_dir="$(mktemp -d)"
trap 'rm -rf "$coverage_dir"' EXIT

percentage() {
  go tool cover -func="$1" | awk '/^total:/ {gsub(/%/, "", $3); print $3}'
}

require_at_least() {
  local name="$1" actual="$2" minimum="$3"
  if ! awk -v actual="$actual" -v minimum="$minimum" 'BEGIN {exit !(actual + 0 >= minimum + 0)}'; then
    echo "$name coverage is ${actual}%, expected at least ${minimum}%" >&2
    exit 1
  fi
  printf '%-18s %6s%% (minimum %s%%)\n' "$name" "$actual" "$minimum"
}

go test -tags=integration -coverpkg=./... -coverprofile="$coverage_dir/overall.out" ./...
require_at_least overall "$(percentage "$coverage_dir/overall.out")" 60

go test -tags=integration -coverprofile="$coverage_dir/queue.out" ./internal/queue
require_at_least queue "$(percentage "$coverage_dir/queue.out")" 85

for package in worker signing backoff matcher; do
  go test -coverprofile="$coverage_dir/$package.out" "./internal/$package"
done
require_at_least worker "$(percentage "$coverage_dir/worker.out")" 85
require_at_least signing "$(percentage "$coverage_dir/signing.out")" 100
require_at_least backoff "$(percentage "$coverage_dir/backoff.out")" 100
require_at_least matcher "$(percentage "$coverage_dir/matcher.out")" 100
