#!/usr/bin/env bash
# Merge the current branch into main via PR — without releasing.
# Local checks: go build / vet / test. Then push, open the PR, and wait for the
# CI gate (.github/workflows/gate.yml) on the PR's head commit: green → merge,
# red or timeout → nothing is merged. See EXTEND.md › Merging and releasing.
# Usage: scripts/merge.sh   (normally via `make merge`)
set -euo pipefail
source "$(dirname "$0")/issues.sh"

# The CI wait (framework constants).
GATE_WORKFLOW=gate.yml   # the workflow whose run decides
GATE_TIMEOUT_MIN=45      # waiting longer counts as a failure
GATE_POLL_S=20           # seconds between status checks
GATE_FIND_TRIES=9        # polls (3 minutes) for the run to show up

die() { echo "merge aborted: $*" >&2; exit 1; }

branch=$(git rev-parse --abbrev-ref HEAD)
[[ "$branch" != main && "$branch" != HEAD ]] || die "on '$branch' — run it from the feature branch you want to merge"

if [[ -n "$(git status --porcelain)" ]]; then
	git status --short >&2
	die "working tree not clean — commit or stash first"
fi

# The branch must contain origin/main, so CI tests exactly what lands on main
# (branch protection requires it too). Checked again before pushing and merging.
up_to_date() {
	git fetch -q origin main
	git merge-base --is-ancestor origin/main HEAD ||
		die "branch is behind main — run: git merge origin/main, then make merge again"
}
up_to_date
[[ "$(git rev-list --count origin/main..HEAD)" -gt 0 ]] || die "nothing to merge: '$branch' has no commits beyond main"

# The issues the new commits close (`Closes #<n>` trailers). Checked before the
# checks, so a typo fails fast.
issues=$(closing_issues origin/main..HEAD)
announce_issues $issues

echo "▶ go build ./..."
go build ./...
echo "▶ go vet ./..."
go vet ./...
echo "▶ go test ./..."
go test ./...

up_to_date
sha=$(git rev-parse HEAD)

echo "▶ push $branch"
git push -u origin "$branch"

if ! gh pr view "$branch" --json state -q .state 2>/dev/null | grep -qx OPEN; then
	echo "▶ open PR"
	gh pr create --base main --head "$branch" --fill
fi
pr_ensure_closes "$branch" $issues

# Find the gate run of the PR's head commit: workflow + head SHA + event. None
# (after waiting for it to show up) or several is a loud error.
echo "▶ CI gate: waiting for the $GATE_WORKFLOW run of $sha"
run=""
for ((try = 1; try <= GATE_FIND_TRIES; try++)); do
	runs=$(gh run list --workflow "$GATE_WORKFLOW" --commit "$sha" --event pull_request \
		--json databaseId,url --jq '.[] | "\(.databaseId) \(.url)"') ||
		die "could not list the CI runs (gh run list --workflow $GATE_WORKFLOW --commit $sha failed)"
	case $(grep -c . <<<"$runs" || true) in
		0) sleep "$GATE_POLL_S" ;;
		1) run=$runs; break ;;
		*)
			echo "$runs" | sed 's/^/    /' >&2
			die "several $GATE_WORKFLOW runs for $sha (pull_request) — can't tell which decides; delete the stale ones (gh run delete <id>), then make merge again"
			;;
	esac
done
[[ -n "$run" ]] || die "no $GATE_WORKFLOW run for $sha (pull_request) after $((GATE_FIND_TRIES * GATE_POLL_S))s — is the workflow on this branch, and is the PR open? Nothing merged."
run_id=${run%% *} run_url=${run#* }
echo "  run: $run_url"

result=""
for ((try = 0; try <= GATE_TIMEOUT_MIN * 60 / GATE_POLL_S; try++)); do
	state=$(gh run view "$run_id" --json status,conclusion --jq '"\(.status) \(.conclusion)"') ||
		die "could not read CI run $run_id (gh run view failed): $run_url"
	if [[ "$state" == completed\ * ]]; then
		result=${state#completed }
		break
	fi
	sleep "$GATE_POLL_S"
done

case "$result" in
	success) echo "  CI gate green" ;;
	"")
		die "the CI gate didn't finish within $GATE_TIMEOUT_MIN minutes — nothing merged, the PR stays open: $run_url
  look at the run; once it is green, run make merge again"
		;;
	*)
		die "the CI gate is red ($result) — nothing merged, the PR stays open: $run_url
  fix it on '$branch', push, and run make merge again"
		;;
esac

up_to_date
echo "▶ merge $branch → main"
gh pr merge "$branch" --merge --match-head-commit "$sha" --delete-branch=false
require_closed $issues
echo "  merged $branch into main (no release — run make release to tag)"
