# Schema F

schemaf is an opinionated framework that eliminates infrastructure churn by making all the boring decisions for you. Build production-ready applications with Go backend, Postgres, and static frontend immediately - no setup, no bikeshedding.

The name "Schema F" comes from the German expression meaning "the standard operating procedure" or "the tried-and-true method" - which is exactly what this framework provides: a reliable, proven approach to project infrastructure.

**Documentation:**
- [**INSTALL.md**](INSTALL.md) — set up a new project
- [**EXTEND.md**](EXTEND.md) — add endpoints, CLI commands, database, tests

## Documentation map

Common tasks and where they're documented (jump straight to the section):

| Task | Where |
|---|---|
| Create a new project | [INSTALL › Quickstart](INSTALL.md#quickstart) |
| Wire providers in `main.go` | [EXTEND › main.go Wiring](EXTEND.md#maingo-wiring) |
| Add an API endpoint | [EXTEND › Endpoint Interface](EXTEND.md#endpoint-interface) |
| Stream / upload (raw HTTP) | [EXTEND › Raw Endpoints](EXTEND.md#raw-endpoints) |
| Add a database migration | [EXTEND › Migrations](EXTEND.md#migrations) |
| Migrate a secondary / external DB | [EXTEND › Applying migrations to another database](EXTEND.md#applying-migrations-to-another-database) |
| Write DB queries (sqlc) | [EXTEND › Queries](EXTEND.md#queries) |
| Add a CLI subcommand | [EXTEND › CLI Subcommands](EXTEND.md#cli-subcommands) |
| Run code generation | [EXTEND › Code Generation](EXTEND.md#code-generation) |
| Write & run tests | [EXTEND › Testing](EXTEND.md#testing) |
| Configure database backups | [EXTEND › Database Backups](EXTEND.md#database-backups) |
| Secrets / env files | [INSTALL › Secrets](INSTALL.md#secrets) |
| Docker compose & extra services | [INSTALL › Docker Compose](INSTALL.md#docker-compose) |
| Ports, layout, config conventions | [Port Convention](#port-convention) · [Project Structure](#project-structure-normative) |

## Contents

- [Golden Rule](#golden-rule)
- [What schemaf Provides](#what-schemaf-provides)
- [What You Add](#what-you-add)
- [Project Structure (Normative)](#project-structure-normative)
- [Server Architecture](#server-architecture)
- [Port Convention](#port-convention)
- [The One Binary Principle](#the-one-binary-principle)
- [Repository Map](#repository-map)
- [Design Philosophy](#design-philosophy)

## Golden Rule

**If it can be generalized, put it in schemaf. If arbitrary decisions need to be made: decide them normatively in the framework. Leave only creative decisions to the application layer.**

schemaf deliberately reduces degrees of freedom. We cement:
- Run scripts and CLI tooling
- Code generation workflows (one command → everything)
- Docker compose layout and dependency resolution
- Port assignments
- Database choice and migration patterns
- Server architecture (the Go server is the gateway)
- Glue code generation (migrations provider, endpoint provider, etc.)

You focus on:
- Your database schema
- Your business logic
- Your API endpoints
- Your frontend UI

This repository is the framework itself — it contains no committed example project. Instead, `e2e/build-example.sh` builds a fresh project from scratch against a published tag (the real onboarding lifecycle), which doubles as the framework's end-to-end test.

## What schemaf Provides

schemaf is **batteries-included**. These are built-in, not optional:

- **Server with default endpoints**: `/health`, `/status`, auth layer
  - Go server is the gateway
  - Binds `/api/*` for your handlers + framework endpoints
  - Binds `/` for frontend (proxy in dev, embed in production)
  - Single exposed port (default 8000, configurable via schemaf.toml)
- **Authentication**: JWT-based auth, fully managed by the framework
  - Bearer token in `Authorization` header
  - Signing key is auto-generated on first boot and stored in Postgres (`_schemaf_config` table)
  - No configuration, no secrets to manage — the framework owns the key entirely
  - Auth is declared per-endpoint (see [Endpoint Interface](EXTEND.md#endpoint-interface)) — `/health` and `/status` are always open
- **Database**: Postgres is the built-in, always-present SQL database
  - schemaf provisions and manages Postgres — no setup, no configuration
  - It is the one and only SQL database in a schemaf project
  - You add tables, migrations, and queries; the infrastructure is handled for you
  - See [Database](EXTEND.md#database) for details
  - Need a graph DB, NoSQL store, or other data layer? Add it as a Docker container in your project's `compose/`
- **API endpoints**: Structs implementing a typed interface — not plain functions
  - Request and response types are Go generics on the struct — the framework handles JSON decode, validate, encode
  - No boilerplate: your `Handle` method receives a typed request and returns a typed response
  - Codegen scans endpoint structs → generates `api.Provider` + full OpenAPI spec → generates TypeScript and Go clients
  - See [Endpoint Interface](EXTEND.md#endpoint-interface) for details
- **Docker compose**: Built-in compose for backend and Postgres — merged with `compose/` in your project
- **Code generation**: One command generates all glue code — see [Code Generation](EXTEND.md#code-generation)
- **Ports**: Fixed allocation (see below)
- **Project entrypoint**: `schemaf.sh codegen`, `schemaf.sh test`, `schemaf.sh run`, `schemaf.sh dev`

## What You Add

Projects built with schemaf **add only creative logic**:

- **Database schema**: Write SQL migrations in `go/db/migrations/`
- **Database queries**: Write SQL queries in `go/db/queries/`
- **API handlers**: Write Go handlers in `go/api/`
- **Frontend**: Any static site framework in `frontend/` (React, Vue, Svelte, etc.)
- **Optional services**: Additional Docker containers if needed (Redis, workers, etc.)
- **Optional CLI commands**: Custom subcommands via `app.AddSubcommand()`
- **Tests**: Go tests and TypeScript integration tests

**That's it.** No configuration files (except minimal `schemaf.toml`). No binding framework commands. No decisions about project structure - it's normative.

## Project Structure (Normative)

schemaf expects a specific directory layout. No configuration, no flexibility - this is the structure:

```
myapp/
├── schemaf.toml                    # Minimal config (title, name)
├── schemaf.sh                      # Copy from schemaf repo — project entrypoint
├── compose.gen.yml                 # Generated: merged compose definition
├── Dockerfile.gen                  # Generated: production Dockerfile
├── gen/                            # Other generated files
│   └── openapi.json                # Generated: OpenAPI spec
├── go/                            # All Go code (CLI + server unified)
│   ├── main.go                    # Wire up providers, start app
│   ├── db/                        # All database concerns
│   │   ├── migrations/            # Your SQL migrations
│   │   │   ├── 001_users.sql
│   │   │   └── 002_posts.sql
│   │   ├── queries/               # Your SQL queries (sqlc input)
│   │   │   └── users.sql
│   │   ├── queries.gen.go         # Generated: sqlc query functions
│   │   └── migrations.gen.go      # Generated: db.Provider
│   ├── api/                       # Your API handlers (normative!)
│   │   ├── users.go              # Your handler implementations
│   │   └── endpoints.gen.go      # Generated: api.Provider
│   └── apiclient/
│       └── client.gen.go         # Generated: Go API client (oapi-codegen)
├── frontend/                      # Your static site (any language/framework)
│   ├── api/
│   │   └── openapi.gen.ts        # Generated: TypeScript client
│   ├── src/
│   └── package.json
└── compose/                       # Optional: *.yml for all envs, *.dev.yml for dev only
```

**Why normative paths?**
- Zero decisions about "where do I put this?"
- Codegen knows exactly where to write generated providers
- No configuration needed
- Clone any schemaf project and the structure is identical

**Generated file naming:**
All generated files use `.gen.` infix (e.g., `*.gen.go`, `*.gen.ts`) making them instantly recognizable. Generated files **must be committed** — they are required for the project to compile and run.

## Server Architecture

The Go server built from schemaf **is the gateway**.

```
Your Application (port 8000)
├── /api/*        → Go handlers (your business logic)
└── /*            → Frontend
    ├── Dev:      Proxy to frontend dev server (port 8002)
    └── Prod:     Serve embedded static files
```

In production, frontend assets are embedded at build time via `//go:embed` and served directly by the Go server. In dev mode, the Go server proxies frontend requests to the dev server on port 8002.

**Default endpoints:**
- `/health` - Health check (built-in)
- `/status` - Service status (built-in)
- `/api/*` - Your handlers + auth layer (framework provides auth, you add business logic)

## Port Convention

schemaf uses a fixed port allocation scheme based on a single configurable base port (default `8000`):

```
port           - Application server (main entry point)
                 Serves /api (Go handlers) and / (frontend)
port + 1       - Reserved (future use)
port + 2       - Frontend dev server (Vite, Next.js dev, etc.)
port + 3       - Postgres
port + 4..9    - schemaf framework reserved
port + 10+     - Project-specific services (Redis, workers, etc.)
```

Configure the base port in `schemaf.toml`:
```toml
port = 6000    # optional, default 8000
```

**Why a single port with offsets?**
- One number to change, all services follow
- No environment variables needed for service discovery
- Docker compose networking "just works"
- Clear convention: base+X for core, base+10+ for project services

## The One Binary Principle

Most application stacks are a collection of tools: a server process, a migration runner, a codegen CLI, a dev runner script, a separate frontend build, scattered admin utilities. Each has its own install, its own config, its own mental model.

schemaf collapses all of this into a single compiled binary:

| What | How |
|---|---|
| HTTP server | `./myapp server` — runs inside the container |
| Compose orchestration | `./schemaf.sh run/dev` — execs docker compose, then exits |
| Code generation | `./schemaf.sh codegen` — `go run`s the framework CLI, reads your files |
| Database migrations | embedded SQL, applied automatically on server startup |
| Frontend | embedded via `//go:embed` in production; proxied from frontend dev server in dev (no rebuild needed) |
| TypeScript API client | generated from OpenAPI spec at codegen time |
| Go API client | generated from OpenAPI spec via oapi-codegen at codegen time |
| Admin / custom tools | `./myapp <subcommand>` — anything you add via `app.AddSubcommand()` |

The binary has full knowledge of itself. Its endpoint structs are compiled in — so it can reflect over its own API to generate the OpenAPI spec and typed clients (TypeScript + Go) without a running server. Its migrations are embedded — so it can apply them on startup without external files. Its frontend is embedded — so production deployment is a single binary copy.

**Deployment is therefore trivial:**
```bash
go build -o myapp go/main.go   # one artifact
./schemaf.sh codegen            # generates compose.gen.yml, Dockerfile.gen, etc.
./schemaf.sh run                # everything runs
```

No package manager. No deployment pipeline that installs twelve tools. No config files spread across the filesystem. One binary, one compose file, done.

## Repository Map

```
compose/        - Reusable compose blocks (postgres, future: redis, etc.)
e2e/            - From-scratch onboarding e2e + DB integration harness
api/            - API registry + OpenAPI generation
schemaf/        - App lifecycle (schemaf.New, app.Run)
cli/            - schemaf CLI framework (subcommands, config/state)
compose/        - Compose dependency resolver (x-schemaf metadata)
db/             - Database helpers + migrations
```

## Design Philosophy

schemaf is **documentation-first**. We design by writing the README and docs for features that don't exist yet. The documentation is the source of truth for how the framework should work.

**Core principles:**
1. **Maximize decisions made** - Every choice you don't have to make is time saved
2. **Minimize configuration** - Zero config is the goal; convention over configuration
3. **Maximize generation** - If we can generate it, you don't write it
4. **Cement boilerplate** - Run scripts, codegen, compose layout, ports, database choice, glue code
5. **Single responsibility** - Framework handles infra, you handle business logic
6. **Fast to production** - Clone, add schema + handlers, run codegen, deploy
