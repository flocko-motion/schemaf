# gh-runs.sh — waiting for a GitHub Actions run (sourced by example-release.sh;
# tested in scripts/scripts_test.go with a stubbed gh).

# await_run <repo> <run id> — waits until GitHub serves the run (one that was
# just created can answer 404 for a moment), then until it completes, and
# prints its conclusion (success, failure, cancelled, ...). Fails only if the
# run never becomes visible (2 minutes) or doesn't complete within 60 minutes;
# whether the run passed is decided by the caller from what it prints, never
# by a gh exit code.
await_run() {
  local repo="$1" id="$2" i state
  for i in $(seq 1 60); do
    gh run view "$id" -R "$repo" --json status >/dev/null 2>&1 && break
    if [ "$i" -eq 60 ]; then
      echo "run $id of $repo did not become visible within 2 minutes: https://github.com/$repo/actions/runs/$id" >&2
      return 1
    fi
    sleep 2
  done
  for i in $(seq 1 240); do
    state=$(gh run view "$id" -R "$repo" --json status,conclusion -q '.status + " " + .conclusion' 2>/dev/null || true)
    case "$state" in
      "completed "?*) echo "${state#completed }"; return 0 ;;
    esac
    sleep 15
  done
  echo "run $id of $repo did not complete within 60 minutes: https://github.com/$repo/actions/runs/$id" >&2
  return 1
}
