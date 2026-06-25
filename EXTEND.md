# Extending Your Project

Back to [README](README.md) | See also: [Installation](INSTALL.md)

## Contents

- [main.go Wiring](#maingo-wiring)
- [Frontend](#frontend)
- [Built-in Endpoints](#built-in-endpoints)
- [Endpoint Interface](#endpoint-interface)
- [Authentication](#authentication)
- [Raw Endpoints](#raw-endpoints)
- [Database](#database)
  - [Migrations](#migrations)
  - [Migration identifiers](#migration-identifiers-independent-migration-lines)
  - [Applying migrations to another database](#applying-migrations-to-another-database)
  - [Queries](#queries)
- [CLI Subcommands](#cli-subcommands)
- [Code Generation](#code-generation)
- [Testing](#testing)
- [Database Backups](#database-backups)

## main.go Wiring

Your `go/main.go` wires up providers — generated and custom:

```go
package main

import (
    "context"
    "log"

    "github.com/flocko-motion/schemaf/schemaf"

    "myapp/api"
    "myapp/db"
    "myapp/importer"
)

func main() {
    ctx := context.Background()
    app := schemaf.New(ctx)

    app.AddDb(db.Provider)                        // generated: migrations + queries
    app.AddApi(api.Provider)                       // generated: endpoint registration
    app.SetFrontend(FrontendFS())                  // generated: embedded frontend assets
    app.AddSubcommand(importer.SubcommandProvider) // custom: CLI commands

    if err := app.Run(); err != nil {
        log.Fatal(err)
    }
}
```

`app.Run()` hands over to Cobra. The `server` command (default) starts the HTTP server. Custom subcommands are available alongside it.

## Frontend

### Stack & conventions

Schemaf enforces a **Vite + React + TypeScript** frontend. If `frontend/` doesn't exist when you run codegen, it's scaffolded automatically with a minimal working setup. If it exists, codegen validates that the required files are present.

**Normative decisions (framework-enforced):**
- Vite as build tool and dev server
- React + TypeScript
- Port derived from schemaf.toml (default 8002) with `strictPort: true`
- Entry point: `index.html` → `src/main.tsx`
- npm as package manager

**Project decisions (up to you):**
- UI framework (MUI, Tailwind, shadcn, etc.)
- State management (Redux, Zustand, etc.)
- Routing (React Router, TanStack, etc.)
- Any additional dependencies

### Architecture

The Go server is the single gateway — it serves both API routes and the frontend:

```
localhost:{port}
├── /api/*   → Go handlers
└── /*       → Frontend
    ├── Dev:  reverse proxy to Vite dev server (port+2)
    └── Prod: embedded static files from frontend/dist/
```

### Wiring

Codegen generates `go/frontend.gen.go` with `FrontendFS()` returning the embedded assets. Wire it in `main.go`:

```go
app.SetFrontend(FrontendFS())
```

### Codegen

`./schemaf.sh codegen` does two things for the frontend:

1. **Scaffold** — if `frontend/` doesn't exist, creates the full React+Vite+TS setup and runs `npm install`
2. **Generate** — creates `go/frontend.gen.go` (embeds `frontend/dist/` into the Go binary) and `frontend/src/api/generated/api.gen.ts` (type-safe API client from OpenAPI spec)

### Development workflow

Each service is started explicitly. Combine with commas:

```bash
./schemaf.sh dev                          # no args: shows available services
./schemaf.sh dev db                       # just postgres
./schemaf.sh dev infrastructure           # postgres + project compose services
./schemaf.sh dev backend                  # Go server (warns if postgres not running)
./schemaf.sh dev frontend                 # Vite dev server
./schemaf.sh dev db,backend               # postgres + Go server
./schemaf.sh dev db,backend,frontend      # postgres + Go server + Vite
./schemaf.sh dev all                      # everything
```

In dev mode, the Go server reverse-proxies all non-API requests to the Vite dev server.

### Production

The generated `Dockerfile` includes a Node build stage that compiles the frontend, then embeds the output into the Go binary via `//go:embed`. No separate frontend container needed.

## Built-in Endpoints

The framework provides these endpoints out of the box:

- `/api/health` — health check (returns `{"status": "ok", "db": "ok", ...}`)
- `/api/status` — service status (uptime, backup status, custom providers)
- `/api/user/me` — current user info (requires auth)
- `/openapi.json` — OpenAPI 3.0 spec

### Extending /api/health

Register custom health checks that are evaluated on every `/api/health` request. If any checker returns an error, the overall status becomes `"error"` and the response code is 503:

```go
import schemafapi "github.com/flocko-motion/schemaf/api"

schemafapi.RegisterHealth("s3", func() error {
    return s3Client.Ping()
})
```

Response when healthy:
```json
{"status": "ok", "db": "ok", "s3": "ok"}
```

Response when unhealthy:
```json
{"status": "error", "db": "ok", "s3": "error: connection refused"}
```

### Extending /api/status

Register custom status providers to include project-specific information in the `/api/status` response:

```go
import schemafapi "github.com/flocko-motion/schemaf/api"

schemafapi.RegisterStatus("s3", func() any {
    return map[string]any{"bucket": cfg.Bucket, "connected": s3.IsConnected()}
})
```

The response will include your provider alongside the built-in fields:

```json
{
  "status": "ok",
  "uptime": "2h30m",
  "backup": { "last": "...", "ago": "..." },
  "s3": { "bucket": "my-bucket", "connected": true }
}
```

## Endpoint Interface

API endpoints are structs implementing a typed interface — not plain `http.HandlerFunc` functions. This gives the framework enough information to handle serialization, auth, and OpenAPI generation automatically.

The interface (defined in `api/endpoint.go`):

```go
type Endpoint[Req, Resp any] interface {
    Method() string                                    // HTTP method: "GET", "POST", "PUT", "DELETE"
    Path() string                                      // Route path, e.g. "/api/todos/{id}"
    Auth() bool                                        // Whether JWT authentication is required
    Handle(ctx context.Context, req Req) (Resp, error) // Your business logic
}
```

Example implementation:

```go
// go/api/users.go
type GetUserEndpoint struct{}

func (e GetUserEndpoint) Method() string { return "GET" }
func (e GetUserEndpoint) Path()   string { return "/api/users/{id}" }
func (e GetUserEndpoint) Auth()   bool   { return true }

func (e GetUserEndpoint) Handle(ctx context.Context, req GetUserReq) (GetUserResp, error) {
    user, err := db.GetUser(ctx, req.ID)
    return GetUserResp{User: user}, err
}

type GetUserReq struct {
    ID string `path:"id"`
}

type GetUserResp struct {
    User db.User `json:"user"`
}
```

Each endpoint struct has four methods:
- `Method()` — HTTP method (GET, POST, PUT, DELETE)
- `Path()` — route with `{param}` placeholders
- `Auth()` — whether JWT auth is required
- `Handle(ctx, req) (resp, error)` — your business logic

**What the framework does for you:**
- Decodes the request (path params, query params, JSON body) into the request type
- Checks the JWT if `Auth()` returns `true`
- Calls `Handle(ctx, req)`
- Encodes the response as JSON
- On error: maps the error to an appropriate HTTP status (see below)

**Error responses:** return one of the framework's sentinel errors (directly or wrapped with `%w`) to control the status code; any other error is `500`.

| Return | Status |
|---|---|
| `api.ErrBadRequest` | 400 Bad Request |
| `api.ErrForbidden` | 403 Forbidden |
| `api.ErrNotFound` | 404 Not Found |
| `api.ErrConflict` | 409 Conflict |
| `api.ErrUnavailable` | 503 Service Unavailable |
| any other `error` | 500 Internal Server Error |

```go
func (e GetOrderEndpoint) Handle(ctx context.Context, req GetOrderReq) (GetOrderResp, error) {
    order, err := db.GetOrder(ctx, req.ID)
    if errors.Is(err, sql.ErrNoRows) {
        return GetOrderResp{}, fmt.Errorf("order %s: %w", req.ID, api.ErrNotFound) // → 404
    }
    return GetOrderResp{Order: order}, err
}
```

**Request type struct tags:**
```go
type ExampleReq struct {
    ID     string `path:"id"`      // from URL path: /api/things/{id}
    Text   string `json:"text"`    // from JSON body
}
```

**OpenAPI generation:**
The Go doc comment on the endpoint struct becomes the OpenAPI summary. The first line is the summary, subsequent lines become the description:

```go
// CreateUserEndpoint creates a new user account.
// Sends a welcome email after creation.
type CreateUserEndpoint struct{}
```

**What codegen does:**
- Scans all structs in `go/api/` that implement the endpoint interface
- Generates `go/api/endpoints.gen.go` with `api.Provider` (handler registration)
- Generates `gen/openapi.json` — OpenAPI 3.0 spec
- Generates `frontend/src/api/generated/api.gen.ts` — type-safe TypeScript client

You write the struct. Everything else is generated or framework-provided.

## Authentication

schemaf has built-in JWT auth, fully managed by the framework:

- **Per-endpoint**: return `true` from `Auth()` to require a valid token (see [Endpoint Interface](#endpoint-interface)). `/api/health` and `/api/status` are always open.
- **Bearer JWTs**: `Authorization: Bearer <token>`, HMAC-SHA256 signed.
- **Signing key is auto-managed**: generated on first boot and stored in Postgres (`_schemaf_config`). Nothing to configure, no secret to manage.
- In a handler, `api.Subject(ctx)` returns the authenticated subject; `/api/user/me` returns the current user.

### Getting a token in development

The framework has no built-in login/password flow — your app decides how users authenticate in production. For **local development**, mint a token for any subject with the built-in command:

```bash
# the database must be running, e.g. ./schemaf.sh dev db
TOKEN=$(./schemaf.sh auth token alice)
curl -H "Authorization: Bearer $TOKEN" localhost:8000/api/user/me
```

- `auth token <subject>` prints a signed JWT for that subject, so you can call authenticated endpoints as any user.
- `--ttl <duration>` sets an expiry (e.g. `--ttl 24h`); without it, the token never expires.
- **Dev-only**: the command refuses to run in production (`SCHEMAF_ENV=docker`).

### Issuing tokens from your own code

In production you issue tokens yourself, after authenticating the user however you choose:

```go
import schemafapi "github.com/flocko-motion/schemaf/api"

token, err := schemafapi.IssueToken(userID, time.Now().Add(24*time.Hour))
// time.Time{} (zero) as the second argument issues a token with no expiry.
```

## Raw Endpoints

For endpoints that need direct HTTP access — binary uploads, streaming downloads, SSE, or any non-JSON content — use `HandleRaw` instead of `Handle`:

```go
// go/api/blobs.go
// DownloadBlobEndpoint streams a file download.
type DownloadBlobEndpoint struct{}

func (e DownloadBlobEndpoint) Method() string { return "GET" }
func (e DownloadBlobEndpoint) Path() string   { return "/api/blobs/{id}" }
func (e DownloadBlobEndpoint) Auth() bool     { return true }

func (e DownloadBlobEndpoint) HandleRaw(w http.ResponseWriter, r *http.Request) error {
    id := r.PathValue("id")
    blob, err := storage.Get(r.Context(), id)
    if err != nil {
        return api.ErrNotFound
    }
    w.Header().Set("Content-Type", blob.ContentType)
    _, err = io.Copy(w, blob.Reader)
    return err
}
```

Same struct pattern, same `Method()`, `Path()`, `Auth()`. The only difference is the handler method:

| | `Handle` | `HandleRaw` |
|---|---|---|
| Signature | `Handle(ctx, Req) (Resp, error)` | `HandleRaw(w, r) error` |
| Request decoding | Automatic (JSON + path params) | You handle it |
| Response encoding | Automatic (JSON) | You handle it |
| OpenAPI schemas | Auto-generated from types | Summary/description only |
| Auth | Framework-managed | Framework-managed |

**Rules:**
- Define exactly one of `Handle` or `HandleRaw` — codegen will error if both are present
- If `HandleRaw` returns a non-nil error and you haven't written a response yet, the framework writes a JSON error response with the standard status code mapping (`ErrNotFound` → 404, etc.)
- If you've already started writing a response (set headers, streamed bytes), return `nil` and handle errors yourself
- Auth works identically — if `Auth()` returns `true`, the JWT is validated before `HandleRaw` is called, and `api.Subject(r.Context())` returns the authenticated user ID

## Database

### Migrations

Write SQL files in `go/db/migrations/`:

```sql
-- go/db/migrations/0001_users.sql
CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Migrations are auto-discovered by codegen and embedded into the binary. They run automatically on server startup. Name them with a numeric prefix for ordering.

**Transaction safety:** each migration runs inside its own transaction together with its tracking-table record. If a migration fails, both the schema change and the record are rolled back — the database is never left half-applied or recorded-but-not-applied. (Because of this, statements Postgres forbids inside a transaction block — e.g. `CREATE INDEX CONCURRENTLY` — cannot be used in a migration file.)

### Migration identifiers (independent migration lines)

Every migration is recorded in a single `schemaf_migrations` table, keyed by an **identifier** plus a version (`UNIQUE(prefix, version)`). The identifier is the `Prefix` field of a `MigrationSet`. Each identifier is an independent migration line with its own `0001, 0002, …` sequence — two lines never collide, even on the same database.

The framework itself is just one such line: it registers its migrations under the identifier `"schemaf"`. Your project's migrations are another line under your own identifier. They advance independently and are tracked separately in the same table.

This is what lets **several apps share one database**. Each app applies its own line under its own identifier:

```go
db.RunSet(ctx, sharedDB, db.MigrationSet{Prefix: "appA", Files: appAMigrations})
db.RunSet(ctx, sharedDB, db.MigrationSet{Prefix: "appB", Files: appBMigrations})
```

`appA` and `appB` evolve their schemas independently; the shared `schemaf_migrations` table keeps each line's history apart. If both apps are schemaf apps, the `"schemaf"` line is applied once and skipped by whoever runs second (already-applied versions are never re-run).

### Applying migrations to another database

The framework runs your project's migrations against the built-in Postgres automatically on startup. The same versioned, transaction-safe mechanism is also available for **secondary databases your project owns** — a separate analytics store, a per-tenant database, an external Postgres you provision yourself.

Bring your own connection and your own migration set, and call `db.RunSet`:

```go
import (
    "embed"

    frameworkdb "github.com/flocko-motion/schemaf/db"
)

//go:embed analytics_migrations/*.sql
var analyticsMigrations embed.FS

// otherDB is a *sql.DB you opened yourself (any Postgres).
err := frameworkdb.RunSet(ctx, otherDB, frameworkdb.MigrationSet{
    Prefix: "analytics",          // namespaces versions in schemaf_migrations
    Files:  analyticsMigrations,  // your NNNN_name.sql files
})
```

`RunSet` ensures the `schemaf_migrations` tracking table exists on the target database, then applies **only the set you pass** — recording versions under `Prefix`. It does *not* run framework migrations or any other registered set, so the target database carries only your set's tables (plus the tracking table itself). Already-applied versions are skipped, so calling it repeatedly is safe.

This is the same primitive the framework uses internally — one `MigrationSet` can serve both the built-in database (automatically, on startup) and any other database (explicitly, via `RunSet`), with uniform versioning and no duplicate migration code.

### Queries

Write [sqlc](https://sqlc.dev) queries in `go/db/queries/`:

```sql
-- go/db/queries/users.sql

-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at DESC;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (email) VALUES ($1) RETURNING *;

-- name: UpdateUser :one
UPDATE users SET email = $2 WHERE id = $1 RETURNING *;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;
```

The `-- name:` comment declares:
- Function name (e.g., `GetUser`)
- Return type: `:many` (slice), `:one` (single row), `:exec` (no return)

Codegen generates type-safe Go functions and model structs from your schema. Multi-parameter queries automatically get a `Params` struct.

### Wiring

Wire the database in `main.go`:
```go
app.AddDb(db.Provider)
```

`db.Provider` is generated in `go/db/migrations.gen.go` and returns the embedded migration files. The framework handles connection, migration execution, and query setup.

## CLI Subcommands

For admin tasks, data imports, or one-off scripts, add subcommands to your binary. Subcommands use the same `cli.SubcommandProvider` pattern as the framework itself (see `cli/cmd/*` for examples).

### Writing a provider

Create `go/importer/importer.go`:
```go
package importer

import (
    "fmt"

    "github.com/spf13/cobra"
    "github.com/flocko-motion/schemaf/cli"
)

// SubcommandProvider returns the "import" command tree.
func SubcommandProvider(_ *cli.Context) []*cobra.Command {
    cmd := &cobra.Command{
        Use:   "import",
        Short: "Import data from external sources",
    }

    cmd.AddCommand(newUsersCmd())
    return []*cobra.Command{cmd}
}

func newUsersCmd() *cobra.Command {
    var filePath string

    cmd := &cobra.Command{
        Use:   "users",
        Short: "Import users from CSV",
        RunE: func(cmd *cobra.Command, args []string) error {
            fmt.Printf("Importing users from %s\n", filePath)
            // your import logic here
            return nil
        },
    }

    cmd.Flags().StringVar(&filePath, "file", "", "Path to CSV file")
    cmd.MarkFlagRequired("file")

    return cmd
}
```

### Wiring

```go
app.AddSubcommand(importer.SubcommandProvider)
```

Now your binary has `./myapp import users --file data.csv` alongside the built-in `./myapp server`.

### Provider pattern

The provider signature is:
```go
type SubcommandProvider func(ctx *cli.Context) []*cobra.Command
```

Providers receive a `*cli.Context` with access to config, state, and HTTP utilities. They return `[]*cobra.Command` — a single provider can mount a whole command tree with nested subcommands.

### Services vs. subcommands

| | `app.AddSubcommand()` | `app.AddService()` |
|---|---|---|
| What | CLI command (runs and exits) | Background goroutine |
| Use for | Data imports, admin tasks, scripts | Workers, schedulers, event loops |
| Lifecycle | Runs when invoked from CLI | Starts/stops with the server |

`app.AddService()` providers are **only started** when running `./myapp server` — codegen and subcommands never start services.

## Code Generation

**One command generates everything:**

```bash
./schemaf.sh codegen
```

`schemaf.sh` uses `go run` to build a standalone schemaf CLI on the fly — no project binary needed, no dependencies beyond Go itself.

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
   - Generates `gen/openapi.json` — OpenAPI 3.0 spec

4. **OpenAPI spec → TypeScript client**
   - Generates `frontend/src/api/generated/api.gen.ts` — type-safe client for your frontend
   - No running server needed

5. **Compose → compose.gen.yml**
   - Merges schemaf's built-in compose (backend, Postgres) with your `compose/*.yml`
   - Files matching `*.dev.yml` are dev/test only — not included in `compose.gen.yml` (prod)
   - Used by `./schemaf.sh run` (prod) and `./schemaf.sh dev` (dev)

**Zero configuration.** Just run `./schemaf.sh codegen` and all the glue code appears.

## Testing

**Running tests:**
```bash
./schemaf.sh test                  # regenerate code, then run all tests
./schemaf.sh test --verbose        # verbose go test output
./schemaf.sh test --no-cache       # bypass test cache
```

`./schemaf.sh test` always runs codegen first, then `go test ./go/...` and `npx tsc --noEmit`. This guarantees tests always run against freshly generated code.

### Go tests

Standard Go test files in `go/api/*_test.go` using `httptest`.

### TypeScript tests

Write exported async functions named `test*` in `go/tests/*.test.ts`:

```typescript
// go/tests/api.test.ts
export async function testCreateUser(baseUrl: string) {
    const resp = await fetch(`${baseUrl}/api/users`, { method: "POST", ... })
    if (!resp.ok) throw new Error(`expected 200, got ${resp.status}`)
}
```

Codegen generates Go wrappers (`go/tests/ts.gen_test.go`) that start an `httptest.Server`, run the TypeScript via `npx tsx`, and report pass/fail as a standard Go test.

To skip a TS test, add a comment on the preceding line:

```typescript
// skip: requires clock docker service
export async function testClockTime(baseUrl: string) { ... }
```

The generated Go wrapper will call `t.Skip(...)` with that message.

### Test output

Test output is formatted by [gotestsum](https://github.com/gotestyourself/gotestsum) when installed (recommended):

```bash
go install gotest.tools/gotestsum@latest
```

## Database Backups

Schemaf includes built-in database backup to remote SFTP servers (e.g. Hetzner Storage Box).

### Configuration

Set these environment variables in `~/.<name>/etc/env`:

| Variable | Required | Default | Description |
|---|---|---|---|
| `BACKUP_SSH_HOST` | yes | — | SFTP server hostname |
| `BACKUP_SSH_PORT` | no | `22` | SFTP server port |
| `BACKUP_SSH_USER` | yes | — | SFTP username |
| `BACKUP_SSH_KEY_PATH` | yes | — | Host path to SSH private key (mounted as Docker secret) |
| `BACKUP_PATH` | no | `/backups` | Remote directory |
| `BACKUP_RETAIN` | no | `30` | Number of backups to keep |
| `BACKUP_HOUR` | no | `3` | UTC hour for daily auto-backup (0-23) |

### Automatic backups

When `BACKUP_SSH_HOST` is set, the server automatically runs daily backups at the configured hour (default: 03:00 UTC). Old backups beyond the retention count are deleted automatically. No cron or external scheduling needed.

### Manual commands

```bash
# One-shot backup to SFTP
./myapp db backup

# Backup to local file
./myapp db backup --local /tmp/backup.sql.gz

# List remote backups
./myapp db restore

# Restore specific backup
./myapp db restore myapp-2026-03-25_03-00-00.sql.gz

# Restore most recent backup
./myapp db restore --latest

# Restore from local file
./myapp db restore --local /tmp/backup.sql.gz
```
