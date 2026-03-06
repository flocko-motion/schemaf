#!/bin/bash
# schemaf.sh — project entrypoint. Copy this file next to your schemaf.toml.
set -euo pipefail

if [ ! -f "$(dirname "$0")/schemaf.toml" ]; then
  echo "ERROR: schemaf.toml not found in the same directory as this script."
  echo "       Place schemaf.sh next to your schemaf.toml — see schemaf documentation."
  exit 1
fi

cd "$(dirname "$0")"

CMD="${1:-}"
shift 2>/dev/null || true

case "$CMD" in
  codegen)
    exec go run schemaf.local/base/cmd/schemaf codegen all
    ;;
  test)
    go run schemaf.local/base/cmd/schemaf codegen all
    exec go run schemaf.local/base/cmd/schemaf test "$@"
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
