# atlas-base

atlas-base is a framework that canonically fixes the infrastructure decisions that normally get rebuilt for every new project. The goal is to eliminate setup churn and start building product logic immediately.

This repository is the framework itself. The example project lives in `example/` and demonstrates how a project consumes the framework. The Atlas product is a separate project built on top of atlas-base and is not this repository.

## What atlas-base Fixes

These choices are **fixed and not re-litigated** per project:

- docker compose dependency resolution (`x-atlas` metadata)
- gateway (nginx reverse proxy for `/api` and `/`)
- backend API structure (Go mux + default endpoints)
- database (Postgres + migrations + sqlc workflow)
- API codegen (`/openapi.ts` for TypeScript clients)
- ports (see below)

## What Projects Extend

Projects built with atlas-base **add**:

- CLI subcommands (project-specific commands mounted into Zeus)
- API routes (additional handlers registered by the project)
- database schema/migrations (project-specific tables)
- frontend (any framework; atlas-base only provides the generated API client)

## Zeus CLI Concept

There is always **one CLI per project**, called Zeus. The framework ships the CLI **framework**, not a binary. Each project builds its own Zeus CLI by mounting framework subcommands (compose/codegen) plus its own commands.

The example project shows this pattern in `example/cli/main.go` and `example/cli/zeus.sh`.

## Codegen (Documentation-First)

Zeus provides canonical code generation commands:

```
zeus codegen openapi
zeus codegen sqlc
```

- OpenAPI generation runs from code (no running server required)
- SQLC generation merges framework SQL with project SQL
- All paths are canonical and relative to `project.toml`

See `docs/CODEGEN.md` for details.

## Port Convention

```
7000    - nginx gateway (main entry point)
7001    - backend API
7002    - frontend dev server
7003    - postgres
7004 - 7009    - atlas-base reserved
7010+   - project services
```

## Repository Map

```
compose/        - reusable compose blocks (nginx, postgres)
example/        - example project using atlas-base
go/api/         - API registry + OpenAPI generation
go/atlas/       - app lifecycle and DB bootstrapping
go/cli/         - Zeus CLI framework (subcommands, config/state)
go/compose/     - compose dependency resolver
go/db/          - database helpers + migrations
```

## Further Reading

- `example/README.md`
- `compose/README.md`
- `go/cli/README.md`
