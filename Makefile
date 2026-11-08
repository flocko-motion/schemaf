# Makefile — commands for working ON the schemaf framework.
# (schemaf.sh is the entrypoint for PROJECTS that consume schemaf; this is not.)
# Recipes stay one-liners (logic lives in scripts/) so this works with macOS's GNU Make 3.81.
SHELL := bash
.DEFAULT_GOAL := help
.PHONY: help build test release unit db e2e all major minor patch breaking feature fix

help: ## List available targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  make %-9s %s\n", $$1, $$2}'

build: ## Compile the framework
	go build ./...

test: ## Run tests: make test <unit|db|e2e|all>  (no arg lists the options)
	@REF="$(REF)" ./scripts/test.sh $(firstword $(filter-out test,$(MAKECMDGOALS)))

release: ## Release current main, tagging <major|minor|patch> (aliases: breaking|feature|fix); from a branch it merges first
	@./scripts/release.sh $(firstword $(filter-out release,$(MAKECMDGOALS)))

# No-op targets that absorb the positional word in `make test <kind>` and
# `make release <bump>`, so the extra goal doesn't fail with "No rule to make
# target". They carry no ## doc, so they stay out of `make help`.
unit db e2e all major minor patch breaking feature fix:
	@:
