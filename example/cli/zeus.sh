#!/bin/sh
# zeus.sh — run the project CLI via `go run` without needing a compiled binary.
# Usage: ./zeus.sh <args>   e.g.  ./zeus.sh ctl start ../compose/test.yml
ZEUS_CLI_DIR="$(cd "$(dirname "$0")" && pwd)"
exec go run "$ZEUS_CLI_DIR" "$@"
