# Makefile — commands for working ON the schemaf framework.
# (schemaf.sh is the entrypoint for PROJECTS that consume schemaf; this is not.)
SHELL := bash
.ONESHELL:
.DEFAULT_GOAL := help
.PHONY: help build test db-test e2e release

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  make %-9s %s\n", $$1, $$2}'

build: ## Compile the framework
	go build ./...

test: ## Unit tests (real-Postgres tests auto-skip without DATABASE_URL)
	go test ./...

db-test: ## DB integration tests against an ephemeral Postgres
	./e2e/db-test.sh

e2e: ## From-scratch onboarding e2e against the latest online tag
	./e2e/build-example.sh

release: ## Tag + push a release: make release BUMP=<major|minor|patch> (aliases: breaking|feature|fix)
	@set -euo pipefail
	latest=$$(git tag -l 'v*' --sort=-v:refname | head -1)
	[[ -n "$$latest" ]] || latest="v0.0.0"
	IFS='.' read -r major minor patch <<< "$${latest#v}"
	case "$(BUMP)" in
		major | breaking) major=$$((major + 1)); minor=0; patch=0 ;;
		minor | feature)  minor=$$((minor + 1)); patch=0 ;;
		patch | fix)      patch=$$((patch + 1)) ;;
		*) echo "Usage: make release BUMP=<major|minor|patch>  (aliases: breaking=major, feature=minor, fix=patch)" >&2; exit 1 ;;
	esac
	new="v$${major}.$${minor}.$${patch}"
	echo "  $$latest → $$new"
	git tag "$$new"
	git push origin main "$$new"
	echo "  released $$new"
