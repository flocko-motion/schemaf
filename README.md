# schemaf

> **⚠️ SPECIFICATION-FIRST PROJECT**  
> This project is designed **documentation-first**. The README is the source of truth.  
> **DO NOT modify the architecture without first discussing and implementing changes to the documentation together with the user.**  
> All design decisions are made through pair programming and captured here before implementation.

schemaf is an opinionated framework that eliminates infrastructure churn by making all the boring decisions for you. Build production-ready applications with Go backend, Postgres, and static frontend immediately - no setup, no bikeshedding.

The name "Schema F" comes from the German expression meaning "the standard operating procedure" or "the tried-and-true method" - which is exactly what this framework provides: a reliable, proven approach to project infrastructure.

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
- Your API endpoints
- Your frontend UI

This repository is the framework itself. The example project lives in `example/` and demonstrates how a project consumes the framework.

## Quick Start

```bash
# 1. Create your project structure (normative paths)
mkdir myapp && cd myapp
mkdir -p go/db/migrations go/db/queries go/api go/db frontend

# 2. Create minimal main.go
cat > go/main.go <<EOF
package main
import (
    "context"
    "github.com/yourorg/schemaf"
    "myapp/go/api"
    "myapp/go/db"
)
func main() {
    ctx := context.Background()
    app := schemaf.New(ctx)
    app.AddDb(db.Provider)
    app.AddApi(api.Provider)
    app.Run()
}
EOF

# 3. Write a migration
cat > go/db/migrations/001_users.sql <<EOF
CREATE TABLE users (
  id SERIAL PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ DEFAULT NOW()
);
EOF

# 4. Write a query
cat > go/db/queries/users.sql <<EOF
-- name: GetUser :one
SELECT * FROM users WHERE id = $1;
EOF

# 5. Write a handler
cat > go/api/users.go <<EOF
package api
func GetUserHandler(w http.ResponseWriter, r *http.Request) {
  // Your handler logic
}
EOF

# 6. Generate
./codegen.sh

# 7. Run
./myapp compose dev
```

**That's it.** The framework:
- Generates `go/db/queries.gen.go` → type-safe query functions (sqlc)
- Generates `go/db/migrations.gen.go` → `db.Provider` with embedded SQL
- Generates `go/api/endpoints.gen.go` → `api.Provider` with handler registration
- Generates `frontend/api/openapi.gen.ts` → TypeScript client
- Wires everything via provider pattern in main.go
- Provides `/health`, `/status`, auth layer
- Serves your frontend (if present)
- All on port 7000

You write SQL, handlers, and frontend. The framework generates all glue code.

## Project Structure (Normative)

schemaf expects a specific directory layout. No configuration, no flexibility - this is the structure:

```
myapp/
├── schemaf.toml                    # Minimal config (title, name)
├── codegen.sh                      # Copy from schemaf repo — bootstraps codegen
├── compose.gen.yml                 # Generated: merged compose definition (commit this)
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
│   └── api/                       # Your API handlers (normative!)
│       ├── users.go              # Your handler implementations
│       └── endpoints.gen.go      # Generated: api.Provider
├── frontend/                      # Your static site (any language/framework)
│   ├── api/
│   │   └── openapi.gen.ts        # Generated: TypeScript client
│   ├── src/
│   └── package.json
└── compose/                       # Optional: docker-compose overrides
```

**Why normative paths?**
- Zero decisions about "where do I put this?"
- Codegen knows exactly where to write generated providers
- No configuration needed
- Clone any schemaf project and the structure is identical

**Path rules:**
- `go/db/migrations/*.sql` → migrations (input)
- `go/db/queries/*.sql` → sqlc input
- `go/db/migrations.gen.go` → generated `db.Provider`
- `go/db/queries.gen.go` → generated query functions
- `go/api/*.go` → your API handlers (normative!)
- `go/api/endpoints.gen.go` → generated `api.Provider`
- `frontend/api/openapi.gen.ts` → generated TypeScript client

**Generated file naming:**
All generated files use `.gen.` infix (e.g., `*.gen.go`, `*.gen.ts`) making them instantly recognizable and easy to `.gitignore`.

**Recommended .gitignore:**
```gitignore
*.gen.go
*.gen.ts
```

`compose.gen.yml` is an exception — commit it. It is the production compose definition and must be present without running codegen first.

Run `./codegen.sh` after checkout to regenerate everything else.

## What schemaf Provides

schemaf is **batteries-included**. These are built-in, not optional:

- **Server with default endpoints**: `/health`, `/status`, auth layer
  - Go server is the gateway
  - Binds `/api/*` for your handlers + framework endpoints
  - Binds `/` for frontend (proxy in dev, embed in production)
  - Single exposed port (7000)
- **Authentication**: JWT-based auth, fully managed by the framework
  - Bearer token in `Authorization` header
  - Signing key is auto-generated on first boot and stored in Postgres (`_schemaf_config` table)
  - No configuration, no secrets to manage — the framework owns the key entirely
  - Auth is declared per-endpoint (see Endpoint Interface below) — `/health` and `/status` are always open
- **Database**: Postgres is the built-in, always-present SQL database
  - schemaf provisions and manages Postgres — no setup, no configuration
  - It is the one and only SQL database in a schemaf project
  - You add tables, migrations, and queries; the infrastructure is handled for you
  - Write SQL in `go/db/migrations/` and `go/db/queries/`
  - Run `./myapp codegen .` - generates providers
  - Wire up: `app.AddDb(db.Provider)` in main.go
  - Generated: `go/db/queries.gen.go`, `go/db/migrations.gen.go`
  - Need a graph DB, NoSQL store, or other data layer? Add it as a Docker container in your project's `compose/` — it becomes part of the app
- **API endpoints**: Structs implementing a typed interface — not plain functions
  - Request and response types are Go generics on the struct — the framework handles JSON decode, validate, encode
  - No boilerplate: your `Handle` method receives a typed request and returns a typed response
  - Auth, method, and path are declared as interface methods on the struct
  - Codegen scans endpoint structs → generates `api.Provider` + full OpenAPI spec → generates TypeScript client
  - Type-safe end to end, no running server needed for codegen
  - Wire up: `app.AddApi(api.Provider)` in main.go
- **Docker compose**: Built-in compose for backend, frontend, and Postgres — merged with `compose/` in your project into a single generated `compose.gen.yml`
- **Ports**: Fixed allocation (see below)
- **CLI commands**: `compose prod`, `compose dev`, `codegen`, `test` - built-in, ready to use

## What You Add

Projects built with schemaf **add only creative logic**:

- **Database schema**: Write SQL migrations in `go/db/migrations/`
- **Database queries**: Write SQL queries in `go/db/queries/`
- **API handlers**: Write Go handlers in `go/api/` (normative!)
- **Frontend**: Any static site framework in `frontend/` (React, Vue, Svelte, etc.)
- **Optional services**: Additional Docker containers if needed (Redis, workers, etc.)
- **Optional CLI commands**: Add custom commands to `main.go` if needed (rarely necessary)
- **Tests**: Write test suites, hook them in via `testing.Provider` — same pattern as everything else

**That's it.** No configuration files (except minimal `schemaf.toml`). No binding framework commands. No decisions about project structure - it's normative.

## Server Architecture

The Go server built from schemaf **is the gateway**.

```
Your Application (port 7000)
├── /api/*        → Go handlers (your business logic)
└── /*            → Frontend
    ├── Dev:      Proxy to frontend dev server (port 7002)
    └── Prod:     Serve embedded static files
```

In production, frontend assets are embedded at build time via `//go:embed` and served directly by the Go server. In dev mode, the Go server proxies frontend requests to the dev server on port 7002.

**Default endpoints:**
- `/health` - Health check (built-in)
- `/status` - Service status (built-in)
- `/api/*` - Your handlers + auth layer (framework provides auth, you add business logic)

## schemaf CLI

Your application **is** a CLI. The binary has built-in commands and can be extended with your own:

```bash
./myapp server               # Run the HTTP server (used inside the Docker container)
./myapp compose prod         # Syscall docker compose — starts the full production stack
./myapp compose dev          # Syscall docker compose — starts the full dev stack
./myapp compose dev postgres # Syscall docker compose — start only specific services
./myapp compose down         # Syscall docker compose — stop all services
./myapp import               # Example custom subcommand — does whatever you implement
```

`compose prod` and `compose dev` are thin run-scripts. They exec into docker compose and hand off — the binary itself exits. The actual server runs inside the container via `./myapp server`.

`./myapp server` is what runs inside the container — pure HTTP server, no compose logic.

Custom subcommands added via `app.AddSubcommand()` run directly — no server, no compose, just your code. Use them for data imports, admin tasks, one-off scripts, anything that benefits from being bundled in the same binary.

**The CLI uses Cobra for command routing:**
- `app.Run()` hands over to Cobra
- Cobra decides which command to execute based on CLI args

**The CLI has full knowledge of your application** — it's the same binary that runs your server:
- Endpoint structs are compiled in — codegen can reflect over them
- Migrations are embedded — no external files in production
- Optional: add custom commands to `go/main.go`

Your `go/main.go` wires everything up — all framework commands are already there.

## Docker Compose

schemaf ships with a **built-in compose configuration** that covers the full standard stack:

- Go backend (port 7000)
- Frontend dev server (port 7002)
- Postgres (port 7003)

You never write or maintain these service definitions. They are part of the framework.

**Extending with project services:**

If your project needs additional services (Redis, a worker container, a vector DB, etc.), add a `compose/` directory to your project:

```
myapp/
└── compose/
    └── services.yml    # Your additional services only
```

These become a full part of the application. Codegen merges the framework's built-in compose with everything in your `compose/` and produces `compose.gen.yml`. That generated file is the complete, self-contained compose definition for your app — commit it and use it in production.

**No chicken-egg problem:** compose codegen is pure file I/O. `codegen.sh` handles it the same way as SQL and API codegen — no project binary needed.

**Running the stack:**

```bash
./myapp compose prod          # Full stack, production mode (uses compose.gen.yml)
./myapp compose dev           # Full stack, dev mode (hot reload, frontend dev server)
./myapp compose dev postgres  # Only specific services (useful during development)
./myapp compose down          # Stop everything
```

`compose dev <args>` lets you start only a subset of services. This is useful when you want to run the Go server directly on the host (e.g. with a debugger) while still having Postgres managed by compose.

**Separation of concerns:** `./myapp compose prod/dev` are run-scripts — they exec docker compose and exit. The actual server is `./myapp server`, which runs inside the container. Docker compose handles orchestration; the binary handles HTTP.

## Code Generation

**One command generates everything:**

```bash
./codegen.sh
```

Copy `codegen.sh` from the schemaf repository into your project root and commit it. It uses `go run` to build a standalone schemaf CLI on the fly — no project binary needed, no dependencies beyond Go itself. The script always changes to its own directory first, so it works correctly regardless of where you call it from.

The schemaf CLI used here (`cmd/schemaf`) is a standalone entrypoint in the framework repository. It has no knowledge of your application — it only reads your project files from disk.

**What gets generated:**

1. **SQL → Go (sqlc)**
   - Auto-discovers `go/db/queries/*.sql`
   - Generates type-safe Go query functions → `go/db/queries.gen.go`

2. **Migrations → db.Provider**
   - Auto-discovers `go/db/migrations/*.sql`
   - Generates `go/db/migrations.gen.go` with `db.Provider` function
   - Provider returns embedded migrations to framework

3. **Endpoint structs → api.Provider + OpenAPI spec**
   - Auto-discovers endpoint structs in `go/api/*.go`
   - Generates `go/api/endpoints.gen.go` with `api.Provider` (handler registration)
   - Generates OpenAPI spec from typed request/response structs

4. **OpenAPI spec → TypeScript client**
   - Generates `frontend/api/openapi.gen.ts` — type-safe client for your frontend
   - No running server needed

5. **Compose → compose.gen.yml**
   - Merges schemaf's built-in compose (backend, frontend, Postgres) with your `compose/*.yml`
   - Generates `compose.gen.yml` — the complete, self-contained compose definition
   - Used by `./myapp compose prod/dev`; commit it for production deployments

**Zero configuration.** Just run `./codegen.sh` and all the glue code appears.

## What is main.go?

Your `go/main.go` is the application entry point. It wires up the generated providers.

**Minimal example:**
```go
package main

import (
    "context"
    "github.com/yourorg/schemaf"
    "myapp/go/api"
    "myapp/go/db"
)

func main() {
    ctx := context.Background()
    app := schemaf.New(ctx)
    
    // Wire up generated providers (pass function references, not calls!)
    app.AddDb(db.Provider)      // Generated: migrations + queries
    app.AddApi(api.Provider)    // Generated: endpoint registration
    
    app.Run() // Hands over to Cobra CLI - blocking
}
```

**With optional customizations:**
```go
func main() {
    ctx := context.Background()
    app := schemaf.New(ctx)
    
    app.AddDb(db.Provider)
    app.AddApi(api.Provider)
    
    // Optional: mount custom CLI commands
    app.AddSubcommand("import", importer.SubcommandProvider)
    
    // Optional: register background services
    // These only run when "server" or "dev" command is used
    app.AddService(worker.ServiceProvider)  // Starts when server starts
    
    app.Run()  // Cobra handles command routing
}
```

**How services work:**

`app.AddService()` and `compose/*.yml` are two different extension points — they are not interchangeable:

| | `app.AddService()` | `compose/*.yml` |
|---|---|---|
| What | Go function run as a goroutine inside the binary | Additional Docker container |
| Use for | Background workers, schedulers, event loops | Redis, vector DBs, external processes |
| Lifecycle | Starts with the server, stops with the server | Managed by Docker compose |

- `app.AddService()` providers are **only started** when running `./myapp compose prod` or `./myapp compose dev`
- `codegen` never starts services — stays fast and lightweight

**What gets wired:**
- `db.Provider` → function generated in `go/db/migrations.gen.go` (embedded SQL)
- `api.Provider` → function generated in `go/api/endpoints.gen.go` (handler registration)
- Framework calls these providers at the right time
- Everything else is framework-provided

## Endpoint Interface

API endpoints are structs implementing a typed interface — not plain `http.HandlerFunc` functions. This gives the framework enough information to handle serialization, auth, and OpenAPI generation automatically.

```go
// Your endpoint — in go/api/users.go
type GetUserEndpoint struct{}

func (e GetUserEndpoint) Method() string { return "GET" }
func (e GetUserEndpoint) Path()   string { return "/api/users/{id}" }
func (e GetUserEndpoint) Auth()   bool   { return true }

func (e GetUserEndpoint) Handle(ctx context.Context, req GetUserRequest) (GetUserResponse, error) {
    // your logic — no JSON parsing, no response writing, no auth checking
    user, err := db.GetUser(ctx, req.ID)
    return GetUserResponse{User: user}, err
}

type GetUserRequest struct {
    ID int64 `path:"id"`
}

type GetUserResponse struct {
    User db.User `json:"user"`
}
```

**What the framework does for you:**
- Decodes the request (path params, query params, JSON body) into `Req`
- Validates the request struct
- Checks the JWT if `Auth()` returns `true`
- Calls `Handle(ctx, req)`
- Encodes the response as JSON and writes the status code
- On error: maps the error to an appropriate HTTP status

**What codegen does with this:**
- Scans all structs in `go/api/` that implement the endpoint interface
- Registers them in `endpoints.gen.go` (`api.Provider`)
- Extracts `Req`/`Resp` types and generates a full OpenAPI spec
- Generates `frontend/api/openapi.gen.ts` — type-safe TypeScript client

You write the struct. Everything else is generated or framework-provided.

## Configuration: schemaf.toml

Projects have a minimal `schemaf.toml` file that defines:

```toml
title = "My Application"
name = "myapp"
```

That's it. Migrations live at `go/db/migrations/`, queries at `go/db/queries/` — normative, not configurable.

**Philosophy**: Maximum automation. If schemaf can generate it, you don't write it. We auto-discover files, generate glue code, and wire everything together. Paths are normative - no configuration needed.

## Port Convention

schemaf uses a fixed port allocation scheme to eliminate configuration:

```
7000           - Application server (main entry point)
                 Serves /api (Go handlers) and / (frontend)
7001           - Reserved (future use)
7002           - Frontend dev server (Vite, Next.js dev, etc.)
7003           - Postgres
7004 - 7009    - schemaf framework reserved
7010+          - Project-specific services (Redis, workers, etc.)
```

**Why fixed ports?**
- No port conflicts across projects (each gets its own range)
- No environment variables needed for service discovery
- Docker compose networking "just works"
- Clear convention: 700X for core, 701X+ for project services

## Repository Map

```
compose/        - Reusable compose blocks (postgres, future: redis, etc.)
example/        - Example project demonstrating schemaf usage
go/api/         - API registry + OpenAPI generation
go/server/      - Server framework (gateway, routing, frontend proxy/embed)
go/atlas/       - App lifecycle and DB bootstrapping
go/cli/         - schemaf CLI framework (subcommands, config/state)
go/compose/     - Compose dependency resolver (x-schemaf metadata)
go/db/          - Database helpers + migrations
```

## Testing

schemaf provides a first-class testing system. The same provider pattern used everywhere applies to tests — you write suites, hook them in, the framework handles the infrastructure.

**Test suites, not test files.** A suite is a named group of related tests that share setup and teardown. This is a deliberate design choice: suites express intent and structure, not just a flat list of functions in a file.

```go
// go/tests/users_suite.go
type UsersTestSuite struct{}

func (s UsersTestSuite) Name() string { return "Users" }

func (s UsersTestSuite) Tests() []testing.Test {
    return []testing.Test{
        {Name: "create user", Run: s.TestCreateUser},
        {Name: "reject duplicate email", Run: s.TestDuplicateEmail},
        {Name: "get user requires auth", Run: s.TestGetUserAuth},
    }
}

func (s UsersTestSuite) TestCreateUser(ctx testing.Context) error {
    user := ctx.CreateUser("test@example.com")
    resp, err := ctx.API().Post("/api/users", user)
    // ...
}
```

**What the framework provides per test:**
- A blank database (migrations applied, no data)
- A running server instance
- Mock user creation with valid JWTs
- A typed API client pointed at the test server
- Automatic teardown after each suite

**Wiring:**
```go
// go/main.go
app.AddTests(tests.Provider)   // generated from go/tests/ by codegen
```

The `tests.Provider` is generated by codegen — it discovers all suite structs in `go/tests/` and registers them. Same pattern as `api.Provider` and `db.Provider`.

**Running:**
```bash
./myapp test           # run all suites
./myapp test Users     # run a specific suite
```

You write the test logic. The framework provides a clean server, a clean database, and the utilities to drive them.

## The One Binary Principle

Most application stacks are a collection of tools: a server process, a migration runner, a codegen CLI, a dev runner script, a separate frontend build, scattered admin utilities. Each has its own install, its own config, its own mental model.

schemaf collapses all of this into a single compiled binary:

| What | How |
|---|---|
| HTTP server | `./myapp server` — runs inside the container |
| Compose orchestration | `./myapp compose prod/dev` — execs docker, then exits |
| Code generation | `./codegen.sh` — `go run`s the framework CLI, reads your files |
| Database migrations | embedded SQL, applied automatically on server startup |
| Frontend | embedded via `//go:embed` in production; proxied from port 7002 in dev (no rebuild needed) |
| TypeScript API client | generated from compiled-in endpoint structs at codegen time |
| Admin / custom tools | `./myapp <subcommand>` — anything you add via `app.AddSubcommand()` |

The binary has full knowledge of itself. Its endpoint structs are compiled in — so it can reflect over its own API to generate the OpenAPI spec and TypeScript client without a running server. Its migrations are embedded — so it can apply them on startup without external files. Its frontend is embedded — so production deployment is a single binary copy.

**What makes this unusual** is the self-referential quality of codegen: the binary looks inward to generate its own client. The same code that handles a `GET /api/users/{id}` request also describes that endpoint well enough to produce a type-safe TypeScript function for it. No separate spec, no annotations, no second source of truth.

The only thing outside the binary is docker compose — but even that is generated by the binary (`codegen.sh`) and driven by it (`./myapp compose`).

**Deployment is therefore trivial:**
```bash
go build -o myapp go/main.go   # one artifact
./codegen.sh                    # generates compose.gen.yml
./myapp compose prod            # everything runs
```

No package manager. No deployment pipeline that installs twelve tools. No config files spread across the filesystem. One binary, one compose file, done.

## Design Philosophy

schemaf is **documentation-first**. We design by writing the README and docs for features that don't exist yet. The documentation is the source of truth for how the framework should work.

**Core principles:**
1. **Maximize decisions made** - Every choice you don't have to make is time saved
2. **Minimize configuration** - Zero config is the goal; convention over configuration
3. **Maximize generation** - If we can generate it, you don't write it
4. **Cement boilerplate** - Run scripts, codegen, compose layout, ports, database choice, glue code
5. **Single responsibility** - Framework handles infra, you handle business logic
6. **Fast to production** - Clone, add schema + handlers, run codegen, deploy

**The codegen philosophy:**
- One command (`schemaf codegen .`) generates everything
- Auto-discovery: find SQL files, Go handlers, migrations
- Auto-generation: sqlc code, migration providers, endpoint providers, TypeScript clients
- Auto-wiring: hook generated code into framework automatically
- No manual registration, no manual imports, no glue code

## Further Reading

- `example/README.md` - How to build a project with schemaf
- `compose/README.md` - Docker compose dependency system
- `go/cli/README.md` - CLI framework internals
- `docs/CODEGEN.md` - Code generation workflows
