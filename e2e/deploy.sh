#!/usr/bin/env bash
# deploy.sh — e2e for the two-user deployment (INSTALL.md › Deployment):
# build an artifact, load it as a second user, run it, check the app responds,
# upgrade to a second tag, roll back to the first.
#
# Usage: e2e/deploy.sh <project-dir>     (called by build-example.sh)
#
# The second user is simulated with its own HOME. Its container store:
#   - podman on Linux: its own rootless store (XDG_DATA_HOME, XDG_CONFIG_HOME,
#     XDG_RUNTIME_DIR under that HOME), exactly like a second OS user;
#   - podman on macOS / Docker: one store per machine (the podman VM, the
#     Docker daemon), so the builder's images are removed before each load to
#     make sure the image really arrives through the artifact.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=runtime.sh
source "$SCRIPT_DIR/runtime.sh"

DIR="$(cd "${1:?usage: e2e/deploy.sh <project-dir>}" && pwd)"
cd "$DIR"
NAME=$(grep '^name' schemaf.toml | cut -d= -f2 | tr -d ' "')
PORT=$(grep '^port' schemaf.toml | cut -d= -f2 | tr -d ' ' || true)
PORT=${PORT:-8000}

REAL_HOME="$HOME"
RUN_HOME="$(mktemp -d)"
DROP="$(mktemp -d)"
RUN_SH="$RUN_HOME/.${NAME}/bin/schemaf.sh"

SEPARATE_STORE=0
if [ "$RUNTIME" = podman ] && [ "$(uname)" = Linux ]; then
  SEPARATE_STORE=1
  mkdir -p "$RUN_HOME/.local/share" "$RUN_HOME/.config" "$RUN_HOME/run"
  chmod 700 "$RUN_HOME/run"
fi

# as_runner runs a command as the simulated second user.
as_runner() {
  if [ "$SEPARATE_STORE" = 1 ]; then
    HOME="$RUN_HOME" XDG_DATA_HOME="$RUN_HOME/.local/share" XDG_CONFIG_HOME="$RUN_HOME/.config" \
      XDG_RUNTIME_DIR="$RUN_HOME/run" "$@"
  else
    # Keep the real podman machine connection (macOS) / Docker config.
    HOME="$RUN_HOME" XDG_DATA_HOME="${XDG_DATA_HOME:-$REAL_HOME/.local/share}" \
      XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$REAL_HOME/.config}" DOCKER_CONFIG="${DOCKER_CONFIG:-$REAL_HOME/.docker}" "$@"
  fi
}

# rt_runner runs the container CLI in the second user's store (test plumbing).
rt_runner() { as_runner "$RUNTIME" "$@"; }

cleanup() {
  rc=$?
  as_runner "$RUN_SH" stop >/dev/null 2>&1 || true
  for t in ${TAG1:-} ${TAG2:-}; do rt_runner image rm "${NAME}:${t}" >/dev/null 2>&1 || true; done
  rt_runner volume rm "${NAME}-postgres-data" >/dev/null 2>&1 || true
  rm -rf "$RUN_HOME" "$DROP"
  exit $rc
}
trap cleanup EXIT

say() { echo "  · $*"; }

# health checks that the app answers and the backend runs the expected tag.
health() {
  local tag="$1" i running
  for i in $(seq 1 60); do
    if curl -fsS "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
      running=$(rt_runner container inspect --format '{{.Config.Image}}' "${NAME}-backend")
      [ "${running##*:}" = "$tag" ] || { echo "backend runs ${running}, want tag ${tag}" >&2; exit 1; }
      say "app responds on :${PORT}, running ${tag}"
      return 0
    fi
    sleep 1
  done
  echo "app did not respond on :${PORT}/api/health" >&2
  as_runner "$RUN_SH" logs backend >&2 || true
  exit 1
}

# hand_over simulates the file handover: on a shared store, drop the builder's image.
hand_over() {
  if [ "$SEPARATE_STORE" = 0 ]; then
    rt image rm "${NAME}:$1" >/dev/null
  fi
}

say "commit the project (build names its image after a commit)"
if [ ! -d .git ]; then
  git init -q
fi
git add -A
git -c user.name=e2e -c user.email=e2e@example.invalid -c commit.gpgsign=false commit -q -m "e2e: initial" || true
TAG1=$(git rev-parse --short HEAD)

say "build user: ./schemaf.sh build --output (tag ${TAG1})"
./schemaf.sh build --output "$DROP/${NAME}-${TAG1}.run"
hand_over "$TAG1"

say "run user: load, write env, run --image ${TAG1}"
mkdir -p "$RUN_HOME/.${NAME}/etc"
echo "DB_PASS=$(openssl rand -hex 16)" > "$RUN_HOME/.${NAME}/etc/env"
[ "$(as_runner "$DROP/${NAME}-${TAG1}.run" load)" = "$TAG1" ]
[ "$(as_runner "$DROP/${NAME}-${TAG1}.run" load 2>/dev/null)" = "$TAG1" ]   # same commit again: no-op
as_runner "$RUN_SH" run --image "$TAG1" -d --wait
health "$TAG1"

say "upgrade: a second commit, built and loaded as a second tag"
git -c user.name=e2e -c user.email=e2e@example.invalid -c commit.gpgsign=false commit -q --allow-empty -m "e2e: second"
TAG2=$(git rev-parse --short HEAD)
./schemaf.sh build --output "$DROP/${NAME}-${TAG2}.run"
hand_over "$TAG2"
[ "$(as_runner "$DROP/${NAME}-${TAG2}.run" load)" = "$TAG2" ]
as_runner "$RUN_SH" run --image "$TAG2" -d --wait
health "$TAG2"

say "image ls marks the running tag; image rm refuses it"
as_runner "$RUN_SH" image ls | tee /dev/stderr | grep -E "^${TAG2} .*\*$" >/dev/null
if as_runner "$RUN_SH" image rm "$TAG2" 2>/dev/null; then
  echo "image rm removed the running tag" >&2
  exit 1
fi

say "an app subcommand runs in the deployed backend"
as_runner "$RUN_SH" db --help >/dev/null

say "rollback: run --image ${TAG1}"
as_runner "$RUN_SH" run --image "$TAG1" -d --wait
health "$TAG1"

echo "  ✓ deploy: built, handed over, ran, upgraded and rolled back"
