#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
source scripts/http.sh

printf 'Mobix ride demo  ·  %s\n\n' "$base_url"
wait_ready
login_operator
suffix=$(demo_suffix)
password="Mobix-demo-${suffix}-${RANDOM}"
rider_session=$(provision_account rider "$suffix" "${password}-rider")
driver_session=$(provision_account driver "$suffix" "${password}-driver")
rider_token=$(jq -er '.access_token' <<< "$rider_session")
rider_id=$(jq -er '.account.id' <<< "$rider_session")
driver_token=$(jq -er '.access_token' <<< "$driver_session")
printf '✓ Operator provisioned individual rider and driver accounts\n'

use_token "$driver_token"
driver=$(new_driver "$suffix" --fail)
driver_id=$(jq -er '.id' <<< "$driver")
json PATCH "/v1/drivers/${driver_id}/availability" '{"status":"available"}' --fail >/dev/null
printf '✓ Driver signed in and became available  %s\n' "$driver_id"

use_token "$rider_token"
trip=$(new_trip --fail -H "Idempotency-Key: demo-${suffix}")
trip_id=$(jq -er '.id' <<< "$trip")
jq -e --arg rider "$rider_id" '.status == "requested" and .rider_id == $rider and .pickup != .dropoff' <<< "$trip" >/dev/null
printf '✓ Signed-in rider requested a ride       %s\n' "$trip_id"

use_token "$operator_token"
trip=$(transition "$trip_id" assigned "$driver_id" --fail)
jq -e '.status == "assigned"' <<< "$trip" >/dev/null
printf '✓ Operator assigned the driver\n'

use_token "$driver_token"
for status in in_progress completed; do
  trip=$(transition "$trip_id" "$status" '' --fail)
  jq -e --arg status "$status" '.status == $status' <<< "$trip" >/dev/null
  printf '✓ Assigned driver → %s\n' "$status"
done

use_token "$rider_token"
payment=$(wait_receipt "$trip_id")
fare=$(jq -er '.fare_cents' <<< "$trip")
jq -e --arg id "$trip_id" --argjson fare "$fare" '.trip_id == $id and .amount_cents == $fare' <<< "$payment" >/dev/null
printf '\nRider payment receipt\n'
jq . <<< "$payment"
