#!/bin/sh
# test.sh — CLI smoke tests for the example project.
set -e

TEST_NAME="cli smoke"
ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
ZEUS="$ROOT_DIR/cli/zeus.sh"

echo "[test] $TEST_NAME"

status=0

if ! "$ZEUS" codegen openapi >/dev/null 2>&1; then
  echo "[fail] codegen openapi"
  status=1
else
  if [ ! -f "$ROOT_DIR/frontend/src/api/api.gen.ts" ]; then
    echo "[fail] openapi output missing: frontend/src/api/api.gen.ts"
    status=1
  else
    echo "[pass] codegen openapi"
  fi
fi

if ! "$ZEUS" codegen sqlc >/dev/null 2>&1; then
  echo "[fail] codegen sqlc"
  status=1
else
  if [ ! -d "$ROOT_DIR/backend/db" ]; then
    echo "[fail] sqlc output missing: backend/db"
    status=1
  else
    echo "[pass] codegen sqlc"
  fi
fi

if [ $status -ne 0 ]; then
  exit 1
fi

echo "[pass] $TEST_NAME"
