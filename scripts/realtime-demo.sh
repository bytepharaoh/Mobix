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

wait_for_snapshot_state() {
  local trip_id=$1
  local status=$2
  for _ in {1..100}; do
    if awk 'substr($0, 1, 6) == "data: " { print substr($0, 7) }' "$stream_file" |
      jq -se --arg trip "$trip_id" --arg status "$status" \
        'any(.[]; .id == $trip and .status == $status and .version >= 1)' \
        >/dev/null 2>&1; then
      return 0
    fi
    if ! kill -0 "$stream_pid" 2>/dev/null; then
      printf 'SSE stream closed before the %s snapshot for trip %s arrived\n' \
        "$status" "$trip_id" >&2
      cat "$stream_file" >&2
      return 1
    fi
    sleep 0.05
  done
  printf 'Timed out waiting for the %s snapshot for trip %s\n' \
    "$status" "$trip_id" >&2
  cat "$stream_file" >&2
  return 1
}

wait_for_trip_state() {
  local trip_id=$1
  local status=$2
  local version=$3
  for _ in {1..100}; do
    if awk 'substr($0, 1, 6) == "data: " { print substr($0, 7) }' "$stream_file" |
      jq -se --arg trip "$trip_id" --arg status "$status" --argjson version "$version" \
        'any(.[]; .trip_id == $trip and .status == $status and .version == $version)' \
        >/dev/null 2>&1; then
      return 0
    fi
    if ! kill -0 "$stream_pid" 2>/dev/null; then
      printf 'SSE stream closed before trip %s reached %s at version %s\n' \
        "$trip_id" "$status" "$version" >&2
      cat "$stream_file" >&2
      return 1
    fi
    sleep 0.05
  done
  printf 'Timed out waiting for trip %s to reach %s at version %s\n' \
    "$trip_id" "$status" "$version" >&2
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
  "${base_url}/v1/trips/${trip_id}/events" >"$stream_file" 2>/dev/null &
stream_pid=$!
wait_for_stream 'event: snapshot'
wait_for_snapshot_state "$trip_id" requested

cancelled_trip=$(transition "$trip_id" cancelled '')
cancelled_version=$(jq -er '.version' <<< "$cancelled_trip")
jq -e '.status == "cancelled"' <<< "$cancelled_trip" >/dev/null
wait_for_stream "id: ride-event:${trip_id}:${cancelled_version}"
wait_for_trip_state "$trip_id" cancelled "$cancelled_version"

printf '✓ Authorized SSE snapshot and RabbitMQ trip update\n'
