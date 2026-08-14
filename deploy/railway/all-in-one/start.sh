#!/bin/sh
set -eu

: "${JWT_SECRET:?JWT_SECRET must be set to a long random value}"

# Railway does not automatically share variables between services. Accept its
# common URL aliases, the PG* variables, or the application's DB_* variables so
# linking Postgres works without duplicating every field by hand.
DATABASE_URL="${DATABASE_URL:-${DATABASE_PRIVATE_URL:-${POSTGRES_URL:-${POSTGRESQL_URL:-}}}}"
DB_HOST="${DB_HOST:-${PGHOST:-}}"
DB_PORT="${DB_PORT:-${PGPORT:-5432}}"
DB_USER="${DB_USER:-${PGUSER:-}}"
DB_PASSWORD="${DB_PASSWORD:-${PGPASSWORD:-}}"
DB_NAME="${DB_NAME:-${PGDATABASE:-}}"
DB_SSLMODE="${DB_SSLMODE:-disable}"

if [ -z "$DATABASE_URL" ] && [ -n "$DB_HOST" ] && [ -n "$DB_USER" ] && [ -n "$DB_NAME" ]; then
  DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=${DB_SSLMODE}"
fi

if [ -z "$DATABASE_URL" ]; then
  cat >&2 <<'EOF'
PostgreSQL is not linked to this application.
In Railway: add PostgreSQL, open this app's Variables tab, choose
"Add Reference", and set DATABASE_URL=${{Postgres.DATABASE_URL}}.
EOF
  exit 1
fi

# A DATABASE_URL is enough for the all-in-one deploy. Derive DB_* for the Go
# services when Railway did not also provide individual PG*/DB_* references.
if [ -z "$DB_HOST" ]; then
  db_url_no_scheme=${DATABASE_URL#*://}
  db_authority=${db_url_no_scheme%%/*}
  db_auth=${db_authority%@*}
  db_hostport=${db_authority##*@}
  db_path=${db_url_no_scheme#*/}
  export DB_USER=${db_auth%%:*}
  export DB_PASSWORD=${db_auth#*:}
  export DB_HOST=${db_hostport%%:*}
  export DB_PORT=${db_hostport##*:}
  export DB_NAME=${db_path%%\?*}
else
  export DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME
fi
export DATABASE_URL DB_SSLMODE

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
