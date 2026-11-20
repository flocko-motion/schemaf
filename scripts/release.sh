#!/usr/bin/env bash
# Release current main, tagging <major|minor|patch> (aliases: breaking|feature|fix).
# From a branch with new commits it gates them, merges into main via PR, then tags.
# Usage: scripts/release.sh <major|minor|patch>   (normally via `make release <bump>`)
set -euo pipefail
source "$(dirname "$0")/issues.sh"

git fetch -q --tags origin
# Latest RELEASE version (vX.Y.Z only) — the base for the bump.
latest=$(git tag -l 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1 || true)
[[ -n "$latest" ]] || latest="v0.0.0"
IFS='.' read -r major minor patch <<< "${latest#v}"
case "${1:-}" in
	major | breaking) major=$((major + 1)); minor=0; patch=0 ;;
	minor | feature)  minor=$((minor + 1)); patch=0 ;;
	patch | fix)      patch=$((patch + 1)) ;;
	*) echo "Usage: make release <major|minor|patch>  (aliases: breaking=major, feature=minor, fix=patch)" >&2; exit 1 ;;
esac
new="v${major}.${minor}.${patch}"

if [[ -n "$(git status --porcelain)" ]]; then
	echo "release aborted: working tree not clean — commit or stash first:" >&2
	git status --short >&2
	exit 1
fi

# gate the current HEAD: run the full onboarding e2e against the pushed commit,
# by its full hash. A commit is immutable, so Go fetches it through the module
# proxy as a pseudo-version; no tag is created, so a failed gate leaves nothing.
gate() {
	local head
	head=$(git rev-parse HEAD)
	echo "▶ gate: onboarding e2e against $head"
	./e2e/build-example.sh "$head"
}

git fetch -q origin main
branch=$(git rev-parse --abbrev-ref HEAD)

# Never release while sitting on main (protected-branch workflow). Branch off
# FIRST — carrying any local commits — and reset main back to origin/main, so
# the release proceeds from a feature branch and never leaves you on main.
if [[ "$branch" == main ]]; then
	if [[ "$(git rev-parse HEAD)" == "$(git rev-parse origin/main)" && "$(git rev-list --count "$latest"..HEAD 2>/dev/null || echo 999)" == 0 ]]; then
		echo "nothing to release: main is already at $latest." >&2
		exit 1
	fi
	if [[ ! -t 0 ]]; then
		echo "release aborted: on main — re-run from a feature branch (no TTY to prompt)." >&2
		exit 1
	fi
	echo "You're on main; releases run from a feature branch."
	read -r -p "  branch name to release from [work]: " fb
	fb=${fb:-work}
	git switch -c "$fb"
	git branch -f main origin/main
	branch="$fb"
	echo "  → on '$fb'; main reset to origin/main"
fi

if [[ "$(git rev-list --count origin/main..HEAD)" -gt 0 ]]; then
	# New commits not yet on main → gate, then merge into main via PR (merge commit).
	issues=$(closing_issues origin/main..HEAD)
	announce_issues $issues
	echo "▶ local sanity: go test ./..."
	go test ./...
	git push -u origin "$branch"
	gate
	echo "▶ merge $branch → main"
	gh pr create --base main --head "$branch" --title "release $new" --body "Automated release $new" 2>/dev/null || true
	pr_ensure_closes "$branch" $issues
	gh pr merge "$branch" --merge --delete-branch=false
	git fetch -q origin main
	release_ref=origin/main
	close_issues() { require_closed $issues; }
else
	# Branch already matches main → release code merged to main earlier.
	count=$(git rev-list --count "$latest"..HEAD 2>/dev/null || echo 999)
	if [[ "$count" == 0 ]]; then
		echo "nothing to release: main is already at $latest." >&2
		exit 1
	fi
	# No PR here: the issues these commits close are closed after tagging.
	issues=$(closing_issues "$latest"..HEAD --merged)
	echo "▶ local sanity: go test ./..."
	go test ./...
	gate
	release_ref=HEAD
	close_issues() { close_released "$new" $issues; }
fi

echo "  $latest → $new"
git tag "$new" "$release_ref"
git push origin "$new"
gh release create "$new" --title "$new" --generate-notes || echo "  (gh release create failed; tag $new is pushed — create the GitHub release manually if needed)"
close_issues
echo "  released $new on main; now on '$branch'"
