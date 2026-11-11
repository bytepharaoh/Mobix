#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
source scripts/http.sh

wait_offer() {
  local driver_token=$1 trip_id=$2 found deadline=$((SECONDS + 90))
  use_token "$driver_token"
  while (( SECONDS < deadline )); do
    if found=$(request GET /v1/drivers/me/offers --fail |
       jq -ce --arg trip "$trip_id" 'first(.[] | select(.trip_id == $trip))'); then
      printf '%s\n' "$found"
      return 0
    fi
    sleep 0.25
  done
  printf 'Timed out waiting for an offer for trip %s\n' "$trip_id" >&2
  return 1
}

printf 'Mobix automatic dispatch demo  ·  %s\n\n' "$base_url"
wait_ready
login_operator
suffix=$(demo_suffix)
password="Mobix-dispatch-${suffix}-${RANDOM}"
rider=$(provision_account rider "$suffix" "${password}-rider")
rider_token=$(jq -er '.access_token' <<< "$rider")
driver_tokens=()
driver_ids=()
cleanup_drivers() {
  for i in 0 1 2 3; do
    if [[ -n ${driver_ids[$i]:-} ]]; then
      use_token "${driver_tokens[$i]}"
      json PATCH "/v1/drivers/${driver_ids[$i]}/availability" '{"status":"offline"}' >/dev/null 2>&1 || true
    fi
  done
}
trap cleanup_drivers EXIT
for i in 0 1 2 3; do
  use_token "$operator_token"
  driver_suffix=$((suffix + i))
  session=$(provision_account driver "$driver_suffix" "${password}-driver-${i}")
  driver_tokens[$i]=$(jq -er '.access_token' <<< "$session")
  use_token "${driver_tokens[$i]}"
  driver_ids[$i]=$(new_driver "$driver_suffix" --fail | jq -er '.id')
  json PATCH "/v1/drivers/${driver_ids[$i]}/availability" '{"status":"available"}' --fail >/dev/null
done

# Refresh all four location timestamps together and place them in distance order.
for i in 0 1 2 3; do
  use_token "${driver_tokens[$i]}"
  location=$(jq -nc --argjson i "$i" '{lat:(37.781 + $i * 0.002),lng:-122.42}')
  json PATCH "/v1/drivers/${driver_ids[$i]}/location" "$location" --fail >/dev/null
done

use_token "$rider_token"
trip_id=$(json POST /v1/trips '{"pickup":{"lat":37.78,"lng":-122.42},"dropoff":{"lat":37.80,"lng":-122.40}}' \
  --fail -H "Idempotency-Key: dispatch-${suffix}" | jq -er '.id')
printf '✓ Rider requested trip %s\n' "$trip_id"
first_id=''
expiry=''
for i in 0 1 2; do
  offer=$(wait_offer "${driver_tokens[$i]}" "$trip_id")
  candidate_expiry=$(jq -er '.expires_at' <<< "$offer")
  if [[ $i == 0 ]]; then
    first_id=$(jq -er '.id' <<< "$offer")
    expiry=$candidate_expiry
  else
    [[ $expiry == "$candidate_expiry" ]]
  fi
done
printf '✓ The three nearest drivers received offers with the same deadline\n'

use_token "${driver_tokens[3]}"
request GET /v1/drivers/me/offers --fail |
  jq -e --arg trip "$trip_id" 'all(.[]; .trip_id != $trip)' >/dev/null
second=$(wait_offer "${driver_tokens[3]}" "$trip_id")
second_id=$(jq -er '.id' <<< "$second")
printf '✓ The first batch expired and the next nearest driver received an offer\n'

use_token "${driver_tokens[0]}"
late=$(request POST "/v1/trips/${trip_id}/offers/${first_id}/accept" -w $'\n%{http_code}')
[[ ${late##*$'\n'} == 409 ]]
jq -e '.code == "OFFER_EXPIRED"' <<< "${late%$'\n'*}" >/dev/null
printf '✓ The expired offer was rejected\n'

use_token "${driver_tokens[3]}"
assigned=$(request POST "/v1/trips/${trip_id}/offers/${second_id}/accept" --fail)
jq -e --arg driver "${driver_ids[3]}" '.status == "assigned" and .driver_id == $driver' <<< "$assigned" >/dev/null
printf '✓ The next driver accepted and was assigned to the ride\n'
transition "$trip_id" in_progress '' --fail >/dev/null
transition "$trip_id" completed '' --fail >/dev/null
use_token "$rider_token"
receipt=$(wait_receipt "$trip_id")
printf '\nRider payment receipt\n'
jq . <<< "$receipt"
