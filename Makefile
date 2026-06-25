# Makefile — commands for working ON the schemaf framework.
# (schemaf.sh is the entrypoint for PROJECTS that consume schemaf; this is not.)
SHELL := bash
.ONESHELL:
.DEFAULT_GOAL := help
.PHONY: help build test release unit db e2e all major minor patch breaking feature fix

help: ## List available targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  make %-9s %s\n", $$1, $$2}'

build: ## Compile the framework
	go build ./...

test: ## Run tests: make test <unit|db|e2e|all>  (no arg lists the options)
	@set -euo pipefail
	case "$(firstword $(filter-out test,$(MAKECMDGOALS)))" in
		unit) go test ./... ;;
		db)   ./e2e/db-test.sh ;;
		e2e)  ./e2e/build-example.sh $(REF) ;;
		all)
			go test ./...
			./e2e/db-test.sh
			./e2e/build-example.sh $(REF)
			;;
		"")
			echo "Usage: make test <unit|db|e2e|all>"
			echo "  unit  fast Go unit tests (real-Postgres tests auto-skip)"
			echo "  db    DB integration tests on an ephemeral Postgres"
			echo "  e2e   from-scratch onboarding e2e (online tag; REF=<tag>)"
			echo "  all   unit + db + e2e"
			;;
		*) echo "unknown test kind '$(firstword $(filter-out test,$(MAKECMDGOALS)))' — use unit|db|e2e|all" >&2; exit 1 ;;
	esac

release: ## Release: gate, merge current branch into main via PR, tag <major|minor|patch> (aliases: breaking|feature|fix)
	@set -euo pipefail
	git fetch -q --tags origin
	# Latest RELEASE version — ignore -pre prereleases when picking what to bump.
	latest=$$(git tag -l 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$$' | head -1)
	[[ -n "$$latest" ]] || latest="v0.0.0"
	IFS='.' read -r major minor patch <<< "$${latest#v}"
	case "$(firstword $(filter-out release,$(MAKECMDGOALS)))" in
		major | breaking) major=$$((major + 1)); minor=0; patch=0 ;;
		minor | feature)  minor=$$((minor + 1)); patch=0 ;;
		patch | fix)      patch=$$((patch + 1)) ;;
		*) echo "Usage: make release <major|minor|patch>  (aliases: breaking=major, feature=minor, fix=patch)" >&2; exit 1 ;;
	esac
	new="v$${major}.$${minor}.$${patch}"
	branch=$$(git rev-parse --abbrev-ref HEAD)
	# main is protected (no direct push) — release runs from a feature branch and
	# merges it into main via a PR.
	if [[ "$$branch" == main ]]; then
		echo "release aborted: run from a feature branch, not main." >&2
		echo "  main is protected; release merges your branch into main via a PR." >&2
		exit 1
	fi
	if [[ -n "$$(git status --porcelain)" ]]; then
		echo "release aborted: working tree not clean — commit or stash first:" >&2
		git status --short >&2
		exit 1
	fi
	echo "▶ local sanity: go test ./..."
	go test ./...
	echo "▶ push branch '$$branch'"
	git push -u origin "$$branch"
	# Gate: publish the exact commit as a unique, immutable prerelease and run the
	# full onboarding e2e against it BEFORE anything lands on main.
	pre="$$new-pre.$$(date -u +%Y%m%d%H%M%S).g$$(git rev-parse --short HEAD)"
	echo "▶ gate: onboarding e2e against $$pre"
	git tag "$$pre"
	git push origin "$$pre"
	./e2e/build-example.sh "$$pre"
	# Merge the branch into main via PR (merge commit). main is protected, so this
	# is the only way the release reaches main.
	echo "▶ merge $$branch → main"
	gh pr create --base main --head "$$branch" --title "release $$new" --body "Automated release $$new" 2>/dev/null || true
	gh pr merge "$$branch" --merge --delete-branch=false
	# Tag the merged commit on main and push the tag (CI builds on vX.Y.Z, not -pre).
	git fetch -q origin main
	echo "  $$latest → $$new"
	git tag "$$new" origin/main
	git push origin "$$new"
	# Clean up the prerelease tag; we never left '$$branch'.
	git push origin ":refs/tags/$$pre" >/dev/null 2>&1 || true
	git tag -d "$$pre" >/dev/null 2>&1 || true
	echo "  released $$new on main (merged from $$branch); still on $$branch"

# No-op targets that absorb the positional word in `make test <kind>` and
# `make release <bump>`, so the extra goal doesn't fail with "No rule to make
# target". They carry no ## doc, so they stay out of `make help`.
unit db e2e all major minor patch breaking feature fix:
	@:
