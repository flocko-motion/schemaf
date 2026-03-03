This directory contains modular compose blocks used by atlas-base projects.

Compose files are wired together using the `x-atlas` metadata. A project defines an entry compose file (see `example/compose/app.yml`), which declares dependencies on base blocks like `compose/nginx.yml` and `compose/postgres.yml`.

## x-atlas Metadata

These fields are read by the Zeus CLI resolver:

- `project`: canonical project name
- `short-name`: short alias for dev selection
- `depends-on-compose`: list of compose file dependencies
- `dev-db-pass`: default DB password in dev if unset
- `dev-instructions`: how to run a service natively when excluded from compose
- `env-overrides-when-absent`: env overrides when a service is excluded

## Dependency Resolution

Zeus resolves the dependency graph starting from the entry compose file. Dependencies are walked depth-first and merged before the entry file.

## Environment Injection

Zeus injects:

- `PROJECT_NAME` from `x-atlas.project`
- `DB_PASS` from `x-atlas.dev-db-pass` when not already set

## Example

- Entry file: `example/compose/app.yml`
- Base blocks: `compose/nginx.yml`, `compose/postgres.yml`

## CLI Operations (Documentation-First)

These commands are intended for the Zeus CLI:

- `zeus compose up <compose-file>`
- `zeus compose down <compose-file>`
- `zeus compose build <compose-file>`
- `zeus compose status <compose-file>`

## See Also

- `docs/CODEGEN.md`
- `go/cli/README.md`
