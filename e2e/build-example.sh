#!/usr/bin/env bash
# build-example.sh — Prove the real schemaf onboarding lifecycle from ZERO.
#
# This is both a TEST and executable DOCUMENTATION. It assumes a bare machine:
# nothing schemaf-related is installed. The entire project is created by ONE
# command — `go run …/cmd/schemaf@<ref> init <name>` — which fetches schemaf
# from an ONLINE ref and scaffolds a complete, working project (the create-
# react-app experience). The script then builds and tests it.
#
# Usage:
#   e2e/build-example.sh                # use the latest online v* release tag
#   e2e/build-example.sh v1.8.1         # a specific release tag
#   e2e/build-example.sh "$(git rev-parse HEAD)"
#                                       # a pushed commit, by its full 40-char hash
#                                       #   (what the CI gate and make release run)
#   e2e/build-example.sh main           # a moving ref: a branch or tag
#                                       #   (Go pins it to a commit pseudo-version)
#   KEEP=1 e2e/build-example.sh ...     # keep the /tmp project on success
#
# In the CI gate (EXTEND.md › Testing the framework: the CI gate):
#   E2E_REFERENCE=1                     # a check that can't run fails, never skips
#   E2E_BUILD_USER=… E2E_RUN_USER=…     # deploy.sh uses two real Unix users
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
# The stack runs under SCHEMAF_RUNTIME (podman by default, or docker) — it must be up.
# shellcheck source=runtime.sh
source "$SCRIPT_DIR/runtime.sh"
if ! rt info >/dev/null 2>&1; then
  echo "${RUNTIME} is installed but not usable (\`${RUNTIME} info\` failed)" >&2
  [ "$RUNTIME" = podman ] && [ "$(uname)" = Darwin ] && echo "  start it: podman machine start" >&2
  exit 1
fi
echo "  go=$(go version | awk '{print $3}') npm=$(npm --version) runtime=${RUNTIME}"

# --- which schemaf to fetch: default = latest online v* tag (via ls-remote, NOT
#     `@latest`, which the Go proxy caches for ~30 min — same reason as upgrade) ---
REF="${1:-}"
if [ -z "$REF" ]; then
  step "resolve latest online release tag"
  TAGS=$(git ls-remote --tags --sort=-v:refname "$SCHEMAF_REPO" 'v*' 2>/dev/null || true)
  REF=${TAGS%%$'\n'*}; REF=${REF##*/}; REF=${REF%%^*}
  [ -n "$REF" ] || { echo "could not resolve a release tag from $SCHEMAF_REPO" >&2; exit 1; }
fi

# How Go may fetch schemaf@<ref>:
#   - release tag (vX.Y.Z) or full 40-char commit hash: immutable, so the module
#     proxy can't serve a stale version — no override, everything via the proxy.
#   - moving ref (a branch or tag): the proxy may serve what it resolved
#     earlier, so schemaf — and ONLY schemaf — bypasses proxy+sumdb via GOPRIVATE.
#     All other dependencies still come through the proxy.
# Anything else is refused: an abbreviated hash, an unknown name, or a name that
# is both a branch and a tag (or a 40-hex name that is also a ref) is ambiguous.
SCHEMAF_MODULE="github.com/flocko-motion/schemaf"
if [[ "$REF" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "  (ref '$REF' is a release tag → module proxy, no override)"
else
  REFS=$(git ls-remote --heads --tags "$SCHEMAF_REPO" "$REF") \
    || { echo "could not list refs of $SCHEMAF_REPO (git ls-remote failed)" >&2; exit 1; }
  IS_BRANCH=0; IS_TAG=0
  while read -r _ name; do
    case "$name" in
      "refs/heads/$REF") IS_BRANCH=1 ;;
      "refs/tags/$REF" | "refs/tags/$REF^{}") IS_TAG=1 ;;
    esac
  done <<< "$REFS"
  if [[ "$REF" =~ ^[0-9a-f]{40}$ ]]; then
    if [ "$IS_BRANCH$IS_TAG" != 00 ]; then
      echo "ref '$REF' is ambiguous: a commit hash and also a branch or tag on $SCHEMAF_REPO" >&2
      exit 1
    fi
    echo "  (ref '$REF' is a commit hash → module proxy, no override)"
  elif [ "$IS_BRANCH$IS_TAG" = 11 ]; then
    echo "ref '$REF' is ambiguous: both a branch and a tag on $SCHEMAF_REPO" >&2
    exit 1
  elif [ "$IS_BRANCH$IS_TAG" != 00 ]; then
    export GOPRIVATE="$SCHEMAF_MODULE"
    kind=branch; [ "$IS_TAG" = 1 ] && kind=tag
    echo "  (ref '$REF' is a moving ref ($kind) → GOPRIVATE=$GOPRIVATE: only schemaf bypasses the module proxy)"
  elif [[ "$REF" =~ ^[0-9a-fA-F]{4,39}$ ]]; then
    echo "ref '$REF' looks like an abbreviated commit hash — pass the full 40-character hash (git rev-parse <ref>)" >&2
    exit 1
  else
    echo "ref '$REF' is not a release tag (vX.Y.Z), a full commit hash, or a branch or tag on $SCHEMAF_REPO" >&2
    exit 1
  fi
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
for f in schemaf.toml schemaf.sh go/main.go go/app/app.go go/app/constants.gen.go go/app/frontend.gen.go go/tests/health_test.go; do
  [ -f "$f" ] || { echo "expected $f to exist after init" >&2; exit 1; }
  echo "    ✓ $f"
done
if grep -q 'AddDb\|AddApi' go/main.go; then
  echo "go/main.go still wires providers; they belong in go/app/app.go (Wire)" >&2; exit 1
fi
if grep -q postgres compose.test.gen.yml; then
  echo "compose.test.gen.yml still has postgres; tests get their databases from schemaftest" >&2; exit 1
fi
echo "    ✓ main.go only wires through app.Wire; the test stack has no postgres"

step "compile"
( cd go && go build ./... )

# Path and query parameters (#58): a typed path param, a required scalar, an
# optional pointer and a repeated slice, declared in the spec and enforced.
step "add an endpoint with a typed path param and query params"
cat > go/api/echo.go <<'GO'
package api

import (
	"context"

	schemafapi "github.com/flocko-motion/schemaf/api"
)

// EchoParamsEndpoint answers with the path and query params it received.
type EchoParamsEndpoint struct{}

func (EchoParamsEndpoint) Method() string             { return "GET" }
func (EchoParamsEndpoint) Path() string               { return "/api/echo/{n}" }
func (EchoParamsEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (EchoParamsEndpoint) Handle(ctx context.Context, req EchoParamsReq) (EchoParamsResp, error) {
	return EchoParamsResp{N: req.N, Sort: req.Sort, Search: req.Search, Tags: req.Tags}, nil
}

type EchoParamsReq struct {
	N      int      `path:"n"`
	Sort   string   `query:"sort"`
	Search *string  `query:"search"`
	Tags   []string `query:"tag"`
}

type EchoParamsResp struct {
	N      int      `json:"n"`
	Sort   string   `json:"sort"`
	Search *string  `json:"search"`
	Tags   []string `json:"tags"`
}
GO

# The #21 scenario: a TypeScript test against an authenticated endpoint (with
# the token the wrapper passes) and a DB-backed one, on its own instance.
step "add a TypeScript test against an authenticated, a DB-backed and a parameterised endpoint"
cat > go/tests/api.test.ts <<'TS'
import { Api } from "../../frontend/src/api/generated/api.gen"

export async function testUserMe(baseUrl: string, token: string) {
  const resp = await fetch(`${baseUrl}/api/user/me`, { headers: { Authorization: `Bearer ${token}` } })
  if (resp.status !== 200) throw new Error(`GET /api/user/me: expected 200, got ${resp.status}`)
}

export async function testNumber(baseUrl: string) {
  const put = await fetch(`${baseUrl}/api/number`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ number: 42 }),
  })
  if (put.status !== 200) throw new Error(`PUT /api/number: expected 200, got ${put.status}`)
  const got = await (await fetch(`${baseUrl}/api/number`)).json()
  if (got.number !== 42) throw new Error(`GET /api/number: expected 42, got ${JSON.stringify(got)}`)
}

export async function testParams(baseUrl: string) {
  // Through the generated client: the spec's params are its typed arguments.
  const api = new Api({ baseUrl })
  const full = await api.api.getApiEchoN(7, { sort: "name", search: "foo", tag: ["a", "b"] })
  const got = JSON.stringify(full.data)
  const want = JSON.stringify({ n: 7, sort: "name", search: "foo", tags: ["a", "b"] })
  if (got !== want) throw new Error(`getApiEchoN (all params): expected ${want}, got ${got}`)

  const minimal = await api.api.getApiEchoN(7, { sort: "name" })
  const gotMin = JSON.stringify(minimal.data)
  const wantMin = JSON.stringify({ n: 7, sort: "name", search: null, tags: [] })
  if (gotMin !== wantMin) throw new Error(`getApiEchoN (required only): expected ${wantMin}, got ${gotMin}`)

  // The client's types forbid these two requests, so they go through fetch.
  const missing = await fetch(`${baseUrl}/api/echo/7`)
  if (missing.status !== 400) throw new Error(`GET /api/echo/7 without sort: expected 400, got ${missing.status}`)
  const body = await missing.json()
  if (body.code !== "bad_request" || body.error !== 'query param "sort" is required')
    throw new Error(`GET /api/echo/7 without sort: unexpected body ${JSON.stringify(body)}`)

  const badPath = await fetch(`${baseUrl}/api/echo/abc?sort=name`)
  if (badPath.status !== 400) throw new Error(`GET /api/echo/abc: expected 400, got ${badPath.status}`)
}
TS

# #98: enum values that equal another type's name ("medium" vs type Medium)
# or another enum's value ("medium" in Tier and Quality). The generated Go
# client must name its constants <Type><Value>, or it doesn't compile.
step "add enums whose values collide with a type name and with each other"
cat > go/api/enums.go <<'GO'
package api

import (
	"context"

	schemafapi "github.com/flocko-motion/schemaf/api"
)

type Tier string

const (
	TierLow    Tier = "low"
	TierMedium Tier = "medium"
	TierHigh   Tier = "high"
)

type Medium string

const (
	MediumText  Medium = "text"
	MediumAudio Medium = "audio"
)

type Quality string

const (
	QualityMedium Quality = "medium"
	QualityBest   Quality = "best"
)

// EchoEnumsEndpoint answers with the enums it received.
type EchoEnumsEndpoint struct{}

func (EchoEnumsEndpoint) Method() string             { return "GET" }
func (EchoEnumsEndpoint) Path() string               { return "/api/enums" }
func (EchoEnumsEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (EchoEnumsEndpoint) Handle(ctx context.Context, req EchoEnumsReq) (EchoEnumsResp, error) {
	return EchoEnumsResp{Tier: req.Tier, Medium: MediumText, Quality: QualityMedium}, nil
}

type EchoEnumsReq struct {
	Tier Tier `query:"tier"`
}

type EchoEnumsResp struct {
	Tier    Tier    `json:"tier"`
	Medium  Medium  `json:"medium"`
	Quality Quality `json:"quality"`
}
GO
./schemaf.sh codegen

step "the generated Go client prefixes enum constants and compiles"
for c in 'TierMedium +Tier += "medium"' 'MediumText +Medium += "text"' 'QualityMedium +Quality += "medium"'; do
  grep -Eq "^[[:space:]]*${c}\$" go/apiclient/client.gen.go || { echo "go/apiclient/client.gen.go lacks: ${c}" >&2; exit 1; }
done
( cd go && go build ./... )
echo "    ✓ TierMedium, MediumText, QualityMedium; go build ./... passes"

step "run the tests under ${RUNTIME} (each instance on its own database, no postgres in compose)"
./schemaf.sh test

step "the scaffolded integration test and the TypeScript tests really ran"
OUT="$( cd go && go test ./tests/ -count=1 -v -run 'TestHealth|TestTSApi' 2>&1 )" || { echo "$OUT" >&2; exit 1; }
for t in TestHealth TestTSApi/UserMe TestTSApi/Number TestTSApi/Params; do
  grep -q -- "--- PASS: ${t} " <<<"$OUT" || { echo "$OUT" >&2; echo "expected ${t} to pass" >&2; exit 1; }
  echo "    ✓ ${t}"
done

step "parallel dev instances: the default and instance 1 side by side"
"$SCRIPT_DIR/instances.sh" "$DIR"

step "deploy as two users: build an artifact, load it as a second user, run, upgrade, roll back"
"$SCRIPT_DIR/deploy.sh" "$DIR"

step "data volume: refused while the other runtime holds it, starts when only ${RUNTIME} does"
"$SCRIPT_DIR/volumes.sh" "$DIR"

say "✓ SUCCESS — onboarded '${NAME}' from ${SCHEMAF_CMD}@${REF} with a single command"
if [ "${KEEP:-0}" = 1 ]; then
  echo "  kept: $DIR"
else
  rm -rf "$PARENT"
  echo "  cleaned up"
fi
