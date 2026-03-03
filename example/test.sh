#!/bin/sh
# test.sh — run all example test suites.
set -e

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT_DIR"

status=0

for t in tests/test.sh tests/*/test.sh; do
  if [ -f "$t" ]; then
    echo "[test] $t"
    if sh "$t"; then
      echo "[pass] $t"
    else
      echo "[fail] $t"
      status=1
    fi
    echo ""
  fi
done

exit $status
