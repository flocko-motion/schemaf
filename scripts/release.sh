#!/usr/bin/env bash
# Release main, tagging <major|minor|patch> (aliases: breaking|feature|fix).
# On main (HEAD = origin/main): local checks go build / vet / test, then the
# release. On a feature branch: merge it first (scripts/merge.sh: PR, CI gate,
# merge), then release the merge commit on origin/main without switching
# branches. On macOS the full local gate (make test all under podman) runs
# against the commit to release, which CI doesn't cover. Only if all pass: tag,
# push the tag, create the GitHub release. See EXTEND.md › Merging and
# releasing.
# Usage: scripts/release.sh <major|minor|patch>   (normally via `make release <bump>`)
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/issues.sh

die() { echo "release aborted: $*" >&2; exit 1; }

case "${1:-}" in
	major | breaking | minor | feature | patch | fix) bump=$1 ;;
	*) echo "Usage: make release <major|minor|patch>  (aliases: breaking=major, feature=minor, fix=patch)" >&2; exit 1 ;;
esac

if [[ -n "$(git status --porcelain)" ]]; then
	git status --short >&2
	die "working tree not clean — commit or stash first"
fi

git fetch -q --tags origin
git fetch -q origin main
branch=$(git rev-parse --abbrev-ref HEAD)
checked=0 # whether merge.sh already ran the fast local checks on this tree
if [[ "$(git rev-parse HEAD)" != "$(git rev-parse origin/main)" ]]; then
	[[ "$branch" != main && "$branch" != HEAD ]] ||
		die "HEAD ($(git rev-parse --short HEAD), $branch) is not origin/main ($(git rev-parse --short origin/main)) — a release tags what is on main: git pull --ff-only, or push your commits through a PR"
	echo "▶ on feature branch '$branch': merge it first (make merge), then release main"
	./scripts/merge.sh || die "the merge of '$branch' didn't happen — nothing tagged (see above)"
	git fetch -q origin main
	# The merge commit must carry exactly the branch's tree: then the checks
	# that ran on the branch (local and CI) tested what gets tagged.
	[[ "$(git rev-parse 'origin/main^{tree}')" == "$(git rev-parse 'HEAD^{tree}')" ]] ||
		die "'$branch' is merged, but origin/main ($(git rev-parse --short origin/main)) is not exactly its content (main moved in between) — nothing tagged; release from main: git switch main && git pull --ff-only && make release $bump"
	checked=1
fi
head=$(git rev-parse origin/main)

# Latest RELEASE version (vX.Y.Z only) — the base for the bump.
latest=$(git tag -l 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1 || true)
if [[ -n "$latest" ]]; then
	range="$latest..$head"
	[[ "$(git rev-list --count "$range")" -gt 0 ]] || die "nothing to release: main is already at $latest"
else
	latest="v0.0.0"
	range=$head
fi
IFS='.' read -r major minor patch <<< "${latest#v}"
case "$bump" in
	major | breaking) major=$((major + 1)); minor=0; patch=0 ;;
	minor | feature)  minor=$((minor + 1)); patch=0 ;;
	patch | fix)      patch=$((patch + 1)) ;;
esac
new="v${major}.${minor}.${patch}"
echo "▶ release $latest → $new at $(git rev-parse --short "$head")"

# Issues the released commits close that are still open (commits that reached
# main without a closing PR) are closed after tagging.
issues=$(closing_issues "$range" --merged)

if [[ "$checked" == 0 ]]; then
	echo "▶ go build ./..."
	go build ./...
	echo "▶ go vet ./..."
	go vet ./...
	echo "▶ go test ./..."
	go test ./...
fi

# The local gate: on macOS the full suite, against the commit by its full hash
# (it is on origin/main, so pushed). On Linux the commit already passed the CI
# gate as part of its up-to-date PR.
if [[ "$(uname)" == Darwin ]]; then
	echo "▶ local gate (macOS): make test all under podman against $head"
	SCHEMAF_RUNTIME=podman REF="$head" ./scripts/test.sh all
else
	echo "▶ no local gate on $(uname): $head passed the CI gate in its PR"
fi

git tag "$new" "$head"
git push origin "$new"
gh release create "$new" --title "$new" --generate-notes || echo "  (gh release create failed; tag $new is pushed — create the GitHub release manually if needed)"
close_released "$new" $issues
echo "  released $new"
