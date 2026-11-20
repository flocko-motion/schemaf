# Setting Up a New Project

Back to [README](README.md) | Next: [Extending Your Project](EXTEND.md)

## Contents

- [Prerequisites](#prerequisites)
- [Quickstart](#quickstart) — create a project with one command
- Manual setup (what `init` does, step by step):
  - [1. Create the project](#1-create-the-project-what-init-does)
  - [2. Wire up the app](#2-wire-up-the-app)
  - [3. Add a database migration](#3-add-a-database-migration)
  - [4. Add database queries](#4-add-database-queries)
  - [5. Add an API endpoint](#5-add-an-api-endpoint)
  - [6. Generate and run](#6-generate-and-run)
- [schemaf.sh](#schemafsh)
- [schemaf.toml](#schemaftoml)
- [Secrets](#secrets)
- [Deployment](#deployment) — `build` an image, `run` it
  - [Separate build and run users](#separate-build-and-run-users)
  - [What the run host provides](#what-the-run-host-provides)
- [Container runtime](#container-runtime) — podman by default, Docker on request
- [Migrating from Docker to podman](#migrating-from-docker-to-podman)
- [Compose](#compose)
- [Parallel dev instances](#parallel-dev-instances) — several dev stacks at once, e.g. one per git worktree

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
  systemctl --user enable --now podman.socket   # once: podman's API socket, which docker-compose talks to
  ```
  `podman compose` hands off to an external compose provider. schemaf requires it to be **docker-compose**, not `podman-compose`; podman picks docker-compose automatically when it is on your `PATH` (check with `podman compose version` — it must print `Docker Compose version …`). docker-compose talks to podman through podman's API socket (`DOCKER_HOST` if set, otherwise podman's own). On macOS the podman machine provides it; on Linux it is the user's `podman.socket`, which has to be enabled once, as above, by every user that runs compose (`dev`, `test`, `run`); `build` and `load` don't need it.
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

`init` creates `myapp/` with the full structure (`schemaf.toml`, `go.work`, the Go module, the app wiring in `go/app/app.go` with a three-line `go/main.go`, a sample endpoint + migration + query, an integration test in `go/tests/health_test.go`, and the frontend scaffold) and runs codegen. You write nothing by hand. Pin a specific release with `@v1.2.3` in place of `@latest`.

That's the whole onboarding. **The sections below explain what `init` does under the hood — you don't need them to get started.**

## 1. Create the project (what `init` does)

```bash
mkdir myapp && cd myapp
mkdir -p go/app go/db/migrations go/db/queries go/api go/tests frontend
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

## 2. Wire up the app

Create `go/app/app.go`. `Wire` holds every `Add*`/`On*`/`Set*` call; `main.go` and the integration tests share it (see [EXTEND › App Wiring](EXTEND.md#app-wiring-goappappgo)):
```go
package app

import (
    "github.com/flocko-motion/schemaf/schemaf"

    "myapp/api"
    "myapp/db"
)

// Wire registers the app's providers, hooks and services.
func Wire(app *schemaf.App) {
    app.AddDb(db.Provider)
    app.AddApi(api.Provider)
    app.SetFrontend(FrontendFS())
}
```

Create `go/main.go`:
```go
package main

import (
    "context"
    "log"

    "github.com/flocko-motion/schemaf/schemaf"

    "myapp/app"
)

func main() {
    a := schemaf.New(context.Background())
    app.Wire(a)
    if err := a.Run(); err != nil {
        log.Fatal(err)
    }
}
```

`schemaf.New(ctx)` takes the project name that codegen compiles in from `schemaf.toml` (`go/app/constants.gen.go`), and `FrontendFS()` comes from `go/app/frontend.gen.go`; both appear with the first codegen. `a.Run()` hands over to Cobra — the `server` command starts the HTTP server, and any custom subcommands you register are available alongside it.

Create `go/tests/health_test.go`, the first [integration test](EXTEND.md#integration-tests-schemaftest): it starts the app on its own database, calls `GET /api/health`, mints a token and calls `GET /api/user/me` with it.

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
go run github.com/flocko-motion/schemaf/cmd/schemaf codegen

# From now on, use the generated script:
./schemaf.sh codegen    # regenerate all glue code
./schemaf.sh dev        # start the full dev stack
```

Codegen produces:
- `schemaf.sh` → project CLI entrypoint (auto-updated on every codegen)
- `go/db/migrations.gen.go` → `db.Provider` (embedded SQL)
- `go/db/db.gen.go`, `go/db/models.gen.go`, `go/db/users.sql.gen.go` → type-safe query functions (sqlc, one `.sql.gen.go` per query file)
- `go/api/endpoints.gen.go` → `api.Provider` (handler registration, and the OpenAPI spec the server serves)
- `gen/openapi.json` → OpenAPI spec
- `gen/schema-sources` → the packages declaring the API's types (input to the freshness check)
- `frontend/src/api/generated/api.gen.ts` → TypeScript client

## schemaf.sh

`schemaf.sh` is generated by codegen and should never be edited. It delegates to the Go CLI via `go run`. All CLI logic lives in Go.

```bash
./schemaf.sh codegen         # Generate all code (SQL, endpoints, TypeScript client)
./schemaf.sh test            # Check generated files are fresh, then run all tests
./schemaf.sh test --verbose  # Verbose test output
./schemaf.sh test --no-cache # Bypass test cache
./schemaf.sh build           # Build the production image (see Deployment)
./schemaf.sh build --output myapp-3f9a2c1.run   # ...and write a deployment artifact
./schemaf.sh run             # Build, then start the production compose stack
./schemaf.sh dev all         # Start the dev stack: Postgres + project services, backend, Vite
./schemaf.sh dev db,backend  # Start only some of it: db | infrastructure | backend | frontend | all
./schemaf.sh dev all --instance 349   # A second dev stack next to the default one
./schemaf.sh instances       # List the project's dev instances on this machine
```

`run` starts the production stack with `<runtime> compose` (`podman compose` by default, see [Container runtime](#container-runtime)); the server runs inside the backend container. `dev` starts only Postgres and the project's compose services in containers. The backend runs natively on the host (`go run`), and Vite runs with `npm run dev`. Ctrl-C stops the backend and Vite; the containers keep running. `--instance` runs a separate dev stack, see [Parallel dev instances](#parallel-dev-instances). `build` and `run` need only the container runtime, git and the repository; they never call Go or Node on the host (both run inside the image build).

To add custom commands, use `app.AddSubcommand()` in your Go code — see [CLI Subcommands](EXTEND.md#cli-subcommands).

## schemaf.toml

Projects have a minimal `schemaf.toml` file:

```toml
title = "My Application"
name = "myapp"
port = 8000    # optional, default 8000
```

The `name` field determines the database name, container name prefixes, the Postgres volume name (`<name>-postgres-data`), and config directory. The optional `port` field sets the base port — all service ports are derived from it (backend = port, frontend dev = port+2, postgres = port+3). Default is 8000. A [parallel dev instance](#parallel-dev-instances) `n` shifts all three by `10 × n` and adds `-<n>` to the container, volume and network names.

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

**How secrets reach the server:** `schemaf.sh` loads the env file on every invocation (`run` and the [run-side commands](#the-run-side-schemafsh) → `~/.<name>/etc/env`, everything else → `~/.<name>/dev/etc/env`), setting any variables not already in your environment. The exception is `build`, which runs third-party code (`npm install`): **`build` reads only `SCHEMAF_RUNTIME` from the env file, never any other value.** It reads it from `~/.<name>/dev/etc/env` for `build`, and from `~/.<name>/etc/env` for the build step of `run` (so `run` builds and starts with the same runtime), and only when your environment doesn't set it already. Every command that loads `~/.<name>/dev/etc/env` also runs in [development mode](EXTEND.md#development-mode) (`SCHEMAF_DEV=1`); `build` and `run` never do. Env files must not set `SCHEMAF_DEV`: `schemaf.sh` refuses to start if one does. In dev, the backend runs natively and sees them directly. In production, the generated `compose.gen.yml` passes the whole `~/.<name>/etc/env` to the backend container (`env_file`), and Compose fills `${DB_PASS}` in the Postgres service via environment variable substitution. Secrets flow from your home directory into containers without ever touching version control.

> **Never commit secrets.** The `~/.<name>/` directory is outside your project tree by design. If you see `The "DB_PASS" variable is not set` warnings from Compose, create the env file above.

## Deployment

Deploying is two steps: **build** an image from a commit, then **run** it. On a single-user host, `./schemaf.sh run` does both. A host that keeps secrets in its database can split them across two users (see [Separate build and run users](#separate-build-and-run-users)).

### Generated files must be fresh

`build` runs no codegen: generated files are written by developers and committed. To catch a commit whose generated files don't match its inputs, codegen records a hash of its inputs in `gen/inputs.sha256`, and `build` and `test` recompute it. On a mismatch they stop:

```
generated files are stale — run ./schemaf.sh codegen and commit
```

The inputs are `go/api/*.go` (without `*.gen.go` and `*_test.go`), the files in `go/db/migrations/` and `go/db/queries/`, `schemaf.toml`, `compose/*.yml`, the packages listed in `gen/schema-sources` (those declaring types the API uses, e.g. `go/obj/*.go`), and the schemaf version required in `go/go.mod`. See [Code Generation](EXTEND.md#code-generation).

The check verifies what the build uses:

- **`build --output`** (a deployment artifact) hashes the inputs **as committed**: the input files of the commit, the commit's `gen/schema-sources` and `go/go.mod`, compared with the commit's `gen/inputs.sha256`. An untracked or gitignored input is not part of that hash. So if codegen ran while, say, a gitignored `compose/extra.yml` was present, the committed `compose.gen.yml` includes a file the artifact won't contain, and `build --output` stops as stale instead of shipping a stack that fails on the run host. An input committed as a symlink is refused: `input <path> is a symlink in commit <short> — an artifact ships the commit, commit the file itself`.
- **Plain `build`, `run` and `test`** hash the working tree, uncommitted and untracked inputs included, so local testing with uncommitted changes works.

### `build`

```bash
./schemaf.sh build [--tag <tag>] [--output <file>] [--force]
```

Builds `Dockerfile.gen` into the image `<name>:<tag>`. It needs the container runtime, git and the repository, and nothing else: no Go, no Node, and **no secrets**.

- **Env file**: `build` reads only `SCHEMAF_RUNTIME` from the env file, never any other value (see [Secrets](#secrets)), so no secret reaches the image build.

- **Tag**: the short commit hash by default; `--tag` sets your own (letters, digits, `_`, `.`, `-`).
- **Exactly the commit**: with a clean working tree the image is built from `git archive HEAD`, so untracked and ignored files (e.g. `node_modules/`) never reach it. A commit that was built before is not built again, unless `--force`.
- **Uncommitted changes**: a plain `build` is allowed for local testing; it builds the working tree, always rebuilds, and tags `<short-hash>-dirty` by default. `build --output` refuses: `commit first — a deployment artifact must name exactly one commit`. There is no override.
- **`--output <file>`** writes a deployment artifact (below). An existing file is an error, never overwritten.
- **Empty inputs**: before building, `build` prints codegen's warnings about empty inputs from `gen/empty-inputs`, e.g. `⚠ no migrations in go/db/migrations — db.Provider() is empty`. They never stop the build. See [Empty inputs](EXTEND.md#empty-inputs).

### The artifact: `<name>-<tag>.run`

`build --output` writes a self-extracting executable: a short shell header followed by the archive. It contains

- the image (`<runtime> save`),
- the commit's compose files (`compose.gen.yml` and the `compose/*.yml` it includes),
- the run-side `schemaf.sh`,
- a manifest: project name, tag, commit, `git describe`, schemaf version, image platform.

It **never contains secrets**, so the same file can go to staging and to production. On the run host:

```bash
./myapp-3f9a2c1.run load
```

`load`, as the user who will run the stack:

- loads the image into **that user's** container store,
- unpacks the compose files into `~/.<name>/deploy/<tag>/`,
- installs `~/.<name>/bin/schemaf.sh`, or replaces it (see below),
- prints the tag.

**The run-side script is never downgraded: newest wins.** `load` replaces `~/.<name>/bin/schemaf.sh` only when the artifact's schemaf version (from its manifest) is equal to or newer than the installed script's (its `SCHEMAF_VER="…"` line). An older artifact keeps the installed script and prints one line: `keeping run-side schemaf.sh v1.13.0 (artifact has v1.12.1)`. The run-side script runs the deploy directories of older artifacts too, so a rollback is `run --image <older tag>` with the newest script. An installed script without a readable version is refused: remove it, then load again. Artifacts built by a schemaf release older than this rule replace the script unconditionally, because their own `schemaf.sh` performs the load.

Tags are free-form, so a tag must never silently change meaning:

| Already loaded | `load` |
|---|---|
| tag not loaded | loads it |
| same tag, same commit | does nothing, and says so |
| same tag, different commit | refuses: `v1.4 is already loaded from commit 3f9a2c1, this archive is commit 8b1d0e4 — remove it first (schemaf.sh image rm v1.4) or build with a different tag` |

An image built for another platform (e.g. arm64 on an amd64 host) is refused too.

### The run-side `schemaf.sh`

`~/.<name>/bin/schemaf.sh` is the same generated script. Without a checkout next to it, it offers only:

| Command | Does |
|---|---|
| `run --image <tag> [compose up flags]` | loads `~/.<name>/etc/env`, checks the [runtime and data volume](#container-runtime), starts the stack from `~/.<name>/deploy/<tag>/` with that image. Never builds; a tag that isn't loaded is an error. |
| `stop` | stops the stack |
| `status` | lists the stack's containers |
| `logs [compose logs flags]` | shows the stack's logs, e.g. `logs -f backend` |
| `image ls` | the loaded tags with commit, `git describe`, size, and which one is running |
| `image rm <tag>` | removes the image and `~/.<name>/deploy/<tag>/`; refuses while that tag is running |
| any other word: app subcommands (`db backup`, `db restore`, `db query`, your own `AddSubcommand` commands) | runs in the running backend container (`<runtime> exec <name>-backend /app <word> …`), i.e. exactly the deployed version; if the backend isn't running: `start it first: schemaf.sh run --image <tag>` |

The run side can't list your app's subcommands, so the rule is: every word that isn't one of the commands above and isn't a refused developer command goes into the backend container. An unknown word therefore fails loudly with the app binary's own error and exit code.

Developer commands (`codegen`, `test`, `dev`, `build`, `upgrade`) stop with `this host has no checkout; that command runs on a dev or build host`, and `server` with `the server runs in the backend container — start it with: schemaf.sh run --image <tag>`. Nothing on the run side needs Go.

Old images are rollback targets, so nothing is ever pruned automatically; `image ls` and `image rm` are the tooling. The database lives in the named volume `<name>-postgres-data`, which outlives every version; migrations run when the server starts, as always.

### Single-user hosts: `run`

`./schemaf.sh run` (without `--image`) is `build` (only `SCHEMAF_RUNTIME` from the env file, freshness check, commit tag, rebuild for uncommitted changes) followed by starting that image with the checkout's own compose files. Flags after `run` go to `<runtime> compose up`, e.g. `./schemaf.sh run -d`. It is the same build path and the same start path as the two-user setup, just in one step. `stop`, `status` and `logs` belong to the run side only (on a checkout those names stay free for your own subcommands); on a checkout host use compose directly: `<runtime> compose -p <name> down`, `… ps`, `… logs`.

Compose never builds: the backend service in `compose.gen.yml` names its image as `${SCHEMAF_IMAGE}`, which `schemaf.sh` sets. A manual `podman compose up` without it fails with `set by schemaf.sh — use ./schemaf.sh run`, and an image that isn't there is an error, never a build or a pull.

### Separate build and run users

`build` runs third-party code (`npm install` and its install scripts); the running stack owns the database. A deployment that holds secrets in its database keeps those two apart: one unprivileged user builds and can reach neither the containers nor the database volume, another runs the stack and never builds. With rootless podman each user has its own container store, so the image crosses over as a file.

The example uses the project `myapp`, the build user `myapp-build` and the run user `myapp`:

```
myapp-build (repo + runtime, no secrets)       myapp (runtime + ~/.myapp/etc/env, no repo, no Go/Node)
────────────────────────────────────────       ─────────────────────────────────────────────────────
git pull
./schemaf.sh build --output /srv/drop/myapp-3f9a2c1.run
        │  (file handover, e.g. a drop directory — schemaf doesn't manage it)
        └──────────────────────────────────►  /srv/drop/myapp-3f9a2c1.run load
                                              ~/.myapp/bin/schemaf.sh run --image 3f9a2c1 -d
                                              … later: run --image <older tag> -d   (rollback)
```

**1. The build user** has git, the container runtime and a clone of the repository, nothing else:

```bash
git clone <repo> && cd myapp
./schemaf.sh build --output /srv/drop/myapp-$(git rev-parse --short HEAD).run
```

**2. The handover** is a plain file. Give the run user read access to it, e.g. a directory both users can reach (`/srv/drop`, group-readable). schemaf doesn't move, sign or delete artifacts.

**3. The run user** has the container runtime and the env file (see [What the run host provides](#what-the-run-host-provides)), no repository, no Go, no Node:

```bash
/srv/drop/myapp-3f9a2c1.run load            # prints: 3f9a2c1
~/.myapp/bin/schemaf.sh run --image 3f9a2c1 -d
```

The first start creates the database volume.

**Upgrade**: build and hand over the new commit, `load` it, `run --image <new tag> -d`. Compose replaces the running containers; the database volume stays, and the new version's migrations run when it starts.

**Rollback**: `~/.myapp/bin/schemaf.sh run --image <older tag> -d`. Migrations are forward-only, so an older version runs against the newer schema; keep schema changes backward-compatible if you rely on rollback.

**Pruning**: `~/.myapp/bin/schemaf.sh image ls`, then `image rm <tag>` for versions you no longer want to roll back to. The running tag can't be removed.

### What the run host provides

The artifact is self-contained except for:

| Provided by the run host | Examples |
|---|---|
| `~/.<name>/etc/env` | `DB_PASS`, `PUBLIC_*` (incl. `PUBLIC_AUTH0_*`), `BACKUP_*`, `SCHEMAF_RUNTIME` |
| Files the env refers to | the backup SSH key (`BACKUP_SSH_KEY_PATH`), if backups are set up |
| Container runtime | podman (the default) or Docker, see [Container runtime](#container-runtime) |
| Database volume | `<name>-postgres-data`, created by the runtime on first start, kept across versions |

## Container runtime

schemaf runs its containers with **rootless podman** by default. Docker's daemon runs as root, so anything that can talk to it is root-equivalent on the machine; rootless podman runs containers as your own user. That is the safer default for running other people's images next to the database that holds your users' secrets.

**Choosing the runtime.** The runtime is a property of the machine, not of the project, so it is an environment variable and not a `schemaf.toml` field:

| `SCHEMAF_RUNTIME` | Runtime |
|---|---|
| unset | podman (the default) |
| `podman` | podman (the default, spelled out) |
| `docker` | Docker |

Any other value stops schemaf with an error listing the valid values. Upgrading from a schemaf release that used Docker: a machine that has only Docker must set `SCHEMAF_RUNTIME=docker`; a server moving to podman with existing data follows [the migration steps](#migrating-from-docker-to-podman). Set it in your shell profile, or in the project's env files (`~/.<name>/etc/env` for `run` and the run side, `~/.<name>/dev/etc/env` for everything else), which `schemaf.sh` loads on every invocation (`build` reads only this one key from them, see [Secrets](#secrets)):

```bash
echo "SCHEMAF_RUNTIME=docker" >> ~/.myapp/etc/env
```

**Compose** is always `<runtime> compose`. Under podman, `podman compose` must hand off to **docker-compose** (the reference Compose v2), never `podman-compose`: schemaf relies on `up --wait`, `depends_on: condition: service_healthy`, long-form `env_file` with `required: false`, and file `secrets`. Both runtimes therefore use the same compose implementation on the same generated compose files; only the engine underneath differs.

**Missing tools fail loudly.** Before `dev`, `build`, `run`, `load` and `test` touch a container, schemaf checks the runtime and stops with a message naming what's missing and how to fix it. It never falls back to the other runtime and never guesses:

| Situation | What schemaf tells you |
|---|---|
| podman not installed | install podman, or set `SCHEMAF_RUNTIME=docker` |
| `podman compose` uses `podman-compose`, or no provider at all | install docker-compose |
| Linux: podman's API socket isn't running (docker-compose can't reach podman) | `systemctl --user enable --now podman.socket`, or point `DOCKER_HOST` at podman's socket |
| `DOCKER_HOST` is set but empty (podman compose passes it on, so docker-compose would use Docker's default socket) | unset it, or point it at podman's socket |
| macOS: podman machine not running | `podman machine start` (and `podman machine init` first if no machine exists) |
| Docker selected but not installed | install Docker, or unset `SCHEMAF_RUNTIME` to use podman |
| Docker selected but the daemon is not running | start Docker (Docker Desktop, or `sudo systemctl start docker`) |
| Docker selected but `docker compose` missing | install the Docker Compose plugin |

**The data volume belongs to one runtime.** The project's Postgres data lives in a named volume, `<name>-postgres-data`. podman and Docker each keep their own volumes, so the same name can exist in both, holding different data. schemaf does not accept that ambiguity: when `dev`, `run` or `test` starts, it asks the *other* runtime whether it has `<name>-postgres-data`. If it does, schemaf refuses to start, whether or not the selected runtime has one too. The other runtime is only asked when its CLI is installed and usable; if it isn't, the check is skipped.

The error names your project's real volume, says so when the selected runtime has one as well (then it is unclear which holds the real data), and gives three ways out:

1. **Migrate** the data to the selected runtime: the error lists the exact `pg_dump` / `psql` commands with your project's names filled in; step by step, see [Migrating from Docker to podman](#migrating-from-docker-to-podman).
2. **Or** switch `SCHEMAF_RUNTIME` to the runtime that has the data.
3. **Or**, if the data is throwaway, delete the other runtime's volume: `<other> volume rm <name>-postgres-data`.

schemaf never migrates or deletes data itself.

**Rootless works unchanged.** The generated `compose.gen.yml`, `compose.dev.gen.yml` and `compose.test.gen.yml` run under rootless podman exactly as generated: healthchecks, `depends_on` conditions, `env_file`, the backup SSH key as a secret, named volumes and published ports. There are no podman-specific variants.

## Migrating from Docker to podman

A machine whose project data sits in a Docker volume can't start under podman until that volume is gone (see [the data volume](#container-runtime)). Move the data with a logical dump: `pg_dump` out of the old Postgres, `psql` into the new one. Never copy the volume's files: rootless podman maps user IDs differently, and a copied Postgres data directory fails with wrong ownership.

It is a move, not a copy: the last step deletes the Docker volume. The steps are the same on a production server and on a dev machine (dev and production share the one volume); they use the project `myapp` with the production env file, so for dev use `~/.myapp/dev/etc/env` instead. On a run host without a checkout, run them in `~/.myapp/deploy/<tag>/`, which holds the same `compose.gen.yml`, and start with `~/.myapp/bin/schemaf.sh run --image <tag>` in step 7.

```bash
cd myapp                                   # the project root, next to compose.gen.yml
set -a; . ~/.myapp/etc/env; set +a         # DB_PASS etc. for compose variable substitution
export SCHEMAF_IMAGE=myapp:unused           # compose.gen.yml requires it; only postgres starts here

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

Files ending in `.dev.yml` are only included in the dev and test compose files — not in production. Regular `*.yml` files are included everywhere. The test compose file (`compose.test.gen.yml`) holds only these project services, no Postgres: tests get their databases from the [test harness](EXTEND.md#integration-tests-schemaftest).

Codegen merges the framework's built-in compose with your extensions and produces `compose.gen.yml` (prod) and `compose.dev.gen.yml` (dev) at the project root.

**Running the stack:**

```bash
./schemaf.sh run              # Build, then full stack, production mode (see Deployment)
./schemaf.sh dev all          # Dev: Postgres + project services in containers, backend and Vite on the host
./schemaf.sh dev infrastructure   # Only the containers (Postgres + project services)
```

`dev` takes one comma-separated list of targets: `db` (Postgres), `infrastructure` (Postgres and the project's compose services), `backend` (the Go server, run natively; implies `infrastructure`), `frontend` (Vite) and `all`. The backend never runs in a container in dev. To run the Go server yourself (e.g. under a debugger), start `./schemaf.sh dev infrastructure` and launch the server from your IDE.

## Parallel dev instances

The dev stack's ports are fixed, so only one checkout of a project can run `./schemaf.sh dev` at a time. An **instance** is a second dev stack with its own ports, database and session cookie. Several checkouts of the same project, e.g. one git worktree per issue, run their dev stacks side by side:

```bash
# the main checkout: the default instance, as always
./schemaf.sh dev all

# a worktree for issue 349, at the same time
cd ../myapp-349
./schemaf.sh dev all --instance 349
```

**Names.** An instance is named with a whole number ≥ 1, usually the issue number. Without a number, commands use the `default` instance, which is exactly the dev stack described everywhere else. schemaf never picks a name for you.

**The name sticks to the checkout.** `dev --instance 349` writes `349` to `.schemaf/instance` in the checkout (`.schemaf/` carries its own `.gitignore`, so you don't edit yours). From then on every command in that checkout — `dev`, `db …`, `auth token`, `instances` and your own subcommands — uses instance 349 without the flag, and prints `instance 349` as the first line after the `schemaf.sh` banner.

- An explicit `--instance` that differs from `.schemaf/instance` fails. schemaf never guesses which one you meant.
- `dev --instance default` switches the checkout back to the default instance and removes the file.
- Only `dev` writes or removes the file. Other commands accept `--instance` for that one call, in a checkout without the file, e.g. `db restore --instance 349` from the main checkout.
- `--instance` is dev-only. Production (`build`, `run`, the run side, the server in its container) ignores `.schemaf/instance` and rejects the flag.

**Ports.** Instance `n` gets the base port `port + 10 × n`, with the usual offsets:

| | default (`port = 7100`) | instance 349 |
|---|---|---|
| backend | 7100 | 10590 |
| Vite | 7102 | 10592 |
| Postgres | 7103 | 10593 |

`dev` prints the ports when it starts, and `instances` shows them. An instance's backend always listens on its base port, even when `PORT` is set (e.g. in the shared `~/.<name>/dev/etc/env`). A number whose base port would exceed 65525 is refused, and a port that is already taken stops `dev` as it always does. Vite gets its port on the command line (`npm run dev -- --port <base+2> --strictPort`), which overrides the port in `vite.config.ts`. Project services from `compose/*.yml` keep their ports in every instance; if one collides with another instance, `dev` stops with the usual "port in use" error. Please report it if that happens to you.

**What is separate, and what is shared.** For a project named `myapp`:

| | default | instance 349 |
|---|---|---|
| compose project | `myapp` | `myapp-349` |
| containers | `myapp-postgres`, `myapp-backend` | `myapp-349-postgres`, `myapp-349-backend` |
| network | `myapp-net` | `myapp-349-net` |
| Postgres volume | `myapp-postgres-data` | `myapp-349-postgres-data` |
| data directory | `~/.myapp/dev/var/` | `~/.myapp/dev/instances/349/var/` |
| session cookie | `myapp_session` | `myapp-349_session` |

Every instance shares the secrets in `~/.myapp/dev/etc/env`. The database inside each volume is named `myapp` in every instance. Only the default instance stops a running production stack when `dev` starts, because only it shares a volume and ports with production; instances leave production alone.

`dev` records each instance in `~/.myapp/dev/instances/<n>/instance.toml`: its ports and the absolute path of its checkout. An instance belongs to one checkout: `dev --instance 349` fails while the recorded checkout still claims 349 in its `.schemaf/instance` (pick another number, or remove it with `instances rm 349`). If the recorded checkout is gone (e.g. after `git worktree move`) or has switched to another instance, `dev` takes the instance over, says so, and updates the record.

**`instances`** lists every instance of the project on this machine, `default` included:

```
$ ./schemaf.sh instances
INSTANCE  RUNNING  BACKEND  VITE   POSTGRES  CHECKOUT
default   yes      7100     7102   7103      -
349       yes      10590    10592  10593     /home/me/myapp-349
```

The ports are calculated from the current `port` in `schemaf.toml`. If it changed since an instance last ran, the row also shows the recorded ports, and the instance's next `dev` updates the record. `RUNNING` says whether the instance's containers are up; for the default instance it says `production` when the production stack runs instead (they share the compose project). The default instance has no recorded checkout, since any checkout without `.schemaf/instance` uses it.

**`instances rm <n>`** stops the instance's containers and deletes its Postgres volume, its data directory and its record. It asks for confirmation unless you pass `-y`, and refuses `default`. There is no `instances stop`: as always, Ctrl-C stops a dev stack's backend and Vite. It doesn't touch the checkout, so a checkout whose `.schemaf/instance` still names the removed instance gets a fresh one on its next `dev`.

**A new instance starts with an empty database**, and its backend runs migrations as usual. To start from the default instance's data, restore a backup into it before its backend first starts:

```bash
# in the worktree: create instance 349 with Postgres only, so its database stays empty
./schemaf.sh dev db --instance 349

# in the main checkout: copy the default instance's data into instance 349
./schemaf.sh db backup --local /tmp/myapp.sql.gz
./schemaf.sh db restore --local /tmp/myapp.sql.gz --instance 349
rm /tmp/myapp.sql.gz

# in the worktree: start the rest; the backend applies only the branch's new migrations
./schemaf.sh dev all
```

**Auth0.** The browser reaches an instance at `http://localhost:<base>`, so each instance's origin must be in the Auth0 application's **Allowed Callback URLs**, **Allowed Logout URLs** and **Allowed Web Origins** (see [Activating Auth0](EXTEND.md#activating-auth0)), e.g. `http://localhost:10590` for instance 349 with `port = 7100`. Add it once: an instance's ports never change.
