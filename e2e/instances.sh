#!/usr/bin/env bash
# instances.sh — e2e for parallel dev instances (INSTALL.md › Parallel dev
# instances): the default instance and instance 1 run side by side from two
# checkouts, each answers on its own ports with its own database, `instances`
# lists both, and `instances rm 1 -y` removes instance 1.
#
# Usage: e2e/instances.sh <project-dir>     (called by build-example.sh)
#
# Needs the ports port..port+3 and port+10..port+13 free (port from
# schemaf.toml, default 8000). A taken port fails the run; nothing is killed.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=runtime.sh
source "$SCRIPT_DIR/runtime.sh"

DIR="$(cd "${1:?usage: e2e/instances.sh <project-dir>}" && pwd)"
cd "$DIR"
NAME=$(grep '^name' schemaf.toml | cut -d= -f2 | tr -d ' "')
PORT=$(grep '^port' schemaf.toml | cut -d= -f2 | tr -d ' ' || true)
PORT=${PORT:-8000}
IPORT=$((PORT + 10))   # instance 1: port + 10 × 1

# The second checkout, e.g. a git worktree: a copy of the project.
WT="${DIR}-i1"
LOGS="$(mktemp -d)"
PIDS=""

say() { echo "  · $*"; }
fail() { echo "✗ $*" >&2; for f in "$LOGS"/*.log; do echo "--- $f" >&2; tail -n 40 "$f" >&2 || true; done; exit 1; }

# stop_dev stops the dev processes (backend + Vite); their containers stay.
stop_dev() {
  local p
  for p in $PIDS; do kill -TERM -- "-$p" 2>/dev/null || true; done
  for p in $PIDS; do wait "$p" 2>/dev/null || true; done
  PIDS=""
}

# remove_stack removes everything compose created for a compose project.
remove_stack() {
  local ids
  ids=$(rt ps -aq --filter "label=com.docker.compose.project=$1"); [ -z "$ids" ] || rt rm -f $ids >/dev/null
  ids=$(rt volume ls -q --filter "label=com.docker.compose.project=$1"); [ -z "$ids" ] || rt volume rm $ids >/dev/null
  ids=$(rt network ls -q --filter "label=com.docker.compose.project=$1"); [ -z "$ids" ] || rt network rm $ids >/dev/null
}

cleanup() {
  rc=$?
  stop_dev
  # The default dev volume must go: the deploy e2e starts production on the
  # same volume name with another DB_PASS.
  remove_stack "$NAME" 2>/dev/null || true
  remove_stack "${NAME}-1" 2>/dev/null || true
  rm -rf "$WT" "$LOGS" "$HOME/.${NAME}/dev/instances"
  exit $rc
}
trap cleanup EXIT

# start_dev <dir> <log> <args...> starts ./schemaf.sh dev in its own process group.
start_dev() {
  local dir="$1" log="$2"
  shift 2
  set -m
  ( cd "$dir" && exec ./schemaf.sh dev "$@" ) </dev/null >"$LOGS/$log.log" 2>&1 &
  PIDS="$PIDS $!"
  set +m
}

# wait_http <url> waits until the URL answers.
wait_http() {
  local i
  for i in $(seq 1 120); do
    curl -fsS -o /dev/null "$1" 2>/dev/null && return 0
    sleep 1
  done
  fail "$1 did not answer"
}

# table_count <dir> <table> prints how many tables of that name the checkout's instance has.
table_count() {
  ( cd "$1" && ./schemaf.sh db query --json "SELECT count(*) AS n FROM pg_tables WHERE tablename = '$2'" 2>/dev/null ) \
    | tr -d ' \n' | sed 's/.*"n":\([0-9]*\).*/\1/'
}

say "a second checkout: ${WT}"
cp -R "$DIR" "$WT"
# A fresh worktree has no frontend packages: dev must install them itself.
rm -rf "$WT/frontend/node_modules"

say "default instance in ${DIR} (ports ${PORT}..$((PORT + 3)))"
start_dev "$DIR" default all
say "instance 1 in ${WT} (ports ${IPORT}..$((IPORT + 3)))"
start_dev "$WT" instance1 all --instance 1

say "both answer on their own ports: backend and Vite"
wait_http "http://127.0.0.1:${PORT}/api/health"
wait_http "http://127.0.0.1:${IPORT}/api/health"
wait_http "http://localhost:$((PORT + 2))/"
wait_http "http://localhost:$((IPORT + 2))/"
say "both backends serve the page through Vite (200, not 502)"
for p in "$PORT" "$IPORT"; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${p}/" || true)
  [ "$code" = 200 ] || fail "GET / on the backend :${p} answered ${code}, want 200"
done
grep -q "frontend packages missing or outdated — running npm ci" "$LOGS/instance1.log" \
  || fail "dev --instance 1 did not install the frontend packages"
[ -e "$WT/frontend/node_modules/.package-lock.json" ] || fail "dev did not install $WT/frontend/node_modules"
grep -q "ports: backend :${IPORT}, Vite :$((IPORT + 2)), Postgres :$((IPORT + 3))" "$LOGS/instance1.log" \
  || fail "dev --instance 1 did not print its ports"

say "the instance sticks to its checkout"
[ "$(cat "$WT/.schemaf/instance")" = 1 ] || fail "$WT/.schemaf/instance does not hold 1"
[ ! -e "$DIR/.schemaf/instance" ] || fail "the default checkout got an instance file"
ERR=$( cd "$WT" && ./schemaf.sh db query "SELECT 1" 2>&1 >/dev/null )
grep -qx "instance 1" <<<"$ERR" || fail "db query in the instance checkout did not print 'instance 1':
$ERR"
if ( cd "$WT" && ./schemaf.sh db query --instance 2 "SELECT 1" ) >/dev/null 2>&1; then
  fail "--instance 2 in an instance-1 checkout must fail"
fi

say "each instance has its own database"
( cd "$DIR" && ./schemaf.sh db query "CREATE TABLE e2e_default_only (x int)" ) >/dev/null 2>&1
[ "$(table_count "$DIR" e2e_default_only)" = 1 ] || fail "the table is missing in the default instance"
[ "$(table_count "$WT" e2e_default_only)" = 0 ] || fail "instance 1 sees the default instance's table"

say "instances lists both"
LIST=$( cd "$DIR" && ./schemaf.sh instances 2>/dev/null )
echo "$LIST" | sed 's/^/    /'
echo "$LIST" | grep -Eq "^default +yes +${PORT} +$((PORT + 2)) +$((PORT + 3)) +-$" || fail "instances: no running default row"
echo "$LIST" | grep -Eq "^1 +yes +${IPORT} +$((IPORT + 2)) +$((IPORT + 3)) +$(cd "$WT" && pwd -P)$" || fail "instances: no running instance 1 row"

say "instances rm 1 -y removes its containers, volume and record"
stop_dev
( cd "$DIR" && ./schemaf.sh instances rm 1 -y ) >/dev/null 2>&1
if rt volume inspect "${NAME}-1-postgres-data" >/dev/null 2>&1; then fail "volume ${NAME}-1-postgres-data survived instances rm"; fi
[ -z "$(rt ps -aq --filter "label=com.docker.compose.project=${NAME}-1")" ] || fail "containers of instance 1 survived instances rm"
[ ! -e "$HOME/.${NAME}/dev/instances/1" ] || fail "$HOME/.${NAME}/dev/instances/1 survived instances rm"
LIST=$( cd "$DIR" && ./schemaf.sh instances 2>/dev/null )
! grep -Eq "^1 " <<<"$LIST" || fail "instances still lists instance 1"
rt volume inspect "${NAME}-postgres-data" >/dev/null 2>&1 || fail "instances rm 1 touched the default volume"

echo "  ✓ instances: default and instance 1 ran side by side, separate, listed, removed"
