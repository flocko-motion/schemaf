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

release: ## Release current main, tagging <major|minor|patch> (aliases: breaking|feature|fix); from a branch it merges first
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

	if [[ -n "$$(git status --porcelain)" ]]; then
		echo "release aborted: working tree not clean — commit or stash first:" >&2
		git status --short >&2
		exit 1
	fi

	# gate the current HEAD: publish a unique, immutable prerelease tag and run
	# the full onboarding e2e against it. cleanup_pre removes it afterward.
	PRE=""
	gate() {
		PRE="$$new-pre.$$(date -u +%Y%m%d%H%M%S).g$$(git rev-parse --short HEAD)"
		echo "▶ gate: onboarding e2e against $$PRE"
		git tag "$$PRE"
		git push origin "$$PRE"
		./e2e/build-example.sh "$$PRE"
	}
	cleanup_pre() {
		[[ -n "$$PRE" ]] || return 0
		git push origin ":refs/tags/$$PRE" >/dev/null 2>&1 || true
		git tag -d "$$PRE" >/dev/null 2>&1 || true
	}

	git fetch -q origin main
	branch=$$(git rev-parse --abbrev-ref HEAD)

	# Never release while sitting on main (protected-branch workflow). Branch off
	# FIRST — carrying any local commits — and reset main back to origin/main, so
	# the release proceeds from a feature branch and never leaves you on main.
	if [[ "$$branch" == main ]]; then
		if [[ "$$(git rev-parse HEAD)" == "$$(git rev-parse origin/main)" && "$$(git rev-list --count "$$latest"..HEAD 2>/dev/null || echo 999)" == 0 ]]; then
			echo "nothing to release: main is already at $$latest." >&2
			exit 1
		fi
		if [[ ! -t 0 ]]; then
			echo "release aborted: on main — re-run from a feature branch (no TTY to prompt)." >&2
			exit 1
		fi
		echo "You're on main; releases run from a feature branch."
		read -r -p "  branch name to release from [work]: " fb
		fb=$${fb:-work}
		git switch -c "$$fb"
		git branch -f main origin/main
		branch="$$fb"
		echo "  → on '$$fb'; main reset to origin/main"
	fi

	if [[ "$$(git rev-list --count origin/main..HEAD)" -gt 0 ]]; then
		# New commits not yet on main → gate, then merge into main via PR (merge commit).
		echo "▶ local sanity: go test ./..."
		go test ./...
		git push -u origin "$$branch"
		gate
		echo "▶ merge $$branch → main"
		gh pr create --base main --head "$$branch" --title "release $$new" --body "Automated release $$new" 2>/dev/null || true
		gh pr merge "$$branch" --merge --delete-branch=false
		git fetch -q origin main
		release_ref=origin/main
	else
		# Branch already matches main → release code merged to main earlier.
		count=$$(git rev-list --count "$$latest"..HEAD 2>/dev/null || echo 999)
		if [[ "$$count" == 0 ]]; then
			echo "nothing to release: main is already at $$latest." >&2
			exit 1
		fi
		echo "▶ local sanity: go test ./..."
		go test ./...
		gate
		release_ref=HEAD
	fi

	echo "  $$latest → $$new"
	git tag "$$new" "$$release_ref"
	git push origin "$$new"
	gh release create "$$new" --title "$$new" --generate-notes || echo "  (gh release create failed; tag $$new is pushed — create the GitHub release manually if needed)"
	cleanup_pre
	echo "  released $$new on main; now on '$$branch'"

# No-op targets that absorb the positional word in `make test <kind>` and
# `make release <bump>`, so the extra goal doesn't fail with "No rule to make
# target". They carry no ## doc, so they stay out of `make help`.
unit db e2e all major minor patch breaking feature fix:
	@:
