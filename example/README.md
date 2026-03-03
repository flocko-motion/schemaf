# atlas-base example project

This is a minimal project built on top of atlas-base. It demonstrates how a project consumes the framework and extends the CLI, API, and database.

## Run (Documentation-First)

From the repo root:

```
cd example
./cli/zeus.sh ctl start
```

Zeus walks upward from `example/cli/zeus.sh` to find `project.toml`, learns the project name, and uses that to resolve the main compose file and project home directory.

## Codegen (Documentation-First)

```
cd example
./cli/zeus.sh codegen openapi
./cli/zeus.sh codegen sqlc
```

- OpenAPI codegen runs from code (no server required)
- SQLC merges framework SQL with project SQL
- Canonical output paths are enforced (see `docs/CODEGEN.md`)

## Project Layout

- `project.toml` defines `name` and `title`
- `cli/` contains the example Zeus CLI (extends base subcommands)
- `compose/app.yml` is the entry compose file
- `backend/` is the Go API server
- `frontend/` is a minimal vanilla frontend

## What This Example Demonstrates

- A project-specific Zeus CLI that mounts base subcommands plus custom commands
- Compose dependency resolution via `x-atlas` metadata
- Backend API defaults + project routes
- TypeScript API client generation from `/openapi.ts`
- Postgres migrations and sqlc integration

## Environment

The project home directory is derived from the project name:

- `~/.<project>/etc/` (or `~/.<project>/dev/etc/` in dev mode)
- `~/.<project>/var/` (or `~/.<project>/dev/var/` in dev mode)

Zeus should create these directories and a `.env` file if missing, then error if `DB_PASS` is empty. This is documentation-first behavior and will be validated as the framework is finished.
