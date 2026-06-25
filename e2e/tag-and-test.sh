#!/usr/bin/env bash
# tag-and-test.sh — the dev loop: publish the CURRENT commit online under a
# moving, non-semver 'e2e' tag, then build a project from it via the real
# online lifecycle.
#
# Why a non-semver tag: `go get …@e2e` resolves a non-semver tag to the commit
# and pins a pseudo-version (v0.0.0-<ts>-<commit>), so moving the tag to a new
# commit yields fresh content — sidestepping Go's "a version is immutable"
# proxy/sumdb caching. And because it isn't a v* tag, the upgrade/release
# scripts (which filter `git ls-remote … 'v*'`) never see it.
#
# Usage:
#   e2e/tag-and-test.sh                 # tag HEAD as 'e2e', push, build from it
#   E2E_TAG=foo REMOTE=upstream e2e/tag-and-test.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REMOTE="${REMOTE:-origin}"
TAG="${E2E_TAG:-e2e}"

echo "▶ tag HEAD ($(git rev-parse --short HEAD)) as '${TAG}' and push to ${REMOTE}"
git tag -f "$TAG"
git push -f "$REMOTE" "$TAG"

exec "$SCRIPT_DIR/build-example.sh" "$TAG"
