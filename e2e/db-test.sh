#!/usr/bin/env bash
# db-test.sh — Run the framework's real-Postgres integration tests against an
# ephemeral database (no consumer project needed). Spins postgres, exports
# DATABASE_URL, runs the gated tests in ./db, and always tears down.
#
# Usage:
#   e2e/db-test.sh                 # run ./db integration tests
#   e2e/db-test.sh -run TestRunSet # pass extra flags through to `go test`
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CONTAINER="schemaf-db-test"

trap 'rc=$?; docker rm -f "$CONTAINER" >/dev/null 2>&1 || true; exit $rc' EXIT

echo "▶ starting ephemeral postgres"
docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
docker run -d --name "$CONTAINER" \
  -e POSTGRES_USER=schemaf -e POSTGRES_PASSWORD=dev -e POSTGRES_DB=schemaf_test \
  -p 127.0.0.1::5432 postgres:17-alpine >/dev/null

echo "▶ waiting for postgres"
until docker exec "$CONTAINER" pg_isready -U schemaf -d schemaf_test >/dev/null 2>&1; do
  sleep 0.5
done

PORT="$(docker inspect --format '{{ (index (index .NetworkSettings.Ports "5432/tcp") 0).HostPort }}' "$CONTAINER")"
export DATABASE_URL="postgres://schemaf:dev@127.0.0.1:${PORT}/schemaf_test?sslmode=disable"
echo "▶ DATABASE_URL=${DATABASE_URL}"

echo "▶ go test ./db/..."
go -C "$REPO_ROOT" test ./db/... "$@"
