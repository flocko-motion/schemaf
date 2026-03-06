#!/bin/sh
# schemaf.sh — run the project CLI via `go run` without needing a compiled binary.
# Usage: ./schemaf.sh <args>   e.g.  ./schemaf.sh ctl start ../compose/test.yml
SCHEMAF_CLI_DIR="$(cd "$(dirname "$0")" && pwd)"
exec go run "$SCHEMAF_CLI_DIR" "$@"
