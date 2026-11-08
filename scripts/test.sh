#!/usr/bin/env bash
# Run framework tests: scripts/test.sh <unit|db|e2e|all>   (normally via `make test <kind>`)
# REF=<tag> selects the online tag for the onboarding e2e.
set -euo pipefail

case "${1:-}" in
	unit) go test ./... ;;
	db)   ./e2e/db-test.sh ;;
	e2e)  ./e2e/build-example.sh ${REF:-} ;;
	all)
		go test ./...
		./e2e/db-test.sh
		./e2e/build-example.sh ${REF:-}
		;;
	"")
		echo "Usage: make test <unit|db|e2e|all>"
		echo "  unit  fast Go unit tests (real-Postgres tests auto-skip)"
		echo "  db    DB integration tests on an ephemeral Postgres"
		echo "  e2e   from-scratch onboarding e2e (online tag; REF=<tag>)"
		echo "  all   unit + db + e2e"
		;;
	*) echo "unknown test kind '$1' — use unit|db|e2e|all" >&2; exit 1 ;;
esac
