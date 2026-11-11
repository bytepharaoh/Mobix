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
use_token ''
expect 401 UNAUTHORIZED request GET /v1/auth/me
expect 401 UNAUTHORIZED request GET /v1/trips/000000000000000000000001 \
  -H 'X-Mobix-Identity: forged.operator' -H 'X-Mobix-Role: operator'
use_token invalid-session
expect 401 UNAUTHORIZED request GET /v1/auth/me
use_token ''
login_operator
suffix=$(demo_suffix)
alternate_suffix=$((suffix + 1))
password="Mobix-verify-${suffix}-${RANDOM}"
rider_session=$(provision_account rider "$suffix" "${password}-rider")
other_rider_session=$(provision_account rider "$alternate_suffix" "${password}-other-rider")
driver_session=$(provision_account driver "$suffix" "${password}-driver")
other_driver_session=$(provision_account driver "$alternate_suffix" "${password}-other-driver")
rider_token=$(jq -er '.access_token' <<< "$rider_session")
rider_id=$(jq -er '.account.id' <<< "$rider_session")
other_rider_token=$(jq -er '.access_token' <<< "$other_rider_session")
other_rider_id=$(jq -er '.account.id' <<< "$other_rider_session")
driver_token=$(jq -er '.access_token' <<< "$driver_session")
other_driver_token=$(jq -er '.access_token' <<< "$other_driver_session")
expect 401 UNAUTHORIZED login "rider-${suffix}@example.test" wrong-password
printf '✓ Individual accounts reject missing, forged, and invalid credentials\n'

use_token "$rider_token"
expect 200 '' request GET /v1/auth/me
jq -e --arg id "$rider_id" '.id == $id and .role == "rider"' "$temp_dir/response.json" >/dev/null
expect 403 FORBIDDEN json POST /v1/accounts "$(jq -nc --arg email "blocked-${suffix}@example.test" --arg password "$password" '{email:$email,password:$password,role:"operator"}')"
expect 403 FORBIDDEN new_driver "$suffix"
expect 400 INVALID_INPUT request GET /v1/trips/invalid-id
expect 404 TRIP_NOT_FOUND request GET /v1/trips/000000000000000000000001
expect 400 INVALID_INPUT json POST /v1/trips '{"pickup":'
expect 400 INVALID_INPUT json POST /v1/trips "$(jq -nc --arg rider "$other_rider_id" '{rider_id:$rider,pickup:{lat:0,lng:0},dropoff:{lat:1,lng:1}}')"
expect 400 INVALID_PICKUP json POST /v1/trips '{"pickup":{"lat":0},"dropoff":{"lat":0,"lng":0}}'
expect 400 INVALID_PICKUP json POST /v1/trips '{"pickup":{"lat":91,"lng":0},"dropoff":{"lat":0,"lng":0}}'
expect 201 '' json POST /v1/trips '{"pickup":{"lat":0,"lng":0},"dropoff":{"lat":-90,"lng":180}}'
printf '✓ Request validation and coordinate boundaries\n'

use_token "$driver_token"
expect 404 DRIVER_NOT_FOUND request GET /v1/drivers/me
driver=$(new_driver "$suffix" --fail | jq -er '.id')
expect 200 '' request GET /v1/drivers/me
jq -e --arg id "$driver" '.id == $id' "$temp_dir/response.json" >/dev/null
expect 409 DRIVER_ALREADY_EXISTS new_driver "$suffix"
expect 403 FORBIDDEN new_trip
use_token "$other_driver_token"
expect 404 DRIVER_NOT_FOUND request GET "/v1/drivers/${driver}"
expect 404 DRIVER_NOT_FOUND request GET "/v1/drivers/${driver}/activity"
expect 404 DRIVER_NOT_FOUND json PATCH "/v1/drivers/${driver}/availability" '{"status":"available"}'
expect 404 DRIVER_NOT_FOUND json PATCH "/v1/drivers/${driver}/location" '{"lat":0,"lng":0}'
use_token "$rider_token"
expect 403 FORBIDDEN request GET "/v1/drivers/${driver}"
expect 404 DRIVER_NOT_FOUND request GET "/v1/drivers/${driver}/activity"
printf '✓ Driver profile, availability, and location belong to the signed-in driver\n'

trip=$(new_trip --fail -H "Idempotency-Key: verify-${suffix}")
jq -e --arg rider "$rider_id" '.rider_id == $rider' <<< "$trip" >/dev/null
trip=$(jq -er '.id' <<< "$trip")
same=$(new_trip --fail -H "Idempotency-Key: verify-${suffix}" | jq -er '.id')
[[ $trip == "$same" ]]
expect 409 IDEMPOTENCY_CONFLICT json POST /v1/trips '{"pickup":{"lat":40,"lng":-73},"dropoff":{"lat":41,"lng":-74}}' \
  -H "Idempotency-Key: verify-${suffix}"
expect 403 FORBIDDEN transition "$trip" assigned "$driver"
use_token "$other_rider_token"
expect 404 TRIP_NOT_FOUND request GET "/v1/trips/${trip}" \
  -H "X-Mobix-Account-ID: ${rider_id}" -H 'X-Mobix-Role: operator' -H 'X-Mobix-Identity: forged.operator'
expect 404 TRIP_NOT_FOUND transition "$trip" cancelled ''
other_rider_trip=$(new_trip --fail -H "Idempotency-Key: verify-${suffix}")
jq -e --arg rider "$other_rider_id" '.rider_id == $rider' <<< "$other_rider_trip" >/dev/null
other_rider_trip=$(jq -er '.id' <<< "$other_rider_trip")
[[ $trip != "$other_rider_trip" ]]
expect 200 '' transition "$other_rider_trip" cancelled ''
use_token "$rider_token"
other=$(new_trip --fail | jq -er '.id')
use_token "$operator_token"
expect 403 FORBIDDEN new_trip
expect 409 DRIVER_BUSY transition "$trip" assigned "$driver"
expect 403 FORBIDDEN json PATCH "/v1/drivers/${driver}/availability" '{"status":"available"}'
use_token "$driver_token"
expect 404 TRIP_NOT_FOUND transition "$trip" assigned "$driver"
expect 204 '' json PATCH "/v1/drivers/${driver}/availability" '{"status":"available"}'
printf '✓ Trip ownership and create retries are isolated between riders\n'

use_token "$operator_token"
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
expect 403 FORBIDDEN transition "$loser" in_progress ''
expect 403 FORBIDDEN transition "$loser" completed ''
use_token "$rider_token"
expect 200 '' request GET "/v1/trips/${loser}"
expect 403 FORBIDDEN transition "$loser" in_progress ''
use_token "$other_driver_token"
expect 404 TRIP_NOT_FOUND request GET "/v1/trips/${loser}"
expect 404 TRIP_NOT_FOUND transition "$loser" in_progress ''
use_token "$driver_token"
expect 200 '' request GET "/v1/trips/${loser}"
expect 422 INVALID_TRIP_STATUS transition "$loser" completed ''
expect 200 '' transition "$loser" in_progress ''
printf '✓ Operator dispatch is atomic; only the assigned driver starts the trip\n'

if [[ ${MOBIX_SKIP_BROKER_TEST:-0} != 1 ]]; then
  docker compose stop rabbitmq
  restore_broker=true
fi
expect 200 '' transition "$loser" completed ''
use_token "$operator_token"
if "$restore_broker"; then
  expect 404 PAYMENT_NOT_FOUND request GET "/v1/payments/${loser}"
  docker compose restart trip
  wait_ready
  docker compose start rabbitmq
  restore_broker=false
fi
use_token "$rider_token"
receipt=$(wait_receipt "$loser")
use_token "$driver_token"
expect 200 '' transition "$loser" completed ''
expect 403 FORBIDDEN request GET "/v1/payments/${loser}"
use_token "$other_driver_token"
expect 404 TRIP_NOT_FOUND transition "$loser" completed ''
expect 403 FORBIDDEN request GET "/v1/payments/${loser}"
use_token "$other_rider_token"
expect 404 TRIP_NOT_FOUND request GET "/v1/trips/${loser}"
expect 404 '' request GET "/v1/payments/${loser}"
expect 404 TRIP_NOT_FOUND transition "$loser" completed ''
use_token "$operator_token"
expect 403 FORBIDDEN transition "$loser" completed ''
same_receipt=$(wait_receipt "$loser")
[[ $receipt == "$same_receipt" ]]
activity=$(request GET "/v1/drivers/${driver}/activity" --fail)
jq -e '.status == "available" and .completed_trips == 1 and (has("active_trip_id") | not)' <<< "$activity" >/dev/null
printf '✓ Completed trips and receipts retain account permissions after retries and recovery\n'

use_token "$other_rider_token"
expect 204 '' request POST /v1/auth/logout
expect 401 UNAUTHORIZED request GET /v1/auth/me
printf '✓ Signing out revokes the individual session\n'
