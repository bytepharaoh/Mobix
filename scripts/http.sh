# Shared by demo.sh and verify.sh.
base_url=${MOBIX_URL:-http://127.0.0.1:8080}
base_url=${base_url%/}
auth=()

use_token() {
  auth=()
  if [[ -n ${1:-} ]]; then
    auth=(-H "Authorization: Bearer $1")
  fi
}

request() {
  local method=$1 path=$2
  shift 2
  # The default-value expansion supports macOS Bash 3 with set -u and an empty array.
  curl --silent --show-error --max-time 12 -X "$method" "${auth[@]+"${auth[@]}"}" "$@" "${base_url}${path}"
}

json() {
  local method=$1 path=$2 body=$3
  shift 3
  request "$method" "$path" -H 'Content-Type: application/json' --data "$body" "$@"
}

login() {
  local email=$1 password=$2
  shift 2
  json POST /v1/auth/login "$(jq -nc --arg email "$email" --arg password "$password" '
    {email:$email,password:$password}')" "$@"
}

login_operator() {
  local email=${MOBIX_OPERATOR_EMAIL:-${MOBIX_BOOTSTRAP_EMAIL:-operator@mobix.test}}
  local password=${MOBIX_OPERATOR_PASSWORD:-${MOBIX_BOOTSTRAP_PASSWORD:-mobix-local-operator-password}}
  if [[ -z $email || -z $password ]]; then
    printf 'Export MOBIX_OPERATOR_EMAIL and MOBIX_OPERATOR_PASSWORD, or the bootstrap operator credentials, before running this script.\n' >&2
    return 1
  fi
  local session
  session=$(login "$email" "$password" --fail) || return
  jq -e '.account.role == "operator"' <<< "$session" >/dev/null || return
  operator_token=$(jq -er '.access_token' <<< "$session") || return
  use_token "$operator_token"
}

provision_account() {
  local role=$1 suffix=$2 password=$3
  local email="${role}-${suffix}@example.test"
  local account
  account=$(json POST /v1/accounts "$(jq -nc --arg email "$email" --arg password "$password" --arg role "$role" '
    {email:$email,password:$password,role:$role}')" --fail) || return
  jq -e --arg role "$role" '.role == $role and (.id | length > 0)' <<< "$account" >/dev/null || return
  login "$email" "$password" --fail
}

new_driver() {
  local suffix=$1
  shift
  local email="driver-${suffix}@example.test"
  local phone="+1${suffix}"
  local plate="MBX-${suffix}"
  json POST /v1/drivers "$(jq -nc --arg email "$email" --arg phone "$phone" --arg plate "$plate" '
    {name:"Alex Morgan",email:$email,phone:$phone,carNumber:$plate,carModel:"Toyota Prius",
     location:{lat:40.73061,lng:-73.935242}}')" "$@"
}

new_trip() {
  json POST /v1/trips '{"pickup":{"lat":40.73061,"lng":-73.935242},"dropoff":{"lat":40.74844,"lng":-73.98566}}' "$@"
}

transition() {
  local trip_id=$1 status=$2 driver_id=${3:-}
  shift 3
  json PATCH "/v1/trips/${trip_id}/status" "$(jq -nc --arg status "$status" --arg driver "$driver_id" '
    {status:$status} + if $driver == "" then {} else {driver_id:$driver} end')" "$@"
}

wait_ready() {
  local deadline=$((SECONDS + 90))
  until request GET /ready --fail >/dev/null 2>&1; do
    if (( SECONDS >= deadline )); then
      printf 'Gateway readiness timed out at %s\n' "$base_url" >&2
      return 1
    fi
    sleep 1
  done
}

wait_receipt() {
  local trip_id=$1 result deadline=$((SECONDS + 60))
  while (( SECONDS < deadline )); do
    if result=$(request GET "/v1/payments/${trip_id}" --fail 2>/dev/null) &&
       jq -e '.status == "simulated_paid"' <<< "$result" >/dev/null; then
      printf '%s\n' "$result"
      return 0
    fi
    sleep 1
  done
  printf 'Payment receipt timed out for trip %s\n' "$trip_id" >&2
  return 1
}

demo_suffix() {
  printf '2%09d%03d' "$(( $(date +%s) % 1000000000 ))" "$((RANDOM % 1000))"
}
