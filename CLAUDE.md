# CLAUDE.md — schemaf

## Documentation-First

**The README is the source of truth.**

This project is designed documentation-first. Features are designed by writing the README and docs _before_ implementation. If the README doesn't describe it, the feature doesn't exist yet.

- Read `README.md` before touching any code
- If implementation diverges from the README, the README wins — fix the code, not the docs
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
go/api/          - API registry + OpenAPI generation
go/server/       - Server framework (gateway, routing, frontend proxy/embed)
go/schemaf/      - App lifecycle (schemaf.New, app.Run)
go/cli/          - CLI framework (subcommands, config/state)
go/compose/      - Compose dependency resolver (x-schemaf metadata)
go/db/           - Database helpers + migrations
compose/         - Reusable compose blocks (postgres, etc.)
example/         - Example project consuming the framework
```

## Normative Conventions

These are framework-wide conventions. Do not deviate.

**Generated files** use `.gen.` infix: `*.gen.go`, `*.gen.ts`

**Project layout** (enforced, not configurable):
- `go/db/migrations/` → input migrations
- `go/db/queries/` → sqlc input
- `go/db/migrations.gen.go` → generated `db.Provider`
- `go/db/queries.gen.go` → generated sqlc query functions
- `go/api/*.go` → endpoint struct implementations
- `go/api/endpoints.gen.go` → generated `api.Provider`
- `frontend/src/api/generated/api.gen.ts` → generated TypeScript client
- `compose.gen.yml` → generated base compose (postgres + backend)
- `compose.dev.yml` → generated dev overlay (exposed ports)
- `compose/*.yml` → project-specific compose extensions

**Port allocation** (fixed):
- 7000 — application server
- 7002 — frontend dev server
- 7003 — Postgres
- 7010+ — project-specific services

**Config file**: `schemaf.toml` (two fields only: `title`, `name`)

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

The `/example` directory contains an example project built using SchemaF. Run `/example/codegen.sh` to generate code (including `test.gen.sh`) and then run `test.gen.sh` to execute the tests.
Don't run tests directly unless you're debugging a specific issue. The `test.gen.sh` script handles dependencies and ensures consistent test execution - it executes both unit and integration tests, 
including tests written in golang as well as in TypeScript (wrapped in golang). Only running `teste.gen.sh` guarantees that all tests are executed consistently.
