#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
source scripts/http.sh

temp_dir=$(mktemp -d)
restore_broker=false
cleanup() {
  if "$restore_broker"; then docker compose start rabbitmq >&2; fi
  rm -rf "$temp_dir"
}
trap cleanup EXIT

expect() {
  local want=$1 code=$2
  shift 2
  local actual
  actual=$("$@" -w '%{http_code}' -o "$temp_dir/response.json")
  if [[ $want != "$actual" ]] ||
     { [[ -n $code ]] && ! jq -e --arg code "$code" '.code == $code' "$temp_dir/response.json" >/dev/null; }; then
    printf 'Expected HTTP %s (%s), got %s\n' "$want" "$code" "$actual" >&2
    cat "$temp_dir/response.json" >&2
    return 1
  fi
}

wait_ready
if [[ -n ${MOBIX_API_KEY:-} ]]; then
  code=$(curl --silent --show-error --max-time 12 -w '%{http_code}' -o /dev/null "${base_url}/v1/trips/000000000000000000000001")
  [[ $code == 401 ]]
fi
expect 400 INVALID_INPUT request GET /v1/trips/invalid-id
expect 404 TRIP_NOT_FOUND request GET /v1/trips/000000000000000000000001
expect 400 INVALID_INPUT json POST /v1/trips '{"rider_id":'
expect 400 INVALID_PICKUP json POST /v1/trips '{"rider_id":"check","pickup":{"lat":0},"dropoff":{"lat":0,"lng":0}}'
expect 400 INVALID_PICKUP json POST /v1/trips '{"rider_id":"check","pickup":{"lat":91,"lng":0},"dropoff":{"lat":0,"lng":0}}'
expect 201 '' json POST /v1/trips '{"rider_id":"check","pickup":{"lat":0,"lng":0},"dropoff":{"lat":-90,"lng":180}}'
printf '✓ Request validation and coordinate boundaries\n'

suffix=$(demo_suffix)
driver=$(new_driver "$suffix" --fail | jq -er '.id')
expect 409 DRIVER_ALREADY_EXISTS new_driver "$suffix"
trip=$(new_trip "verify-${suffix}" --fail -H "Idempotency-Key: verify-${suffix}" | jq -er '.id')
same=$(new_trip "verify-${suffix}" --fail -H "Idempotency-Key: verify-${suffix}" | jq -er '.id')
[[ $trip == "$same" ]]
expect 409 IDEMPOTENCY_CONFLICT new_trip another-rider -H "Idempotency-Key: verify-${suffix}"
expect 409 DRIVER_BUSY transition "$trip" assigned "$driver"
expect 422 INVALID_TRIP_STATUS transition "$trip" completed ''
expect 204 '' json PATCH "/v1/drivers/${driver}/availability" '{"status":"available"}'
printf '✓ Duplicate driver and trip retry handling; illegal transitions rejected\n'

other=$(new_trip "other-${suffix}" --fail | jq -er '.id')
transition "$trip" assigned "$driver" -w '%{http_code}' -o "$temp_dir/a.json" > "$temp_dir/a.code" &
a=$!
transition "$other" assigned "$driver" -w '%{http_code}' -o "$temp_dir/b.json" > "$temp_dir/b.code" &
b=$!
wait "$a" "$b"
if [[ $(cat "$temp_dir/b.code") == 200 ]]; then
  winner=$other loser=$trip rejected=a
else
  winner=$trip loser=$other rejected=b
fi
[[ $(cat "$temp_dir/a.code")$(cat "$temp_dir/b.code") =~ ^(200409|409200)$ ]]
jq -e '.code == "DRIVER_ASSIGNED"' "$temp_dir/${rejected}.json" >/dev/null
activity=$(request GET "/v1/drivers/${driver}/activity" --fail)
jq -e --arg trip "$winner" '.active_trip_id == $trip and .availability == "available" and .status == "on_trip" and .completed_trips == 0' <<< "$activity" >/dev/null
expect 200 '' transition "$winner" cancelled ''
expect 404 PAYMENT_NOT_FOUND request GET "/v1/payments/${winner}"
expect 200 '' transition "$loser" assigned "$driver"
expect 200 '' transition "$loser" in_progress ''
printf '✓ Concurrent dispatch permits one active trip; cancellation releases the driver\n'

if [[ ${MOBIX_SKIP_BROKER_TEST:-0} != 1 ]]; then
  docker compose stop rabbitmq
  restore_broker=true
fi
expect 200 '' transition "$loser" completed ''
if "$restore_broker"; then
  expect 404 PAYMENT_NOT_FOUND request GET "/v1/payments/${loser}"
  docker compose restart trip
  wait_ready
  docker compose start rabbitmq
  restore_broker=false
fi
receipt=$(wait_receipt "$loser")
expect 200 '' transition "$loser" completed ''
same_receipt=$(wait_receipt "$loser")
activity=$(request GET "/v1/drivers/${driver}/activity" --fail)
jq -e '.status == "available" and .completed_trips == 1 and (has("active_trip_id") | not)' <<< "$activity" >/dev/null
[[ $receipt == "$same_receipt" ]]
printf '✓ Completed trip returns one stable simulated receipt\n'
