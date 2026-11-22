# pull.sh — sourced by deploy.sh: the pull flow (INSTALL.md › Pulling releases)
# with real signatures, real containers and the real run user.
#
# The dev side (the current user) builds releases of the project and signs
# them with keyless Sigstore under the gate job's own identity (GitHub Actions'
# OIDC token: the gate workflow needs id-token: write), and serves a
# releases.json from a local HTTP server. The run user's box is set up as
# INSTALL.md instructs, with SCHEMAF_SIGNER set to that identity, and then:
#   - a first install of the newest release on an empty box;
#   - an upgrade, with the data kept;
#   - a deliberately broken release (its migration changes data, its health
#     check fails): automatic rollback, database restored, previous version
#     serving;
#   - refusals: an older version, a prerelease on the stable channel, a
#     tampered artifact, a version that doesn't match the artifact's manifest,
#     a wrong signer.
# Needs Linux and the OIDC token; without them it is skipped loudly, except
# with E2E_REFERENCE=1, where that is a failure.
#
# Uses deploy.sh's helpers and variables: as_runner, as_user, rt_runner,
# rt (runtime.sh), health, say, fail, NAME, PORT, RUNTIME, MODE, RUN_HOME, RUN_SH,
# DROP, DIR.

PULL_TAGS=""     # version images to remove (cleanup)
PULL_SERVER=""   # the HTTP server's PID (cleanup)

pull_skip() {
  if [ "${E2E_REFERENCE:-0}" = 1 ]; then
    fail "pull flow: $1 — E2E_REFERENCE=1 never skips"
  fi
  echo "  ⚠ pull flow SKIPPED: $1" >&2
}

# pull_cleanup — called from deploy.sh's cleanup.
pull_cleanup() {
  [ -z "$PULL_SERVER" ] || kill "$PULL_SERVER" 2>/dev/null || true
  for t in $PULL_TAGS; do
    rt_runner image rm "${NAME}:${t}" >/dev/null 2>&1 || true
    rt image rm "${NAME}:${t}" >/dev/null 2>&1 || true
  done
}

COSIGN_IMAGE=$(grep '^SCHEMAF_IMG_COSIGN=' "$DIR/schemaf.sh" | cut -d'"' -f2)

# pull_release <version> [<listed as>]: builds the current commit as <version>
# into $REL, signs it, and lists it in the index as <listed as> (default:
# <version>), with the artifact renamed to match.
pull_release() {
  local v="$1" listed="${2:-$1}" art user
  art="$REL/${NAME}-${listed}.run"
  ./schemaf.sh build --tag "$v" --output "$REL/${NAME}-${v}.run.tmp" >/dev/null 2>&1 \
    || { ./schemaf.sh build --tag "$v" --output "$REL/${NAME}-${v}.run.tmp" >&2 || true; fail "pull flow: build of $v failed"; }
  mv "$REL/${NAME}-${v}.run.tmp" "$art"
  PULL_TAGS="$PULL_TAGS $v"
  # The bundle is written into the mounted directory: as the current user.
  user="0:0"
  [ "$RUNTIME" = docker ] && user="$(id -u):$(id -g)"
  rt run --rm --network host --user "$user" -e HOME=/tmp \
    -e ACTIONS_ID_TOKEN_REQUEST_URL -e ACTIONS_ID_TOKEN_REQUEST_TOKEN \
    -v "$REL:/w" "$COSIGN_IMAGE" attest-blob --yes \
    --predicate /w/predicate.json --type slsaprovenance1 \
    --bundle "/w/${NAME}-${listed}.run.sigstore.json" "/w/${NAME}-${listed}.run" >/dev/null 2>"$REL/sign.log" \
    || { cat "$REL/sign.log" >&2; fail "pull flow: signing $listed failed"; }
  chmod 644 "$art" "$art.sigstore.json"
  INDEX=$(jq --arg v "$listed" --arg base "http://127.0.0.1:${HPORT}/${NAME}-${listed}.run" \
    '. + [{version: $v, prerelease: ($v | contains("-")), artifact: $base, attestation: ($base + ".sigstore.json"), notes: ""}]' <<<"$INDEX")
  printf '%s\n' "$INDEX" > "$REL/releases.json"
}

# run_env <signer>: the run user's ~/.<name>/etc/env, as INSTALL.md instructs.
run_env() {
  local f="$DROP/pull-env"
  {
    echo "DB_PASS=$DB_PASS_PULL"
    echo "SCHEMAF_RUNTIME=$RUNTIME"
    echo "SCHEMAF_RELEASES=http://127.0.0.1:${HPORT}/releases.json"
    echo "SCHEMAF_SIGNER=$1"
    echo "SCHEMAF_CHANNEL=stable"
  } > "$f"
  chmod 644 "$f"
  as_runner bash -c 'mkdir -p "$1" && umask 077 && cat "$2" > "$1/env"' _ "$RUN_HOME/.${NAME}/etc" "$f"
  rm -f "$f"
}

# expect_fail <what> <message> <cmd...>: the command fails and says <message>.
expect_fail() {
  local what="$1" want="$2" out
  shift 2
  if out=$("$@" 2>&1); then
    echo "$out" >&2
    fail "pull flow: $what succeeded"
  fi
  grep -qF -- "$want" <<<"$out" || { echo "$out" >&2; fail "pull flow: $what failed without '$want'"; }
  say "refused: $what"
}

number() { curl -fsS "http://127.0.0.1:${PORT}/api/number" | jq -r .number; }

run_status() { as_runner cat "$RUN_HOME/.${NAME}/updates/status.json"; }

pull_flow() {
  [ "$(uname)" = Linux ] || { pull_skip "the pull flow runs on Linux only"; return 0; }
  if [ -z "${ACTIONS_ID_TOKEN_REQUEST_URL:-}" ] || [ -z "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ] || [ -z "${GITHUB_WORKFLOW_REF:-}" ]; then
    pull_skip "signing needs GitHub Actions' OIDC token (a workflow with id-token: write)"
    return 0
  fi
  command -v jq >/dev/null || fail "pull flow: jq is required"
  command -v python3 >/dev/null || fail "pull flow: python3 is required (the local HTTP server)"
  [ -n "$COSIGN_IMAGE" ] || fail "pull flow: no SCHEMAF_IMG_COSIGN in schemaf.sh"
  local signer="https://github.com/${GITHUB_WORKFLOW_REF}" out
  REL=$(mktemp -d)
  chmod 755 "$REL"
  HPORT=$((PORT + 19))
  INDEX='[]'
  DB_PASS_PULL=$(openssl rand -hex 16)
  cat > "$REL/predicate.json" <<'JSON'
{"buildDefinition": {"buildType": "https://github.com/flocko-motion/schemaf/e2e", "externalParameters": {}},
 "runDetails": {"builder": {"id": "https://github.com/flocko-motion/schemaf/.github/workflows/gate.yml"}}}
JSON
  printf '[]\n' > "$REL/releases.json"
  python3 -m http.server --bind 127.0.0.1 --directory "$REL" "$HPORT" >/dev/null 2>&1 &
  PULL_SERVER=$!

  say "pull: a fresh box — the deploy stack and the run user's state removed"
  as_runner "$RUN_SH" stop >/dev/null 2>&1 || true
  rt_runner volume rm "${NAME}-postgres-data" >/dev/null 2>&1 || true
  as_runner rm -rf "${RUN_HOME:?}/.${NAME:?}"

  say "pull: set up the box as INSTALL.md instructs (env file, the repository's schemaf.sh)"
  run_env "$signer"
  cp "$DIR/schemaf.sh" "$DROP/schemaf.sh" && chmod 644 "$DROP/schemaf.sh"
  as_runner bash -c 'mkdir -p "$(dirname "$1")" && cp "$2" "$1" && chmod 755 "$1"' _ "$RUN_SH" "$DROP/schemaf.sh"

  say "pull: release 0.1.0 (signed by $signer)"
  pull_release 0.1.0
  out=$(as_runner "$RUN_SH" releases) || fail "pull flow: releases failed"
  [ "$out" = 0.1.0 ] || fail "pull flow: releases on an empty box printed '$out', want 0.1.0"
  as_runner "$RUN_SH" install 0.1.0 || fail "pull flow: the first install failed"
  health 0.1.0
  curl -fsS -X PUT -H 'Content-Type: application/json' -d '{"number": 42}' "http://127.0.0.1:${PORT}/api/number" >/dev/null
  [ "$(number)" = 42 ] || fail "pull flow: could not store 42"

  say "pull: releases 0.2.0, 0.3.0 (broken), 0.4.0-rc.1 (prerelease)"
  git -c user.name=e2e -c user.email=e2e@example.invalid -c commit.gpgsign=false commit -q --allow-empty -m "e2e: 0.2.0"
  pull_release 0.2.0
  pull_release 0.4.0-rc.1
  # 0.3.0: a migration that changes data, and a health check that always fails.
  cat > go/db/migrations/0002_e2e_broken.sql <<'SQL'
CREATE TABLE e2e_broken (id INTEGER);
UPDATE number SET value = 666 WHERE id = 1;
SQL
  cat > go/api/e2e_broken.go <<'GO'
package api

import (
	"errors"

	schemafapi "github.com/flocko-motion/schemaf/api"
)

func init() {
	schemafapi.RegisterHealth("e2e-broken", func() error { return errors.New("a deliberately broken release") })
}
GO
  ./schemaf.sh codegen >/dev/null 2>&1 || { ./schemaf.sh codegen >&2 || true; fail "pull flow: codegen for 0.3.0 failed"; }
  git add -A
  git -c user.name=e2e -c user.email=e2e@example.invalid -c commit.gpgsign=false commit -q -m "e2e: 0.3.0, broken"
  pull_release 0.3.0
  git -c user.name=e2e -c user.email=e2e@example.invalid -c commit.gpgsign=false revert --no-edit HEAD >/dev/null

  out=$(as_runner "$RUN_SH" releases) || fail "pull flow: releases failed"
  [ "$out" = "0.2.0"$'\n'"0.3.0" ] || fail "pull flow: releases printed '$out', want 0.2.0 and 0.3.0 (no prerelease on the stable channel)"
  expect_fail "a prerelease on the stable channel" "0.4.0-rc.1 is a prerelease, and this host's channel is stable" \
    as_runner "$RUN_SH" install 0.4.0-rc.1

  say "pull: upgrade to 0.2.0"
  as_runner "$RUN_SH" install 0.2.0 || fail "pull flow: install 0.2.0 failed"
  health 0.2.0
  [ "$(number)" = 42 ] || fail "pull flow: the upgrade lost the data ($(number), want 42)"
  [ "$(run_status | jq -r '.running + " " + .previous + " " + .install.state')" = "0.2.0 0.1.0 installed" ] \
    || fail "pull flow: status after the upgrade: $(run_status)"
  expect_fail "an older version" "0.1.0 is not newer than the running 0.2.0" as_runner "$RUN_SH" install 0.1.0

  say "pull: the broken 0.3.0 rolls back"
  expect_fail "the broken 0.3.0" "0.3.0 failed its health check; rolled back to 0.2.0, the database restored from the backup" \
    as_runner "$RUN_SH" install 0.3.0
  health 0.2.0
  [ "$(number)" = 42 ] || fail "pull flow: after the rollback the number is $(number), want 42 (the backup)"
  [ "$(run_status | jq -r '.running + " " + .install.state')" = "0.2.0 rolled-back" ] \
    || fail "pull flow: status after the rollback: $(run_status)"
  as_runner bash -c 'grep -q "deliberately broken" "$1"' _ "$(run_status | jq -r .install.log)" \
    || fail "pull flow: the broken version's logs were not saved"

  say "pull: a wrong signer"
  run_env "flocko-motion/not-the-signer"
  expect_fail "install with a wrong signer" "does not verify for signer flocko-motion/not-the-signer" \
    as_runner "$RUN_SH" install 0.3.0
  run_env "$signer"

  say "pull: a tampered artifact, a version that doesn't match its manifest"
  pull_release 0.5.0
  printf 'x' >> "$REL/${NAME}-0.5.0.run"
  pull_release 0.6.0 0.7.0
  expect_fail "releases with foreign artifacts" "some releases failed their checks" as_runner "$RUN_SH" releases
  out=$(as_runner "$RUN_SH" releases 2>&1 || true)
  grep -qF "0.5.0: the attestation does not verify" <<<"$out" || { echo "$out" >&2; fail "pull flow: the tampered 0.5.0 was not refused"; }
  grep -qF "0.7.0: the artifact's manifest names ${NAME} 0.6.0, the index says ${NAME} 0.7.0" <<<"$out" || { echo "$out" >&2; fail "pull flow: the mismatched 0.7.0 was not refused"; }
  for v in 0.5.0 0.7.0; do
    if as_runner test -e "$RUN_HOME/.${NAME}/releases/$v"; then fail "pull flow: $v was kept"; fi
  done
  health 0.2.0

  echo "  ✓ pull: first install, upgrade, rollback with the database restored, refusals ($MODE users, $RUNTIME)"
}
