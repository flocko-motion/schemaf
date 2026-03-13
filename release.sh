#!/usr/bin/env bash
set -euo pipefail

BUMP="${1:-}"
if [[ -z "$BUMP" || ! "$BUMP" =~ ^(major|minor|patch)$ ]]; then
  echo "Usage: ./release.sh <major|minor|patch>" >&2
  exit 1
fi

# Get latest tag
LATEST=$(git tag -l 'v*' --sort=-v:refname | head -1)
if [[ -z "$LATEST" ]]; then
  LATEST="v0.0.0"
fi

# Parse version
IFS='.' read -r MAJOR MINOR PATCH <<< "${LATEST#v}"

case "$BUMP" in
  major) MAJOR=$((MAJOR + 1)); MINOR=0; PATCH=0 ;;
  minor) MINOR=$((MINOR + 1)); PATCH=0 ;;
  patch) PATCH=$((PATCH + 1)) ;;
esac

NEW="v${MAJOR}.${MINOR}.${PATCH}"

echo "  ${LATEST} → ${NEW}"
git tag "$NEW"
git push origin main "$NEW"
echo "  released ${NEW}"