# Setting Up a New Project

Back to [README](README.md) | Next: [Extending Your Project](EXTEND.md)

## Contents

- [Prerequisites](#prerequisites)
- [Quickstart](#quickstart) — create a project with one command
- Manual setup (what `init` does, step by step):
  - [1. Create the project](#1-create-the-project-what-init-does)
  - [2. Wire up main.go](#2-wire-up-maingo)
  - [3. Add a database migration](#3-add-a-database-migration)
  - [4. Add database queries](#4-add-database-queries)
  - [5. Add an API endpoint](#5-add-an-api-endpoint)
  - [6. Generate and run](#6-generate-and-run)
- [schemaf.sh](#schemafsh)
- [schemaf.toml](#schemaftoml)
- [Secrets](#secrets)
- [Container runtime](#container-runtime) — podman by default, Docker on request
- [Migrating from Docker to podman](#migrating-from-docker-to-podman)
- [Compose](#compose)

## Prerequisites

- Go
- [podman](https://podman.io/docs/installation) (rootless) and [docker-compose](https://docs.docker.com/compose/install/standalone/) — the default [container runtime](#container-runtime)
  ```bash
  # macOS
  brew install podman docker-compose
  podman machine init      # once: creates the Linux VM podman runs containers in
  podman machine start     # after every reboot

  # Linux: podman from your distribution, docker-compose v2 from its release page
  sudo apt install podman                     # or dnf, pacman, ...
  # docker-compose: https://github.com/docker/compose/releases → ~/.local/bin/docker-compose
  ```
  `podman compose` hands off to an external compose provider. schemaf requires it to be **docker-compose**, not `podman-compose`; podman picks docker-compose automatically when it is on your `PATH` (check with `podman compose version` — it must print `Docker Compose version …`).
- Or Docker with the Compose plugin, if you set `SCHEMAF_RUNTIME=docker` (see [Container runtime](#container-runtime))
- Node.js (for TypeScript codegen and frontend)
- `gotestsum` for pretty test output (recommended):
  ```bash
  go install gotest.tools/gotestsum@latest
  ```

## Quickstart

One command scaffolds a complete, working project — it fetches schemaf and generates everything, like `create-react-app`:

```bash
go run github.com/flocko-motion/schemaf/cmd/schemaf@latest init myapp
cd myapp
./schemaf.sh dev all
```

`init` creates `myapp/` with the full structure (`schemaf.toml`, `go.work`, the Go module, a sample endpoint + migration + query, and the frontend scaffold) and runs codegen. You write nothing by hand. Pin a specific release with `@v1.2.3` in place of `@latest`.

That's the whole onboarding. **The sections below explain what `init` does under the hood — you don't need them to get started.**

## 1. Create the project (what `init` does)

```bash
mkdir myapp && cd myapp
mkdir -p go/db/migrations go/db/queries go/api frontend
```

Create `schemaf.toml`:
```toml
title = "My Application"
name = "myapp"
```

Initialize Go modules:
```bash
cd go && go mod init myapp && go get github.com/flocko-motion/schemaf@latest && cd ..
```

Create `go.work` in the project root (required — `schemaf.sh` runs Go from the project root):
```
go 1.25.0

use (
    ./go
)
```

## 2. Wire up main.go

Create `go/main.go`:
```go
package main

import (
    "context"
    "log"

    "github.com/flocko-motion/schemaf/schemaf"

    "myapp/api"
    "myapp/db"
)

func main() {
    ctx := context.Background()
    app := schemaf.New(ctx)

    app.AddDb(db.Provider)
    app.AddApi(api.Provider)

    if err := app.Run(); err != nil {
        log.Fatal(err)
    }
}
```

`schemaf.New(ctx)` reads the project name from `schemaf.toml`. `app.Run()` hands over to Cobra — the `server` command starts the HTTP server, and any custom subcommands you register are available alongside it.

## 3. Add a database migration

Create `go/db/migrations/0001_users.sql`:
```sql
CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Migrations are plain SQL files, auto-discovered by codegen. They run automatically on server startup.

## 4. Add database queries

Create `go/db/queries/users.sql`:
```sql
-- name: ListUsers :many
SELECT * FROM users ORDER BY created_at DESC;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (email) VALUES ($1) RETURNING *;
```

This is [sqlc](https://sqlc.dev) syntax. Codegen generates type-safe Go functions: `ListUsers(ctx)`, `GetUser(ctx, id)`, `CreateUser(ctx, email)`.

## 5. Add an API endpoint

Create `go/api/users.go`:
```go
package api

import (
    "context"

    schemafapi "github.com/flocko-motion/schemaf/api"

    "myapp/db"
)

// ListUsersEndpoint returns all users ordered by creation date.
type ListUsersEndpoint struct{}

func (e ListUsersEndpoint) Method() string { return "GET" }
func (e ListUsersEndpoint) Path() string   { return "/api/users" }
func (e ListUsersEndpoint) Auth() schemafapi.AuthLevel { return schemafapi.AuthNone }
func (e ListUsersEndpoint) Handle(ctx context.Context, req ListUsersReq) (ListUsersResp, error) {
    users, err := db.ListUsers(ctx)
    if err != nil {
        return ListUsersResp{}, err
    }
    return ListUsersResp{Users: users}, nil
}

type ListUsersReq struct{}

type ListUsersResp struct {
    Users []db.User `json:"users"`
}
```

See [Endpoint Interface](EXTEND.md#endpoint-interface) for the full pattern.

## 6. Generate and run

```bash
# First run — generates schemaf.sh + all glue code:
go run github.com/flocko-motion/schemaf/cmd/schemaf codegen all

# From now on, use the generated script:
./schemaf.sh codegen    # regenerate all glue code
./schemaf.sh dev        # start the full dev stack
```

Codegen produces:
- `schemaf.sh` → project CLI entrypoint (auto-updated on every codegen)
- `go/db/migrations.gen.go` → `db.Provider` (embedded SQL)
- `go/db/queries.gen.go` → type-safe query functions (sqlc)
- `go/api/endpoints.gen.go` → `api.Provider` (handler registration)
- `gen/openapi.json` → OpenAPI spec
- `frontend/src/api/generated/api.gen.ts` → TypeScript client

## schemaf.sh

`schemaf.sh` is generated by codegen and should never be edited. It delegates to the Go CLI via `go run`. All CLI logic lives in Go.

```bash
./schemaf.sh codegen         # Generate all code (SQL, endpoints, TypeScript client)
./schemaf.sh test            # Regenerate, then run all tests
./schemaf.sh test --verbose  # Verbose test output
./schemaf.sh test --no-cache # Bypass test cache
./schemaf.sh run             # Start production compose stack
./schemaf.sh dev             # Start dev compose stack (exposes service ports)
./schemaf.sh dev postgres    # Start only specific services
```

`run` and `dev` start the stack with `<runtime> compose` (`podman compose` by default, see [Container runtime](#container-runtime)) — the actual server runs inside the container.

To add custom commands, use `app.AddSubcommand()` in your Go code — see [CLI Subcommands](EXTEND.md#cli-subcommands).

## schemaf.toml

Projects have a minimal `schemaf.toml` file:

```toml
title = "My Application"
name = "myapp"
port = 8000    # optional, default 8000
```

The `name` field determines the database name, container name prefixes, the Postgres volume name (`<name>-postgres-data`), and config directory. The optional `port` field sets the base port — all service ports are derived from it (backend = port, frontend dev = port+2, postgres = port+3). Default is 8000.

## Secrets

schemaf keeps secrets **out of the project directory**. The database password and any other sensitive values are stored per-project under your home directory:

| Environment | Path |
|-------------|------|
| Dev/local   | `~/.<name>/dev/etc/env` |
| Production  | `~/.<name>/etc/env` |

The file format is plain `KEY=VALUE`, one per line. Comments (`#`) and blank lines are allowed.

**Minimum required: `DB_PASS`**

The Postgres container and backend both need `DB_PASS`. Set it up before your first `run` or `dev`:

```bash
# For a project named "myapp":
mkdir -p ~/.myapp/dev/etc
echo "DB_PASS=changeme" > ~/.myapp/dev/etc/env
```

For production, use a strong password:
```bash
mkdir -p ~/.myapp/etc
echo "DB_PASS=$(openssl rand -hex 16)" > ~/.myapp/etc/env
```

**Public frontend config: `PUBLIC_*`.** Values the frontend needs at runtime and that differ per instance (a Sentry DSN, the public URL, ...) go into the same files, as variables whose names start with `PUBLIC_`. The server sends every `PUBLIC_*` variable to every browser that opens the app, so **never put a secret in a `PUBLIC_` variable**. Variables without the prefix are never exposed. See [Runtime config](EXTEND.md#runtime-config-public_).

**Optional: Auth0.** Add `PUBLIC_AUTH0_DOMAIN` and `PUBLIC_AUTH0_CLIENT_ID` to the same file to switch on the built-in Auth0 login (both or neither). See [Activating Auth0](EXTEND.md#activating-auth0).

**How secrets reach the server:** `schemaf.sh` loads the env file on every invocation (`run` → `~/.<name>/etc/env`, everything else → `~/.<name>/dev/etc/env`), setting any variables not already in your environment. In dev, the backend runs natively and sees them directly. In production, the generated `compose.gen.yml` passes the whole `~/.<name>/etc/env` to the backend container (`env_file`), and Compose fills `${DB_PASS}` in the Postgres service via environment variable substitution. Secrets flow from your home directory into containers without ever touching version control.

> **Never commit secrets.** The `~/.<name>/` directory is outside your project tree by design. If you see `The "DB_PASS" variable is not set` warnings from Compose, create the env file above.

## Container runtime

schemaf runs its containers with **rootless podman** by default. Docker's daemon runs as root, so anything that can talk to it is root-equivalent on the machine; rootless podman runs containers as your own user. That is the safer default for running other people's images next to the database that holds your users' secrets.

**Choosing the runtime.** The runtime is a property of the machine, not of the project, so it is an environment variable and not a `schemaf.toml` field:

| `SCHEMAF_RUNTIME` | Runtime |
|---|---|
| unset | podman (the default) |
| `podman` | podman (the default, spelled out) |
| `docker` | Docker |

Any other value stops schemaf with an error listing the valid values. Upgrading from a schemaf release that used Docker: a machine that has only Docker must set `SCHEMAF_RUNTIME=docker`; a server moving to podman with existing data follows [the migration steps](#migrating-from-docker-to-podman). Set it in your shell profile, or in the project's env files (`~/.<name>/etc/env` for `run`, `~/.<name>/dev/etc/env` for everything else), which `schemaf.sh` loads on every invocation:

```bash
echo "SCHEMAF_RUNTIME=docker" >> ~/.myapp/etc/env
```

**Compose** is always `<runtime> compose`. Under podman, `podman compose` must hand off to **docker-compose** (the reference Compose v2), never `podman-compose`: schemaf relies on `up --wait`, `depends_on: condition: service_healthy`, long-form `env_file` with `required: false`, and file `secrets`. Both runtimes therefore use the same compose implementation on the same generated compose files; only the engine underneath differs.

**Missing tools fail loudly.** Before `dev`, `run` and `test` touch a container, schemaf checks the runtime and stops with a message naming what's missing and how to fix it. It never falls back to the other runtime and never guesses:

| Situation | What schemaf tells you |
|---|---|
| podman not installed | install podman, or set `SCHEMAF_RUNTIME=docker` |
| `podman compose` uses `podman-compose`, or no provider at all | install docker-compose |
| macOS: podman machine not running | `podman machine start` (and `podman machine init` first if no machine exists) |
| Docker selected but not installed | install Docker, or unset `SCHEMAF_RUNTIME` to use podman |
| Docker selected but the daemon is not running | start Docker (Docker Desktop, or `sudo systemctl start docker`) |
| Docker selected but `docker compose` missing | install the Docker Compose plugin |

**The data volume belongs to one runtime.** The project's Postgres data lives in a named volume, `<name>-postgres-data`. podman and Docker each keep their own volumes, so the same name can exist in both, holding different data. schemaf does not accept that ambiguity: when `dev`, `run` or `test` starts, it asks the *other* runtime whether it has `<name>-postgres-data`. If it does, schemaf refuses to start, whether or not the selected runtime has one too. The other runtime is only asked when its CLI is installed and usable; if it isn't, the check is skipped.

The error names your project's real volume and gives three ways out:

1. **Migrate** the data to the selected runtime, see [Migrating from Docker to podman](#migrating-from-docker-to-podman).
2. **Or** switch `SCHEMAF_RUNTIME` to the runtime that has the data.
3. **Or**, if the data is throwaway, delete the other runtime's volume: `<other> volume rm <name>-postgres-data`.

schemaf never migrates or deletes data itself.

**Rootless works unchanged.** The generated `compose.gen.yml`, `compose.dev.gen.yml` and `compose.test.gen.yml` run under rootless podman exactly as generated: healthchecks, `depends_on` conditions, `env_file`, the backup SSH key as a secret, named volumes and published ports. There are no podman-specific variants.

## Migrating from Docker to podman

A machine whose project data sits in a Docker volume can't start under podman until that volume is gone (see [the data volume](#container-runtime)). Move the data with a logical dump: `pg_dump` out of the old Postgres, `psql` into the new one. Never copy the volume's files: rootless podman maps user IDs differently, and a copied Postgres data directory fails with wrong ownership.

It is a move, not a copy: the last step deletes the Docker volume. The steps are the same on a production server and on a dev machine (dev and production share the one volume); they use the project `myapp` with the production env file, so for dev use `~/.myapp/dev/etc/env` instead.

```bash
cd myapp                                   # the project root, next to compose.gen.yml
set -a; . ~/.myapp/etc/env; set +a         # DB_PASS etc. for compose variable substitution

# 1. Start only the old Postgres, under Docker
docker compose -f compose.gen.yml down
docker compose -f compose.gen.yml up -d --wait postgres

# 2. Dump it, then stop it
docker exec myapp-postgres pg_dump -U schemaf -d myapp > myapp.sql
docker compose -f compose.gen.yml down

# 3. Start the new Postgres, under podman (creates an empty volume)
podman compose -f compose.gen.yml up -d --wait postgres

# 4. Restore
podman exec -i myapp-postgres psql -U schemaf -d myapp -v ON_ERROR_STOP=1 < myapp.sql

# 5. Verify: the tables and their row counts match what Docker had
podman exec myapp-postgres psql -U schemaf -d myapp -c '\dt'
podman exec myapp-postgres psql -U schemaf -d myapp -c 'SELECT count(*) FROM users'
podman compose -f compose.gen.yml down

# 6. Remove the old volume — only after step 5 checks out
docker volume rm myapp-postgres-data

# 7. Start normally
./schemaf.sh run
```

Keep `myapp.sql` until the app runs correctly under podman, then delete it: it holds your users' data in plain text. To move the other way, swap `docker` and `podman` and set `SCHEMAF_RUNTIME=docker`.

## Compose

schemaf ships with a **built-in compose configuration** that covers the full standard stack:

- Go backend (port 8000)
- Frontend dev server (port 8002)
- Postgres (port 8003)

You never write or maintain these service definitions. They are part of the framework.

**Extending with project services:**

If your project needs additional services (Redis, a worker container, a vector DB, etc.), add a `compose/` directory to your project:

```
myapp/
└── compose/
    ├── redis.yml         # All environments (prod + dev + test)
    └── minio.dev.yml     # Dev/test only (not included in prod)
```

Files ending in `.dev.yml` are only included in the dev and test compose files — not in production. Regular `*.yml` files are included everywhere.

Codegen merges the framework's built-in compose with your extensions and produces `compose.gen.yml` (prod) and `compose.dev.gen.yml` (dev) at the project root.

**Running the stack:**

```bash
./schemaf.sh run              # Full stack, production mode
./schemaf.sh dev              # Full stack, dev mode (exposes service ports)
./schemaf.sh dev postgres     # Only specific services (useful during development)
```

`./schemaf.sh dev <args>` passes extra args through to `<runtime> compose up` — useful for starting only a subset of services while running the Go server directly on the host (e.g. with a debugger).
