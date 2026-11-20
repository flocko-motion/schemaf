#!/usr/bin/env bash
# Merge the current branch into main via PR — without releasing.
# Gate: go build / vet / test. The full onboarding e2e runs once per release
# (`make release`), over everything merged since the last one.
# Usage: scripts/merge.sh   (normally via `make merge`)
set -euo pipefail
source "$(dirname "$0")/issues.sh"

die() { echo "merge aborted: $*" >&2; exit 1; }

branch=$(git rev-parse --abbrev-ref HEAD)
[[ "$branch" != main && "$branch" != HEAD ]] || die "on '$branch' — run it from the feature branch you want to merge"

if [[ -n "$(git status --porcelain)" ]]; then
	git status --short >&2
	die "working tree not clean — commit or stash first"
fi

# The branch must contain origin/main, so the checks below test exactly what
# lands on main. Checked again right before merging.
up_to_date() {
	git fetch -q origin main
	git merge-base --is-ancestor origin/main HEAD ||
		die "branch is behind main — run: git merge origin/main, then make merge again"
}
up_to_date
[[ "$(git rev-list --count origin/main..HEAD)" -gt 0 ]] || die "nothing to merge: '$branch' has no commits beyond main"

# The issues the new commits close (`Closes #<n>` trailers). Checked before the
# gate, so a typo fails fast.
issues=$(closing_issues origin/main..HEAD)
announce_issues $issues

echo "▶ go build ./..."
go build ./...
echo "▶ go vet ./..."
go vet ./...
echo "▶ go test ./..."
go test ./...

up_to_date

echo "▶ push $branch"
git push -u origin "$branch"

if ! gh pr view "$branch" --json state -q .state 2>/dev/null | grep -qx OPEN; then
	echo "▶ open PR"
	gh pr create --base main --head "$branch" --fill
fi
pr_ensure_closes "$branch" $issues

echo "▶ merge $branch → main"
gh pr merge "$branch" --merge --delete-branch=false
require_closed $issues
echo "  merged $branch into main (no release — run make release to tag)"
