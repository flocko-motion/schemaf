# Extending Your Project

Back to [README](README.md) | See also: [Installation](INSTALL.md)

## Contents

- [App Wiring (`go/app/app.go`)](#app-wiring-goappappgo)
- [Frontend](#frontend)
  - [Runtime config: `PUBLIC_*`](#runtime-config-public_)
- [Built-in Endpoints](#built-in-endpoints)
- [Routing](#routing)
- [Development mode](#development-mode)
- [Endpoint Interface](#endpoint-interface)
  - [Auth levels](#auth-levels)
  - [Error responses](#error-responses)
  - [Request headers](#request-headers)
  - [Content types](#content-types)
- [Authentication](#authentication)
  - [Identity providers](#identity-providers)
  - [Activating Auth0](#activating-auth0)
  - [Sessions](#sessions)
  - [Per-request hook: `OnRequest`](#per-request-hook-onrequest)
- [Updates](#updates) — the app shows new releases and requests an upgrade (`/api/updates`)
- [Stream Endpoints](#stream-endpoints)
- [Raw Endpoints](#raw-endpoints)
- [Database](#database)
  - [Migrations](#migrations)
  - [Migration identifiers](#migration-identifiers-independent-migration-lines)
  - [Applying migrations to another database](#applying-migrations-to-another-database)
  - [Queries](#queries)
- [CLI Subcommands](#cli-subcommands)
  - [Before-serve hook: `OnBeforeServe`](#before-serve-hook-onbeforeserve)
- [Code Generation](#code-generation)
  - [Empty inputs](#empty-inputs)
- [Testing](#testing)
  - [Integration tests (`schemaftest`)](#integration-tests-schemaftest)
  - [TypeScript tests](#typescript-tests)
- [Database Backups](#database-backups)

## App Wiring (`go/app/app.go`)

Your app's wiring lives in one function, `Wire` in `go/app/app.go` (package `app`). It registers providers, generated and custom, and hooks. `main.go` and the [integration tests](#integration-tests-schemaftest) both call it, so the tests boot exactly the app that production runs:

```go
// go/app/app.go
package app

import (
    "github.com/flocko-motion/schemaf/schemaf"

    "myapp/api"
    "myapp/db"
    "myapp/importer"
)

// Wire registers the app's providers, hooks and services.
func Wire(app *schemaf.App) {
    app.AddDb(db.Provider)                        // generated: migrations + queries
    app.AddApi(api.Provider)                       // generated: endpoint registration
    app.SetFrontend(FrontendFS())                  // generated: embedded frontend assets (go/app/frontend.gen.go)
    app.AddSubcommand(importer.SubcommandProvider) // custom: CLI commands
    // optional: app.OnLogin(...), app.AddIdentityProvider(...) — see Authentication
    // optional: app.OnRequest(...) — see Per-request hook
    // optional: app.CanSeeErrorDetails(...) — see Error details
    // optional: app.SetUpdatesAuth(...) — see Updates
    // optional: app.OnBeforeServe(...) — see Before-serve hook
    // optional: app.AddService(...) — see Services vs. subcommands
}
```

Every `Add*`, `On*` and `Set*` call goes into `Wire`. `go/main.go` only creates the app, wires it and runs it:

```go
// go/main.go
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

`a.Run()` hands over to Cobra. The `server` command (default) starts the HTTP server. Custom subcommands are available alongside it. Name the variable anything but `app`, so it doesn't shadow the package.

Codegen writes two files into `go/app/` next to `app.go`: `constants.gen.go` (the project name and port from `schemaf.toml`) and `frontend.gen.go` (`FrontendFS()`, see [Frontend › Wiring](#wiring)). Because they live in package `app`, a test binary that imports `app` gets them too.

#### Upgrading: `main.go` → `go/app/app.go`

Codegen stops while `go/app/app.go` doesn't define `func Wire(app *schemaf.App)`:

```
go/app/app.go doesn't exist: move your wiring from go/main.go into func Wire in go/app/app.go (EXTEND.md › App Wiring)
```

Move every `app.Add*`/`On*`/`Set*` call from `go/main.go` into `Wire`, and shrink `main.go` to the three lines above. Then run `./schemaf.sh codegen`: it writes `go/app/constants.gen.go` and `go/app/frontend.gen.go`, and removes the old generated `go/constants.gen.go`, `go/frontend.gen.go` and `go/frontend_dist/.gitkeep`.

## Frontend

### Stack & conventions

Schemaf requires a **Vite + TypeScript** frontend built with **npm**. Which UI framework you use on top is up to you: React, Vue, Svelte or none. If `frontend/` doesn't exist when you run codegen, it's scaffolded automatically with a minimal working setup, using **React** as the default. If it exists, codegen validates that the required files are present.

**Normative decisions (framework-enforced):**
- Vite as build tool and dev server, configured in `vite.config.ts`
- TypeScript
- Port derived from schemaf.toml (default 8002) with `strictPort: true`
- Entry point: `index.html` → `src/main.tsx` or `src/main.ts`
- npm as package manager

Required files: `frontend/package.json`, `frontend/vite.config.ts`, `frontend/index.html`, and one entry file, `frontend/src/main.tsx` or `frontend/src/main.ts`.

**Project decisions (up to you):**
- UI framework (React — the scaffold default —, Vue, Svelte, ...)
- UI component library (MUI, Tailwind, shadcn, etc.)
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

Codegen generates `go/app/frontend.gen.go` with `FrontendFS()` returning the embedded assets. Wire it in `Wire` (`go/app/app.go`):

```go
app.SetFrontend(FrontendFS())
```

### Codegen

`./schemaf.sh codegen` does two things for the frontend:

1. **Scaffold** — if `frontend/` doesn't exist, creates a Vite + TypeScript setup with React (the default) and runs `npm install`, which writes `frontend/package-lock.json`. Commit it: `build` installs exactly what it pins (see [Lockfiles](INSTALL.md#lockfiles))
2. **Generate** — creates `go/app/frontend.gen.go` (embeds `frontend/dist/` into the Go binary), `frontend/src/api/generated/api.gen.ts` (type-safe API client from OpenAPI spec) and `frontend/src/api/generated/config.gen.ts` ([runtime config](#runtime-config-public_))

If `frontend/` exists, codegen validates it: the required files must be present, and `vite.config.ts`, `index.html` and the entry file (`src/main.tsx` or `src/main.ts`) must contain the [runtime config](#runtime-config-public_) setup. If something is missing, codegen stops and prints what to add.

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
./schemaf.sh dev all --instance 349       # everything, as a second stack next to the default one
```

`--instance` runs a separate dev stack with its own ports, database and session cookie, e.g. in a git worktree; see [Parallel dev instances](INSTALL.md#parallel-dev-instances).

In [development mode](#development-mode), the Go server reverse-proxies all non-API requests to the Vite dev server.

### Production

The generated `Dockerfile.gen` (built by `./schemaf.sh build`, see [Deployment](INSTALL.md#deployment)) includes a Node build stage that installs exactly what `frontend/package-lock.json` pins (`npm ci`) and compiles the frontend, then embeds the output into the Go binary via `//go:embed`. No separate frontend container needed.

### Runtime config: `PUBLIC_*`

The frontend is built once and the same build is deployed to many instances, so settings that differ per instance (an identity provider's client settings, a Sentry DSN, the public URL, ...) can't be baked in at build time. The server hands them to the browser at runtime instead.

**Declaring a value:** every environment variable whose name starts with `PUBLIC_` is public frontend config. There is nothing to register. Put the variables into the instance's env file (see [Secrets](INSTALL.md#secrets)), next to `DB_PASS`:

```bash
# ~/.myapp/dev/etc/env
DB_PASS=changeme
PUBLIC_SENTRY_DSN=https://abc123@o0.ingest.sentry.io/0
```

- Every `PUBLIC_*` value is sent to every browser that opens the app. **Never put a secret in a `PUBLIC_` variable.**
- Variables without the prefix are never exposed.
- Dev: `schemaf.sh` loads `~/.<name>/dev/etc/env` and the backend runs natively, so it sees the variables directly. Prod: the generated `compose.gen.yml` passes the whole `~/.<name>/etc/env` to the backend container (`env_file`).
- The server reads the variables once at startup. After changing a value, restart the server.

**Serving:** `GET /api/config` returns all `PUBLIC_*` variables as a flat JSON object, keyed by their full name:

```json
{ "PUBLIC_SENTRY_DSN": "https://abc123@o0.ingest.sentry.io/0", "PUBLIC_AUTH0_DOMAIN": "myapp.eu.auth0.com" }
```

- Open (no auth), sent with `Cache-Control: no-cache`.
- A variable that is set but empty is included as `""`. A variable that isn't set is absent.
- `/api/config` is in `openapi.json`, like every route under `/api/` (see [Routing](#routing)), so the generated API clients have it too. In the frontend, read it through `config()`.

**Reading a value in the frontend:** import `config` from the generated `config.gen.ts`:

```ts
import { config } from "./api/generated/config.gen";

const dsn = config("PUBLIC_SENTRY_DSN"); // string | undefined
```

- `config.gen.ts` loads `/api/config` with a top-level `await` the first time it is imported. Every module that imports it waits for the load, so `config()` always returns the loaded value. There is nothing to call before using it.
- Keys are plain strings: which variables exist depends on the deployment, not on the code. An unset variable returns `undefined`.
- The scaffolded `src/main.tsx` imports `config.gen.ts` first, so the config is loaded before the app renders.
- If the load fails (backend down, non-2xx response, invalid JSON), the browser shows the alert `Could not load app configuration` and the app does not start.

**Browser support:** the top-level `await` needs `build.target: "es2022"` in `vite.config.ts` (Vite's default target is older). Supported browsers: Chrome/Edge 89+, Firefox 89+, Safari 15+ (all released in 2021). An older browser can't run the app at all. For that case the scaffolded `index.html` contains a small inline script: if the app hasn't started once the page has loaded, it shows the alert `This browser is too old to run this app. Please update it.` It stays quiet when the config load failed, which already showed its own alert.

**Existing projects:** frontends scaffolded before runtime config need three additions. Codegen checks all three and stops with a message listing what is missing.

1. `frontend/vite.config.ts`: add the build target.
   ```ts
   export default defineConfig({
     // ...
     build: {
       target: "es2022",
     },
   });
   ```
2. `frontend/index.html`: add this script to `<head>`, unchanged:
   ```html
   <script>
     // schemaf: old-browser check (see EXTEND.md, Runtime config)
     window.addEventListener("load", function () {
       if (!window.__schemaf_boot) alert("This browser is too old to run this app. Please update it.");
     });
   </script>
   ```
3. `frontend/src/main.tsx` (or `src/main.ts`): add this as the first import, so the config loads before the app renders and the old-browser check sees the app start:
   ```ts
   import "./api/generated/config.gen";
   ```

## Built-in Endpoints

The framework provides these endpoints out of the box:

- `/api/health` — health check (returns `{"status": "ok", "db": "ok", ...}`)
- `/api/status` — service status ([mode](#development-mode), uptime, backup status, active [identity providers](#active-providers-apistatus), custom providers)
- `/api/config` — the instance's public frontend config (see [Runtime config](#runtime-config-public_))
- `/api/user/me` — current user info (requires auth)
- `/api/auth/{provider}/login`, `/api/auth/logout`, `/api/auth/refresh` — login and sessions (see [Authentication](#authentication))
- `/api/errors/{id}` — the details of a 5xx by its `id` (see [Error details](#error-details))
- `/api/updates`, `/api/updates/request`, `/api/updates/check` — the instance's updates, the request to install one and the request to check for new releases now, closed until the app authorizes someone (see [Updates](#updates))
- `/openapi.json` — OpenAPI 3.0 spec: exactly `gen/openapi.json`, compiled into the binary by codegen (see [Code Generation](#code-generation))

All of them except `/openapi.json` are part of `openapi.json` (`GET /api/health`, `GET /api/status`, `GET /api/config`, `GET /api/user/me`, `GET /api/errors/{id}`, `GET /api/updates`, `POST /api/updates/request`, `POST /api/updates/check` and the auth routes), so the generated TypeScript and Go clients can call them with types, and the [route gate](#routing) lets them through. `/openapi.json` is the one route outside `/api/`.

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
  "mode": "production",
  "uptime": "2h30m",
  "backup": { "last": "...", "ago": "..." },
  "auth": { "providers": [] },
  "s3": { "bucket": "my-bucket", "connected": true }
}
```

## Routing

**`openapi.json` is the truth, and the server enforces it.** The server reads the spec it serves (`gen/openapi.json`, handed over by the generated `Provider()`) at startup. Every request to `/api` or under `/api/` is checked against the spec's paths and methods before any handler or auth runs:

| Request | Response |
|---|---|
| The path isn't in the spec | `404` `{"error": "not found", "code": "not_found"}`, for every method |
| The path is in the spec, the method isn't | `405` `{"error": "method not allowed", "code": "method_not_allowed"}`, with an `Allow` header listing the spec's methods for that path (`HEAD` is allowed wherever `GET` is) |
| Path and method are in the spec | the endpoint's handler (auth, decoding, ...) |

- A path matches a spec path when every segment matches; a `{param}` segment matches any non-empty segment. `/api/todos/42` matches `/api/todos/{id}`; `/api/todos/` and `/api/todos/42/` don't.
- The check runs before auth, so an unknown path is a `404` even without credentials, never a `401`.
- A client calling a route that doesn't exist (a typo, a renamed endpoint, a client generated against an older spec) gets a JSON error, never the frontend's HTML with `200`.

**Endpoints live under `/api/`.** `Path()` must start with `/api/`. Codegen stops on any other path and names the endpoint:

```
endpoint StripeWebhookEndpoint: Path() "/webhooks/stripe" must be under /api/
```

An endpoint on one of schemaf's own routes (e.g. `GET /api/status`) is a codegen error too, as are two endpoints on the same method and path.

**One `{param}` per segment.** Go's multi-segment wildcard `{name...}` and its end anchor `{$}` can't be expressed in OpenAPI, where a parameter is always exactly one segment, so the spec couldn't describe the route truthfully. Codegen rejects them in `Path()`, and the startup check rejects them in hand-registered routes:

```
endpoint BlobEndpoint: Path() "/api/blob/{path...}" uses {path...}, which OpenAPI can't express — use one {param} per segment
```

**The server checks the spec against the handlers at startup.** It refuses to start, with an error that names every mismatch, if:

- a spec operation has no handler;
- a handler (an endpoint, or one of schemaf's own routes) has no spec operation, so the gate would make it unreachable;
- a handler is registered outside `/api/` (the only exception is schemaf's `GET /openapi.json`);
- two handlers serve the same method and path;
- a handler's path uses `{name...}` or `{$}`;
- no spec is set at all: wire the generated provider in `Wire` (`go/app/app.go`), `app.AddApi(api.Provider)`;
- a spec path item is a `$ref`, or declares no operation. Codegen never writes either, so the spec was edited by hand or is broken.

Codegen builds the spec and the handler registration from the same endpoint structs, so these only fire when something was registered by hand or the generated files are out of date. `api.NewMux()` (for tests with `httptest`) panics with the same error, as `http.ServeMux` does on conflicting patterns; the server returns it from `app.Run`.

**Frontend space is `GET` and `HEAD` only.** Every path outside `/api/` and `/openapi.json` belongs to the frontend: the dev server in [development mode](#development-mode), the embedded build with its SPA fallback (unknown paths get `index.html`) in production. Only `GET` and `HEAD` reach it; any other method gets `405` `{"error": "method not allowed", "code": "method_not_allowed"}` with `Allow: GET, HEAD`. The same goes for `POST /openapi.json`.

#### Upgrading: route gate

- Run `./schemaf.sh codegen` after upgrading. A `gen/openapi.json` from an older schemaf lacks `/api/health`, `/api/status`, `/api/config` and `/api/user/me`, so the server refuses to start with `GET /api/health: handler has no operation in the OpenAPI spec` and similar lines.
- Move endpoints with a `Path()` outside `/api/` under `/api/`; codegen names each one.
- A server without `app.AddApi(api.Provider)` no longer starts. Before, it started and only `/openapi.json` failed.
- Clients relying on a wrong method or an unknown `/api/` path reaching the frontend (with `200` and HTML) now get `404` or `405` with a JSON error.

## Development mode

schemaf tells your app whether the server runs in development. Development mode is opt-in: only the commands that use the dev env file turn it on, and every other way of starting the server runs in production.

```go
import schemafapi "github.com/flocko-motion/schemaf/api"

if schemafapi.DevMode() {
    // dev-only endpoints, mock services, preseed
}
```

Gate dev-only behaviour on `DevMode()` — endpoints that create accounts without sign-in, mock AI platforms, database preseeds — instead of inferring the mode from what is at hand.

| Started by | Mode |
|---|---|
| `./schemaf.sh dev` | development |
| `./schemaf.sh auth token`, your own subcommands through `./schemaf.sh` | development |
| `./schemaf.sh run`, the run side (`~/.<name>/bin/schemaf.sh run --image <tag>`) | production |
| the generated `compose.gen.yml`, a built image | production |
| a bare `./myapp …` | production |

- **One rule**: development mode and the dev env file go together. `schemaf.sh` sets `SCHEMAF_DEV=1` for every command that loads `~/.<name>/dev/etc/env` — everything except `build` and `run` (see [Secrets](INSTALL.md#secrets)).
- **Read-only**: `DevMode()` only reads `SCHEMAF_DEV`; there is no setter.
- **Strict values**: `SCHEMAF_DEV=1` is development; unset or empty is production. Any other value (`true`, `yes`, `0`, …) stops the app at startup with an error naming the variable and the accepted values.
- **Not in env files**: an env file that sets `SCHEMAF_DEV`, to any value, makes `schemaf.sh` stop with an error naming the file, before it starts anything — for every command that reads that file, `build` included. Remove the line; the mode is never silently overridden.
- **Production stays production**: `schemaf.sh build`, `run` and the run side unset a `SCHEMAF_DEV` exported by your shell, and the generated `compose.gen.yml` pins `SCHEMAF_DEV: ""` on the backend, which beats its `env_file`.
- **Reported**: `/api/status` reports `"mode": "development"` or `"mode": "production"`, and the server logs `"msg":"server mode","mode":"…"` at startup.

schemaf's own dev behaviour follows the same mode:

- **Frontend**: proxied to the Vite dev server in development, served from the embedded build otherwise (see [Frontend](#frontend)).
- **Directories**: config and data live in `~/.<name>/dev/etc/` and `~/.<name>/dev/var/` in development, `~/.<name>/etc/` and `~/.<name>/var/` otherwise. A [parallel dev instance](INSTALL.md#parallel-dev-instances) `n` keeps its data in `~/.<name>/dev/instances/<n>/var/` and shares `~/.<name>/dev/etc/`.
- **Session cookie**: `Secure` everywhere except development (see [Sessions](#sessions)).
- **`auth token`**: refuses to run outside development (see [Getting a token in development](#getting-a-token-in-development)).

`SCHEMAF_ENV=docker`, set by the generated compose, means only "running inside the generated compose". schemaf uses it for one thing: the database host (`postgres` inside compose, `127.0.0.1:<port+3>` natively). It says nothing about the mode.

#### Upgrading: development mode

Before v1.15.0, schemaf treated every process outside the generated compose (`SCHEMAF_ENV` not `docker`) as development. Now:

- Replace app checks such as `os.Getenv("SCHEMAF_ENV") != "docker"` or your own `DEV_MODE` variable with `api.DevMode()`.
- A bare native `./myapp server` now runs in production: it serves the embedded frontend instead of proxying to Vite, uses `~/.<name>/etc/` and `~/.<name>/var/`, sets `Secure` on the session cookie, and `./myapp auth token` refuses. Start it through `./schemaf.sh` to develop.

## Endpoint Interface

API endpoints are structs implementing a typed interface — not plain `http.HandlerFunc` functions. This gives the framework enough information to handle serialization, auth, and OpenAPI generation automatically.

The interface (defined in `api/endpoint.go`):

```go
type Endpoint[Req, Resp any] interface {
    Method() string                                    // HTTP method: "GET", "POST", "PUT", "DELETE"
    Path() string                                      // Route path, e.g. "/api/todos/{id}"
    Auth() api.AuthLevel                               // AuthNone, AuthOptional or AuthRequired
    Handle(ctx context.Context, req Req) (Resp, error) // Your business logic
}
```

Example implementation:

```go
// go/api/users.go
type GetUserEndpoint struct{}

func (e GetUserEndpoint) Method() string { return "GET" }
func (e GetUserEndpoint) Path()   string { return "/api/users/{id}" }
func (e GetUserEndpoint) Auth() api.AuthLevel { return api.AuthRequired }

func (e GetUserEndpoint) Handle(ctx context.Context, req GetUserReq) (GetUserResp, error) {
    user, err := db.GetUser(ctx, req.ID)
    return GetUserResp{User: user}, err
}

type GetUserReq struct {
    ID int64 `path:"id"` // /api/users/42 → 42; /api/users/abc → 400
}

type GetUserResp struct {
    User db.User `json:"user"`
}
```

Each endpoint struct has four methods:
- `Method()` — HTTP method (GET, POST, PUT, DELETE)
- `Path()` — route with `{param}` placeholders; always under `/api/` (see [Routing](#routing))
- `Auth()` — the endpoint's [auth level](#auth-levels)
- `Handle(ctx, req) (resp, error)` — your business logic

**What the framework does for you:**
- Decodes the request (JSON body — or another [content type](#content-types) — then [path and query params](#path-and-query-parameters)) into the request type
- Authenticates the caller according to `Auth()`
- Calls `Handle(ctx, req)`; `ctx` carries the caller ([`api.Subject`](#auth-levels)) and the [request headers](#request-headers), and can [start a session](#starting-sessions-from-your-own-code)
- Encodes the response as JSON (or another [content type](#content-types))
- On error: answers with the status and error body derived from the error (see [Error responses](#error-responses))

### Auth levels

`Auth()` returns one of three levels:

```go
// package api
type AuthLevel int

const (
    AuthNone     AuthLevel = iota // credentials are never looked at
    AuthOptional                  // the subject if credentials are present, anonymous if not
    AuthRequired                  // 401 without valid credentials
)
```

| Level | No credentials | Valid credentials | Invalid credentials |
|---|---|---|---|
| `AuthNone` | handler runs | ignored, handler runs anonymous | ignored, handler runs anonymous |
| `AuthOptional` | handler runs; `api.Subject` → `("", false)` | as `AuthRequired` | **401** + cookie cleared |
| `AuthRequired` | 401 | session check, cookie renewal, `Origin` check | **401** + cookie cleared |

- **Valid** credentials are a token `AuthRequired` accepts: a valid Bearer token, or a session cookie, including an expired cookie token for a live session (renewed, see [Sessions](#sessions)).
- **Invalid** credentials are present but rejected: bad signature, revoked or ended session, expired Bearer token, revoked subject. A signed-in caller is never silently downgraded to anonymous.
- When invalid credentials came from the cookie, the 401 also clears the cookie, so the browser stops sending it and the next request to an optional endpoint is anonymous. A rejected Bearer token leaves the cookie alone.
- The [CSRF `Origin` check](#sessions) applies to cookie-authenticated requests at `AuthOptional` exactly as at `AuthRequired`.
- At every level, an [`OnRequest`](#per-request-hook-onrequest) hook runs after authentication and before the handler; its admission reports the caller as `api.Subject` does in the handler.

Use `AuthOptional` for endpoints that serve signed-in users and guests alike, such as a shared or public page:

```go
func (e GetShareEndpoint) Auth() api.AuthLevel { return api.AuthOptional }

func (e GetShareEndpoint) Handle(ctx context.Context, req GetShareReq) (GetShareResp, error) {
    if sub, ok := api.Subject(ctx); ok {
        return shares.GetForUser(ctx, req.ID, sub)
    }
    return shares.GetPublic(ctx, req.ID)
}
```

In `openapi.json`, `AuthRequired` endpoints declare `security: [{bearerAuth: []}, {cookieAuth: []}]` and `AuthOptional` endpoints `security: [{}, {bearerAuth: []}, {cookieAuth: []}]` (anonymous allowed). `AuthNone` endpoints declare no security. Every operation's error responses, `401` included, are its `default` response (see [Error responses](#error-responses)).

Codegen is strict: an endpoint whose `Auth()` does not return one of the three constants, whose `Method()` or `Path()` does not return a string literal, whose `Handle` does not have the `(ctx, Req) (Resp, error)` shape, that has more than one of `Handle`, `HandleRaw` and [`Stream`](#stream-endpoints), or that has `Method()`, `Path()` and `Auth()` but none of them (e.g. a misnamed `Handler`) is a codegen error, never silently skipped.

#### Upgrading: `Auth() bool` → `api.AuthLevel`

Before v1.11.0, `Auth()` returned `bool`. Codegen rejects the old signature and names the endpoint. Replace:

- `Auth() bool { return true }` → `Auth() api.AuthLevel { return api.AuthRequired }`
- `Auth() bool { return false }` → `Auth() api.AuthLevel { return api.AuthNone }`

**Error responses:** return an error to answer with an error status and the shared error body; see [Error responses](#error-responses).

**Request type struct tags:**
```go
type ExampleReq struct {
    ID     int     `path:"id"`      // from URL path: /api/things/{id}
    DryRun *bool   `query:"dryRun"` // from the query string: ?dryRun=true
    Text   string  `json:"text"`    // from JSON body
}
```

### Path and query parameters

A request field tagged `path:"name"` takes its value from the `{name}` segment of `Path()`; a field tagged `query:"name"` from the query string. Both are declared in `openapi.json` as typed operation parameters, so the generated Go and TypeScript clients take them as arguments.

```go
// ListGamesEndpoint lists a player's games.
type ListGamesEndpoint struct{}

func (e ListGamesEndpoint) Method() string { return "GET" }
func (e ListGamesEndpoint) Path() string   { return "/api/players/{playerId}/games" }
// Auth() and Handle(ctx, ListGamesReq) as usual

type ListGamesReq struct {
    PlayerID int      `path:"playerId"` // required, integer
    Sort     string   `query:"sort"`    // required: ?sort=name
    Search   *string  `query:"search"`  // optional: nil when not sent
    Tags     []string `query:"tag"`     // optional, repeated: ?tag=a&tag=b
}
```

**Supported types**

| Field | Path | Query | OpenAPI parameter |
|---|---|---|---|
| `string`, any integer type, `bool` (or a named type over one, e.g. `type Role string`) | ✓ | ✓ | `string` / `integer` / `boolean`, `required: true`; a named string type with constants is an [enum](#enums) |
| a pointer to one of those, e.g. `*string` | — | ✓ | the element's type, `required: false` |
| a slice of one of those, e.g. `[]int` | — | ✓ | `array` of the element's type, `style: form`, `explode: true`, `required: false` |

A path segment is always there and holds one value, so path fields are never pointers or slices.

**Decoding**

| Request | Result |
|---|---|
| a required (non-pointer) query param isn't sent | `400` `bad_request`, `query param "sort" is required` |
| `?search=` (sent, empty) | `""` for a string; an integer or bool fails to parse → `400` |
| a value that doesn't parse for its type (`?n=abc`, `/api/players/abc/games`) | `400` `bad_request` |
| a pointer or scalar param sent more than once (`?sort=a&sort=b`) | `400` `bad_request`: ambiguous input is an error, not a guess |
| a pointer param isn't sent | `nil` |
| a slice param isn't sent / sent as `?tag=a&tag=b` | an empty slice / `["a", "b"]`; each value is parsed by the element type, a bad one → `400` |
| a query param the type doesn't declare | ignored |

To make a query param optional, declare it as a pointer.

**Shared parameters: embedded structs.** An embedded struct's `path` and `query` fields count as the request type's own, at any depth, as its JSON fields already do. Declare a common set once and embed it:

```go
type PageParams struct {
    Limit  *int `query:"limit"`
    Offset *int `query:"offset"`
}

type ListUsersReq struct {
    PageParams
    Search *string `query:"search"`
}
```

The operation declares `limit`, `offset` and `search`, in field order (an embedded struct's parameters where it is embedded), and schemaf decodes all three. Embed by value: an embedded pointer with parameters is a codegen error. An embedded struct with a `json` name (`PageParams `json:"page"``) is a nested object, not an embed, so it can't carry parameters. Two parameters with the same name are an error naming both fields, even where Go would let the outer field hide the inner one. Generic structs embed the same way (`ListParams[UserSort]`).

**No body.** Path and query fields are not part of the request body: they're left out of the body schema and the request's component schema, and a POST, PUT or PATCH request whose only fields are path and query fields has no `requestBody`. schemaf decodes the body first and applies the path and query values after it, so a body key can never overwrite a path or query field.

**Codegen errors.** Each names the endpoint struct and the field, and says how to fix it:

- a `path` or `query` field of an unsupported type (float, struct, map, pointer to a slice, a pointer or slice on a path field, …)
- a `{name}` in `Path()` without a `path:"name"` field in the request type, or a `path:"name"` field without a `{name}` in `Path()`
- a field tagged both `json` and `path`/`query`, or both `path` and `query`
- two fields tagged with the same `path` name, or with the same `query` name — across the request type and its embedded structs
- an unexported field tagged `path` or `query`
- an embedded pointer to a struct with `path` or `query` fields: embed it by value
- a struct with `path` or `query` fields embedded with a `json` name (or `json:"-"`): it is a nested object of the body (or no JSON at all), not part of the request type, so its parameters would never be read — remove the json name. A tag without a name (`json:",omitempty"`) keeps it an embed
- a type with `path` or `query` fields used anywhere but as an endpoint's request type (a response, a stream event, a field or embedded struct of another type): its schema leaves those fields out, so it can't describe that JSON — declare a separate type
- an exported field of a GET or DELETE request type with neither a `path` nor a `query` tag: these requests have no body, so the field could never be filled

These rules apply to typed endpoints and [stream endpoints](#stream-endpoints). A [raw endpoint](#raw-endpoints) has no request type; its path parameters are declared as strings.

#### Upgrading: path and query parameters

- A request without a required (non-pointer) query param now gets `400` instead of the zero value. Declare the field as a pointer to keep it optional.
- Codegen fails on untagged fields in GET and DELETE request types, on `path` tags that don't match `Path()`, and on fields tagged both `json` and `path`/`query`. Each error names the field.
- Path fields can be integers and bools now; their spec parameters carry the field's type instead of `string`.
- Embedded structs can carry `path` and `query` fields now; codegen no longer refuses them.

**OpenAPI generation:**
The Go doc comment on the endpoint struct becomes the OpenAPI summary. The first line is the summary, subsequent lines become the description:

```go
// CreateUserEndpoint creates a new user account.
// Sends a welcome email after creation.
type CreateUserEndpoint struct{}
```

**Request and response types** can be declared anywhere: in `go/api/`, in another package of your module, or in a dependency, directly or through an alias. Keep your domain types in their own package and share them between the API, the database layer and CLIs:

```go
// go/api/users.go
type UserResp = obj.User // alias to a type in your obj package

type ListUsersResp struct {
    Users []obj.User `json:"users"`
}
```

Codegen type-checks `go/api/` (its `*.gen.go` and `*_test.go` files ignored) and builds each schema from the type's JSON encoding, as `encoding/json` produces it:

| Go | OpenAPI |
|---|---|
| named struct | `$ref` to a schema named after the type, e.g. `obj.User` → `User` |
| field | property named by its `json` tag, or by the Go field name without one; `json:"-"` and unexported fields are left out |
| `omitempty`/`omitzero` or pointer field | optional; every other field is required |
| embedded struct without a `json` name | its fields are flattened into the outer object |
| `string`, `bool`, integers, floats | `string`, `boolean`, `integer`, `number` |
| `time.Time`, `uuid.UUID` (`github.com/google/uuid`) | `string` with format `date-time`, `uuid` |
| `uuid.NullUUID` | `string` with format `uuid`, nullable |
| `[]byte` | `string` with format `byte` (base64) |
| slice, array / `map[K]V` | `array` / `object` with `additionalProperties` |
| instance of a generic struct, e.g. `Page[obj.User]` | `$ref` to a schema named from its type arguments and the type's name (see [Generic types](#generic-types)) |
| named string type with constants, e.g. `type UserSort string` | `$ref` to a schema named after the type: `string` with `enum` (see [Enums](#enums)) |
| named non-struct type, e.g. `type Role string` | the underlying type |
| type implementing `encoding.TextMarshaler`; field with `json:",string"` | `string` |
| `any`, interface, `json.RawMessage` | any JSON value |

A POST, PUT or PATCH request has a body when its type has at least one JSON field ([path and query fields](#path-and-query-parameters) don't count); GET and DELETE requests never have one. A response whose type is a struct without JSON fields has no content.

Codegen is strict, and each of these is a codegen error that names the type:

- two types with the same name from different packages, e.g. `api.Address` and `obj.Address`: rename one
- a type implementing `json.Marshaler` (other than the ones in the table): codegen can't know its JSON shape. If the type encodes as a JSON string (an enum, an ID), implement `encoding.TextMarshaler` instead: `encoding/json` then encodes it as a string, and its schema is `string`. Otherwise use a plain struct in the API
- a type without a JSON shape (channel, function)
- a generic type instantiated with an unnamed type argument other than a basic type, e.g. `Page[[]obj.User]` or `Page[map[string]int]`: use a named type as the type argument
- a POST, PUT or PATCH request type that is not a struct, e.g. `[]obj.User`: a request body is a JSON object
- a type error in `go/api/` or a package it imports

#### Generic types

A generic struct gets one component schema per instantiation, named from its type arguments followed by the type's name:

```go
type Page[T any] struct {
    Items []T `json:"items"`
    Total int `json:"total"`
}

type ListUsersResp = Page[obj.User] // schema "UserPage"
```

| Type | Schema name |
|---|---|
| `Page[User]` | `UserPage` |
| `Pair[A, B]` | `ABPair` |
| `Page[*User]` | `UserPage` (a pointer is named after its element, so `Page[*User]` and `Page[User]` can't both be used: a pointer field is optional, so their schemas differ) |
| `Page[Pair[A, B]]` | `ABPairPage` (nested instances, recursively) |
| `Page[string]` | `StringPage` (a basic type, capitalised) |

A name already taken is an error naming both types, as for plain types. Type arguments that are neither named nor basic (`[]User`, `map[string]int`) are an error: declare a named type. A generic type can be a request type too, with path and query fields.

#### Enums

A named string type with constants is an enum:

```go
type UserSort string

const (
    UserSortName      UserSort = "name"
    UserSortCreatedAt UserSort = "createdAt"
)

type ListUsersReq struct {
    SortBy *UserSort  `query:"sortBy"`
    Filter []UserFilter `query:"filter"`
}
```

It gets a component schema named after the type, `{"type": "string", "enum": ["name", "createdAt"]}`, and every use refers to it: query and path parameters, JSON fields, and the items of a slice. The generated TypeScript client has it as a union, `type UserSort = "name" | "createdAt"`; the Go client as a named string type whose constants carry the type's name, `UserSortName` and `UserSortCreatedAt`, so a value no longer collides with a type of the same name or with an equal value of another enum.

- The values are those of the package-level constants of exactly that type in its declaring package, exported or not, in declaration order. Constants with the same value count once, so an alias (`UserSortDefault = UserSortName`) is fine.
- A named string type without constants stays a plain `string`, inline.
- A type implementing `encoding.TextMarshaler` stays a plain `string`: its constants aren't necessarily what it encodes to.
- Only string types: named integer types with constants stay `integer`.
- A received value outside the enum is not rejected by schemaf: checking it stays with the app.
- The schema name follows the rules for plain types: two types with the same name are an error.

#### Upgrading: generic types and enums

- In the TypeScript client, a field or parameter of a named string type with constants is now a union of its values instead of `string`: code passing an arbitrary string no longer compiles. Pass one of the values, or convert where the value comes from outside.
- Generic response and request types no longer need a non-generic copy per endpoint.

#### Upgrading: Go client enum constants

- In the generated Go client (`go/apiclient/client.gen.go`), enum constants are named `<Type><Value>`, e.g. `TierMedium`, instead of the bare value (`Medium`) as in v1.21.0, where a value equal to a type's name broke the build. Rename the uses of the bare names. The TypeScript client is unchanged.

**What codegen does:**
- Scans all structs in `go/api/` that implement the endpoint interface
- Generates `go/api/endpoints.gen.go` with `api.Provider` (handler registration)
- Generates `gen/openapi.json` — OpenAPI 3.0 spec
- Generates `frontend/src/api/generated/api.gen.ts` — type-safe TypeScript client

You write the struct. Everything else is generated or framework-provided.

### Error responses

Every error response from the API has the same shape, whichever layer answered: a typed endpoint, a [raw endpoint](#raw-endpoints), an identity provider's `Exchange`, `OnLogin`, a plain `net/http` handler using [`api.WriteError`](#outside-the-endpoint-pipeline), the [`OnRequest`](#per-request-hook-onrequest) hook, or schemaf itself (bad request body, auth middleware, login checks). A client never has to care where an error came from.

```json
{"error": "an organisation needs at least one head", "code": "last_head"}
{"error": "Internal Server Error", "code": "internal", "id": "3f9a1c0be47d2a16"}
```

| Field | Present | Content |
|---|---|---|
| `error` | always | the message (see [Message](#message)) |
| `code` | always | machine-readable; the client branches on it (see [Code](#code)) |
| `id` | on 5xx only | 16 hex characters; the full error is logged under it and can be [looked up](#error-details) |

#### Status

The status comes from the sentinel error anywhere in the error chain (`errors.Is`), returned directly or wrapped with `%w`. An error without a sentinel is a 500.

| Sentinel | Status | Default code |
|---|---|---|
| `api.ErrBadRequest` | 400 Bad Request | `bad_request` |
| `api.ErrUnauthorized` | 401 Unauthorized | `unauthorized` |
| `api.ErrForbidden` | 403 Forbidden | `forbidden` |
| `api.ErrNotFound` | 404 Not Found | `not_found` |
| (none: written by schemaf's [route gate](#routing) for a wrong method, never by an endpoint) | 405 Method Not Allowed | `method_not_allowed` |
| (none: written by schemaf when `Accept` matches none of `Produces()`, see [Content types](#content-types)) | 406 Not Acceptable | `not_acceptable` |
| `api.ErrConflict` | 409 Conflict | `conflict` |
| `api.ErrGone` | 410 Gone | `gone` |
| `api.ErrPreconditionFailed` | 412 Precondition Failed | `precondition_failed` |
| `api.ErrTooLarge` | 413 Request Entity Too Large | `too_large` |
| (none: written by schemaf when `Content-Type` is not in `Consumes()`, see [Content types](#content-types)) | 415 Unsupported Media Type | `unsupported_media_type` |
| `api.ErrUnprocessable` | 422 Unprocessable Entity | `unprocessable` |
| `api.ErrTooManyRequests` | 429 Too Many Requests | `too_many_requests` |
| (no sentinel) | 500 Internal Server Error | `internal` |
| `api.ErrNotImplemented` | 501 Not Implemented | `not_implemented` |
| `api.ErrBadGateway` | 502 Bad Gateway | `bad_gateway` |
| `api.ErrUnavailable` | 503 Service Unavailable | `unavailable` |
| `api.ErrGatewayTimeout` | 504 Gateway Timeout | `gateway_timeout` |

501, 502 and 504 are 5xx like any other: their message is withheld, they keep their code, carry an `id` and are logged (see [Message](#message)).

`api.ErrUnauthorized` sets the status and code only. It never touches the session cookie and sets no `WWW-Authenticate` header: cookies are the business of schemaf's [auth layer](#authentication), not of your endpoint.

#### Custom message, custom code

There are two ways to say more than the bare sentinel, one per intent:

```go
// Custom message, default code: wrap the sentinel with %w.
return GetOrderResp{}, fmt.Errorf("order %s: %w", req.ID, api.ErrNotFound)
// → 404 {"error": "order 42: not found", "code": "not_found"}

// Custom message, your own code: api.Error(sentinel, code, msg).
return Resp{}, api.Error(api.ErrConflict, "last_head", "an organisation needs at least one head")
// → 409 {"error": "an organisation needs at least one head", "code": "last_head"}
```

```go
func Error(sentinel error, code, msg string) error
```

The error `api.Error` returns has `Error()` = `msg` (no `": conflict"` suffix) and unwraps to the sentinel, so `errors.Is(err, api.ErrConflict)` holds.

A full handler:

```go
func (e GetOrderEndpoint) Handle(ctx context.Context, req GetOrderReq) (GetOrderResp, error) {
    order, err := db.GetOrder(ctx, req.ID)
    if errors.Is(err, sql.ErrNoRows) {
        return GetOrderResp{}, fmt.Errorf("order %s: %w", req.ID, api.ErrNotFound) // → 404
    }
    return GetOrderResp{Order: order}, err // any other error → 500
}
```

#### Code

1. **Your code**: from `api.Error`, or from any error in the chain implementing `Code() string`. Your own error types carry a code without wrapping:

   ```go
   type ProfaneNameError struct{ Name string }

   func (e ProfaneNameError) Error() string { return "this name is not allowed" }
   func (e ProfaneNameError) Code() string  { return "profane_name" }
   func (e ProfaneNameError) Unwrap() error { return api.ErrBadRequest } // → 400
   ```

2. Otherwise the **default code** of the status (table above).

Rules:

- A code matches `^[a-z][a-z0-9_]*$`, whatever its source. A `Code()` that returns `""` is invalid, not "no code".
- A default code is only valid with its own status. `api.Error(api.ErrNotFound, "not_found", …)` is valid (and redundant); `api.Error(api.ErrConflict, "not_found", …)` is [misuse](#misuse).
- Status and code are independent. A coded error without a sentinel is a 500 that keeps its code. For a 4xx, your error type's `Unwrap()` returns a sentinel, as above.
- Your codes are kept on 5xx too, e.g. 503 `maintenance`: a code is chosen deliberately and leaks nothing.

#### Message

- **4xx** are expected errors, meant for the user: the message is `err.Error()`. If that is empty, it is the status text (`http.StatusText`, e.g. `"Conflict"`).
- **5xx** are the server's fault, and their message is withheld: it is always the status text (`"Internal Server Error"`, `"Service Unavailable"`), whatever the error says. The response carries an `id`, the full error is logged under it, and it can be looked up under the `id` (see [Error details](#error-details)). Messages tend to leak internals (driver errors, SQL, paths); codes don't.
- **schemaf's own 401s** (auth middleware) say `"Unauthorized"` and nothing more. The reason (missing, expired, malformed, bad signature, revoked session) is logged, never sent to the client.

#### Misuse

Misusing the error API is a 500 with code `internal` and an `id`, never a panic (a panic drops the request without a response). The log entry names the problem and the fix. Misuse is:

- `api.Error` with a `nil` sentinel, or with an error that is not one of the sentinels above
- an empty or invalid code, from any source
- a default code with the wrong status
- `Retry-After` on a status other than 429 or 503, or with a duration ≤ 0
- [`api.WriteError`](#outside-the-endpoint-pipeline) with a `nil` error

An empty message is not misuse; it falls back to the status text.

#### `Retry-After`

Any error in the chain implementing `RetryAfter() time.Duration` sets the `Retry-After` header, in whole seconds rounded up (`90*time.Second` → `Retry-After: 90`). `api.RetryAfter` wraps any error with it:

```go
func RetryAfter(err error, d time.Duration) error
```

```go
return Resp{}, api.RetryAfter(
    api.Error(api.ErrTooManyRequests, "token_locked", "too many failed attempts, try again later"),
    90*time.Second)
// → 429, Retry-After: 90, {"error": "too many failed attempts, try again later", "code": "token_locked"}
```

Only on 429 and 503, with a duration > 0; anything else is [misuse](#misuse).

#### Logging

- Every 5xx is logged at error level under its `id`, with method, path, subject (if any), status, code and the full `err.Error()`.
- The reasons for schemaf's own 401s are logged at warn level.
- Other 4xx are not logged.

#### Outside the endpoint pipeline

Endpoints return errors, and schemaf writes the response. A handler outside schemaf's endpoint pipeline (a plain `http.Handler`, middleware, the standalone mux of a package that is also mounted behind schemaf endpoints) answers errors with `api.WriteError`:

```go
func WriteError(w http.ResponseWriter, r *http.Request, err error)
```

```go
mux.HandleFunc("POST /games/{id}/move", func(w http.ResponseWriter, r *http.Request) {
    if !mayMove(r) {
        api.WriteError(w, r, api.Error(api.ErrForbidden, "denied", "it is not your turn"))
        // → 403 {"error": "it is not your turn", "code": "denied"}
        return
    }
    // …
})
```

The response is the same as an endpoint returning `err` would get: status, body, `Retry-After`, logging, the `id` and [error details](#error-details) on 5xx, and the [misuse](#misuse) rules. It is the function schemaf itself uses for every error response. Importing `api` needs no running schemaf app: no database, config or auth.

- Endpoints keep returning errors; don't call `api.WriteError` from `Handle`, `HandleRaw` or `Stream`.
- Call it only before anything of the response is written. As with `http.Error`, it doesn't check.
- Outside the pipeline there is no auth context, so a 5xx is logged without a `subject`.

#### Error details

A 5xx withholds its message from the client but carries an `id`. The details behind that id can be looked up, for example when a user reports "it said Internal Server Error, id `3f9a1c0be47d2a16`". Dev clients fetch them to show the full error.

**The store.** schemaf records every 5xx written through [`api.WriteError`](#outside-the-endpoint-pipeline) (every error response schemaf writes goes through it), in memory in the server process: the newest 1000 entries, each kept for at most 24 hours, oldest dropped first. 4xx are never recorded; they carry their full message and have no `id`. The store is lost on restart, and each replica knows only its own errors. Both values are framework constants.

**What's stored** (never the request body or headers):

| Field | Content |
|---|---|
| `id` | the `id` of the response |
| `time` | when the error was written (RFC 3339) |
| `method`, `path` | the request |
| `status`, `code` | the response |
| `subject` | the caller, if authenticated (omitted otherwise) |
| `error` | the full `err.Error()` |
| `misuse` | the [misuse](#misuse) problem and fix, if any (omitted otherwise) |

**The route.** `GET /api/errors/{id}` (`api.AuthOptional`) returns the stored entry as JSON with the fields above. It is in `openapi.json` with the schema `SchemafErrorDetails`, so the generated clients type it. An unknown or expired id is a 404 `not_found`.

**Who may look up:**

- In [development mode](#development-mode), every caller, anonymous included. Put public dev servers behind HTTP basic auth.
- In production, only callers the app allows with `app.CanSeeErrorDetails` in `Wire`. Without it, nobody: details are off by default.

```go
app.CanSeeErrorDetails(func(ctx context.Context, subject string) bool {
    return users.IsAdmin(ctx, subject)
})
```

An anonymous caller gets 401 `unauthorized`, and a subject the predicate refuses gets 403 `forbidden` (also every subject when no predicate is set). The subject who caused the error gets no special access.

**From Go.** `api.LookupError(id)` returns the stored entry, so a standalone server (one using `api.WriteError` outside the schemaf app) can serve its own lookup:

```go
func LookupError(id string) (api.ErrorDetails, bool)

type ErrorDetails struct {
    ID      string    `json:"id"`
    Time    time.Time `json:"time"`
    Method  string    `json:"method"`
    Path    string    `json:"path"`
    Status  int       `json:"status"`
    Code    string    `json:"code"`
    Subject string    `json:"subject,omitempty"`
    Error   string    `json:"error"`
    Misuse  string    `json:"misuse,omitempty"`
}
```

There is no CLI subcommand: the store lives in the server process.

#### OpenAPI

`openapi.json` has one shared schema, `components.schemas.Error`: `error` (string, required), `code` (string, required; its description lists every default code from the [status table](#status), schemaf's own `method_not_allowed`, `not_acceptable` and `unsupported_media_type` included) and `id` (string, 5xx only). Every operation, raw endpoints and schemaf's auth routes included, has a `default` response referencing it, so the generated TypeScript and Go clients type the error body.

`Error` is a reserved schema name, like the `Schemaf…` schemas of the framework routes (`SchemafErrorDetails` included): a type in `go/api/` that would appear in the spec under one of these names is a codegen error, never a silent overwrite.

#### Upgrading: error responses

Before v1.13.0, an error response was `{"error": "<message>"}`. Now:

- Every error response has a `code`; branch on it rather than on the message.
- 5xx messages are withheld: the message is the status text, and the response carries an `id` to find the full error in the log.
- schemaf's own 401s no longer say why; the reason is in the log.
- `openapi.json` declares errors as one `default` response referencing `Error`, instead of description-only `400`/`401`/`500` responses.

Since the seven sentinels `api.ErrGone`, `api.ErrPreconditionFailed`, `api.ErrTooLarge`, `api.ErrUnprocessable`, `api.ErrNotImplemented`, `api.ErrBadGateway` and `api.ErrGatewayTimeout` were added, their codes are default codes: `gone`, `precondition_failed`, `too_large`, `unprocessable`, `not_implemented`, `bad_gateway`, `gateway_timeout`. An app that uses one of them as its own code with another sentinel (say `api.Error(api.ErrBadRequest, "unprocessable", …)`) now hits [misuse](#misuse) and answers 500. Codegen can't detect this, because codes are runtime values: grep your code for these strings, and rename the code or switch to the matching sentinel. Run `./schemaf.sh codegen` to get `GET /api/errors/{id}` into `gen/openapi.json`; the server refuses to start with a spec that lacks it.

### Request headers

`api.Header` reads a request header from `ctx` in `Handle` and `Stream`:

```go
func Header(ctx context.Context, name string) string
```

```go
func (e AcceptInviteEndpoint) Handle(ctx context.Context, req AcceptInviteReq) (AcceptInviteResp, error) {
    lang := api.Header(ctx, "Accept-Language") // "" when absent
    ...
}
```

- The name is case-insensitive and the first value is returned, as with `http.Header.Get`. A missing header is `""`.
- On a context that doesn't come from an endpoint request (a service, a CLI command, a test's `context.Background()`), it is `""`.
- Headers a handler reads are not part of `openapi.json`. Data the client must send belongs in the request type (path, query or body).

### Content types

A typed endpoint takes and answers JSON. To take or answer other formats with the same request and response types, declare them with the optional methods `Consumes()` and `Produces()`:

```go
// GetGameYAMLEndpoint returns a game as YAML.
type GetGameYAMLEndpoint struct{}

func (GetGameYAMLEndpoint) Method() string      { return "GET" }
func (GetGameYAMLEndpoint) Path() string        { return "/api/games/{id}/yaml" }
func (GetGameYAMLEndpoint) Auth() api.AuthLevel { return api.AuthRequired }
func (GetGameYAMLEndpoint) Produces() []string  { return []string{"application/x-yaml"} }

// UpdateGameYAMLEndpoint replaces a game from YAML or JSON.
type UpdateGameYAMLEndpoint struct{}

func (UpdateGameYAMLEndpoint) Consumes() []string { return []string{"application/x-yaml", "application/json"} }
```

Supported media types (fixed by schemaf; anything else is a codegen error):

| Media type | Format |
|---|---|
| `application/json` | JSON |
| `application/yaml`, `application/x-yaml` | YAML |

YAML uses the same field names as JSON (the `json` tags): a YAML body is the JSON body written as YAML, so both formats share one schema.

- **Default**: without `Consumes()`/`Produces()`, an endpoint consumes and produces `application/json`. The rules below hold for it too: since v1.14.0 a JSON endpoint answers a non-empty body sent as e.g. `text/plain` with 415, and an `Accept` without JSON (or `*/*`) with 406. Before, it read every body as JSON and ignored `Accept`.
- **Request body**: decoded by the request's `Content-Type` (parameters such as `charset` are ignored). A `Content-Type` not listed in `Consumes()` is a **415** with code `unsupported_media_type`. A body without `Content-Type` is decoded as the first entry of `Consumes()`. An empty body is never decoded, whatever its `Content-Type`: the request type keeps its zero values (plus path and query params).
- **Response body**: encoded in the format picked from `Accept`: the entry of `Produces()` with the highest `q`, ties going to the order of `Produces()`; `*/*` and `type/*` match. Without `Accept`, or with `*/*`, it is the first entry of `Produces()`. An `Accept` matching none is a **406** with code `not_acceptable`. Ranges that don't parse or carry an invalid `q` are ignored (RFC 9110), a bare `*` counts as `*/*`, and an `Accept` with no valid range counts as absent. The response's `Content-Type` is the picked media type; with more than one entry in `Produces()` it also carries `Vary: Accept`, so caches keep the formats apart.
- **Errors** are always JSON (see [Error responses](#error-responses)), whatever `Accept` says.
- **In `openapi.json`**, the request body lists every entry of `Consumes()` under `content`, the `200` response every entry of `Produces()`, each with the same schema.

Codegen is strict here too: `Consumes()` and `Produces()` must each be a single return of a `[]string{...}` literal of string literals, non-empty, without duplicates and with supported media types only. `Consumes()` on an endpoint whose method schemaf decodes no body for (anything but `POST`, `PUT`, `PATCH`), and either method on a stream or raw endpoint, is a codegen error.

## Authentication

schemaf has built-in auth, fully managed by the framework:

- **Per-endpoint**: `Auth()` returns `api.AuthRequired`, `api.AuthOptional` or `api.AuthNone` (see [Auth levels](#auth-levels)). `/api/health`, `/api/status`, `/api/config`, `/openapi.json` and the login routes are always open; `/api/user/me` and `/api/auth/refresh` require auth; `/api/errors/{id}` is optional (see [Error details](#error-details)).
- **Optional auth**: at `api.AuthOptional` the handler also runs for anonymous callers, and `api.Subject(ctx)` tells it whether someone is signed in. Present but invalid credentials still give 401 (and clear a dead cookie); the `Origin` check applies to cookie requests as usual.
- **One token, two transports**: schemaf's token (an HMAC-SHA256 JWT) is accepted from the session cookie or from `Authorization: Bearer <token>`. Browsers use the cookie; scripts and other servers use Bearer. If both are present, Bearer wins.
- **Signing key is auto-managed**: generated on first boot and stored in Postgres (`_schemaf_config`). Nothing to configure, no secret to manage.
- In a handler, `api.Subject(ctx)` returns the authenticated subject; `/api/user/me` returns the current user.

Every authenticated request is verified against schemaf's own token and session only. How a user *proves* who they are (Auth0, passkeys, a login code) happens once, at login, through an [identity provider](#identity-providers) or [your own login endpoint](#starting-sessions-from-your-own-code). After that, endpoints and middleware never see the provider. Rights and roles are your app's concern: schemaf only tells you *who* is calling. To turn the subject into your app's own caller once per request, use [`OnRequest`](#per-request-hook-onrequest).

### Identity providers

An identity provider is a login plugin. It verifies a credential in the login request and returns the identity it proves. schemaf maps that identity to a subject and starts its own session for that subject.

```go
// package api
type IdentityProvider interface {
    Name() string // URL-safe, unique per app: "auth0", "passkey", ...
    // Exchange verifies the provider's credential in the login request and
    // returns the identity it proves. Errors answer like endpoint errors:
    // e.g. api.ErrForbidden (or wrap it) refuses with 403, any error without
    // a sentinel is a 500. See "Error responses".
    Exchange(ctx context.Context, r *http.Request) (api.Identity, error)
}

type Identity struct {
    Provider string         // the provider that proved it (set by schemaf)
    Subject  string         // the provider's subject, e.g. Auth0's sub
    Claims   map[string]any // all further claims (email, name, ...)
}
```

schemaf ships one built-in provider, [`auth0`](#activating-auth0), which is switched on by environment variables alone. Your own providers are registered in `Wire` (see [Writing your own provider](#writing-your-own-provider)).

#### Login routes

| Route | What it does |
|---|---|
| `POST /api/auth/{provider}/login` | Calls the provider's `Exchange`, then `OnLogin` (if set), then starts a session. Sets the cookie and responds `{"subject": "...", "token": "..."}`. The token is always returned, for Bearer clients. An unknown or unconfigured provider gives 404 with code `not_found` and the message `provider not configured`. |

All auth routes are in `openapi.json`, so the generated TypeScript and Go clients have them. `POST /api/auth/auth0/login` is always in the spec, with the body `{"id_token": string}`, whether Auth0 is configured or not: the build output does not depend on deployment config. Your own providers are reached through the generic `POST /api/auth/{provider}/login`, whose body is a free-form JSON object.

#### Mapping identities to subjects: `OnLogin`

By default the subject is the provider's subject, unchanged: `api.Subject(ctx)` returns e.g. the Auth0 `sub` (`google-oauth2|1234...`). Auth0 subjects already carry the upstream connection, so they don't collide.

To map identities to your own users, register an optional `OnLogin` hook in `Wire`:

```go
app.OnLogin(func(ctx context.Context, id api.Identity) (string, error) {
    // Look up, create or link your user, or refuse with api.ErrForbidden (403).
    // The returned string becomes api.Subject(ctx) for the whole session.
    email, _ := id.Claims["email"].(string)
    if verified, _ := id.Claims["email_verified"].(bool); !verified {
        return "", fmt.Errorf("email not verified: %w", api.ErrForbidden)
    }
    return users.Upsert(ctx, id.Provider, id.Subject, email)
})
```

- **Claims are only available at login.** The session stores just the subject. If your app needs `email`, `name` or similar, capture them in `OnLogin`.
- **Claim policy is yours.** Checks like `email_verified`, allowed domains or invite-only signup belong in `OnLogin`. Providers only prove identity.
- `OnLogin` runs for every provider. `id.Provider` tells you which one proved the identity.
- Errors from `Exchange` and `OnLogin` answer exactly like endpoint errors, with the same status, code and message rules: see [Error responses](#error-responses).

#### Rules

- Identity providers need a database (sessions live in Postgres). Any active provider without `app.AddDb(...)` is a startup error.
- Registering a provider named `auth0` (reserved for the built-in) or two providers with the same `Name()` is a startup error.
- The provider is consulted **only at login**. A session outlives changes at the provider, for example the account being deleted there. Revoke sessions from your app if that matters (see [Sessions](#sessions)).
- Active providers are logged when the server starts and listed in `/api/status` (see [Active providers](#active-providers-apistatus)).

#### Active providers: `/api/status`

`/api/status` lists the names of the active providers under `auth.providers`:

```json
{
  "status": "ok",
  "auth": { "providers": ["auth0", "passkey"] }
}
```

- A name is present only for an active provider: present means "show this login button". No providers gives `"providers": []`.
- The client settings the frontend needs to start a provider's sign-in (for `auth0`: domain and client ID) are per instance. They come from [runtime config](#runtime-config-public_), not from `/api/status`. If your own provider needs such settings in the frontend, give them `PUBLIC_` variables.

### Activating Auth0

The built-in `auth0` provider needs no code. Set two environment variables and it is active. Both are `PUBLIC_` variables: the frontend needs the same two values, and reads them through [runtime config](#runtime-config-public_).

#### 1. Create the Auth0 application

1. In the [Auth0 dashboard](https://manage.auth0.com/), go to **Applications → Applications → Create Application**, pick **Single Page Web Applications**, and create it.
2. On the application's **Settings** tab, note the **Domain** (e.g. `myapp.eu.auth0.com`) and the **Client ID**. You don't need the client secret: schemaf only verifies ID tokens.
3. Still on **Settings**, under **Application URIs**, add your app's origins. For dev with the default port:
   - **Allowed Callback URLs**: `http://localhost:8000`
   - **Allowed Logout URLs**: `http://localhost:8000`
   - **Allowed Web Origins**: `http://localhost:8000`

   Add your production URL (e.g. `https://myapp.example.com`) to each list as well, comma-separated. Use your configured `port` if it isn't 8000. Save.

#### 2. Configure schemaf

Put the two values into the project's secrets file (see [Secrets](INSTALL.md#secrets)): `~/.<name>/dev/etc/env` for dev and `~/.<name>/etc/env` for production. For a project named `myapp`:

```bash
# ~/.myapp/dev/etc/env
DB_PASS=changeme
PUBLIC_AUTH0_DOMAIN=myapp-dev.eu.auth0.com
PUBLIC_AUTH0_CLIENT_ID=Xb4tq0LgYxZ1Ux8Gm3aP7cVh2nKw9sRd
```

- `PUBLIC_AUTH0_DOMAIN` is the bare domain: no scheme, no path, no trailing slash (`myapp.eu.auth0.com`, not `https://myapp.eu.auth0.com/`). The Auth0 SPA SDK adds `https://` itself, so the server refuses to start on any other form. If you use an Auth0 custom domain, use that.
- The values are **per instance**: each deployment reads its own env file, so two deployments can use two Auth0 applications (or tenants), or one can leave Auth0 off.
- Both values are public: they ship in every Auth0 SPA anyway. `/api/config` sends them to the browser.
- `schemaf.sh` loads the env file (`dev` → `dev/etc/env`, `run` → `etc/env`), and the generated `compose.gen.yml` passes the production env file to the backend container.

#### 3. What activation does

The variables are checked when the server starts (`server`, `dev`), never for other CLI commands:

| `PUBLIC_AUTH0_DOMAIN` | `PUBLIC_AUTH0_CLIENT_ID` | Result |
|---|---|---|
| set | set | `auth0` is active. The startup log shows `"msg":"identity providers active","providers":["auth0"]` and `/api/status` lists `"auth0"` under `auth.providers`. |
| unset | unset | Auth0 is off. `POST /api/auth/auth0/login` returns `404 provider not configured`. |
| one set | the other unset | The server refuses to start: `auth0: PUBLIC_AUTH0_DOMAIN is set but PUBLIC_AUTH0_CLIENT_ID is not; set both to enable Auth0, or neither to disable it` |

An empty value counts as unset.

Auth0 needs a database like every provider (`app.AddDb(...)`).

#### 4. Frontend: sign in and post the ID token

The frontend reads the domain and client ID with `config()` (see [Runtime config](#runtime-config-public_)). Never hardcode them in the frontend: they differ per instance. If they are set, it signs in with the Auth0 SPA SDK (`npm install @auth0/auth0-spa-js`, or `@auth0/auth0-react` for its React wrapper), takes the **ID token** it received, and posts it to schemaf's login route. From then on, the session cookie authenticates every API call automatically.

```ts
import { createAuth0Client } from "@auth0/auth0-spa-js";
import { Api } from "./api/generated/api.gen";
import { config } from "./api/generated/config.gen";

const api = new Api();

// 1. Is Auth0 active on this instance, and with which application?
const domain = config("PUBLIC_AUTH0_DOMAIN");
const clientId = config("PUBLIC_AUTH0_CLIENT_ID"); // unset → don't show the login button

if (domain && clientId) {
  // 2. Initialise the SDK with this instance's values.
  const auth0 = await createAuth0Client({
    domain,
    clientId,
    authorizationParams: { redirect_uri: window.location.origin },
  });

  if (location.search.includes("code=")) {
    // 3. Back from Auth0: finish sign-in and take the ID token.
    await auth0.handleRedirectCallback();
    history.replaceState({}, "", "/");
    const claims = await auth0.getIdTokenClaims();

    // 4. Exchange it for a schemaf session. The cookie is set;
    //    authenticated endpoints now just work.
    await api.api.postApiAuthAuth0Login({ id_token: claims!.__raw });
  }

  // Login button: onClick={() => auth0.loginWithRedirect()}
}
```

Post the ID token **right after sign-in**: schemaf rejects ID tokens issued more than 5 minutes ago (see [Troubleshooting](#troubleshooting)).

#### 5. Backend: who is calling

In any endpoint with `api.AuthRequired` (or `api.AuthOptional`, when someone is signed in), `api.Subject(ctx)` returns the Auth0 `sub` by default:

```go
func (e ProfileEndpoint) Handle(ctx context.Context, req ProfileReq) (ProfileResp, error) {
    sub, _ := api.Subject(ctx) // e.g. "google-oauth2|1234..."
    return profiles.Get(ctx, sub)
}
```

To map Auth0 users to your own user IDs, or to keep their email (only available at login), register [`OnLogin`](#mapping-identities-to-subjects-onlogin).

#### 6. Logout

`POST /api/auth/logout` ends the schemaf session and clears the cookie. It does not touch Auth0: while the user is still signed in at Auth0, the SDK can sign them back in silently. For a full sign-out, call Auth0's logout as well:

```ts
await api.api.postApiAuthLogout();
await auth0.logout({ logoutParams: { returnTo: window.location.origin } });
```

#### Troubleshooting

| Symptom | Cause |
|---|---|
| `404 provider not configured`, or no `auth0` in `/api/status` | Auth0 isn't active on this server: `PUBLIC_AUTH0_DOMAIN` / `PUBLIC_AUTH0_CLIENT_ID` are missing from the env file the server was started with. Projects set up before runtime config used `AUTH0_DOMAIN` / `AUTH0_CLIENT_ID`: rename them. |
| `403 auth0: token issued too long ago (iat … ago, max 5m0s) …` | The ID token's `iat` is more than 5 minutes old. Post the token right after sign-in, not a cached one from an earlier session. If it happens right after sign-in, check the server's clock. |
| `403 auth0: oidc: id token issued by a different provider, expected "https://…/" got "https://…/"` | `PUBLIC_AUTH0_DOMAIN` differs from the domain that issued the token, e.g. the tenant domain on the server but a custom domain in the frontend. Use the same domain in both. |
| `403 auth0: oidc: expected audience "…" got ["…"]` | The frontend signed in with a different application than `PUBLIC_AUTH0_CLIENT_ID`. Use the same Client ID in both. |
| `503 auth0: OIDC discovery for https://…/ failed` | The server can't reach `https://<PUBLIC_AUTH0_DOMAIN>/.well-known/openid-configuration`: a typo in the domain, or no outbound network. Discovery runs on the first login and is retried on the next one, so a temporary outage does not stop the server from starting. |
| Server won't start: `auth0: PUBLIC_AUTH0_DOMAIN is set but PUBLIC_AUTH0_CLIENT_ID is not` | Only one of the two variables is set. Set both, or neither. |
| Server won't start: `auth0: PUBLIC_AUTH0_DOMAIN must be the bare domain … use PUBLIC_AUTH0_DOMAIN=myapp.eu.auth0.com` | The value has a scheme, a path or a trailing slash. Use the form the message shows. |

### Writing your own provider

Implement `api.IdentityProvider` in your project (e.g. `go/auth/passkey.go`) and register it in `Wire`:

```go
app.AddIdentityProvider(auth.PasskeyProvider{})
```

`Exchange` gets the raw login request, so it reads whatever body it defines:

```go
type PasskeyProvider struct{ /* ... */ }

func (p PasskeyProvider) Name() string { return "passkey" }

func (p PasskeyProvider) Exchange(ctx context.Context, r *http.Request) (api.Identity, error) {
    cred, err := verifyAssertion(ctx, r.Body)
    if err != nil {
        return api.Identity{}, fmt.Errorf("passkey: %w", api.ErrForbidden)
    }
    return api.Identity{Subject: cred.UserHandle}, nil
}
```

The frontend calls it through the generated client's generic login route, `postApiAuthProviderLogin("passkey", body)`. Multi-step ceremonies (a passkey challenge before the assertion) are ordinary endpoints in your project; only the final step that proves the identity is the provider.

### Sessions

A session is what a successful login creates. schemaf stores it in Postgres (`_schemaf_sessions`) and represents it to the client as a short-lived token carrying the session ID.

- **Token lifetime**: 15 minutes. **Session lifetime**: 30 days from login. Both are framework constants.
- **Every authenticated request checks the session**: not revoked, not expired, and not ended by a subject-wide revocation. Revocation is immediate.
- **Renewal**: when a cookie-authenticated request carries a token past half its lifetime, schemaf sets a fresh cookie for the same session. An expired token in the cookie is renewed the same way, as long as its session is still live, so a browser stays signed in for the whole session even after idling. Bearer tokens are not renewed automatically: Bearer clients call `POST /api/auth/refresh` before their token expires; an expired Bearer token gives 401.
- **The cookie**: named `<name>_session` after your project (`myapp_session`), so apps on different localhost ports don't overwrite each other's cookie. A [parallel dev instance](INSTALL.md#parallel-dev-instances) `n` uses `<name>-<n>_session` (`myapp-349_session`), so two instances of one app don't sign each other out; the OpenAPI spec documents the default name. Host-only (no `Domain`), `HttpOnly`, `SameSite=Lax`, `Path=/`, lasting until the session expires, and `Secure` outside [development mode](#development-mode). `Lax` rather than `Strict` so the cookie is sent when another site redirects the browser to you, as in an OIDC `/authorize` redirect.
- **CSRF protection**: a cookie-authenticated request with any method other than `GET` or `HEAD` must carry an `Origin` header matching the request's host (`Host`, or `X-Forwarded-Host` behind a proxy). A mismatched **or missing** `Origin` is rejected with 403. Browsers send `Origin` on every such request; non-browser clients should use Bearer, which is exempt.
- **Login CSRF protection**: `POST /api/auth/{provider}/login` (every provider, built-in and custom) is rejected with 403, and no cookie is set, when it carries an `Origin` header that doesn't match the request's host (same rule as above). Otherwise a malicious site could post the attacker's own credential and sign the visitor in as the attacker. A login without `Origin` (curl, server-to-server) is allowed, since it can't be a cross-site browser request.
- **Cleanup**: the server (not CLI commands) deletes sessions that expired or were revoked more than 7 days ago, at start and then daily. Both values are framework constants. Subject-wide revocations (`_schemaf_revocations`) are kept.

Built-in routes:

| Route | What it does |
|---|---|
| `POST /api/auth/logout` | Ends the current session and clears the cookie. Responds 204, also when there is no session. |
| `POST /api/auth/refresh` | Requires auth with a session token (a stateless token gives 400). Returns `{"token": "..."}`, a fresh token for the current session (and renews the cookie if the request used it). |
| `GET /api/errors/{id}` | `api.AuthOptional`. Returns the details of a 5xx by its `id`: open in development mode, in production only to callers `app.CanSeeErrorDetails` allows (see [Error details](#error-details)). |

#### Starting sessions from your own code

For logins your app handles itself (login codes, guest accounts, invites), start the session from a typed endpoint with `StartSessionCtx`. schemaf sets the session cookie on the response the endpoint writes:

```go
func StartSessionCtx(ctx context.Context, subject string) (token string, err error)
```

```go
import schemafapi "github.com/flocko-motion/schemaf/api"

func (e RedeemCodeEndpoint) Handle(ctx context.Context, req RedeemCodeReq) (RedeemCodeResp, error) {
    userID, err := codes.Redeem(ctx, req.Code)
    if err != nil {
        return RedeemCodeResp{}, schemafapi.ErrForbidden
    }
    // Sets the cookie on the response and returns the token for Bearer clients.
    token, err := schemafapi.StartSessionCtx(ctx, userID)
    if err != nil {
        return RedeemCodeResp{}, err
    }
    return RedeemCodeResp{Token: token}, nil
}
```

- It works in `Handle` and in `Stream` (before `Stream` returns). On any other context (a service, a CLI command, a raw endpoint's `r.Context()`) it returns an error naming the fix, and starts no session.
- The session exists once `StartSessionCtx` returns. If `Handle` then returns an error, the error response still carries the cookie: the caller is signed in.

A [raw endpoint](#raw-endpoints) has the `http.ResponseWriter` and calls `StartSession` with it instead:

```go
func (e RedeemCodeEndpoint) HandleRaw(w http.ResponseWriter, r *http.Request) error {
    userID, err := codes.Redeem(r.Context(), r.FormValue("code"))
    if err != nil {
        return schemafapi.ErrForbidden
    }
    // Sets the cookie on w and returns the token for Bearer clients.
    token, err := schemafapi.StartSession(r.Context(), w, userID)
    if err != nil {
        return err
    }
    return json.NewEncoder(w).Encode(map[string]string{"token": token})
}
```

Sessions started this way have an empty `provider` column in `_schemaf_sessions`.

#### Ending sessions early

```go
// The current request's session (false for stateless tokens):
sid, ok := schemafapi.SessionID(ctx)

err = schemafapi.RevokeSession(ctx, sid)     // one session
err = schemafapi.RevokeSubject(ctx, subject) // every session and every stateless token of that subject
```

`RevokeSubject` ends everything issued for the subject up to now; logging in again afterwards starts a fresh session as usual.

### Getting a token in development

For **local development**, mint a token for any subject with the built-in command:

```bash
# the database must be running, e.g. ./schemaf.sh dev db
TOKEN=$(./schemaf.sh auth token alice)
curl -H "Authorization: Bearer $TOKEN" localhost:8000/api/user/me
```

- `auth token <subject>` prints a signed [stateless token](#issuing-stateless-tokens) for that subject, so you can call authenticated endpoints as any user.
- `--ttl <duration>` sets an expiry (e.g. `--ttl 24h`); without it, the token never expires.
- **Dev-only**: the command refuses to run outside [development mode](#development-mode). `./schemaf.sh auth token` runs in development mode; a bare `./myapp auth token` does not.

### Issuing stateless tokens

`IssueToken` mints a token that is not tied to a session: it is not checked against `_schemaf_sessions` and is never renewed. Only `RevokeSubject` ends it before its expiry. Use `StartSession` for users; use this for service-to-service tokens:

```go
import schemafapi "github.com/flocko-motion/schemaf/api"

token, err := schemafapi.IssueToken("svc:importer", time.Now().Add(24*time.Hour))
// time.Time{} (zero) as the second argument issues a token with no expiry.
```

### Per-request hook: `OnRequest`

schemaf tells your app *who* is calling: a subject string. If your app keeps its own users, roles or tenants behind that subject, register one `OnRequest` hook in `Wire`. It runs on every API call, after authentication and before the handler, so mapping the subject to your app's own caller (and checking account state such as deleted, disabled or half-registered) sits at one point in code instead of in every handler:

```go
// go/app/app.go
app.OnRequest(func(ctx context.Context, a *api.Admission) (context.Context, error) {
    subject, ok := a.Subject()                     // ("", false) for an anonymous caller
    caller, err := users.Resolve(ctx, subject, ok) // your own lookup and account checks
    if err != nil {
        return nil, err // refuses the request, answered like an endpoint error
    }
    return users.WithCaller(ctx, caller), nil // the handler gets this context
})
```

The hook can call as many of your own functions as it likes; that is how you compose several steps.

- **Where it runs**: on every request that reaches a route in `openapi.json`: your endpoints at all three [auth levels](#auth-levels) (`AuthNone` included), typed, [raw](#raw-endpoints) and [stream](#stream-endpoints), and schemaf's own `/api/*` routes (`/api/health`, `/api/status`, `/api/config`, the login, logout and refresh routes, `/api/user/me`, `/api/errors/{id}`). It does **not** run for the frontend, for `/openapi.json`, or for the [route gate's](#routing) own 404 and 405.
- **Order**: route gate → authentication → `OnRequest` → handler. A failed authentication (401, 403) answers before the hook; the hook never sees it.
- **Who is calling**: `a.Subject()` gives the authenticated subject, or `("", false)` for an anonymous caller on `AuthOptional` and for every caller on `AuthNone` (credentials are still never looked at there). The hook's `ctx` carries the authentication result too, so `api.Subject(ctx)` reports the same.
- **Enrich**: the returned context is the one the handler gets: `ctx` in `Handle` and `Stream`, `r.Context()` in `HandleRaw` and schemaf's own handlers. Everything schemaf calls with that context sees it too, such as [`CanSeeErrorDetails`](#error-details).
- **Refuse**: a returned error answers the request exactly like an endpoint error (status, code, message, logging, the 5xx `id`, see [Error responses](#error-responses)), and the handler does not run. An `api.ErrUnauthorized` from the hook leaves the session cookie alone, as from an endpoint; a cookie renewal that authentication already did stays done.
- **One hook**: calling `app.OnRequest` twice, or with `nil`, is a startup error. Put all steps into the one hook.
- **Strict**: returning a `nil` context with a `nil` error is a 500; the logged error names the hook.
- Without `OnRequest`, requests run exactly as without it. [Integration tests](#integration-tests-schemaftest) run the hook too, since they share `Wire`.

#### The admission: identities only the hook can mint

`*api.Admission` is the ingredient that only schemaf provides, and only to the `OnRequest` hook. Use it to make your app's own caller type unforgeable by the rest of your app: give that type a single constructor that requires an `*api.Admission` (or that *is* the hook), and keep it in a protected part of your code (code owners). Then a caller can only ever be built in the hook, from the subject schemaf authenticated:

```go
// core/caller.go: the only way to make a Caller
func Admit(ctx context.Context, a *api.Admission) (context.Context, error) {
    subject, ok := a.Subject()
    // ... look up the account, build the sealed Caller, put it into ctx
}

// go/app/app.go
app.OnRequest(core.Admit)
```

- **Only schemaf makes a valid one.** Its fields are unexported: `&api.Admission{}`, a copy of one, or `nil` is never valid.
- **It reaches the hook and nothing else**: never the context, never an endpoint, no getter.
- **It carries the authenticated subject**, so identity and the proof that schemaf authenticated it are one value.
- **It works only while the hook runs.** schemaf invalidates it when the hook returns, so an admission kept by mistake (in a goroutine, a global) is useless afterwards. Copies are invalidated with it.
- **Misuse is loud**: `a.Subject()` on an admission that is not valid (zero value, `nil`, or used after its hook returned) panics, naming the cause; it never reports an anonymous caller instead. `a.Valid()` checks without panicking.
- **Nothing else is hidden**: `api.Subject(ctx)` and `api.SessionID(ctx)` keep working in the handler. The guarantee comes from your caller type requiring an admission, not from hiding the subject.
- The hook runs in [integration tests](#integration-tests-schemaftest) through `schemaftest.Start` as in production; schemaf offers no other way to obtain a valid admission.

## Updates

On a run host that [pulls releases](INSTALL.md#pulling-releases-linux) with the [updater](INSTALL.md#the-updater) enabled, the app can show what an upgrade would bring and ask for one: an instance admin starts an upgrade from inside the app, with no operator in the loop. The run host looks for new releases once a day; an admin who just published one can have it look now. **The app asks, the run side decides:** the app only writes a version string (or the word `check`); the updater installs it only if it is a verified release on the host's channel that is newer than the running one. A compromised app can at most start an install of a genuine newer release, or a check at most once a minute: no image choice, no command, no downgrade, no channel switch.

### The built-in endpoints

Three endpoints are always live and always in `openapi.json` and the generated TypeScript and Go clients, whatever the configuration (like the [Auth0 login route](#activating-auth0)). All three need a signed-in caller ([`AuthRequired`](#auth-levels)):

| Endpoint | Does |
|---|---|
| `GET /api/updates` | `api.Updates()` as JSON (below) |
| `POST /api/updates/request` | body `{"version": "1.5.0"}`: `api.RequestUpgrade(version)`; answers `202` with `{"version": "1.5.0"}` |
| `POST /api/updates/check` | no body: `api.CheckForUpdates()`; answers `202` with no content |

**Closed by default.** Until the app calls `app.SetUpdatesAuth` in `Wire`, all three answer `403` with `updates: the app has not authorized anyone (app.SetUpdatesAuth)`. The authorizer decides for all three endpoints. It runs after authentication and the [per-request hook](#per-request-hook-onrequest), so it sees the app's own caller; a returned error refuses the request and is answered in the [standard error shape](#error-responses) (an `api.Error(api.ErrForbidden, …)` is a 403, a plain error a 500):

```go
app.SetUpdatesAuth(func(ctx context.Context) error {
    if !users.FromContext(ctx).IsAdmin {
        return api.Error(api.ErrForbidden, "forbidden", "only admins manage updates")
    }
    return nil
})
```

### The Go API

```go
state, err := api.Updates()          // api.UpdateState
err = api.RequestUpgrade("1.5.0")
err = api.CheckForUpdates()
```

`api.Updates()` returns:

| Field | Value |
|---|---|
| `Running` | the running version (the tag of the backend's image); empty in development |
| `Status` | `updater not enabled`, `idle`, `installing` or `checking` |
| `Available` | the verified versions newer than the running one, oldest first, each with its release `Notes` (from [`releases.json`](INSTALL.md#releases-from-ci-releasegenyml)); refreshed by every `releases` run |
| `Checked` | when `releases` last refreshed `Available`: it moves when a check finishes |
| `Install` | the last or current install: `Version`, `From`, `State` (`installing`, `installed`, `rolled-back`, `failed`, `rollback-failed`), `Started`, `Finished`, `Message`; nil before the first one |
| `Request` | the last install request the updater handled: `Version`, `State` (`accepted`, `rejected`), `Message`, `Time`; nil before the first one |
| `Check` | the last or current check for new releases: `State` (`checking`, `checked`, `failed`, `rejected`), `Trigger` (`timer`: the daily timer, `app`: `CheckForUpdates`, `operator`: `schemaf.sh releases` by hand), `Started`, `Finished`, `Message`; nil before the first one |

`api.RequestUpgrade(version)` writes the version to the updater's request file. `api.CheckForUpdates()` writes the word `check` to it: the updater runs [`releases`](INSTALL.md#releases) right away, which fetches and verifies, and never installs. Poll `api.Updates()`: `Status` is `checking` while it runs, and `Check` and `Checked` show the outcome when it's done. Both refuse, with typed errors to compare with `errors.Is`:

| Error | When | HTTP |
|---|---|---|
| `api.ErrUpdaterNotEnabled` | no updater on this host: development, macOS, a host without `schemaf.sh updater enable` | 409 `updater_not_enabled` |
| `api.ErrVersionNotAvailable` | `RequestUpgrade`: the version isn't in `Available` | 422 `version_not_available` |
| `api.ErrInstallRunning` | an install is running | 409 `install_running` |
| `api.ErrCheckRunning` | `CheckForUpdates`: a check is running (from the timer or an earlier request) | 409 `check_running` |
| `api.ErrCheckTooSoon` | `CheckForUpdates`: the last check finished less than a minute ago; the message says how long to wait, and the response carries `Retry-After` | 429 `check_too_soon` |

Without an updater, `api.Updates()` returns the running version, an empty `Available` and the status `updater not enabled`. Never a silent no-op: nothing is written without an updater to act on it. The updater checks the request again on its side (INSTALL.md › The updater), so these checks are for a clear answer, not for safety. A `checking` whose check started more than 30 minutes ago is treated as interrupted: `Status` doesn't report it, and `CheckForUpdates` passes the request on to the updater.

**Where the files are.** The backend container sees the run host's `~/.<name>/updates/` at `/schemaf/updates/`: `available.json`, `status.json` and the marker `updater` read-only, and `request` writable (two separate mounts). The run side creates them before it starts the stack.

#### Upgrading: checking for updates

- Run `./schemaf.sh codegen` to get `POST /api/updates/check` into `gen/openapi.json`; the server refuses to start with a spec that lacks it.
- The run host's timer now looks for releases once a day instead of hourly. `load` rewrites the units of an enabled updater, so a host gets the new timer with the first release built with this schemaf version; nobody has to run `schemaf.sh updater enable` again ([The updater](INSTALL.md#the-updater)).
- `status.json` gains the `check` record and `Status` the value `checking`; a frontend that switches over `Status` should handle it.

## Stream Endpoints

A stream endpoint answers with [server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html). Define `Stream` instead of `Handle`: it returns a channel, and schemaf writes each value sent on it to the client as one event:

```go
// go/api/workshops.go
// WorkshopEventsEndpoint streams a workshop's live events.
type WorkshopEventsEndpoint struct{}

func (WorkshopEventsEndpoint) Method() string      { return "GET" }
func (WorkshopEventsEndpoint) Path() string        { return "/api/workshops/{id}/events" }
func (WorkshopEventsEndpoint) Auth() api.AuthLevel { return api.AuthRequired }

func (WorkshopEventsEndpoint) Stream(ctx context.Context, req WorkshopEventsReq) (<-chan WorkshopEvent, error) {
    sub, err := workshops.Subscribe(ctx, req.ID) // errors here answer like Handle errors
    if err != nil {
        return nil, err
    }
    events := make(chan WorkshopEvent)
    go func() {
        defer close(events) // closing the channel ends the stream
        for {
            select {
            case <-ctx.Done(): // the client left
                return
            case e := <-sub:
                select {
                case events <- WorkshopEvent{Kind: e.Kind, Player: e.Player}:
                case <-ctx.Done():
                    return
                }
            }
        }
    }()
    return events, nil
}

type WorkshopEventsReq struct {
    ID string `path:"id"`
}

type WorkshopEvent struct {
    Kind   string `json:"kind"`
    Player string `json:"player"`
}
```

**What the framework does for you:**
- Authenticates the caller according to `Auth()`, decodes the request from path and query params, and calls `Stream(ctx, req)`
- If `Stream` returns an error, answers with the standard [error response](#error-responses), exactly as for `Handle`
- Otherwise answers `200` with `Content-Type: text/event-stream`, `Cache-Control: no-cache` and `X-Accel-Buffering: no` (so proxies don't buffer), and flushes the headers right away
- Writes each value received from the channel as one event, `data: <value as JSON>`, and flushes it
- Writes a comment line (`: keepalive`) every 15 seconds while no event is sent, so proxies and load balancers don't drop an idle stream. The interval is a framework constant
- Ends the response when the channel is closed
- Cancels `ctx` when the client disconnects, and stops reading the channel

**Rules:**
- The method is `GET`, so browsers can use `EventSource`. Any other method is a codegen error.
- `Stream` must have the signature `Stream(ctx context.Context, req Req) (<-chan Event, error)` with named `Req` and `Event` types. Define exactly one of `Handle`, `HandleRaw` or `Stream`; codegen errors otherwise.
- One event type per stream. Events carry only `data`, no `event:` name or `id:`, so clients read them with `onmessage`. A stream with several kinds of event uses one type with a field telling them apart (`Kind` above).
- **Stop when `ctx` is done.** The goroutine sending on the channel must select on `ctx.Done()` and return (closing the channel): after the client leaves, nobody reads the channel any more, and a goroutine blocked on a send leaks.
- `Stream` returning a `nil` channel with a `nil` error is [misuse](#misuse) (500).
- An event that fails to encode as JSON ends the stream; the error is logged at error level with method, path and subject.
- `api.Header`, `api.Subject` and `api.StartSessionCtx` work in `Stream` as in `Handle`. Cookies (a [started session](#starting-sessions-from-your-own-code), a renewed session cookie) must be set before `Stream` returns: once the channel is returned, headers are sent, and a later `StartSessionCtx` returns an error and starts no session.
- Stream endpoints have no `Consumes()`/`Produces()`.

**In `openapi.json`**, the `200` response has content type `text/event-stream` with the schema of the event type: the schema of each event's `data`. The generated TypeScript client thus types the events; open the stream with `EventSource` (cookie auth, same origin) or `fetch` (Bearer):

```ts
const source = new EventSource(`/api/workshops/${id}/events`)
source.onmessage = (m) => {
  const event: WorkshopEvent = JSON.parse(m.data)
}
```

## Raw Endpoints

For endpoints that need direct HTTP access — binary uploads, streaming downloads, or formats that are not [content types](#content-types) schemaf supports — use `HandleRaw` instead of `Handle`. Request headers, starting sessions, YAML and [server-sent events](#stream-endpoints) don't need a raw endpoint:

```go
// go/api/blobs.go
// DownloadBlobEndpoint streams a file download.
type DownloadBlobEndpoint struct{}

func (e DownloadBlobEndpoint) Method() string { return "GET" }
func (e DownloadBlobEndpoint) Path() string   { return "/api/blobs/{id}" }
func (e DownloadBlobEndpoint) Auth() api.AuthLevel { return api.AuthRequired }

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
- Define exactly one of `Handle`, `HandleRaw` or [`Stream`](#stream-endpoints) — codegen errors otherwise
- If `HandleRaw` returns a non-nil error and you haven't written a response yet, the framework writes the standard [error response](#error-responses), exactly as for `Handle` (`ErrNotFound` → 404 `not_found`, `api.Error` codes, `Retry-After`, etc.)
- If you've already started writing a response (set headers, streamed bytes), return `nil` and handle errors yourself
- Auth works identically, with the same [three levels](#auth-levels) — at `api.AuthRequired` the token and session are validated before `HandleRaw` is called, and `api.Subject(r.Context())` returns the authenticated user ID; at `api.AuthOptional` it returns `("", false)` for anonymous callers

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

Within one binary, an identifier names one set of files. Registering it again with the same files (wiring the app again, as every [test instance](#integration-tests-schemaftest) does) keeps one set; registering it with different files is a wiring bug, and the app panics at registration, naming the identifier.

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

sqlc writes into `go/db/`: `db.gen.go` (the `Queries` type), `models.gen.go` (the model structs) and one `<file>.sql.gen.go` per query file (e.g. `users.sql.gen.go`). Codegen owns these files: before every sqlc step it removes each file at the top level of `go/db/` that carries sqlc's header `// Code generated by sqlc. DO NOT EDIT.` in its first five lines, then sqlc writes the current set. So deleting a query file deletes its functions, and deleting the last one deletes all query code. Each file removed for good (not rewritten by sqlc) is printed:

```
ℹ removed stale sqlc output: go/db/old_query.sql.gen.go
```

Files are identified only by that header, never by name: your own files in `go/db/` (e.g. `go/db/helpers.go`) are never touched. This also removes files from schemaf versions that kept sqlc's default names (`db.go`, `models.go`, `<file>.sql.go`): the first codegen after upgrading reports them as stale and writes the `.gen.go` files.

### Wiring

Wire the database in `Wire` (`go/app/app.go`):
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

A service gets a context that is cancelled when the server stops. On SIGINT or SIGTERM (Ctrl-C in `./schemaf.sh dev`, `docker stop`/`podman stop` in production) the server shuts down gracefully: it cancels the services' context, stops accepting connections and waits for in-flight requests (`http.Server.Shutdown`), waits for every service to return, then closes the database pool. All of that has 8 seconds, inside the 10 seconds a container runtime gives a container to stop. A service must return once its context is done; one that doesn't makes the shutdown fail loudly instead of hanging.

### Before-serve hook: `OnBeforeServe`

Work that must finish before the app takes requests (seeding a development database, promoting a configured admin, initialising telemetry) goes into an `OnBeforeServe` hook in `Wire`:

```go
// go/app/app.go
app.OnBeforeServe(func(ctx context.Context) error {
    // the database is open and migrated; nothing is served yet
    return core.Bootstrap(ctx)
})
```

- **Order**: database opened and migrated, auth initialised → `OnBeforeServe` hooks, in registration order → services → serving. `api.DevMode()` is already set, so the hook can branch on it.
- **Refuse**: a returned error aborts the start: the later hooks don't run, no service starts and nothing is served. `Run` (the server command) and `Start` return the error as `before-serve hook: …`.
- **Where it runs**: on every start of the server, `./myapp server` as well as [`schemaftest.Start`](#integration-tests-schemaftest), so tests see the same ready state as production. Codegen and subcommands never run it.

## Code Generation

**One command generates everything:**

```bash
./schemaf.sh codegen
```

Codegen is fast, so it always runs every step; there are no single-step subcommands. (`codegen all` is still accepted as the old spelling, which `schemaf.sh` scripts generated by earlier versions call, e.g. during `upgrade`.)

`schemaf.sh` uses `go run` to build a standalone schemaf CLI on the fly — no project binary needed, no dependencies beyond Go itself.

**What gets generated:**

1. **SQL → Go (sqlc)**
   - Auto-discovers `go/db/queries/*.sql`
   - Generates type-safe Go query functions → `go/db/db.gen.go`, `go/db/models.gen.go` and one `go/db/<file>.sql.gen.go` per query file
   - Removes sqlc's earlier output first, so nothing stale survives (see [Queries](#queries))

2. **Migrations → db.Provider**
   - Auto-discovers `go/db/migrations/*.sql`
   - Generates `go/db/migrations.gen.go` with `db.Provider` function
   - Provider returns embedded migrations to framework

3. **Endpoint structs → api.Provider + OpenAPI spec**
   - Auto-discovers endpoint structs in `go/api/*.go` (without `*.gen.go` and `*_test.go`)
   - Generates `go/api/endpoints.gen.go` with `api.Provider` (handler registration). It also carries the spec: `Provider()` hands it to schemaf, and the server answers `GET /openapi.json` with it byte for byte. There is no second spec built at runtime
   - Generates `gen/openapi.json` — OpenAPI 3.0 spec
   - Generates `gen/schema-sources` — the packages outside `go/api/` that declare a type the spec's schemas are built from, one glob per line (e.g. `go/obj/*.go`), plus `go/go.mod` when a schema type comes from a dependency. Empty when every type is declared in `go/api/`

4. **OpenAPI spec → TypeScript client**
   - Generates `frontend/src/api/generated/api.gen.ts` — type-safe client for your frontend
   - No running server needed

   Codegen also writes `frontend/src/api/generated/config.gen.ts`, the reader for [runtime config](#runtime-config-public_).

5. **Compose → compose.gen.yml**
   - Merges schemaf's built-in compose (backend, Postgres) with your `compose/*.yml`
   - The backend service never builds: its image is `${SCHEMAF_IMAGE}`, set by `schemaf.sh` (`run` builds first, `run --image <tag>` starts a loaded image); compose without it fails loudly
   - The backend service loads the production env file `~/.<name>/etc/env` (`env_file`), so every variable in it, including `PUBLIC_*` [runtime config](#runtime-config-public_), reaches the container
   - Files matching `*.dev.yml` are dev/test only — not included in `compose.gen.yml` (prod)
   - Used by `./schemaf.sh run` (prod) and `./schemaf.sh dev` (dev)

6. **Release workflow → .github/workflows/release.gen.yml**
   - A pushed tag `vX.Y.Z` (or `vX.Y.Z-<pre>`) builds the artifact, attests it with keyless Sigstore and publishes it with the app's `releases.json`. See [Releases from CI](INSTALL.md#releases-from-ci-releasegenyml)

7. **Inputs → gen/inputs.sha256**
   - One SHA-256 over codegen's inputs: `go/api/*.go` (without `*.gen.go` and `*_test.go`), the files in `go/db/migrations/` and `go/db/queries/`, `schemaf.toml`, `compose/*.yml`, the files matched by the globs in `gen/schema-sources` (without `*.gen.go` and `*_test.go`), and the schemaf version required in `go/go.mod`
   - So editing a type in another package that the API uses, e.g. a field of `obj.User`, makes the generated files stale, while editing a package the API doesn't use does not
   - Computed as: for each input file in byte-wise sorted path order, the line `<sha256 of the file>  <path>` (as `sha256sum` prints it); then the line `schemaf <version>`; the hash of that text is the file's content. Dotfiles are not inputs
   - `./schemaf.sh build` and `./schemaf.sh test` recompute it (with `sha256sum`, or `shasum -a 256` on macOS) and stop on a mismatch: `generated files are stale — run ./schemaf.sh codegen and commit`
   - `./schemaf.sh build --output` (a deployment artifact) recomputes it over the committed inputs instead: the input files of the commit (via `git ls-tree` / `git cat-file`), with the commit's `gen/schema-sources` and `go/go.mod`, compared with the commit's `gen/inputs.sha256`. Untracked and gitignored inputs are not part of that hash, so generated files that depend on one (e.g. a `compose.gen.yml` including a gitignored `compose/extra.yml`) are stale for the artifact. An input committed as a symlink stops it with its own error. Plain `build`, `run` and `test` hash the working tree (following symlinks). See [Generated files must be fresh](INSTALL.md#generated-files-must-be-fresh)

8. **Empty inputs → gen/empty-inputs**
   - The list of empty inputs (see below), one line per input, written next to `gen/inputs.sha256`. An empty or missing file means nothing is empty

### Empty inputs

A project with no endpoints, no migrations or no queries is valid, e.g. while porting an existing app onto schemaf. Every generated Go file compiles with empty input; empty input selects a variant of the generated output, never a placeholder file:

| Input | Empty when | What is generated |
|---|---|---|
| `go/api/*.go` | no endpoint structs | `endpoints.gen.go` with a `Provider()` that registers nothing |
| `go/db/migrations/` | no `*.sql` files | `migrations.gen.go` without `//go:embed`; `Provider()` returns an empty `embed.FS` |
| `go/db/queries/` | no `*.sql` files | nothing: sqlc does not run (it refuses an empty query set), and its earlier output is removed (see [Queries](#queries)) |

Codegen prints one warning per empty input. It never stops:

```
⚠ no endpoints in go/api — Provider() registers nothing
⚠ no migrations in go/db/migrations — db.Provider() is empty
⚠ no queries in go/db/queries — no query code generated
```

Each line of `gen/empty-inputs` is one of these warnings. `./schemaf.sh build` prints them before it builds, so whoever builds the image sees them too.

TypeScript tests are optional and not an input: with no tests in `go/tests/*.test.ts`, `ts.gen_test.go` and `runner.gen.ts` are generated with no tests, and there is no warning. Without a `go/tests/` directory nothing is generated for tests.

**One exception: `go/app/frontend_dist/.gitkeep`.** `go/app/frontend.gen.go` embeds `go/app/frontend_dist/`, which the frontend build fills at image build time (`Dockerfile.gen`). Its contents aren't known at codegen time, so codegen can't pick a variant; it creates `go/app/frontend_dist/.gitkeep` instead, so the embed always matches. Commit it.

**Zero configuration.** Just run `./schemaf.sh codegen` and all the glue code appears. Generated files are committed: `build` runs no codegen and needs no Go, so a commit must carry generated files that match its inputs.

## Testing

**Running tests:**
```bash
./schemaf.sh test                  # check generated files are fresh, then run all tests
./schemaf.sh test --verbose        # verbose go test output
./schemaf.sh test --no-cache       # bypass test cache
```

`./schemaf.sh test` runs no codegen. It first checks `gen/inputs.sha256` (see [Code Generation](#code-generation)) and stops if generated files are stale — `run ./schemaf.sh codegen and commit` — then runs `go test ./go/...` and `npx tsc --noEmit`. Tests therefore always run against generated code that matches its inputs, and CI catches a commit that forgot codegen.

The test stack (`compose.test.gen.yml`) holds only your project services (`compose/*.yml`), with no volumes; it has no Postgres. Each test that boots the app gets its own database from the [harness](#integration-tests-schemaftest). `./schemaf.sh test` starts the stack with the selected [container runtime](INSTALL.md#container-runtime) (podman by default, Docker with `SCHEMAF_RUNTIME=docker`), sets the services' URLs (`gen/test-env.sh`), and tears it down after the tests. A project without services starts no stack at all. A project service in the test stack can't depend on `postgres` (`depends_on`): there is none, so codegen stops with a message naming the service.

### Where tests live

| Test | Where | Package |
|---|---|---|
| Unit test of one file | next to it, e.g. `go/api/users_test.go` | the file's package |
| Integration test that boots the app | `go/tests/*_test.go` | `tests` |
| TypeScript test against the running app | `go/tests/*.test.ts` | — |

`init` scaffolds `go/tests/health_test.go`: it starts an instance, calls `GET /api/health`, mints a token and calls `GET /api/user/me` with it.

Codegen does not read test files: an endpoint struct declared in a `*_test.go` file (in package `api` or `api_test`) belongs to the test, e.g. to serve a route through `api.NewRawRoute`, and is never registered in `endpoints.gen.go` or listed in `gen/openapi.json`.

### Integration tests (`schemaftest`)

`github.com/flocko-motion/schemaf/schemaftest` starts your app in the test process, against its own fresh database, on a free port. An app's tests import only this package:

```go
// go/tests/users_test.go
package tests

import (
    "net/http"
    "testing"

    "github.com/flocko-motion/schemaf/schemaftest"

    "myapp/app"
)

func TestUsers(t *testing.T) {
    inst := schemaftest.Start(t, app.Wire) // migrations applied, server listening

    inst.URL()           // http://127.0.0.1:<port>
    inst.DB()            // *sql.DB of this instance's database
    tok := inst.Token("alice")       // a signed token for a subject
    inst.AuthHeader(tok) // http.Header{"Authorization": {"Bearer <tok>"}}, e.g. for the generated apiclient
    inst.Restart()       // graceful stop, then start again on the same database and port

    req, _ := http.NewRequest("GET", inst.URL()+"/api/user/me", nil)
    req.Header = inst.AuthHeader(tok)
    // ...
}   // t.Cleanup: the server stops, the pool closes, the database is dropped
```

| | |
|---|---|
| `Start(t, app.Wire)` | A fresh instance: a new database with the full schema and no data, then the server started the way `./myapp server` starts it (migrations, auth, sessions and their cleanup, your `AddService` services). `t` is a `testing.TB`. |
| `URL()` | The instance's base URL. It stays the same across `Restart`, so clients built from it stay valid. |
| `DB()` | A connection pool on the instance's database, for setting up and checking data. The server sees what you write, and you see what it writes. |
| `Token(subject)` | A token signed with the instance's key, minted in the test process: stateless and without expiry, like `api.IssueToken(subject, time.Time{})`. `api.RevokeSubject` still ends it. |
| `AuthHeader(tok)` | An `http.Header` carrying `Authorization: Bearer <tok>`. |
| `Restart()` | Stops the server gracefully and starts it again on the same database and port, with nothing kept in memory, as after a deployment. Data, the signing key and sessions survive, since they all live in the database, so a token minted before the restart still works. |

Every failure stops the test with `t.Fatal` and says what to do: the container runtime is missing or not running, the image pull failed, a migration failed, the server couldn't start.

**Development mode, always.** An instance runs in [development mode](#development-mode) however the tests were started: `Start` sets `SCHEMAF_DEV=1` for the test (`t.Setenv`, restored at cleanup), so `./schemaf.sh test` and a plain `go test` from your IDE behave the same. Your `api.DevMode()` branches run, and session cookies carry no `Secure` flag, so cookie logins work over the instance's plain `http://`. (Because of `t.Setenv`, a test that starts an instance can't call `t.Parallel()`, which instances don't support anyway.)

**What an instance runs.** Migrations, auth, sessions and their cleanup, and your services. Not the SFTP backup scheduler and not the frontend dev proxy: the embedded frontend is served if `Wire` calls `SetFrontend`, and other paths outside `/api/` are a 404. Identity providers come only from `Wire` (`AddIdentityProvider`), never from the OIDC environment variables, so tests don't depend on your secrets. Project compose services (MinIO etc.) come from the test stack, not from the harness.

**Databases.** The first `Start` in a test binary starts one Postgres container (`postgres:17-alpine`, on a free `127.0.0.1` port, with the selected container runtime) and runs your migrations, the framework's and the app's, once into a template database. Each `Start` then creates its own database from that template (`CREATE DATABASE … TEMPLATE …`), so it begins with the full schema, no data, no signing key and no sessions, and `t.Cleanup` drops it. The container dies with the test process, even on Ctrl-C or SIGKILL: the process holds it through a pipe, and the runtime removes it (`--rm`). No `TestMain` is needed. `go test ./...` runs packages as parallel processes; each has its own container, so they never clash.

**One instance at a time.** The server's signing key, sessions and database connection are process-wide, so a test binary runs one instance at a time: start a fresh one per test, one after another. A second `Start` while one is still running stops the test: `schemaftest: another instance is still running; instances run one after another, not in parallel`. `t.Parallel()` across instances is not supported.

**Production safety.** Neither `DB()` nor `Token()` can reach a real database or mint a token that works anywhere else:
- `Start` panics outside a `go test` binary (`testing.Testing()`), and refuses to run with `SCHEMAF_ENV=docker`.
- The harness builds its database address only from the container it started. It never reads `DATABASE_URL`, `DB_HOST`, `DB_PASS` or the app's own DSN, and there is no option to pass one in.
- An `Instance` has no exported constructor or fields.
- Each instance generates its signing key fresh in its own throwaway database, so its tokens fail everywhere else.

### TypeScript tests

Write exported async functions named `test*` in `go/tests/*.test.ts`. Each gets the instance's base URL and a token for the subject `test` (minted with `Token("test")`):

```typescript
// go/tests/api.test.ts
export async function testCreateUser(baseUrl: string, token: string) {
    const resp = await fetch(`${baseUrl}/api/users`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
        ...
    })
    if (!resp.ok) throw new Error(`expected 200, got ${resp.status}`)
}
```

A test that doesn't need the token can leave out the second parameter.

Codegen generates Go wrappers (`go/tests/ts.gen_test.go`): one Go test per `.test.ts` file, named `TestTS<File>` (`api.test.ts` → `TestTSApi`), which starts one [instance](#integration-tests-schemaftest) for the file. Its `test*` functions run as subtests (`TestTSApi/CreateUser`) against that instance, one after another, so they share its database. Each runs the TypeScript via `npx tsx` and reports pass or fail as a Go test.

To skip a TS test, add a comment on the preceding line:

```typescript
// skip: requires clock compose service
export async function testClockTime(baseUrl: string) { ... }
```

The generated subtest calls `t.Skip(...)` with that message. A file whose tests are all skipped starts no instance.

### Testing the framework: the CI gate

For work on schemaf itself. `make test <unit|db|e2e|all>` runs the test layers locally; `e2e/db-test.sh` and `e2e/build-example.sh` follow `SCHEMAF_RUNTIME` like everything else (default podman). The reference, though, is the **CI gate**: the GitHub Actions workflow `.github/workflows/gate.yml`, whose check is named `gate`. `schemaftest`'s own tests start containers, so they skip unless `SCHEMAF_CONTAINER_TESTS=1`; `e2e/db-test.sh` (`make test db`) sets it and runs them, and a project's `./schemaf.sh test`, which also runs the framework's tests, stays fast.

**Where**: the pinned Linux amd64 runner image `ubuntu-24.04`, the kind of machine schemaf apps run on: rootless podman runs natively, not in a VM, and nothing is left over from earlier runs. It runs the oldest Go schemaf supports, the `go` directive in `go.mod` (`actions/setup-go` with `go-version-file: go.mod`, `GOTOOLCHAIN=local`), so a dependency that needs a newer Go fails the gate.

**What**: two parallel jobs, `podman` and `docker`. Each sets `SCHEMAF_RUNTIME` to its runtime and runs the full sequence (`make test all`):

1. unit tests (`go test ./...`) and the DB integration tests (`e2e/db-test.sh`), which start their containers through the selected runtime too;
2. the onboarding e2e (`e2e/build-example.sh <head sha>`), which also runs
   - the [lockfiles](INSTALL.md#lockfiles) (`e2e/deploy.sh`): `init`'s `frontend/package-lock.json` and `go/go.sum` are committed, and a `package.json` changed without its lockfile, or an emptied `go.sum`, stops `build` with the file and the fix;
   - the two-user deployment (`e2e/deploy.sh`) with **two real Unix users** the workflow creates (`useradd`): the build user clones the project and builds the artifact, the run user loads, runs, upgrades and rolls back. Under podman each has its own rootless store, so the image really crosses over as a file; under Docker both use the machine's one daemon, so the builder's image is removed before each load;
   - the [pull flow](INSTALL.md#pulling-releases-linux) (`e2e/pull.sh`, run by `deploy.sh` on the same run user): the runner builds releases of the project and signs them for real with keyless Sigstore under the gate job's own identity (`id-token: write`; `SCHEMAF_SIGNER` is that full certificate identity), and serves a `releases.json` from a local HTTP server. The run user's box, set up as INSTALL.md instructs, does a first install on the empty box and an upgrade with its data kept; a deliberately broken release (its migration changes data, its health check fails) rolls back automatically, with the database restored and the previous version serving; an older version, a prerelease on the stable channel, a tampered artifact, a version that doesn't match its manifest and a wrong signer are refused; and with the [updater](INSTALL.md#the-updater) enabled (the run user's real systemd units), the backend container sees `available.json` read-only, and requests written as the app writes them (one from inside the backend container) are rejected when older, off the channel, unverified or not a version, while a valid one is installed;
   - the data-volume check (`e2e/volumes.sh`) with real volumes: the other runtime holds `<name>-postgres-data` → `test` and `run` refuse; both runtimes hold it → they refuse; only the selected one → `test` starts. The `podman` job covers "Docker has it", the `docker` job "podman has it".

`gate` is green only if both jobs are.

**When**: on pull requests (opened, synchronized, reopened) and manually, for any pushed branch:

```bash
gh workflow run gate --ref <branch>
gh run watch                      # follow it
```

It doesn't run on plain pushes to branches.

**Which commit**: the PR's head commit (for a manual run, the head of the dispatched branch), passed to `build-example.sh` by its full hash. GitHub's `refs/pull/<n>/merge` commit is never tested: the Go module proxy can't fetch it. Because `main` only accepts up-to-date branches (below), the head commit is exactly what lands on `main`.

**Caches**: downloads only — the Go module and build caches (keyed by `go.sum`) and the npm cache (keyed by `cli/cmd/codegen/scaffold/package.json.tmpl`, which decides what a new project installs; the repository has no `package-lock.json`). Never container volumes, the project's images, podman/Docker storage, database state, or anything else from a previous run.

**The scripts are the single source of truth.** The workflow only provisions the runner (the two users, the compose provider, the podman API sockets: the composite action `.github/actions/runner-setup`, shared with `example-pull.yml`) and calls `make test all`. It sets:

| Variable | Effect |
|---|---|
| `E2E_REFERENCE=1` | this is the reference environment: a check that can't run fails instead of being skipped (e.g. `volumes.sh` without the other runtime, `deploy.sh` without two real users, `pull.sh` without Linux or the OIDC token) |
| `E2E_BUILD_USER`, `E2E_RUN_USER` | `deploy.sh` builds as the one and runs as the other, through `sudo`, instead of simulating the second user with its own `HOME` |

Without them (a local run), `deploy.sh` simulates the second user — under podman on Linux with its own rootless store under that `HOME` — and `volumes.sh` skips loudly when the other runtime isn't usable. The pull flow skips loudly outside GitHub Actions (it signs with the workflow's OIDC token) and on macOS (it is Linux only), so `make test all` and `make release` on a Mac don't cover it; CI does, on every PR. GitHub gives no OIDC token to a pull request from a fork, so such a PR can't pass the gate: push its branch to this repository and let the gate run there. A local `make test all` is for debugging, e.g. on macOS; Docker locally is optional: `SCHEMAF_RUNTIME=docker make test e2e`.

**One-time repository setting (maintainer).** In the repository settings, a branch protection rule for `main` (Settings › Branches › Add rule, pattern `main`) with:

- **Require status checks to pass before merging** → required check: `gate` (source: GitHub Actions);
- **Require branches to be up to date before merging**.

Then nothing — not `gh pr merge` from a script, not a click in the web UI — merges around the gate, and every gate run tests exactly what lands on `main`. In practice: after each merge, the other open branches must merge `origin/main` (and pass the gate again) before they can merge.

### Merging and releasing the framework

Not every merge is a release: a PR another branch needs can be merged without releasing. So there are two commands: `make merge` merges only, `make release` releases `main`, and merges the current branch first when run on one.

**`make merge`** — per PR, from the feature branch:

1. Refuses on `main`, with a dirty working tree, with no commits beyond `main`, or when the branch doesn't contain `origin/main` (`branch is behind main — run: git merge origin/main, then make merge again`).
2. Fast local checks: `go build ./...`, `go vet ./...`, `go test ./...`. A failure stops it before anything is pushed.
3. Pushes the branch and opens the PR, or reuses the open one. This starts the CI gate.
4. Waits for the `gate` run of the PR's head commit, found by workflow and head SHA. No run, or more than one, is a loud error. The wait is limited to 45 minutes; a timeout counts as a failure.
5. Green → merges the PR with a merge commit (`gh pr merge --merge`, honoring the required check, and only if the PR's head is still the commit that passed). Red or timeout → merges nothing and prints the CI run URL. The PR stays open with its red check; push a fix to the branch and run `make merge` again.

No local onboarding e2e, no tag. Issues named in `Closes #<n>` trailers go into the PR body and are checked to be closed after the merge.

**`make release <major|minor|patch>`** — releases what is on `main`. Run it on `main`, or on a feature branch to merge it and release in one go:

1. Refuses an unknown bump and a dirty working tree, before anything is pushed.
2. **On a feature branch:** everything `make merge` does (its refusals, the fast local checks, push, PR, waiting for the CI gate, merge). Red, timeout or any other refusal → stops with nothing merged and nothing tagged, like `make merge`. After the merge it releases the new `origin/main`, the merge commit, without switching branches (so it works in a worktree, where `main` is checked out elsewhere). It first checks that the merge commit's tree is the branch's tree, so the local checks tested exactly what gets tagged; if `main` moved in between, it stops with the PR merged and nothing tagged: run `make release` again on `main`.
3. **On `main`:** refuses unless HEAD equals `origin/main` (after a fetch), and when nothing landed since the last release tag. Then the fast local checks (as above).
4. **On macOS**, the full local gate against the commit to release, by its full hash: `make test all` under podman (unit, `e2e/db-test.sh`, the onboarding e2e). It's the layer CI doesn't give: CI doesn't cover macOS. On Linux, the fast checks are enough locally: the commit already passed CI as part of its up-to-date PR.
5. Green → tags `vX.Y.Z`, pushes the tag, creates the GitHub release, and closes the issues of the released range that are still open (`Released in vX.Y.Z.`). Red → tags nothing; a macOS-only break is fixed by a PR (`make merge`), then `make release` again.

6. **The example release** (`e2e/example-release.sh vX.Y.Z`, below) of the new tag. It needs the tag, so it runs after it: a failure leaves the release in place and stops with `release: vX.Y.Z is released, but its example release failed (see above) — fix it and release a patch`.

No prerelease tags: the only tag a release creates is `vX.Y.Z`. To merge without releasing, use `make merge`.

### The example release

The CI gate tests the release and pull machinery against itself. What it can't test is the real path through GitHub: a tag in a generated project, the release workflow it got from codegen, the attestation GitHub's Sigstore setup produces, and a box that pulls that release. A long-lived project tests it: the GitHub repository [`flocko-motion/schemaf-example`](https://github.com/flocko-motion/schemaf-example), made by `init` and **upgraded** from each schemaf release to the next, like a real app. (Creating a project from scratch is the onboarding e2e's job, on every PR.)

`make release` runs `e2e/example-release.sh vX.Y.Z` after tagging:

1. Clones the example and upgrades it to the new schemaf: `go get github.com/flocko-motion/schemaf@vX.Y.Z`, `./schemaf.sh codegen`, `GOWORK=off go mod tidy`.
2. Commits, pushes `main`, tags the example `vX.Y.Z`, the same version as schemaf, and pushes the tag.
3. Waits for the example's `release.gen.yml` run of that tag.
4. Checks the result: the GitHub release has `schemaf-example-X.Y.Z.run` and its `.sigstore.json` bundle; the attestation verifies (`gh attestation verify --bundle`) as signed by the example's `release.gen.yml` on `refs/tags/vX.Y.Z`; the artifact's manifest names version `X.Y.Z`; `releases.json`, downloaded from its stable URL, has exactly one correct entry for it.
5. **The example pull**: starts `.github/workflows/example-pull.yml` on the schemaf tag and waits for it. Under both runtimes, on a fresh `ubuntu-24.04` runner, `e2e/example-pull.sh X.Y.Z` sets up a new box exactly as INSTALL.md › Pulling releases instructs, as it would have been when v-1 (the example's release before `X.Y.Z`) was the newest: the env file with `SCHEMAF_SIGNER=flocko-motion/schemaf-example`, and the example's `schemaf.sh` at v-1's tag. Then `releases` and `install` put v-1 on the empty box (reading the real index as it was then: its entries up to v-1, served locally), and `releases` and `install X.Y.Z` upgrade it from the real `releases.json`, done by v-1's run-side script. So each release also proves that a box running the previous version can verify and install the next one.

A version that is already released is only checked, never released again. A tag without a release (a failed run) stops it with the run's URL; delete the tag in the example and run it again. By hand, for a pushed branch or tag of schemaf (a branch needs the example's version, e.g. a prerelease): `e2e/example-release.sh <ref> [<version>]`. It pushes to the example repository and starts a workflow, so only on the maintainer's order. It needs `git`, `go`, `jq`, `curl` and `gh`, signed in with push access to the example. The example pull alone: `gh workflow run example-pull.yml --ref <schemaf branch or tag> -f version=<X.Y.Z>`.

**One-time setup** (done by hand, once; not part of the test). The example needs a project and a first release with the pull flow, so that every later release has a v-1:

1. In a clone of the empty repository: `go run github.com/flocko-motion/schemaf/cmd/schemaf@<full commit hash> init schemaf-example`, with a pushed schemaf commit that has the pull flow, then move the project's files into the clone's root, commit and push `main`.
2. Tag the example `v0.0.1` and push the tag: its generated release workflow makes the first attested release and `releases.json`.

From then on, `make release` upgrades it.

### e2e refs

The onboarding e2e (`e2e/build-example.sh <ref>`) creates a project with a single `go run github.com/flocko-motion/schemaf/cmd/schemaf@<ref> init <name>` and builds, tests and deploys it. The gates run it against a pushed commit, by its full hash. Which refs it accepts, and how Go fetches them:

| `<ref>` | Example | Go module proxy |
|---|---|---|
| release tag (default: the latest) | `v1.16.0` | used for everything, no override |
| full 40-character commit hash | `e74a68e984ad6e9f58efe3340aec25f1a7ddb5a0` | used for everything, no override: Go resolves the hash to a pseudo-version, and a commit is immutable, so the proxy can't serve a stale one |
| moving ref: a branch or tag | `main` | `GOPRIVATE=github.com/flocko-motion/schemaf`: only schemaf bypasses the proxy and sumdb; all other dependencies still come through the proxy |

Anything else (an abbreviated hash, a name that is no branch or tag on the remote, a name that is both) is refused. The script prints the mode it uses (`ref '<ref>' is …`).

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
| `BACKUP_SSH_KEY_PATH` | yes | — | Host path to SSH private key (mounted as a compose secret) |
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
