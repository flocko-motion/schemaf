#!/bin/bash
# Test script: runs schemaf init and verifies the scaffolded project works.
set -euo pipefail

cd "$(dirname "$0")"

rm -rf build/test-init
mkdir -p build/test-init
cd build/test-init

echo "=== Running schemaf init ==="
go run ../../cmd/schemaf init mynumber

echo ""
echo "=== Checking generated files ==="
cd mynumber
test -f schemaf.toml        && echo "  schemaf.toml ✓"
test -f go.work              && echo "  go.work ✓"
test -f go/main.go           && echo "  go/main.go ✓"
test -f go/api/number.go     && echo "  go/api/number.go ✓"
test -f go/db/migrations/0001_number.sql && echo "  migration ✓"
test -f go/db/queries/number.sql         && echo "  queries ✓"
test -f schemaf.sh           && echo "  schemaf.sh ✓"
test -f compose.gen.yml      && echo "  compose.gen.yml ✓"
test -f frontend/src/App.tsx && echo "  frontend/src/App.tsx ✓"

echo ""
echo "=== Building Go project ==="
cd go
go build ./...
echo "  build ✓"

echo ""
echo "=== PASS ==="
