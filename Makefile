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

release: ## Release: e2e-gate via a -pre tag, then tag+push <major|minor|patch> (aliases: breaking|feature|fix)
	@set -euo pipefail
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
	# Release only from main, and only code that is actually merged & pushed there.
	branch=$$(git rev-parse --abbrev-ref HEAD)
	if [[ "$$branch" != main ]]; then
		echo "release aborted: must be on 'main' (currently on '$$branch')." >&2
		echo "  merge your work to main first, e.g.: gh pr create --fill --base main && gh pr merge --merge" >&2
		exit 1
	fi
	if [[ -n "$$(git status --porcelain)" ]]; then
		echo "release aborted: working tree not clean — commit or stash first:" >&2
		git status --short >&2
		exit 1
	fi
	git fetch -q origin main
	if [[ "$$(git rev-parse HEAD)" != "$$(git rev-parse origin/main)" ]]; then
		echo "release aborted: HEAD is not in sync with origin/main." >&2
		echo "  release tags an already-merged commit — push/merge so HEAD == origin/main." >&2
		exit 1
	fi
	echo "▶ local sanity: go test ./..."
	go test ./...
	# Gate: publish the exact commit as a unique, immutable prerelease and run the
	# full onboarding e2e against it. The -pre suffix keeps CI from building it;
	# the timestamp guarantees a never-reused version, g<sha> aids traceability.
	pre="$$new-pre.$$(date -u +%Y%m%d%H%M%S).g$$(git rev-parse --short HEAD)"
	echo "▶ gate: onboarding e2e against $$pre"
	git tag "$$pre"
	git push origin "$$pre"
	./e2e/build-example.sh "$$pre"
	# Gate passed → cut the real release. CI should build on vX.Y.Z, never on -pre.
	echo "  $$latest → $$new"
	git tag "$$new"
	git push origin "$$new"
	# The prerelease has served its purpose — remove it to keep the tag list clean.
	git push origin ":refs/tags/$$pre" >/dev/null 2>&1 || true
	git tag -d "$$pre" >/dev/null 2>&1 || true
	echo "  released $$new (pushed; CI triggers on $$new, not on -pre)"

# No-op targets that absorb the positional word in `make test <kind>` and
# `make release <bump>`, so the extra goal doesn't fail with "No rule to make
# target". They carry no ## doc, so they stay out of `make help`.
unit db e2e all major minor patch breaking feature fix:
	@:
