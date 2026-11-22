#!/usr/bin/env bash
# example-pull.sh — a new box pulls the real releases of
# flocko-motion/schemaf-example from GitHub (EXTEND.md › The example release).
#
# The box is set up as INSTALL.md › Pulling releases instructs, as it would
# have been when v-1 (the release before <version>) was the newest: the env
# file and the example repository's schemaf.sh at v-1's tag. Then:
#   1. releases + install: the first install of v-1;
#   2. releases + install <version>: the upgrade, done by v-1's run-side script.
# Both against the example's real attestations (SCHEMAF_SIGNER is the
# example's repository). For step 1 the box reads a copy of the real index
# that ends at v-1, served locally; for step 2 the real releases.json.
#
# Usage: e2e/example-pull.sh <version>       (CI: .github/workflows/example-pull.yml)
# Needs Linux, the container runtime (SCHEMAF_RUNTIME), a real run user
# (E2E_RUN_USER, reached through passwordless sudo, with lingering under
# podman), curl, jq, python3 and openssl.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=runtime.sh
source "$SCRIPT_DIR/runtime.sh"

EXAMPLE_REPO="${EXAMPLE_REPO:-flocko-motion/schemaf-example}"
NAME="${EXAMPLE_REPO##*/}"
INDEX_URL="https://github.com/$EXAMPLE_REPO/releases/download/schemaf-index/releases.json"

say() { echo "  · $*"; }
fail() { echo "✗ example pull: $*" >&2; exit 1; }

VERSION="${1:-}"
[ -n "$VERSION" ] || fail "usage: e2e/example-pull.sh <version>"
[ "$(uname)" = Linux ] || fail "the pull flow runs on Linux only"
RUN_USER="${E2E_RUN_USER:-}"
[ -n "$RUN_USER" ] || fail "set E2E_RUN_USER to the run user (a real Unix user, reached through sudo)"
id -u "$RUN_USER" >/dev/null 2>&1 || fail "user '$RUN_USER' does not exist"
sudo -n true 2>/dev/null || fail "needs passwordless sudo to act as $RUN_USER"
for tool in curl jq python3 openssl; do command -v "$tool" >/dev/null || fail "$tool is required"; done

RUN_HOME="$(getent passwd "$RUN_USER" | cut -d: -f6)"
RUN_SH="$RUN_HOME/.${NAME}/bin/schemaf.sh"
WORK="$(mktemp -d)"
chmod 755 "$WORK"
SERVER=""

# as_runner <cmd...>: as the run user, with its HOME and rootless runtime dir.
as_runner() {
  local uid bus=()
  uid=$(id -u "$RUN_USER")
  if sudo -n test -S "/run/user/$uid/bus"; then bus=("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$uid/bus"); fi
  ( cd / && sudo -n -H -u "$RUN_USER" env XDG_RUNTIME_DIR="/run/user/$uid" "${bus[@]}" \
      SCHEMAF_RUNTIME="$RUNTIME" PODMAN_COMPOSE_WARNING_LOGS=false "$@" )
}

# The container CLI in the run user's store (test plumbing).
rt_runner() { as_runner "$RUNTIME" "$@"; }

cleanup() {
  local rc=$?
  [ -z "$SERVER" ] || kill "$SERVER" 2>/dev/null || true
  as_runner "$RUN_SH" stop >/dev/null 2>&1 || true
  rt_runner volume rm "${NAME}-postgres-data" >/dev/null 2>&1 || true
  rm -rf "${WORK:?}"
  exit $rc
}
trap cleanup EXIT

echo "▶ example pull: $EXAMPLE_REPO, a new box installs v-1, then upgrades to $VERSION ($RUNTIME)"

say "the example's releases.json"
curl -fsSL "$INDEX_URL" -o "$WORK/real.json" || fail "could not download $INDEX_URL"
CHANNEL=stable
case "$VERSION" in *-*) CHANNEL=prerelease ;; esac
jq -e --arg v "$VERSION" 'map(.version) | index($v)' "$WORK/real.json" >/dev/null \
  || fail "$VERSION is not in $INDEX_URL — release it first (e2e/example-release.sh)"
# Semver precedence in jq: X.Y.Z numerically; a prerelease before its release;
# prerelease identifiers left to right, numeric ones numerically and before
# alphanumeric ones, fewer identifiers first.
SEMVER='def semver: capture("^(?<a>[0-9]+)\\.(?<b>[0-9]+)\\.(?<c>[0-9]+)(-(?<p>.*))?$")
  | [(.a | tonumber), (.b | tonumber), (.c | tonumber),
     (if .p == null then [1] else [0, (.p | split(".") | map(if test("^[0-9]+$") then [0, tonumber, ""] else [1, 0, .] end))] end)];'
# v-1: the newest version on the channel before <version>.
PREV=$(jq -r --arg ch "$CHANNEL" --arg v "$VERSION" "$SEMVER"'
  ($v | semver) as $target
  | map(.version | select($ch == "prerelease" or (contains("-") | not)) | select(semver < $target))
  | sort_by(semver) | last // empty' "$WORK/real.json")
[ -n "$PREV" ] || fail "no release of the example before $VERSION — the first release is part of the example's one-time setup (EXTEND.md › The example release)"
say "v-1 is $PREV"

PORT=$(curl -fsSL "https://raw.githubusercontent.com/$EXAMPLE_REPO/v$PREV/schemaf.toml" | sed -n 's/^port *= *//p' | tr -d ' "')
PORT=${PORT:-8000}
HPORT=$((PORT + 19))

# The index as it was when v-1 was the newest: the entries up to v-1.
jq --arg prev "$PREV" "$SEMVER"'($prev | semver) as $p | map(select((.version | semver) <= $p))' \
  "$WORK/real.json" > "$WORK/releases.json"
chmod 644 "$WORK/releases.json"
python3 -m http.server --bind 127.0.0.1 --directory "$WORK" "$HPORT" >/dev/null 2>&1 &
SERVER=$!

# set_env <releases url>: ~/.<name>/etc/env as INSTALL.md instructs.
set_env() {
  printf 'DB_PASS=%s\nSCHEMAF_RUNTIME=%s\nSCHEMAF_RELEASES=%s\nSCHEMAF_SIGNER=%s\nSCHEMAF_CHANNEL=%s\n' \
    "$DB_PASS" "$RUNTIME" "$1" "$EXAMPLE_REPO" "$CHANNEL" > "$WORK/env"
  chmod 644 "$WORK/env"
  as_runner bash -c 'mkdir -p "$1" && umask 077 && cat "$2" > "$1/env"' _ "$RUN_HOME/.${NAME}/etc" "$WORK/env"
  rm -f "$WORK/env"
}

health() { # <version>
  local i
  for i in $(seq 1 60); do
    if curl -fsS "http://127.0.0.1:${PORT}/api/health" 2>/dev/null | grep -q '"status":"ok"'; then
      [ "$(rt_runner container inspect --format '{{.Config.Image}}' "${NAME}-backend" | sed 's/.*://')" = "$1" ] \
        || fail "the backend doesn't run $1"
      say "$1 answers on :$PORT"
      return 0
    fi
    sleep 1
  done
  fail "$1 does not answer on :$PORT/api/health"
}

say "set up the box as INSTALL.md instructs (env file, the example's schemaf.sh at v$PREV)"
as_runner rm -rf "${RUN_HOME:?}/.${NAME:?}"
DB_PASS=$(openssl rand -hex 16)
set_env "http://127.0.0.1:${HPORT}/releases.json"
as_runner bash -c 'mkdir -p "$(dirname "$1")" && curl -fsSL "$2" -o "$1" && chmod 755 "$1"' _ "$RUN_SH" \
  "https://raw.githubusercontent.com/$EXAMPLE_REPO/v$PREV/schemaf.sh"

say "first install: releases, install $PREV"
out=$(as_runner "$RUN_SH" releases) || fail "releases failed"
[ "$out" = "$PREV" ] || fail "releases on a new box printed '$out', want $PREV"
as_runner "$RUN_SH" install "$PREV" || fail "the first install of $PREV failed"
health "$PREV"

say "upgrade: releases (the real index), install $VERSION — by $PREV's run-side script"
set_env "$INDEX_URL"
out=$(as_runner "$RUN_SH" releases) || fail "releases failed"
grep -qx "$VERSION" <<<"$out" || fail "releases printed '$out', which lacks $VERSION"
as_runner "$RUN_SH" install "$VERSION" || fail "the upgrade to $VERSION failed"
health "$VERSION"
st=$(as_runner cat "$RUN_HOME/.${NAME}/updates/status.json")
[ "$(jq -r '.running + " " + .previous + " " + .install.state' <<<"$st")" = "$VERSION $PREV installed" ] \
  || fail "status after the upgrade: $st"

echo "✓ example pull: a new box installed $PREV and upgraded to $VERSION from GitHub ($RUNTIME)"
