# Sourced by merge.sh and release.sh: the GitHub issues a range of commits closes.
#
# Convention (CLAUDE.md): a commit that finishes an issue carries the trailer
# line `Closes #<n>` in its body: exactly that form, one issue per line. The
# `(#<n>)` at the end of a subject is a reference only and closes nothing.

# closing_issues <range> [--merged]: prints the issue numbers the commits in
# <range> close, sorted and unique, one per line. Dies on a line that looks like
# a closing keyword but isn't the exact form, so a typo can't silently leave an
# issue open. With --merged (commits already on main, which can't be reworded)
# it warns instead.
closing_issues() {
	local range=$1 merged=${2:-} sha lines bad="" found=""
	for sha in $(git rev-list "$range"); do
		lines=$(git log -1 --format=%B "$sha" | sed 's/[[:space:]]*$//')
		found+=$(grep -E '^Closes #[0-9]+$' <<<"$lines" | sed 's/^Closes #//' || true)$'\n'
		while IFS= read -r line; do
			[[ -n "$line" ]] && bad+="  $(git log -1 --format='%h %s' "$sha"): \"$line\""$'\n'
		done < <(grep -iE '^[[:space:]]*(close[sd]?|fix(e[sd])?|resolve[sd]?)[[:space:]]*:?[[:space:]]*[A-Za-z0-9_./-]*#[0-9]+' <<<"$lines" |
			grep -vE '^Closes #[0-9]+$' || true)
	done
	if [[ -n "$bad" && "$merged" == --merged ]]; then
		echo "warning: these merged commit lines look like they close an issue, but aren't the form \`Closes #<n>\`; close those issues by hand if they're done:" >&2
		printf '%s' "$bad" >&2
	elif [[ -n "$bad" ]]; then
		echo "aborted: these commit lines look like they close an issue, but aren't the form \`Closes #<n>\` (alone on its line):" >&2
		printf '%s' "$bad" >&2
		echo "  reword the commits (git rebase -i origin/main → reword) so each reads \`Closes #<n>\`, then run again" >&2
		exit 1
	fi
	grep -E '^[0-9]+$' <<<"$found" | sort -nu || true
}

# check_issues <n>...: dies if an issue doesn't exist (a typo in a trailer);
# prints each issue with its title and state.
check_issues() {
	local n info
	for n in "$@"; do
		info=$(gh issue view "$n" --json title,state -q '"\(.state | ascii_downcase): \(.title)"' 2>/dev/null) ||
			{ echo "aborted: a commit says \`Closes #$n\`, but issue #$n doesn't exist in this repo" >&2; exit 1; }
		echo "    #$n $info"
	done
}

# announce_issues <n>...: tells what the merge will close, before it happens.
announce_issues() {
	if [[ $# -eq 0 ]]; then
		echo "▶ closes no issues (no \`Closes #<n>\` trailer in the new commits)"
	else
		echo "▶ closes on merge:"
		check_issues "$@"
	fi
}

# pr_ensure_closes <branch> <n>...: makes sure the open PR of <branch> says
# `Closes #<n>` for each issue, so GitHub closes them when the PR merges.
# Appends missing lines and keeps whatever the body already says.
pr_ensure_closes() {
	local branch=$1 body add="" n
	shift
	[[ $# -gt 0 ]] || return 0
	body=$(gh pr view "$branch" --json body -q .body)
	for n in "$@"; do
		grep -qE "^Closes #$n[[:space:]]*\$" <<<"$body" || add+="Closes #$n"$'\n'
	done
	[[ -n "$add" ]] || return 0
	[[ -z "$body" ]] || body+=$'\n\n'
	gh pr edit "$branch" --body "$body$add" >/dev/null
}

# close_released <version> <n>...: closes the issues that are still open, saying
# which release shipped them. For releases without a PR to close them.
close_released() {
	local version=$1 n
	shift
	for n in $(open_issues "$@"); do
		gh issue close "$n" --comment "Released in $version." >/dev/null
		echo "  closed #$n (released in $version)"
	done
}

# open_issues <n>...: prints the issues that are still open.
open_issues() {
	local n state
	for n in "$@"; do
		state=$(gh issue view "$n" --json state -q .state) ||
			{ echo "error: can't read the state of issue #$n (gh issue view $n failed)" >&2; exit 1; }
		[[ "$state" == OPEN ]] && echo "$n"
	done
	return 0
}

# require_closed <n>...: after a merge, checks that GitHub closed every issue.
# GitHub closes them asynchronously, so it waits up to ~30s. Dies loudly with
# the command to close the rest by hand.
require_closed() {
	[[ $# -gt 0 ]] || return 0
	local left try
	for try in 1 2 3 4 5 6 7 8 9 10; do
		left=$(open_issues "$@")
		[[ -n "$left" ]] || { echo "  closed: $(printf '#%s ' "$@")"; return 0; }
		sleep 3
	done
	echo "error: the merge should have closed these issues, but they are still open: $(printf '#%s ' $left)" >&2
	echo "  check the PR's body for the \`Closes #<n>\` lines, then close them by hand:" >&2
	for n in $left; do echo "    gh issue close $n" >&2; done
	exit 1
}
