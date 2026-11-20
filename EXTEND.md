# Extending Your Project

Back to [README](README.md) | See also: [Installation](INSTALL.md)

## Contents

- [main.go Wiring](#maingo-wiring)
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
- [Stream Endpoints](#stream-endpoints)
- [Raw Endpoints](#raw-endpoints)
- [Database](#database)
  - [Migrations](#migrations)
  - [Migration identifiers](#migration-identifiers-independent-migration-lines)
  - [Applying migrations to another database](#applying-migrations-to-another-database)
  - [Queries](#queries)
- [CLI Subcommands](#cli-subcommands)
- [Code Generation](#code-generation)
  - [Empty inputs](#empty-inputs)
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
    // optional: app.OnLogin(...), app.AddIdentityProvider(...) — see Authentication

    if err := app.Run(); err != nil {
        log.Fatal(err)
    }
}
```

`app.Run()` hands over to Cobra. The `server` command (default) starts the HTTP server. Custom subcommands are available alongside it.

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

Codegen generates `go/frontend.gen.go` with `FrontendFS()` returning the embedded assets. Wire it in `main.go`:

```go
app.SetFrontend(FrontendFS())
```

### Codegen

`./schemaf.sh codegen` does two things for the frontend:

1. **Scaffold** — if `frontend/` doesn't exist, creates a Vite + TypeScript setup with React (the default) and runs `npm install`
2. **Generate** — creates `go/frontend.gen.go` (embeds `frontend/dist/` into the Go binary), `frontend/src/api/generated/api.gen.ts` (type-safe API client from OpenAPI spec) and `frontend/src/api/generated/config.gen.ts` ([runtime config](#runtime-config-public_))

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
```

In [development mode](#development-mode), the Go server reverse-proxies all non-API requests to the Vite dev server.

### Production

The generated `Dockerfile.gen` (built by `./schemaf.sh build`, see [Deployment](INSTALL.md#deployment)) includes a Node build stage that compiles the frontend, then embeds the output into the Go binary via `//go:embed`. No separate frontend container needed.

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
- `/openapi.json` — OpenAPI 3.0 spec: exactly `gen/openapi.json`, compiled into the binary by codegen (see [Code Generation](#code-generation))

All of them except `/openapi.json` are part of `openapi.json` (`GET /api/health`, `GET /api/status`, `GET /api/config`, `GET /api/user/me` and the auth routes), so the generated TypeScript and Go clients can call them with types, and the [route gate](#routing) lets them through. `/openapi.json` is the one route outside `/api/`.

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
- no spec is set at all: wire the generated provider in `go/main.go`, `app.AddApi(api.Provider)`;
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
- **Directories**: config and data live in `~/.<name>/dev/etc/` and `~/.<name>/dev/var/` in development, `~/.<name>/etc/` and `~/.<name>/var/` otherwise.
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
    ID string `path:"id"`
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
- Decodes the request (path params, query params, JSON body — or another [content type](#content-types)) into the request type
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

**Request and response types** can be declared anywhere: in `go/api/`, in another package of your module, or in a dependency, directly or through an alias. Keep your domain types in their own package and share them between the API, the database layer and CLIs:

```go
// go/api/users.go
type UserResp = obj.User // alias to a type in your obj package

type ListUsersResp struct {
    Users []obj.User `json:"users"`
}
```

Codegen type-checks `go/api/` (its `*.gen.go` files ignored) and builds each schema from the type's JSON encoding, as `encoding/json` produces it:

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
| named non-struct type, e.g. `type Role string` | the underlying type |
| type implementing `encoding.TextMarshaler`; field with `json:",string"` | `string` |
| `any`, interface, `json.RawMessage` | any JSON value |

A POST, PUT or PATCH request has a body when its type has at least one JSON field; GET and DELETE requests never have one. A response whose type is a struct without JSON fields has no content.

Codegen is strict, and each of these is a codegen error that names the type:

- two types with the same name from different packages, e.g. `api.Address` and `obj.Address`: rename one
- a type implementing `json.Marshaler` (other than the ones in the table): codegen can't know its JSON shape. If the type encodes as a JSON string (an enum, an ID), implement `encoding.TextMarshaler` instead: `encoding/json` then encodes it as a string, and its schema is `string`. Otherwise use a plain struct in the API
- a type without a JSON shape (channel, function) or a generic type
- a POST, PUT or PATCH request type that is not a struct, e.g. `[]obj.User`: a request body is a JSON object
- a type error in `go/api/` or a package it imports

**What codegen does:**
- Scans all structs in `go/api/` that implement the endpoint interface
- Generates `go/api/endpoints.gen.go` with `api.Provider` (handler registration)
- Generates `gen/openapi.json` — OpenAPI 3.0 spec
- Generates `frontend/src/api/generated/api.gen.ts` — type-safe TypeScript client

You write the struct. Everything else is generated or framework-provided.

### Error responses

Every error response from the API has the same shape, whichever layer answered: a typed endpoint, a [raw endpoint](#raw-endpoints), an identity provider's `Exchange`, `OnLogin`, or schemaf itself (bad request body, auth middleware, login checks). A client never has to care where an error came from.

```json
{"error": "an organisation needs at least one head", "code": "last_head"}
{"error": "Internal Server Error", "code": "internal", "id": "3f9a1c0be47d2a16"}
```

| Field | Present | Content |
|---|---|---|
| `error` | always | the message (see [Message](#message)) |
| `code` | always | machine-readable; the client branches on it (see [Code](#code)) |
| `id` | on 5xx only | 16 hex characters; the full error is logged under it |

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
| (none: written by schemaf when `Content-Type` is not in `Consumes()`, see [Content types](#content-types)) | 415 Unsupported Media Type | `unsupported_media_type` |
| `api.ErrTooManyRequests` | 429 Too Many Requests | `too_many_requests` |
| (no sentinel) | 500 Internal Server Error | `internal` |
| `api.ErrUnavailable` | 503 Service Unavailable | `unavailable` |

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
- **5xx** are the server's fault, and their message is withheld: it is always the status text (`"Internal Server Error"`, `"Service Unavailable"`), whatever the error says. The response carries an `id`, and the full error is logged under it. Messages tend to leak internals (driver errors, SQL, paths); codes don't.
- **schemaf's own 401s** (auth middleware) say `"Unauthorized"` and nothing more. The reason (missing, expired, malformed, bad signature, revoked session) is logged, never sent to the client.

#### Misuse

Misusing the error API is a 500 with code `internal` and an `id`, never a panic (a panic drops the request without a response). The log entry names the problem and the fix. Misuse is:

- `api.Error` with a `nil` sentinel, or with an error that is not one of the sentinels above
- an empty or invalid code, from any source
- a default code with the wrong status
- `Retry-After` on a status other than 429 or 503, or with a duration ≤ 0

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

#### OpenAPI

`openapi.json` has one shared schema, `components.schemas.Error`: `error` (string, required), `code` (string, required; its description lists every default code from the [status table](#status), schemaf's own `method_not_allowed`, `not_acceptable` and `unsupported_media_type` included) and `id` (string, 5xx only). Every operation, raw endpoints and schemaf's auth routes included, has a `default` response referencing it, so the generated TypeScript and Go clients type the error body.

`Error` is a reserved schema name, like the `Schemaf…` schemas of the auth routes: a type in `go/api/` that would appear in the spec under one of these names is a codegen error, never a silent overwrite.

#### Upgrading: error responses

Before v1.13.0, an error response was `{"error": "<message>"}`. Now:

- Every error response has a `code`; branch on it rather than on the message.
- 5xx messages are withheld: the message is the status text, and the response carries an `id` to find the full error in the log.
- schemaf's own 401s no longer say why; the reason is in the log.
- `openapi.json` declares errors as one `default` response referencing `Error`, instead of description-only `400`/`401`/`500` responses.

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

- **Per-endpoint**: `Auth()` returns `api.AuthRequired`, `api.AuthOptional` or `api.AuthNone` (see [Auth levels](#auth-levels)). `/api/health`, `/api/status`, `/api/config`, `/openapi.json` and the login routes are always open; `/api/user/me` and `/api/auth/refresh` require auth.
- **Optional auth**: at `api.AuthOptional` the handler also runs for anonymous callers, and `api.Subject(ctx)` tells it whether someone is signed in. Present but invalid credentials still give 401 (and clear a dead cookie); the `Origin` check applies to cookie requests as usual.
- **One token, two transports**: schemaf's token (an HMAC-SHA256 JWT) is accepted from the session cookie or from `Authorization: Bearer <token>`. Browsers use the cookie; scripts and other servers use Bearer. If both are present, Bearer wins.
- **Signing key is auto-managed**: generated on first boot and stored in Postgres (`_schemaf_config`). Nothing to configure, no secret to manage.
- In a handler, `api.Subject(ctx)` returns the authenticated subject; `/api/user/me` returns the current user.

Every authenticated request is verified against schemaf's own token and session only. How a user *proves* who they are (Auth0, passkeys, a login code) happens once, at login, through an [identity provider](#identity-providers) or [your own login endpoint](#starting-sessions-from-your-own-code). After that, endpoints and middleware never see the provider. Rights and roles are your app's concern: schemaf only tells you *who* is calling.

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

schemaf ships one built-in provider, [`auth0`](#activating-auth0), which is switched on by environment variables alone. Your own providers are registered in `main.go` (see [Writing your own provider](#writing-your-own-provider)).

#### Login routes

| Route | What it does |
|---|---|
| `POST /api/auth/{provider}/login` | Calls the provider's `Exchange`, then `OnLogin` (if set), then starts a session. Sets the cookie and responds `{"subject": "...", "token": "..."}`. The token is always returned, for Bearer clients. An unknown or unconfigured provider gives 404 with code `not_found` and the message `provider not configured`. |

All auth routes are in `openapi.json`, so the generated TypeScript and Go clients have them. `POST /api/auth/auth0/login` is always in the spec, with the body `{"id_token": string}`, whether Auth0 is configured or not: the build output does not depend on deployment config. Your own providers are reached through the generic `POST /api/auth/{provider}/login`, whose body is a free-form JSON object.

#### Mapping identities to subjects: `OnLogin`

By default the subject is the provider's subject, unchanged: `api.Subject(ctx)` returns e.g. the Auth0 `sub` (`google-oauth2|1234...`). Auth0 subjects already carry the upstream connection, so they don't collide.

To map identities to your own users, register an optional `OnLogin` hook in `main.go`:

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

Implement `api.IdentityProvider` in your project (e.g. `go/auth/passkey.go`) and register it in `main.go`:

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
- **The cookie**: named `<name>_session` after your project (`myapp_session`), so apps on different localhost ports don't overwrite each other's cookie. Host-only (no `Domain`), `HttpOnly`, `SameSite=Lax`, `Path=/`, lasting until the session expires, and `Secure` outside [development mode](#development-mode). `Lax` rather than `Strict` so the cookie is sent when another site redirects the browser to you, as in an OIDC `/authorize` redirect.
- **CSRF protection**: a cookie-authenticated request with any method other than `GET` or `HEAD` must carry an `Origin` header matching the request's host (`Host`, or `X-Forwarded-Host` behind a proxy). A mismatched **or missing** `Origin` is rejected with 403. Browsers send `Origin` on every such request; non-browser clients should use Bearer, which is exempt.
- **Login CSRF protection**: `POST /api/auth/{provider}/login` (every provider, built-in and custom) is rejected with 403, and no cookie is set, when it carries an `Origin` header that doesn't match the request's host (same rule as above). Otherwise a malicious site could post the attacker's own credential and sign the visitor in as the attacker. A login without `Origin` (curl, server-to-server) is allowed, since it can't be a cross-site browser request.
- **Cleanup**: the server (not CLI commands) deletes sessions that expired or were revoked more than 7 days ago, at start and then daily. Both values are framework constants. Subject-wide revocations (`_schemaf_revocations`) are kept.

Built-in routes:

| Route | What it does |
|---|---|
| `POST /api/auth/logout` | Ends the current session and clears the cookie. Responds 204, also when there is no session. |
| `POST /api/auth/refresh` | Requires auth with a session token (a stateless token gives 400). Returns `{"token": "..."}`, a fresh token for the current session (and renews the cookie if the request used it). |

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
   - Generates type-safe Go query functions → `go/db/db.gen.go`, `go/db/models.gen.go` and one `go/db/<file>.sql.gen.go` per query file
   - Removes sqlc's earlier output first, so nothing stale survives (see [Queries](#queries))

2. **Migrations → db.Provider**
   - Auto-discovers `go/db/migrations/*.sql`
   - Generates `go/db/migrations.gen.go` with `db.Provider` function
   - Provider returns embedded migrations to framework

3. **Endpoint structs → api.Provider + OpenAPI spec**
   - Auto-discovers endpoint structs in `go/api/*.go`
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

6. **Inputs → gen/inputs.sha256**
   - One SHA-256 over codegen's inputs: `go/api/*.go` (without `*.gen.go`), the files in `go/db/migrations/` and `go/db/queries/`, `schemaf.toml`, `compose/*.yml`, the files matched by the globs in `gen/schema-sources` (without `*.gen.go` and `*_test.go`), and the schemaf version required in `go/go.mod`
   - So editing a type in another package that the API uses, e.g. a field of `obj.User`, makes the generated files stale, while editing a package the API doesn't use does not
   - Computed as: for each input file in byte-wise sorted path order, the line `<sha256 of the file>  <path>` (as `sha256sum` prints it); then the line `schemaf <version>`; the hash of that text is the file's content. Dotfiles are not inputs
   - `./schemaf.sh build` and `./schemaf.sh test` recompute it (with `sha256sum`, or `shasum -a 256` on macOS) and stop on a mismatch: `generated files are stale — run ./schemaf.sh codegen and commit`

7. **Empty inputs → gen/empty-inputs**
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

**One exception: `go/frontend_dist/.gitkeep`.** `frontend.gen.go` embeds `go/frontend_dist/`, which the frontend build fills at image build time (`Dockerfile.gen`). Its contents aren't known at codegen time, so codegen can't pick a variant; it creates `go/frontend_dist/.gitkeep` instead, so the embed always matches. Commit it.

**Zero configuration.** Just run `./schemaf.sh codegen` and all the glue code appears. Generated files are committed: `build` runs no codegen and needs no Go, so a commit must carry generated files that match its inputs.

## Testing

**Running tests:**
```bash
./schemaf.sh test                  # check generated files are fresh, then run all tests
./schemaf.sh test --verbose        # verbose go test output
./schemaf.sh test --no-cache       # bypass test cache
```

`./schemaf.sh test` runs no codegen. It first checks `gen/inputs.sha256` (see [Code Generation](#code-generation)) and stops if generated files are stale — `run ./schemaf.sh codegen and commit` — then runs `go test ./go/...` and `npx tsc --noEmit`. Tests therefore always run against generated code that matches its inputs, and CI catches a commit that forgot codegen.

The tests run against an ephemeral stack (`compose.test.gen.yml`: Postgres without a volume, plus your project services) started with the selected [container runtime](INSTALL.md#container-runtime): podman by default, Docker with `SCHEMAF_RUNTIME=docker`. It runs the same runtime checks as `dev` and `run`, and stops with a clear message if a tool is missing.

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
// skip: requires clock compose service
export async function testClockTime(baseUrl: string) { ... }
```

The generated Go wrapper will call `t.Skip(...)` with that message.

### Testing the framework under both runtimes

For work on schemaf itself (`make test <unit|db|e2e|all>`): `e2e/db-test.sh` and `e2e/build-example.sh` follow `SCHEMAF_RUNTIME` like everything else (default podman), and the release gate (`make release`) runs under podman only. Nothing beyond `make release` is required. To try the Docker path anyway:

```bash
SCHEMAF_RUNTIME=docker make test e2e
```

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
