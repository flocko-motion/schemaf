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
| Wire providers (`go/app/app.go`) | [EXTEND › App Wiring](EXTEND.md#app-wiring-goappappgo) |
| Add an API endpoint | [EXTEND › Endpoint Interface](EXTEND.md#endpoint-interface) |
| Authenticate / mint dev tokens | [EXTEND › Authentication](EXTEND.md#authentication) |
| Sign users in with Auth0 | [EXTEND › Activating Auth0](EXTEND.md#activating-auth0) |
| Per-instance frontend config (`PUBLIC_*`) | [EXTEND › Runtime config](EXTEND.md#runtime-config-public_) |
| Request headers, YAML and other content types | [EXTEND › Request headers](EXTEND.md#request-headers) · [Content types](EXTEND.md#content-types) |
| Server-sent events (SSE) | [EXTEND › Stream Endpoints](EXTEND.md#stream-endpoints) |
| Upload / download (raw HTTP) | [EXTEND › Raw Endpoints](EXTEND.md#raw-endpoints) |
| Add a database migration | [EXTEND › Migrations](EXTEND.md#migrations) |
| Migrate a secondary / external DB | [EXTEND › Applying migrations to another database](EXTEND.md#applying-migrations-to-another-database) |
| Write DB queries (sqlc) | [EXTEND › Queries](EXTEND.md#queries) |
| Add a CLI subcommand | [EXTEND › CLI Subcommands](EXTEND.md#cli-subcommands) |
| Run code generation | [EXTEND › Code Generation](EXTEND.md#code-generation) |
| Write & run tests | [EXTEND › Testing](EXTEND.md#testing) |
| Boot the app in a test (own database, tokens, restart) | [EXTEND › Integration tests](EXTEND.md#integration-tests-schemaftest) |
| Configure database backups | [EXTEND › Database Backups](EXTEND.md#database-backups) |
| Secrets / env files | [INSTALL › Secrets](INSTALL.md#secrets) |
| Deploy: build an image, run it | [INSTALL › Deployment](INSTALL.md#deployment) |
| Build as one user, run as another | [INSTALL › Separate build and run users](INSTALL.md#separate-build-and-run-users) |
| Container runtime (podman / Docker) | [INSTALL › Container runtime](INSTALL.md#container-runtime) |
| Move data from Docker to podman | [INSTALL › Migrating from Docker to podman](INSTALL.md#migrating-from-docker-to-podman) |
| Compose & extra services | [INSTALL › Compose](INSTALL.md#compose) |
| Run several dev stacks at once (worktrees) | [INSTALL › Parallel dev instances](INSTALL.md#parallel-dev-instances) |
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
- Container runtime (rootless podman), compose layout and dependency resolution
- Port assignments
- Database choice and migration patterns
- Server architecture (the Go server is the gateway)
- Glue code generation (migrations provider, endpoint provider, etc.)

You focus on:
- Your database schema
- Your business logic
- Your API endpoints
- Your frontend UI

This repository is the framework itself — it contains no committed example project. Instead, `e2e/build-example.sh` builds a fresh project from scratch against an online ref — by default the latest release tag (the real onboarding lifecycle). It doubles as the framework's end-to-end test: the CI gate runs it against every pull request's head commit, by its full hash, under podman and Docker (see [EXTEND.md › Testing the framework](EXTEND.md#testing-the-framework-the-ci-gate)).

## What schemaf Provides

schemaf is **batteries-included**. These are built-in, not optional:

- **Server with default endpoints**: `/health`, `/status`, auth layer
  - Go server is the gateway
  - Binds `/api/*` for your handlers + framework endpoints
  - Binds `/` for frontend (proxy in dev, embed in production)
  - Single exposed port (default 8000, configurable via schemaf.toml)
- **Development mode**: opt-in, on only for `./schemaf.sh dev` and the other commands that use the dev env file; your app reads it with `api.DevMode()` and `/status` reports it (see [Development mode](EXTEND.md#development-mode))
- **Authentication**: JWT-based auth, fully managed by the framework
  - Accepted from an HttpOnly session cookie or a Bearer token in the `Authorization` header
  - Pluggable identity providers exchanged for schemaf's own session at login: built-in Auth0, switched on by two env vars with no code (see [Activating Auth0](EXTEND.md#activating-auth0)), plus your own (passkeys, login codes, ...)
  - Short-lived, auto-renewed tokens backed by revocable sessions in Postgres
  - One per-request hook (`app.OnRequest`) after authentication turns the subject into your app's own caller, or refuses the request, on every API call; its admission, handed only to the hook, lets the app make that caller unforgeable elsewhere (see [Per-request hook](EXTEND.md#per-request-hook-onrequest))
  - Signing key is auto-generated on first boot and stored in Postgres (`_schemaf_config` table)
  - No configuration, no secrets to manage — the framework owns the key entirely
  - Auth is declared per-endpoint at one of three levels — required, optional (signed-in users and guests on the same endpoint) or none (see [Auth levels](EXTEND.md#auth-levels)) — `/health` and `/status` are always open
- **Database**: Postgres is the built-in, always-present SQL database
  - schemaf provisions and manages Postgres — no setup, no configuration
  - It is the one and only SQL database in a schemaf project
  - You add tables, migrations, and queries; the infrastructure is handled for you
  - See [Database](EXTEND.md#database) for details
  - Need a graph DB, NoSQL store, or other data layer? Add it as a container in your project's `compose/`
- **API endpoints**: Structs implementing a typed interface — not plain functions
  - Request and response types are Go generics on the struct — the framework handles JSON decode, validate, encode
  - No boilerplate: your `Handle` method receives a typed request and returns a typed response
  - Codegen scans endpoint structs → generates `api.Provider` + full OpenAPI spec → generates TypeScript and Go clients
  - See [Endpoint Interface](EXTEND.md#endpoint-interface) for details
- **Container runtime**: rootless [podman](https://podman.io) with [docker-compose](https://docs.docker.com/compose/) by default; Docker on request (`SCHEMAF_RUNTIME=docker`)
  - Docker's daemon is root-equivalent; rootless podman is the safer default for running other people's images and the database holding your users' data
  - Same compose files and the same compose implementation for both runtimes — only the engine differs
  - A missing or unusable tool stops schemaf with a message naming what's missing and how to fix it
  - See [Container runtime](INSTALL.md#container-runtime)
- **Compose**: Built-in compose for backend and Postgres — merged with `compose/` in your project
- **Code generation**: One command generates all glue code — see [Code Generation](EXTEND.md#code-generation)
- **Ports**: Fixed allocation (see below)
- **Project entrypoint**: `schemaf.sh codegen`, `schemaf.sh test`, `schemaf.sh build`, `schemaf.sh run`, `schemaf.sh dev`, `schemaf.sh instances`
- **Parallel dev instances**: `dev --instance <n>` runs a second, fully separate dev stack (own ports, database, session cookie) next to the default one — see [Parallel dev instances](INSTALL.md#parallel-dev-instances)
- **Deployment**: `build` turns a commit into an image, optionally packed with its compose files into one self-extracting `<name>-<tag>.run` file; `run` starts an image. Build and run can be different users on the same host, so the user that runs third-party build code never reaches the database — see [Deployment](INSTALL.md#deployment)
- **Releases from CI**: a generated GitHub workflow turns a pushed tag `vX.Y.Z` into a release whose artifact is built from the lockfiles exactly and signed with keyless Sigstore, plus the app's release index `releases.json` — see [Releases from CI](INSTALL.md#releases-from-ci-releasegenyml)

## What You Add

Projects built with schemaf **add only creative logic**:

- **Database schema**: Write SQL migrations in `go/db/migrations/`
- **Database queries**: Write SQL queries in `go/db/queries/`
- **API handlers**: Write Go handlers in `go/api/`
- **Frontend**: A Vite + TypeScript app in `frontend/`, built with npm, with any UI framework (React — the scaffold default —, Vue, Svelte, etc.)
- **Optional services**: Additional containers if needed (Redis, workers, etc.)
- **Optional CLI commands**: Custom subcommands via `app.AddSubcommand()`
- **Tests**: Go tests and TypeScript integration tests

**That's it.** No configuration files (except minimal `schemaf.toml`). No binding framework commands. No decisions about project structure - it's normative.

## Project Structure (Normative)

schemaf expects a specific directory layout. No configuration, no flexibility - this is the structure:

```
myapp/
├── schemaf.toml                    # Minimal config (title, name)
├── schemaf.sh                      # Generated by codegen — project entrypoint
├── compose.gen.yml                 # Generated: merged compose definition
├── Dockerfile.gen                  # Generated: production Dockerfile
├── .github/workflows/release.gen.yml # Generated: a pushed tag vX.Y.Z → a release with a signed artifact
├── gen/                            # Other generated files
│   ├── openapi.json                # Generated: OpenAPI spec
│   ├── schema-sources              # Generated: packages declaring the API's types
│   └── inputs.sha256               # Generated: hash of codegen's inputs (freshness check)
├── go/                            # All Go code (CLI + server unified)
│   ├── main.go                    # New → app.Wire → Run (three lines)
│   ├── app/                       # The app's wiring, shared by main.go and the tests
│   │   ├── app.go                 # func Wire(app *schemaf.App): every Add*/On*/Set* call
│   │   ├── constants.gen.go       # Generated: project name + port from schemaf.toml
│   │   ├── frontend.gen.go        # Generated: FrontendFS() (embeds frontend_dist/)
│   │   └── frontend_dist/         # Filled by the image build; holds .gitkeep in git
│   ├── db/                        # All database concerns
│   │   ├── migrations/            # Your SQL migrations
│   │   │   ├── 001_users.sql
│   │   │   └── 002_posts.sql
│   │   ├── queries/               # Your SQL queries (sqlc input)
│   │   │   └── users.sql
│   │   ├── db.gen.go              # Generated: sqlc Queries type
│   │   ├── models.gen.go          # Generated: sqlc model structs
│   │   ├── users.sql.gen.go       # Generated: sqlc, one per query file
│   │   └── migrations.gen.go      # Generated: db.Provider
│   ├── api/                       # Your API handlers (normative!)
│   │   ├── users.go              # Your handler implementations
│   │   └── endpoints.gen.go      # Generated: api.Provider
│   ├── apiclient/
│   │   └── client.gen.go         # Generated: Go API client (oapi-codegen)
│   └── tests/                     # Integration tests that boot the app (package tests)
│       ├── health_test.go        # Scaffolded by init: schemaftest.Start(t, app.Wire)
│       ├── *.test.ts             # TypeScript tests against a running instance
│       └── ts.gen_test.go        # Generated: Go wrappers for the TypeScript tests
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

In production, frontend assets are embedded at build time via `//go:embed` and served directly by the Go server. In [development mode](EXTEND.md#development-mode), the Go server proxies frontend requests to the dev server on port 8002.

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

**Parallel dev instances.** Several checkouts of a project (e.g. git worktrees) can each run a dev stack at the same time. `./schemaf.sh dev all --instance <n>` starts instance `n` (a whole number ≥ 1, usually the issue number), whose ports are shifted by `10 × n`:

```
instance base  = port + 10 × n      e.g. port = 7100, instance 349:
backend        = instance base      10590
Vite           = instance base + 2  10592
Postgres       = instance base + 3  10593
```

Without `--instance`, `dev` runs the `default` instance on `port` itself, as always. Each instance has its own database, data directory and session cookie; `./schemaf.sh instances` lists them and `./schemaf.sh instances rm <n>` removes one. Project services from `compose/` keep their ports. See [Parallel dev instances](INSTALL.md#parallel-dev-instances).

**Why a single port with offsets?**
- One number to change, all services follow
- No environment variables needed for service discovery
- Compose networking "just works"
- Clear convention: base+X for core, base+10+ for project services

## The One Binary Principle

Most application stacks are a collection of tools: a server process, a migration runner, a codegen CLI, a dev runner script, a separate frontend build, scattered admin utilities. Each has its own install, its own config, its own mental model.

schemaf collapses all of this into a single compiled binary:

| What | How |
|---|---|
| HTTP server | `./myapp server` — runs inside the container |
| Image build | `./schemaf.sh build` — builds `Dockerfile.gen` with the container runtime, from the lockfiles exactly; no Go or Node on the host |
| Compose orchestration | `./schemaf.sh run/dev` — runs `<runtime> compose` (podman by default), then exits |
| Code generation | `./schemaf.sh codegen` — `go run`s the framework CLI, reads your files |
| Database migrations | embedded SQL, applied automatically on server startup |
| Frontend | embedded via `//go:embed` in production; proxied from frontend dev server in dev (no rebuild needed) |
| TypeScript API client | generated from OpenAPI spec at codegen time |
| Go API client | generated from OpenAPI spec via oapi-codegen at codegen time |
| Admin / custom tools | `./myapp <subcommand>` — anything you add via `app.AddSubcommand()` |

The binary has full knowledge of itself. Its OpenAPI spec is generated from the endpoint structs and their types at codegen time and compiled in — so the typed clients (TypeScript + Go) come without a running server, and the spec the server serves is exactly the one the clients were generated from. Its migrations are embedded — so it can apply them on startup without external files. Its frontend is embedded — so production deployment is a single binary copy.

**Deployment is therefore trivial:**
```bash
./schemaf.sh run                                  # single-user host: build the commit, start it
```
or, built by one user and run by another:
```bash
./schemaf.sh build --output myapp-3f9a2c1.run     # build user: image + compose files in one file
./myapp-3f9a2c1.run load                          # run user: load it
~/.myapp/bin/schemaf.sh run --image 3f9a2c1       # run user: start it
```

No package manager. No deployment pipeline that installs twelve tools. No config files spread across the filesystem. One image, one compose file, done. See [Deployment](INSTALL.md#deployment).

## Repository Map

```
compose/        - Reusable compose blocks (postgres, future: redis, etc.)
e2e/            - From-scratch onboarding e2e + DB integration harness
api/            - API registry + OpenAPI generation
schemaf/        - App lifecycle (schemaf.New, app.Run)
schemaftest/    - Test harness: start the app in-process on its own database
internal/       - Framework-only hooks between packages (Go internal: apps can't import them)
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
