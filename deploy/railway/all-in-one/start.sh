#!/bin/sh
set -eu

: "${DATABASE_URL:?DATABASE_URL must reference the Railway PostgreSQL service}"
: "${JWT_SECRET:?JWT_SECRET must be set to a long random value}"

echo "Applying database migrations..."
if ! ./bin/migrate -path ./db/migrations -database "$DATABASE_URL" up; then
  echo "Database migration failed" >&2
  exit 1
fi

# Internal calls bypass the public gateway and stay inside this container.
export PROMOS_SERVICE_URL="${PROMOS_SERVICE_URL:-http://127.0.0.1:9089}"
export GEO_SERVICE_URL="${GEO_SERVICE_URL:-http://127.0.0.1:9083}"
export NOTIFICATIONS_SERVICE_URL="${NOTIFICATIONS_SERVICE_URL:-http://127.0.0.1:9085}"

pids=""
start_service() {
  name="$1"
  port="$2"
  echo "Starting ${name} on private port ${port}..."
  PORT="$port" SERVICE_NAME="${name}-service" "./bin/${name}" &
  pids="$pids $!"
}

shutdown() {
  echo "Stopping Railway application..."
  # shellcheck disable=SC2086
  kill $pids 2>/dev/null || true
  wait 2>/dev/null || true
}
trap shutdown INT TERM EXIT

start_service auth 9081
start_service mobile 9087
start_service rides 9082
start_service geo 9083
start_service payments 9084

echo "Starting public API gateway on Railway PORT ${PORT:-8080}..."
./bin/railway-gateway &
gateway_pid=$!
pids="$pids $gateway_pid"

# If any critical process exits, terminate the deployment so Railway restarts
# the complete process group. POSIX sh has no portable `wait -n`, so poll the
# small, fixed process list.
while :; do
  for pid in $pids; do
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "A core process exited; requesting a Railway restart" >&2
      exit 1
    fi
  done
  sleep 2
done
