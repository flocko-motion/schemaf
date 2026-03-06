#!/bin/bash
# schemaf.sh — project entrypoint. Copy this file next to your schemaf.toml.
# (Note: In the example project, this is a symlink to the actual schemaf.sh in the base directory)
set -euo pipefail

if [ ! -f "$(dirname "$0")/schemaf.toml" ]; then
  echo "ERROR: schemaf.toml not found in the same directory as this script."
  echo "       Place schemaf.sh next to your schemaf.toml — see schemaf documentation."
  exit 1
fi

cd "$(dirname "$0")"

if [ -f go.work ]; then
  export GOWORK="$(pwd)/go.work"
fi

CMD="${1:-}"
shift 2>/dev/null || true

case "$CMD" in
  codegen)
    exec go run schemaf.local/base/cmd/schemaf codegen all
    ;;
  test)
    go run schemaf.local/base/cmd/schemaf codegen all

    # Start ephemeral test environment (no volumes — all data discarded after tests).
    PROJECT=$(grep '^name' schemaf.toml | cut -d= -f2)
    export DATABASE_URL="postgres://schemaf:dev@localhost:7004/${PROJECT}?sslmode=disable"
    docker compose -f gen/compose.test.yml up -d --wait

    # Run tests, capture exit code, then tear down regardless of result.
    go run schemaf.local/base/cmd/schemaf test "$@"
    TEST_EXIT=$?
    docker compose -f gen/compose.test.yml down
    exit $TEST_EXIT
    ;;
  run)
    exec docker compose -f gen/compose.gen.yml up "$@"
    ;;
  dev)
    exec docker compose -f gen/compose.gen.yml -f gen/compose.dev.yml up "$@"
    ;;
  ""|--help|-h)
    echo "Usage: ./schemaf.sh <command> [args]"
    echo ""
    echo "  codegen    Generate all code (SQL, endpoints, TypeScript API client)"
    echo "  test       Regenerate code, then run all tests [--verbose] [--no-cache]"
    echo "  run        Start production compose setup"
    echo "  dev        Start development compose setup"
    exit 0
    ;;
  *)
    echo "Unknown command: $CMD" >&2
    echo "Run ./schemaf.sh --help for usage." >&2
    exit 1
    ;;
esac
