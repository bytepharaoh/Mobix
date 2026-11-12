#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
source scripts/http.sh

temp_dir=$(mktemp -d)
restore_redis=false
cleanup() {
  if "$restore_redis"; then docker compose start redis >/dev/null; fi
  rm -rf "$temp_dir"
}
trap cleanup EXIT

request_status() {
  local output=$1
  shift
  "$@" --dump-header "$temp_dir/headers" --output "$temp_dir/body" --write-out '%{http_code}' >"$output"
}

wait_redis() {
  local deadline=$((SECONDS + 30))
  until docker compose exec -T redis redis-cli ping 2>/dev/null | grep -q PONG; do
    if (( SECONDS >= deadline )); then
      printf 'Redis health timed out after restart\n' >&2
      return 1
    fi
    sleep 1
  done
}

wait_ready
login_operator
saved_operator_token=$operator_token

suffix=$(demo_suffix)
limited_email="rate-limit-${suffix}@example.test"
for attempt in 1 2 3 4 5; do
  status=$(login "$limited_email" wrong-password --output "$temp_dir/body" --write-out '%{http_code}')
  [[ $status == 401 ]]
done
request_status "$temp_dir/status" login "$limited_email" wrong-password
[[ $(cat "$temp_dir/status") == 429 ]]
jq -e '.code == "RATE_LIMITED"' "$temp_dir/body" >/dev/null
grep -Eiq '^Retry-After: [1-9][0-9]*' "$temp_dir/headers"
printf '✓ Login account quota returns 429 with Retry-After\n'

docker compose stop redis >/dev/null
restore_redis=true
status=$(request GET /ready --output "$temp_dir/body" --write-out '%{http_code}')
[[ $status == 200 ]]
status=$(login "outage-${suffix}@example.test" wrong-password --output "$temp_dir/body" --write-out '%{http_code}')
[[ $status == 503 ]]
jq -e '.code == "RATE_LIMIT_UNAVAILABLE"' "$temp_dir/body" >/dev/null
use_token "$saved_operator_token"
status=$(new_trip --output "$temp_dir/body" --write-out '%{http_code}')
[[ $status == 403 ]]
jq -e '.code == "FORBIDDEN"' "$temp_dir/body" >/dev/null
printf '✓ Redis outage fails login closed, keeps readiness healthy, and lets authenticated traffic reach policy checks\n'

docker compose start redis >/dev/null
wait_redis
restore_redis=false
