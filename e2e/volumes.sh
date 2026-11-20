#!/usr/bin/env bash
# volumes.sh — e2e for the runtime data-volume check (INSTALL.md › Container
# runtime) with real volumes: the project's Postgres volume <name>-postgres-data
# may exist only in the selected runtime.
#
#   the other runtime has it      → test (Go check) and run (shell check) refuse
#   both runtimes have it         → both refuse, saying both have it
#   only the selected runtime has → test starts
#
# Run under podman it covers "Docker has it", under Docker "podman has it"; the
# CI gate runs both (EXTEND.md › Testing the framework: the CI gate).
#
# Usage: e2e/volumes.sh <project-dir>     (called by build-example.sh)
#
# Needs both runtimes. If the other one isn't usable, the check is skipped with a
# warning — except with E2E_REFERENCE=1 (CI), where that is a failure.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=runtime.sh
source "$SCRIPT_DIR/runtime.sh"

DIR="$(cd "${1:?usage: e2e/volumes.sh <project-dir>}" && pwd)"
cd "$DIR"
NAME=$(grep '^name' schemaf.toml | cut -d= -f2 | tr -d ' "')
VOL="${NAME}-postgres-data"

say() { echo "  · $*"; }
fail() { echo "✗ $*" >&2; exit 1; }

if ! rt_other_usable; then
  if [ "${E2E_REFERENCE:-0}" = 1 ]; then
    fail "the data-volume check needs both runtimes, but ${OTHER_RUNTIME} is not usable (\`${OTHER_RUNTIME} info\` failed or it is not installed) — E2E_REFERENCE=1 never skips"
  fi
  echo "  ⚠ SKIPPED the data-volume check: ${OTHER_RUNTIME} is not usable here, it needs both runtimes (the CI gate runs it)" >&2
  exit 0
fi

has_volume() { "$1" volume inspect "$VOL" >/dev/null 2>&1; }

cleanup() {
  rc=$?
  rt volume rm "$VOL" >/dev/null 2>&1 || true
  rt_other volume rm "$VOL" >/dev/null 2>&1 || true
  exit $rc
}

# The e2e steps before this one remove their volumes; a leftover would make the
# cases below meaningless.
has_volume rt && fail "${RUNTIME} already has ${VOL} — an earlier step left it behind"
has_volume rt_other && fail "${OTHER_RUNTIME} already has ${VOL} — an earlier step left it behind"
trap cleanup EXIT

# refuses <case> <expected text>...: test and run must both fail and print each text.
refuses() {
  local what="$1" want out cmd
  shift
  for cmd in test "run -d"; do
    # shellcheck disable=SC2086 # cmd is "run -d": two words on purpose
    if out=$(./schemaf.sh $cmd 2>&1); then
      echo "$out" | tail -n 20 >&2
      fail "${what}: ./schemaf.sh ${cmd} started, but must refuse"
    fi
    for want in "$@"; do
      grep -qF "$want" <<<"$out" || { echo "$out" | tail -n 20 >&2; fail "${what}: ./schemaf.sh ${cmd} failed, but without: ${want}"; }
    done
    say "${what}: ./schemaf.sh ${cmd} refuses"
  done
}

REFUSAL="refusing to start: the Postgres volume ${VOL} exists in ${OTHER_RUNTIME}, but the selected container runtime is ${RUNTIME}."

rt_other volume create "$VOL" >/dev/null
refuses "only ${OTHER_RUNTIME} has ${VOL}" "$REFUSAL"

rt volume create "$VOL" >/dev/null
refuses "both runtimes have ${VOL}" "$REFUSAL" "${RUNTIME} has its own ${VOL} as well"

rt_other volume rm "$VOL" >/dev/null
say "only ${RUNTIME} has ${VOL}: ./schemaf.sh test starts"
./schemaf.sh test >/dev/null 2>&1 || { ./schemaf.sh test 2>&1 | tail -n 30 >&2; fail "only ${RUNTIME} has ${VOL}, but ./schemaf.sh test failed"; }

echo "  ✓ volumes: refused in ${OTHER_RUNTIME}-has-it and both-have-it, started with only ${RUNTIME}'s"
