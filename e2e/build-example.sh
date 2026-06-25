#!/usr/bin/env bash
# build-example.sh — Prove the real schemaf onboarding lifecycle from ZERO.
#
# This is both a TEST and executable DOCUMENTATION. It assumes a bare machine:
# nothing schemaf-related is installed. The entire project is created by ONE
# command — `go run …/cmd/schemaf@<ref> init <name>` — which fetches schemaf
# from an ONLINE tag and scaffolds a complete, working project (the create-
# react-app experience). The script then builds and tests it.
#
# Usage:
#   e2e/build-example.sh                # use the latest online v* release tag
#   e2e/build-example.sh v1.8.1         # a specific release tag
#   e2e/build-example.sh e2e            # the moving non-semver 'e2e' tag
#                                       #   (Go pins it to a commit pseudo-version)
#   KEEP=1 e2e/build-example.sh ...     # keep the /tmp project on success
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCHEMAF_REPO="https://github.com/flocko-motion/schemaf.git"
SCHEMAF_CMD="github.com/flocko-motion/schemaf/cmd/schemaf"

trap 'rc=$?; echo >&2; echo "✗ FAILED at line ${LINENO}: \"${BASH_COMMAND}\" (exit ${rc})" >&2; [ -n "${DIR:-}" ] && echo "  project left for inspection: ${DIR}" >&2; exit ${rc}' ERR

# step auto-numbers each line, so steps can be added/removed/reordered freely.
_STEP=0
step() { _STEP=$((_STEP + 1)); echo; echo "▶ ${_STEP}. $*"; }
say()  { echo; echo "$*"; }

# --- prerequisites: a bare machine has Go; init also scaffolds a frontend (npm) ---
step "check prerequisites"
command -v go  >/dev/null || { echo "go is required"  >&2; exit 1; }
command -v npm >/dev/null || { echo "npm is required (init scaffolds a frontend)" >&2; exit 1; }
HAVE_DOCKER=0; docker info >/dev/null 2>&1 && HAVE_DOCKER=1
echo "  go=$(go version | awk '{print $3}') npm=$(npm --version) docker=$([ $HAVE_DOCKER = 1 ] && echo up || echo no)"

# --- which schemaf to fetch: default = latest online v* tag (via ls-remote, NOT
#     `@latest`, which the Go proxy caches for ~30 min — same reason as upgrade) ---
REF="${1:-}"
if [ -z "$REF" ]; then
  step "resolve latest online release tag"
  TAGS=$(git ls-remote --tags --sort=-v:refname "$SCHEMAF_REPO" 'v*' 2>/dev/null || true)
  REF=${TAGS%%$'\n'*}; REF=${REF##*/}; REF=${REF%%^*}
  [ -n "$REF" ] || { echo "could not resolve a release tag from $SCHEMAF_REPO" >&2; exit 1; }
fi

# Only a clean release tag (vX.Y.Z) is immutable and safe to fetch via the module
# proxy. Moving tags (e2e) and freshly pushed prereleases (vX.Y.Z-pre.<sha>) must
# bypass proxy+sumdb so we test the exact commit just pushed, not a cached or
# sumdb-lagged one.
if [[ ! "$REF" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  export GOPROXY=direct GOSUMDB=off
  echo "  (ref '$REF' is not a release tag → GOPROXY=direct GOSUMDB=off)"
fi

# --- a fresh, legible name in a blank dir OUTSIDE this repo ---
# shellcheck source=names.sh
source "$SCRIPT_DIR/names.sh"
NAME="$(random_name)"
PARENT="$(mktemp -d)"
DIR="$PARENT/$NAME"

# --- THE ONE COMMAND: fetch schemaf@<ref> from online AND scaffold everything.
#     init creates the dir, schemaf.toml, go.work, go module, runs codegen, and
#     scaffolds the frontend. The developer writes nothing by hand. ---
step "fetch + scaffold (one call): go run ${SCHEMAF_CMD}@${REF} init ${NAME}"
( cd "$PARENT" && go run "${SCHEMAF_CMD}@${REF}" init "$NAME" )

cd "$DIR"

# --- it should have produced a complete, configured project ---
step "verify scaffold"
for f in schemaf.toml schemaf.sh go/main.go go/constants.gen.go; do
  [ -f "$f" ] || { echo "expected $f to exist after init" >&2; exit 1; }
  echo "    ✓ $f"
done

step "compile"
( cd go && go build ./... )

if [ "$HAVE_DOCKER" = 1 ]; then
  step "run the test stack (ephemeral postgres + tests)"
  ./schemaf.sh test
else
  echo
  echo "  (docker not available — skipping stack run; fetch→scaffold→compile proven)"
fi

say "✓ SUCCESS — onboarded '${NAME}' from ${SCHEMAF_CMD}@${REF} with a single command"
if [ "${KEEP:-0}" = 1 ]; then
  echo "  kept: $DIR"
else
  rm -rf "$PARENT"
  echo "  cleaned up"
fi
