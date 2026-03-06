#!/bin/sh
# test.sh — run the schemaf example test suite.
# Usage:
#   ./test.sh              # spins up Postgres + native backend automatically
#   ./test.sh -run CRUD    # filter to specific tests
set -e
cd "$(dirname "$0")"

SCHEMAF="../cli/schemaf.sh"
COMPOSE="../compose/test.yml"
BACKEND_PORT=7001
BACKEND_URL="http://localhost:${BACKEND_PORT}"

# Start Postgres via schemaf ctl
$SCHEMAF ctl start --wait "$COMPOSE"
trap '$SCHEMAF ctl stop '"$COMPOSE" EXIT

# Start the native backend in the background (connects to localhost:7003 per PORTS.md convention)
(cd ../backend && PORT=$BACKEND_PORT go run .) &
BACKEND_PID=$!
trap "kill $BACKEND_PID 2>/dev/null; $SCHEMAF ctl stop $COMPOSE" EXIT

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

TEST_NAME="go tests"
echo "[test] $TEST_NAME"
set +e
go test -v "$@" ./...
status=$?
set -e

if [ $status -ne 0 ]; then
  echo "[fail] $TEST_NAME"
  exit $status
fi

echo "[pass] $TEST_NAME"
