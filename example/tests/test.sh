#!/bin/sh
# test.sh — run the atlas-base example test suite.
# Usage:
#   ./test.sh              # spins up Postgres + native backend automatically
#   ./test.sh -run CRUD    # filter to specific tests
set -e
cd "$(dirname "$0")"

ATLAS="../cli/atlas.sh"
COMPOSE="../compose/test.yml"
BACKEND_PORT=7001
BACKEND_URL="http://localhost:${BACKEND_PORT}"

# Start Postgres via atlas compose
$ATLAS compose up --wait "$COMPOSE"
trap '$ATLAS compose down '"$COMPOSE" EXIT

# Start the native backend in the background (connects to localhost:7003 per PORTS.md convention)
(cd ../backend && PORT=$BACKEND_PORT go run .) &
BACKEND_PID=$!
trap "kill $BACKEND_PID 2>/dev/null; $ATLAS compose down $COMPOSE" EXIT

# Wait for backend to be ready (up to 30s)
echo "waiting for backend on $BACKEND_URL ..."
for i in $(seq 1 30); do
  if curl -sf "$BACKEND_URL/api/health" >/dev/null 2>&1; then
    echo "backend ready"
    break
  fi
  sleep 1
done

export BACKEND_URL
go test -v "$@" ./...
