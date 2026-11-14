#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
source scripts/http.sh

stream_file=$(mktemp)
stream_pid=
cleanup() {
  if [[ -n $stream_pid ]]; then
    kill "$stream_pid" 2>/dev/null || true
    wait "$stream_pid" 2>/dev/null || true
  fi
  rm -f "$stream_file"
}
trap cleanup EXIT

wait_for_stream() {
  local pattern=$1
  for _ in {1..100}; do
    if grep -Fq "$pattern" "$stream_file"; then return 0; fi
    if ! kill -0 "$stream_pid" 2>/dev/null; then
      cat "$stream_file" >&2
      return 1
    fi
    sleep 0.05
  done
  printf 'Timed out waiting for SSE frame: %s\n' "$pattern" >&2
  cat "$stream_file" >&2
  return 1
}

wait_ready
use_token ''
login_operator
suffix=$(demo_suffix)
password="Mobix-realtime-${suffix}-${RANDOM}"
rider_session=$(provision_account rider "$suffix" "${password}-rider")
rider_token=$(jq -er '.access_token' <<< "$rider_session")
use_token "$rider_token"
trip=$(new_trip --fail)
trip_id=$(jq -er '.id' <<< "$trip")
jq -e '.version == 1 and .status == "requested"' <<< "$trip" >/dev/null

curl --silent --show-error --no-buffer --max-time 15 \
  -H "Authorization: Bearer ${rider_token}" \
  -H 'Accept: text/event-stream' \
  "${MOBIX_URL}/v1/trips/${trip_id}/events" >"$stream_file" 2>/dev/null &
stream_pid=$!
wait_for_stream "id: snapshot:${trip_id}:1"
wait_for_stream 'event: snapshot'

transition "$trip_id" cancelled '' >/dev/null
wait_for_stream "id: ride-event:${trip_id}:2"
wait_for_stream 'event: trip.status_changed'
grep -F 'data: ' "$stream_file" | tail -1 | cut -c7- |
  jq -e --arg trip "$trip_id" '.trip_id == $trip and .status == "cancelled" and .version == 2' >/dev/null

printf '✓ Authorized SSE snapshot and RabbitMQ trip update\n'
