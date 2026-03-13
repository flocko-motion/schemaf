# Extending Your Project

Back to [README](README.md) | See also: [Installation](INSTALL.md)

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
    app.AddSubcommand(importer.SubcommandProvider) // custom: CLI commands

    log.Fatal(app.Run())
}
```

`app.Run()` hands over to Cobra. The `server` command (default) starts the HTTP server. Custom subcommands are available alongside it.

## Endpoint Interface

API endpoints are structs implementing a typed interface — not plain `http.HandlerFunc` functions. This gives the framework enough information to handle serialization, auth, and OpenAPI generation automatically.

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
- On error: maps the error to an appropriate HTTP status

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

5. **Compose → gen/compose.gen.yml**
   - Merges schemaf's built-in compose (backend, Postgres) with your `compose/*.yml`
   - Used by `./schemaf.sh run` and `./schemaf.sh dev`

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
