#!/usr/bin/env bash
# example-release.sh — the real release path of a generated project, against
# the GitHub repository flocko-motion/schemaf-example (EXTEND.md › Testing the
# framework: the CI gate › The example release).
#
# Upgrades the long-lived example project to schemaf <ref>, pushes it, tags
# v<version>, waits for its generated release.gen.yml run and checks the
# result: the GitHub release with the artifact and its attestation bundle, the
# attestation (signed by the example's release workflow on that tag), the
# version in the artifact's manifest, and the entry in releases.json. Then it
# starts the example pull (.github/workflows/example-pull.yml on <ref>: a new
# box installs v-1 and upgrades to v<version>) and waits for it.
# The example and its first release are a one-time setup, not part of this
# script (EXTEND.md › The example release).
#
# Usage: e2e/example-release.sh <schemaf-ref> [<version>]
#   <schemaf-ref>  a release tag vX.Y.Z: <version> defaults to X.Y.Z;
#                  or a branch or tag of schemaf: <version> is required.
#                  It must be pushed: the example pull runs on it.
#   <version>      X.Y.Z or X.Y.Z-<pre>; the example's tag is v<version>.
# A version that is already released is only checked, never released again.
# Needs git, go, gh (signed in, with push access to the example and the
# right to start workflows in schemaf), jq, curl.
# Runs as part of `make release` after the tag; it pushes to the example
# repository, so run it by hand only on the maintainer's order.
set -euo pipefail

EXAMPLE_REPO="${EXAMPLE_REPO:-flocko-motion/schemaf-example}"
EXAMPLE_NAME="${EXAMPLE_REPO##*/}"
SCHEMAF_MODULE="github.com/flocko-motion/schemaf"

say() { echo "  · $*"; }
fail() { echo "✗ example release: $*" >&2; exit 1; }

REF="${1:-}"
VERSION="${2:-}"
[ -n "$REF" ] || fail "usage: e2e/example-release.sh <schemaf-ref> [<version>]"
num='(0|[1-9][0-9]*)'
if [ -z "$VERSION" ]; then
  [[ "$REF" =~ ^v$num\.$num\.$num$ ]] || fail "schemaf ref '$REF' is not a release tag vX.Y.Z — pass the example's version: e2e/example-release.sh $REF <version>"
  VERSION="${REF#v}"
fi
[[ "$VERSION" =~ ^$num\.$num\.$num(-[0-9A-Za-z.-]+)?$ ]] || fail "version '$VERSION' is not X.Y.Z or X.Y.Z-<pre>"
TAG="v$VERSION"
PRERELEASE=false
case "$VERSION" in *-*) PRERELEASE=true ;; esac

for tool in git go gh jq curl; do
  command -v "$tool" >/dev/null || fail "$tool is required"
done
gh auth status >/dev/null 2>&1 || fail "gh is not signed in: gh auth login"

# The example pull is a workflow run on <ref>, so <ref> is a branch or tag.
SCHEMAF_REPO="flocko-motion/schemaf"
git ls-remote --exit-code --heads --tags "https://github.com/$SCHEMAF_REPO" "$REF" >/dev/null \
  || fail "'$REF' is not a branch or tag of $SCHEMAF_REPO — push it first (the example pull runs on it)"
# A moving ref may be served stale by the module proxy: only schemaf bypasses
# it (as in build-example.sh). Release tags come through it.
if [[ ! "$REF" =~ ^v$num\.$num\.$num$ ]]; then
  export GOPRIVATE="$SCHEMAF_MODULE"
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
CLONE="$WORK/$EXAMPLE_NAME"

echo "▶ example release: $EXAMPLE_REPO $TAG with schemaf $REF"

if gh release view "$TAG" -R "$EXAMPLE_REPO" >/dev/null 2>&1; then
  say "$TAG is already released — checking it only"
else
  say "clone $EXAMPLE_REPO"
  gh repo clone "$EXAMPLE_REPO" "$CLONE" -- -q
  cd "$CLONE"
  if git ls-remote --exit-code --tags origin "refs/tags/$TAG" >/dev/null; then
    fail "the tag $TAG exists in $EXAMPLE_REPO but has no release — its release run failed: see https://github.com/$EXAMPLE_REPO/actions, delete the tag (git push origin :refs/tags/$TAG) and run again"
  fi
  [ -f schemaf.toml ] || fail "$EXAMPLE_REPO has no project yet — set it up once (EXTEND.md › The example release › One-time setup)"
  say "upgrade to schemaf $REF"
  ( cd go && go get "${SCHEMAF_MODULE}@${REF}" && go mod tidy )
  ./schemaf.sh codegen
  [ -f .github/workflows/release.gen.yml ] || fail "codegen wrote no .github/workflows/release.gen.yml"

  say "commit, push main, tag $TAG"
  git add -A
  git -c user.name=schemaf-release -c user.email=schemaf-release@users.noreply.github.com -c commit.gpgsign=false \
    commit -q --allow-empty -m "schemaf $REF"
  git push -q origin HEAD:main
  git tag "$TAG"
  git push -q origin "$TAG"

  say "wait for the release run of $TAG"
  run=""
  for _ in $(seq 1 60); do
    run=$(gh run list -R "$EXAMPLE_REPO" --workflow release.gen.yml --branch "$TAG" --json databaseId -q '.[0].databaseId' 2>/dev/null || true)
    [ -n "$run" ] && break
    sleep 5
  done
  [ -n "$run" ] || fail "no release run started for $TAG within 5 minutes: https://github.com/$EXAMPLE_REPO/actions"
  gh run watch "$run" -R "$EXAMPLE_REPO" --exit-status >/dev/null \
    || fail "the release run of $TAG failed: https://github.com/$EXAMPLE_REPO/actions/runs/$run"
fi

say "check the GitHub release"
ART="${EXAMPLE_NAME}-${VERSION}.run"
BUNDLE="${ART}.sigstore.json"
release=$(gh release view "$TAG" -R "$EXAMPLE_REPO" --json isPrerelease,assets)
[ "$(jq -r .isPrerelease <<<"$release")" = "$PRERELEASE" ] || fail "release $TAG: prerelease is $(jq -r .isPrerelease <<<"$release"), want $PRERELEASE"
for f in "$ART" "$BUNDLE"; do
  jq -e --arg f "$f" '.assets | map(.name) | index($f)' <<<"$release" >/dev/null || fail "release $TAG has no $f"
done

say "verify the attestation: signed by $EXAMPLE_REPO's release.gen.yml on $TAG"
mkdir -p "$WORK/dl"
gh release download "$TAG" -R "$EXAMPLE_REPO" -D "$WORK/dl" -p "$ART" -p "$BUNDLE"
gh attestation verify "$WORK/dl/$ART" --bundle "$WORK/dl/$BUNDLE" --repo "$EXAMPLE_REPO" \
  --signer-workflow "$EXAMPLE_REPO/.github/workflows/release.gen.yml" --source-ref "refs/tags/$TAG" >/dev/null \
  || fail "the attestation of $ART does not verify"

say "the artifact's manifest names version $VERSION"
line=$(awk '/^__SCHEMAF_ARCHIVE__$/ { print NR + 1; exit }' "$WORK/dl/$ART")
[ -n "$line" ] || fail "$ART is not a schemaf artifact (no archive marker)"
manifest=$(tail -n +"$line" "$WORK/dl/$ART" | tar -xzO ./manifest)
grep -qx "tag=$VERSION" <<<"$manifest" || fail "the manifest of $ART names $(grep '^tag=' <<<"$manifest"), want tag=$VERSION"

say "releases.json lists $TAG"
index_url="https://github.com/$EXAMPLE_REPO/releases/download/schemaf-index/releases.json"
index=$(curl -fsSL "$index_url") || fail "could not download $index_url"
base="https://github.com/$EXAMPLE_REPO/releases/download/$TAG"
jq -e --arg v "$VERSION" --argjson pre "$PRERELEASE" --arg art "$base/$ART" --arg att "$base/$BUNDLE" \
  'map(select(.version == $v and .prerelease == $pre and .artifact == $art and .attestation == $att)) | length == 1' \
  <<<"$index" >/dev/null || fail "releases.json has no correct entry for $VERSION: $index"

say "the example pull: a new box installs v-1, then upgrades to $VERSION"
# The new run is the one with an ID above every run that existed before.
pull_runs() { gh run list -R "$SCHEMAF_REPO" --workflow example-pull.yml -L 20 --json databaseId,headBranch "$@"; }
before=$(pull_runs -q 'map(.databaseId) | max // 0') || fail "could not list the example-pull runs"
gh workflow run example-pull.yml -R "$SCHEMAF_REPO" --ref "$REF" -f version="$VERSION" \
  || fail "could not start example-pull.yml on $REF"
run=""
for _ in $(seq 1 60); do
  run=$(pull_runs -q "map(select(.databaseId > $before and .headBranch == \"$REF\")) | min_by(.databaseId).databaseId // empty" 2>/dev/null || true)
  [ -n "$run" ] && break
  sleep 5
done
[ -n "$run" ] || fail "the example pull did not start within 5 minutes: https://github.com/$SCHEMAF_REPO/actions/workflows/example-pull.yml"
gh run watch "$run" -R "$SCHEMAF_REPO" --exit-status >/dev/null \
  || fail "the example pull failed: https://github.com/$SCHEMAF_REPO/actions/runs/$run"

echo "✓ example release $TAG: attested artifact, releases.json, and a new box that pulled v-1 and upgraded to it"
