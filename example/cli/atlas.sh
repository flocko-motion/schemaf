#!/bin/sh
# atlas.sh — run the atlas CLI via `go run` without needing a compiled binary.
# Usage: ./atlas.sh <args>   e.g.  ./atlas.sh compose up ../compose/test.yml
ATLAS_CLI_DIR="$(cd "$(dirname "$0")" && pwd)"
exec go run "$ATLAS_CLI_DIR" "$@"

