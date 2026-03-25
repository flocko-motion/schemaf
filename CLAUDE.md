# CLAUDE.md — schemaf

> **⚠️ SPECIFICATION-FIRST PROJECT**  
> This project is designed **documentation-first**. The README is the source of truth.  
> **DO NOT modify the architecture without first discussing and implementing changes to the documentation together with the user.**  
> All design decisions are made through pair programming and captured here before implementation.

## MANDATORY: Use td for Task Management

**td** is the task system for this project (part of the [Sidecar](https://sidecar.haplab.com) workflow). All state lives in `.todos/db.sqlite` — local only, never transmitted.

### Session Start

```
td usage --new-session
```

Run this at the start of every conversation (or after `/clear`). It shows current state and assigned work. Use `td usage -q` after the first read to suppress the banner.

Optional session naming:
```
td session "name"      # label the current session
td session --new       # force a new session in the same terminal context
```

### During a Session

Log progress as you work — don't wait until the end:

```
td log "what you did"                        # completed work
td log --decision "why this approach"        # architectural choices
td log --blocker "what's blocking"           # impediments
td link <issue-id> [files]                   # track modified files
```

Key navigation commands:
```
td next                  # highest priority open issue
td focus <issue-id>      # set active issue
td start <issue-id>      # transition open → in_progress
td context <issue-id>    # view handoff state from prior session
```

### Session End / Before Context Runs Out

Always hand off before the context window fills:

```
td handoff <issue-id> \
  --done "completed and tested work" \
  --remaining "specific pending tasks" \
  --decision "why the approach was chosen" \
  --uncertain "open questions for next session"
```

### Review Workflow

```
td review <issue-id>              # submit for review
td reviewable                     # list issues awaiting review
td approve <issue-id>             # approve and close
td reject <issue-id> --reason ""  # reject with feedback
```

**Critical**: The session that implements code cannot approve it. Review must come from a different session (human or separate agent).

### Creating Work

```
td create "description" --type [feature|bug|chore|docs|refactor|test] --priority [P0-P3]
td epic create "name" --priority [P0-P3]
td dep add <issue> <depends-on>   # declare dependency
td critical-path                  # optimal work sequence
```

## Documentation-First

**The documentation (`README.md`, `INSTALL.md`, `EXTEND.md`) is the source of truth.**

This project is designed documentation-first. Features are designed by writing the docs _before_ implementation. If the docs don't describe it, the feature doesn't exist yet.

- Read `README.md`, `INSTALL.md`, and `EXTEND.md` before touching any code
- If implementation diverges from the docs, the docs win — fix the code, not the docs
- Never modify the architecture without first discussing and updating the documentation together with the user

## Pair Programming Approach

**Architectural decisions require discussion.**

Do not make architectural changes unilaterally. This includes:
- Adding new packages or top-level directories
- Changing the provider pattern or how framework components are wired
- Changing codegen output paths or naming conventions
- Adding new CLI commands to the framework
- Changing port assignments or compose layout
- Changing the normative project structure
- Changing how framework and project compose files are merged

When you identify a need for an architectural change: surface it, propose it, and wait for the user to agree before implementing. Treat design decisions as pair programming — think out loud, discuss, then act.

## Repository Structure

```
api/             - API registry + OpenAPI generation
schemaf/         - App lifecycle (schemaf.New, app.Run)
cli/             - CLI framework (subcommands, config/state)
compose/         - Compose dependency resolver (x-schemaf metadata)
db/              - Database helpers + migrations
ai/              - AI provider integrations
log/             - Logging
cmd/schemaf/     - CLI entrypoint
gateway/         - nginx gateway (Dockerfile + config)
example/         - Example project consuming the framework
```

## Normative Conventions

These are framework-wide conventions. Do not deviate.

**Generated files** use `.gen.` infix: `*.gen.go`, `*.gen.ts`

**Project layout** (enforced, not configurable):
- `db/migrations/` → input migrations
- `db/queries/` → sqlc input
- `db/migrations.gen.go` → generated `db.Provider`
- `db/queries.gen.go` → generated sqlc query functions
- `api/*.go` → endpoint struct implementations
- `api/endpoints.gen.go` → generated `api.Provider`
- `frontend/src/api/generated/api.gen.ts` → generated TypeScript client
- `compose.gen.yml` → generated base compose (postgres + backend)
- `compose.dev.gen.yml` → generated dev overlay (exposed ports)
- `compose/*.yml` → project-specific compose extensions

**Port allocation** (fixed):
- 7000 — application server
- 7002 — frontend dev server
- 7003 — Postgres
- 7010+ — project-specific services

**Config file**: `schemaf.toml` (two fields only: `title`, `name`)

**Secrets**: stored in `~/.<name>/etc/env` (prod) and `~/.<name>/dev/etc/env` (dev) — never in project directories

## Code Style

- Minimize configuration — convention over configuration everywhere
- Auto-discover files; never require manual registration
- Provider pattern for wiring: `app.AddDb(db.Provider)`, `app.AddApi(api.Provider)`
- Services registered via `app.AddService()` only run for `server`/`dev` commands — keep CLI commands fast
- Do not add glue code that the user should not have to write — that's what codegen is for

## Golden Rule

> If it can be generalized, put it in schemaf. If arbitrary decisions need to be made: decide them normatively in the framework. Leave only creative decisions to the application layer.

When in doubt about whether something belongs in the framework or the application: cement it in the framework.

## Running Tests

The `/example` directory contains an example project built using SchemaF. Run `/example/codegen.sh` to generate code (including `test.gen.sh`) and then run `test.gen.sh` to execute the tests.
Don't run tests directly unless you're debugging a specific issue. The `test.gen.sh` script handles dependencies and ensures consistent test execution - it executes both unit and integration tests, 
including tests written in golang as well as in TypeScript (wrapped in golang). Only running `teste.gen.sh` guarantees that all tests are executed consistently.
