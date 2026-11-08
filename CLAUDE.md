# CLAUDE.md — schemaf

> **⚠️ SPECIFICATION-FIRST PROJECT**  
> This project is designed **documentation-first**. The README is the source of truth.  
> **DO NOT modify the architecture without first discussing and implementing changes to the documentation together with the user.**  
> All design decisions are made through pair programming and captured here before implementation.

## Documentation-First

**The documentation (`README.md`, `INSTALL.md`, `EXTEND.md`) is the source of truth.**

This project is designed documentation-first. Features are designed by writing the docs _before_ implementation. If the docs don't describe it, the feature doesn't exist yet.

- Read `README.md`, `INSTALL.md`, and `EXTEND.md` before touching any code
- If implementation diverges from the docs, the docs win — fix the code, not the docs
- Never modify the architecture without first discussing and updating the documentation together with the user

## Pair Programming Approach

**Architectural decisions require discussion.**

Do not make architectural changes unilaterally. This includes:
- Adding new packages or top-level directories
- Changing the provider pattern or how framework components are wired
- Changing codegen output paths or naming conventions
- Adding new CLI commands to the framework
- Changing port assignments or compose layout
- Changing the normative project structure
- Changing how framework and project compose files are merged

When you identify a need for an architectural change: surface it, propose it, and wait for the user to agree before implementing. Treat design decisions as pair programming — think out loud, discuss, then act.

## Repository Structure

```
api/             - API registry + OpenAPI generation
schemaf/         - App lifecycle (schemaf.New, app.Run)
cli/             - CLI framework (subcommands, config/state)
compose/         - Compose dependency resolver (x-schemaf metadata)
db/              - Database helpers + migrations
ai/              - AI provider integrations
log/             - Logging
cmd/schemaf/     - CLI entrypoint
e2e/             - From-scratch onboarding e2e + DB integration harness
```

## Normative Conventions

These are framework-wide conventions. Do not deviate.

**Generated files** use `.gen.` infix: `*.gen.go`, `*.gen.ts`

**Project layout** (enforced, not configurable):
- `db/migrations/` → input migrations
- `db/queries/` → sqlc input
- `db/migrations.gen.go` → generated `db.Provider`
- `db/queries.gen.go` → generated sqlc query functions
- `api/*.go` → endpoint struct implementations
- `api/endpoints.gen.go` → generated `api.Provider`
- `frontend/src/api/generated/api.gen.ts` → generated TypeScript client
- `apiclient/client.gen.go` → generated Go API client (via oapi-codegen)
- `compose.gen.yml` → generated base compose (postgres + backend)
- `compose.dev.gen.yml` → generated dev overlay (exposed ports)
- `compose/*.yml` → project-specific compose extensions

**Port allocation** (base port configurable via `port` in schemaf.toml, default 8000):
- port — application server
- port+2 — frontend dev server
- port+3 — Postgres
- port+10+ — project-specific services

**Config file**: `schemaf.toml` (fields: `title`, `name`, optional `port` — default 8000)

**Secrets**: stored in `~/.<name>/etc/env` (prod) and `~/.<name>/dev/etc/env` (dev) — never in project directories

## Code Style

- Minimize configuration — convention over configuration everywhere
- Auto-discover files; never require manual registration
- Provider pattern for wiring: `app.AddDb(db.Provider)`, `app.AddApi(api.Provider)`
- Services registered via `app.AddService()` only run for `server`/`dev` commands — keep CLI commands fast
- Do not add glue code that the user should not have to write — that's what codegen is for

## Golden Rule

> If it can be generalized, put it in schemaf. If arbitrary decisions need to be made: decide them normatively in the framework. Leave only creative decisions to the application layer.

When in doubt about whether something belongs in the framework or the application: cement it in the framework.

## Running Tests

There is **no committed example project**. Tests come in three layers:

- **Unit tests** — `go test ./...`. Real-Postgres tests auto-skip when `DATABASE_URL` is unset.
- **DB integration tests** — `e2e/db-test.sh`. Spins an ephemeral Postgres, exports `DATABASE_URL`, and runs the gated tests in `db/` (migration runner, `RunSet`, reset helper). Tears down automatically.
- **From-scratch onboarding e2e** — `e2e/build-example.sh [<ref>]`. Builds a complete project in `/tmp` from a single `go run …/cmd/schemaf@<ref> init` call against an online tag, then compiles and tests it. This is the real onboarding lifecycle and replaces the old in-repo example. The dev loop `e2e/tag-and-test.sh` tags HEAD as the non-semver `e2e` tag, pushes it, and runs the e2e against it.

Container runtime: everything follows `SCHEMAF_RUNTIME` (podman by default, `docker` on request); the release gate runs under podman only. Whenever runtime-related code changes, also run `SCHEMAF_RUNTIME=docker make test e2e` by hand. Every container call goes through `cli.Container` (Go) or `_schemaf_container` (generated `schemaf.sh`) — never call `docker`/`podman` directly.
