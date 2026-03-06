# CLAUDE.md — schemaf

## Documentation-First

**The README is the source of truth.**

This project is designed documentation-first. Features are designed by writing the README and docs _before_ implementation. If the README doesn't describe it, the feature doesn't exist yet.

- Read `README.md` before touching any code
- Read `docs/CODEGEN.md`, `go/cli/README.md`, `compose/README.md` when working in those areas
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
go/atlas/        - App lifecycle and DB bootstrapping
go/cli/          - CLI framework (subcommands, config/state)
go/compose/      - Compose dependency resolver (x-schemaf metadata)
go/db/           - Database helpers + migrations
go/schemaf/      - Core app entrypoint (schemaf.New, app.Run)
compose/         - Reusable compose blocks (postgres, etc.)
example/         - Example project consuming the framework
```

## Normative Conventions

These are framework-wide conventions. Do not deviate.

**Generated files** use `.gen.` infix: `*.gen.go`, `*.gen.ts`

**Project layout** (enforced, not configurable):
- `go/sql/migrations/` → input migrations
- `go/sql/queries/` → sqlc input
- `go/db/migrations.gen.go` → generated `db.Provider`
- `go/db/queries.gen.go` → generated query functions
- `go/api/*.go` → handler implementations
- `go/api/endpoints.gen.go` → generated `api.Provider`
- `frontend/api/openapi.gen.ts` → generated TypeScript client

**Port allocation** (fixed):
- 7000 — application server
- 7002 — frontend dev server
- 7003 — Postgres
- 7010+ — project-specific services

**Config file**: `schemaf.toml` (two fields only: `title`, `name`)

## Code Style

- Minimize configuration — convention over configuration everywhere
- Auto-discover files; never require manual registration
- Provider pattern for wiring: `app.AddDb(db.Provider)`, `app.AddApi(api.Provider)`
- Services registered via `app.AddService()` only run for `server`/`dev` commands — keep CLI commands fast
- Do not add glue code that the user should not have to write — that's what codegen is for

## Golden Rule

> If it can be generalized, put it in schemaf. If arbitrary decisions need to be made: decide them normatively in the framework. Leave only creative decisions to the application layer.

When in doubt about whether something belongs in the framework or the application: cement it in the framework.
