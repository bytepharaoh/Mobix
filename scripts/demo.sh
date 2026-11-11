#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
source scripts/http.sh

printf 'Mobix ride demo  ·  %s\n\n' "$base_url"
wait_ready
printf '✓ Gateway and HTTP services ready\n'
suffix=$(demo_suffix)
driver=$(new_driver "$suffix" --fail)
driver_id=$(jq -er '.id' <<< "$driver")
json PATCH "/v1/drivers/${driver_id}/availability" '{"status":"available"}' --fail >/dev/null
printf '✓ Registered an available driver  %s\n' "$driver_id"

trip=$(new_trip "demo-${suffix}" --fail -H "Idempotency-Key: demo-${suffix}")
trip_id=$(jq -er '.id' <<< "$trip")
jq -e '.status == "requested" and .pickup != .dropoff' <<< "$trip" >/dev/null
printf '✓ Requested a ride                %s\n' "$trip_id"

for status in assigned in_progress completed; do
  assigned_driver=''
  if [[ $status == assigned ]]; then assigned_driver=$driver_id; fi
  trip=$(transition "$trip_id" "$status" "$assigned_driver" --fail)
  jq -e --arg status "$status" '.status == $status' <<< "$trip" >/dev/null
  printf '✓ Trip → %s\n' "$status"
done

payment=$(wait_receipt "$trip_id")
fare=$(jq -er '.fare_cents' <<< "$trip")
jq -e --arg id "$trip_id" --argjson fare "$fare" '.trip_id == $id and .amount_cents == $fare' <<< "$payment" >/dev/null
printf '\nRabbitMQ payment receipt\n'
jq . <<< "$payment"
