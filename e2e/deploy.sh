#!/usr/bin/env bash
# deploy.sh — e2e for the two-user deployment (INSTALL.md › Deployment):
# build an artifact, load it as a second user, run it, check the app responds,
# upgrade to a second tag, roll back to the first. Then the pull flow on the
# same run user (pull.sh).
#
# Usage: e2e/deploy.sh <project-dir>     (called by build-example.sh)
#
# Two modes:
#
#   Real users (the CI gate): E2E_BUILD_USER and E2E_RUN_USER name two existing
#   Unix users, reached through passwordless sudo. The build user gets the
#   project as a git bundle, clones it and builds the artifact (no Go, no Node:
#   sudo's own PATH); the run user loads, runs, upgrades and rolls back. Under
#   podman each has its own rootless store (they need lingering, i.e.
#   /run/user/<uid>); under Docker both must be in the docker group and share
#   the machine's daemon.
#
#   Simulated (local runs): the second user is simulated with its own HOME and
#   the current user builds. Its container store:
#     - podman on Linux: its own rootless store (XDG_DATA_HOME, XDG_CONFIG_HOME,
#       XDG_RUNTIME_DIR under that HOME), exactly like a second OS user;
#     - podman on macOS / Docker: one store per machine (the podman VM, the
#       Docker daemon).
#
# On a shared store (Docker, podman on macOS) the builder's image is removed
# before each load, to make sure the image really arrives through the artifact.
# With E2E_REFERENCE=1 the simulation is refused: the reference needs real users.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=runtime.sh
source "$SCRIPT_DIR/runtime.sh"

DIR="$(cd "${1:?usage: e2e/deploy.sh <project-dir>}" && pwd)"
cd "$DIR"
NAME=$(grep '^name' schemaf.toml | cut -d= -f2 | tr -d ' "')
PORT=$(grep '^port' schemaf.toml | cut -d= -f2 | tr -d ' ' || true)
PORT=${PORT:-8000}

say() { echo "  · $*"; }
fail() { echo "✗ $*" >&2; exit 1; }

BUILD_USER="${E2E_BUILD_USER:-}"
RUN_USER="${E2E_RUN_USER:-}"
if [ -n "$BUILD_USER$RUN_USER" ]; then
  MODE=users
  [ -n "$BUILD_USER" ] && [ -n "$RUN_USER" ] || fail "set both E2E_BUILD_USER and E2E_RUN_USER (got E2E_BUILD_USER='$BUILD_USER', E2E_RUN_USER='$RUN_USER')"
  [ "$BUILD_USER" != "$RUN_USER" ] || fail "E2E_BUILD_USER and E2E_RUN_USER must be two different users, both are '$BUILD_USER'"
  for u in "$BUILD_USER" "$RUN_USER"; do
    id -u "$u" >/dev/null 2>&1 || fail "user '$u' does not exist (create it: sudo useradd --create-home $u)"
    [ "$u" != "$(id -un)" ] || fail "'$u' is the current user — the two deploy users must be other users"
  done
  sudo -n true 2>/dev/null || fail "real-user mode needs passwordless sudo to act as $BUILD_USER and $RUN_USER"
elif [ "${E2E_REFERENCE:-0}" = 1 ]; then
  fail "E2E_REFERENCE=1 needs two real users: set E2E_BUILD_USER and E2E_RUN_USER (the simulation with a separate HOME is for local runs only)"
else
  MODE=simulated
fi

DROP="$(mktemp -d)"   # the file handover: artifacts (and, for real users, the repo bundle)

if [ "$MODE" = users ]; then
  chmod 1777 "$DROP"   # the build user writes artifacts, the run user reads them
  RUN_HOME="$(getent passwd "$RUN_USER" | cut -d: -f6)"
  BUILD_DIR="$(getent passwd "$BUILD_USER" | cut -d: -f6)/${NAME}"
  SEPARATE_STORE=0
  [ "$RUNTIME" = podman ] && SEPARATE_STORE=1
else
  RUN_HOME="$(mktemp -d)"
  BUILD_DIR="$DIR"
  SEPARATE_STORE=0
  if [ "$RUNTIME" = podman ] && [ "$(uname)" = Linux ]; then
    SEPARATE_STORE=1
    mkdir -p "$RUN_HOME/.local/share" "$RUN_HOME/.config" "$RUN_HOME/run"
    chmod 700 "$RUN_HOME/run"
  fi
fi
REAL_HOME="$HOME"
RUN_SH="$RUN_HOME/.${NAME}/bin/schemaf.sh"

# as_user <user> <cmd...> runs a command as a real user: its own HOME, sudo's
# PATH, its rootless runtime dir, and the selected runtime. cwd is /, since the
# current user's directories aren't readable for it.
as_user() {
  local u="$1" uid bus=()
  shift
  uid=$(id -u "$u")
  # Its session bus, if its user systemd provides one (podman then uses the
  # systemd cgroup manager; without one it falls back to cgroupfs). Tested
  # through sudo: /run/user/<uid> is private to that user.
  if sudo -n test -S "/run/user/$uid/bus"; then
    bus=("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$uid/bus")
  fi
  ( cd / && sudo -n -H -u "$u" env XDG_RUNTIME_DIR="/run/user/$uid" "${bus[@]}" \
      SCHEMAF_RUNTIME="$RUNTIME" PODMAN_COMPOSE_WARNING_LOGS=false "$@" )
}

# as_runner runs a command as the (real or simulated) second user.
as_runner() {
  if [ "$MODE" = users ]; then
    as_user "$RUN_USER" "$@"
  elif [ "$SEPARATE_STORE" = 1 ]; then
    HOME="$RUN_HOME" XDG_DATA_HOME="$RUN_HOME/.local/share" XDG_CONFIG_HOME="$RUN_HOME/.config" \
      XDG_RUNTIME_DIR="$RUN_HOME/run" "$@"
  else
    # Keep the real podman machine connection (macOS) / Docker config.
    HOME="$RUN_HOME" XDG_DATA_HOME="${XDG_DATA_HOME:-$REAL_HOME/.local/share}" \
      XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$REAL_HOME/.config}" DOCKER_CONFIG="${DOCKER_CONFIG:-$REAL_HOME/.docker}" "$@"
  fi
}

# as_builder runs a command as the build user (the current user when simulated).
as_builder() {
  if [ "$MODE" = users ]; then as_user "$BUILD_USER" "$@"; else "$@"; fi
}

# The container CLI in each user's store (test plumbing).
rt_runner() { as_runner "$RUNTIME" "$@"; }
rt_builder() { as_builder "$RUNTIME" "$@"; }

if [ "$MODE" = users ]; then
  for u in "$BUILD_USER" "$RUN_USER"; do
    if [ "$RUNTIME" = podman ] && [ ! -d "/run/user/$(id -u "$u")" ]; then
      fail "/run/user/$(id -u "$u") is missing: rootless podman for '$u' needs lingering (sudo loginctl enable-linger $u)"
    fi
    out=$(as_user "$u" "$RUNTIME" info 2>&1) || fail "${RUNTIME} is not usable as '$u'$([ "$RUNTIME" = docker ] && echo " (is it in the docker group?)"). ${RUNTIME} info said:
$out"
  done
  say "real users: build as '$BUILD_USER', run as '$RUN_USER' ($([ "$SEPARATE_STORE" = 1 ] && echo "separate rootless stores" || echo "one shared Docker daemon"))"
else
  say "simulated second user: HOME=$RUN_HOME ($([ "$SEPARATE_STORE" = 1 ] && echo "its own rootless store" || echo "the machine's one store"))"
fi

# The pull flow (INSTALL.md › Pulling releases), run at the end.
# shellcheck source=pull.sh
source "$SCRIPT_DIR/pull.sh"

cleanup() {
  rc=$?
  as_runner "$RUN_SH" stop >/dev/null 2>&1 || true
  pull_cleanup
  for t in ${TAG1:-} ${TAG2:-}; do
    rt_runner image rm "${NAME}:${t}" >/dev/null 2>&1 || true
    rt_builder image rm "${NAME}:${t}" >/dev/null 2>&1 || true
  done
  rt_runner volume rm "${NAME}-postgres-data" >/dev/null 2>&1 || true
  if [ "$MODE" = users ]; then
    as_user "$RUN_USER" rm -rf "$RUN_HOME/.${NAME}" || true
    as_user "$BUILD_USER" rm -rf "$BUILD_DIR" || true
    sudo -n rm -rf "$DROP" || true
  else
    rm -rf "$RUN_HOME" "$DROP"
  fi
  exit $rc
}
trap cleanup EXIT

# health checks that the app answers and the backend runs the expected tag.
health() {
  local tag="$1" i running
  for i in $(seq 1 60); do
    if curl -fsS "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
      running=$(rt_runner container inspect --format '{{.Config.Image}}' "${NAME}-backend")
      [ "${running##*:}" = "$tag" ] || fail "backend runs ${running}, want tag ${tag}"
      say "app responds on :${PORT}, running ${tag}"
      return 0
    fi
    sleep 1
  done
  as_runner "$RUN_SH" logs backend >&2 || true
  fail "app did not respond on :${PORT}/api/health"
}

# build_artifact <tag>: the build user builds the current commit into
# $DROP/<name>-<tag>.run (mode 755, so the run user can execute it). A real
# build user first gets the commit through a git bundle (clone, later pull).
build_artifact() {
  local tag="$1" art="$DROP/${NAME}-$1.run" branch got
  if [ "$MODE" = users ]; then
    branch=$(git symbolic-ref --short HEAD)
    git bundle create -q "$DROP/repo.bundle" "$branch"
    chmod a+r "$DROP/repo.bundle"
    if as_user "$BUILD_USER" test -d "$BUILD_DIR"; then
      as_user "$BUILD_USER" git -C "$BUILD_DIR" pull -q --ff-only "$DROP/repo.bundle" "$branch"
    else
      as_user "$BUILD_USER" git clone -q -b "$branch" "$DROP/repo.bundle" "$BUILD_DIR"
    fi
    got=$(as_user "$BUILD_USER" git -C "$BUILD_DIR" rev-parse --short HEAD)
    [ "$got" = "$tag" ] || fail "the build user's checkout is at ${got}, want ${tag}"
  fi
  as_builder bash -c 'cd "$1" && ./schemaf.sh build --output "$2"' _ "$BUILD_DIR" "$art"
}

# hand_over simulates the file handover: on a shared store, drop the builder's image.
hand_over() {
  if [ "$SEPARATE_STORE" = 0 ]; then
    rt_builder image rm "${NAME}:$1" >/dev/null
  fi
}

say "commit the project (build names its image after a commit)"
if [ ! -d .git ]; then
  git init -q
fi
git add -A
git -c user.name=e2e -c user.email=e2e@example.invalid -c commit.gpgsign=false commit -q -m "e2e: initial" || true
TAG1=$(git rev-parse --short HEAD)

# INSTALL.md › Lockfiles: the image gets exactly what the lockfiles pin.
say "init's lockfiles are committed"
git ls-files --error-unmatch frontend/package-lock.json go/go.sum >/dev/null \
  || fail "init's frontend/package-lock.json or go/go.sum is not in the commit (gitignored?)"

say "stale lockfiles stop build, naming the file and the fix"
# stale_build <what> <expected message>: the working tree has a stale lockfile;
# a plain build (of the working tree) must fail with the message. Restores it.
stale_build() {
  local out
  if out=$(./schemaf.sh build 2>&1); then
    git checkout -q -- .
    fail "$1: build accepted it"
  fi
  git checkout -q -- .
  grep -qF "$2" <<<"$out" || { echo "$out" | tail -n 30 >&2; fail "$1: build failed without '$2'"; }
}
perl -0pi -e 's/"dependencies": \{/"dependencies": {\n    "left-pad": "1.3.0",/' frontend/package.json
grep -q left-pad frontend/package.json || fail "could not add a dependency to frontend/package.json"
stale_build "package.json changed without package-lock.json" \
  "frontend/package-lock.json is out of sync with frontend/package.json — run npm install in frontend/ on your dev host and commit frontend/package-lock.json"
: > go/go.sum
stale_build "an emptied go.sum" \
  "go/go.mod or go/go.sum is out of date — run go mod tidy in go/ on your dev host and commit go/go.mod and go/go.sum"
[ -z "$(git status --porcelain)" ] || fail "stale-lockfile checks left the working tree dirty: $(git status --porcelain)"

say "build user: ./schemaf.sh build --output (tag ${TAG1})"
build_artifact "$TAG1"
hand_over "$TAG1"

say "run user: load, write env, run --image ${TAG1}"
as_runner bash -c 'mkdir -p "$1" && umask 077 && echo "DB_PASS=$(openssl rand -hex 16)" > "$1/env"' _ "$RUN_HOME/.${NAME}/etc"
[ "$(as_runner "$DROP/${NAME}-${TAG1}.run" load)" = "$TAG1" ]
[ "$(as_runner "$DROP/${NAME}-${TAG1}.run" load 2>/dev/null)" = "$TAG1" ]   # same commit again: no-op
as_runner "$RUN_SH" run --image "$TAG1" -d --wait
health "$TAG1"

say "upgrade: a second commit, built and loaded as a second tag"
git -c user.name=e2e -c user.email=e2e@example.invalid -c commit.gpgsign=false commit -q --allow-empty -m "e2e: second"
TAG2=$(git rev-parse --short HEAD)
build_artifact "$TAG2"
hand_over "$TAG2"
[ "$(as_runner "$DROP/${NAME}-${TAG2}.run" load)" = "$TAG2" ]
as_runner "$RUN_SH" run --image "$TAG2" -d --wait
health "$TAG2"

say "image ls marks the running tag; image rm refuses it"
as_runner "$RUN_SH" image ls | tee /dev/stderr | grep -E "^${TAG2} .*\*$" >/dev/null
if as_runner "$RUN_SH" image rm "$TAG2" 2>/dev/null; then
  fail "image rm removed the running tag"
fi

say "an app subcommand runs in the deployed backend"
as_runner "$RUN_SH" db --help >/dev/null

say "rollback: run --image ${TAG1}"
as_runner "$RUN_SH" run --image "$TAG1" -d --wait
health "$TAG1"

echo "  ✓ deploy: built, handed over, ran, upgraded and rolled back ($MODE users)"

say "pull flow: releases and install on a fresh box (INSTALL.md › Pulling releases)"
pull_flow
