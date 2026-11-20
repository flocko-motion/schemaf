# CLAUDE.md — schemaf

> **⚠️ SPECIFICATION-FIRST PROJECT**  
> This project is designed **documentation-first**. The README is the source of truth.  
> **DO NOT modify the architecture without first discussing and implementing changes to the documentation together with the user.**  
> All design decisions are made through pair programming and captured here before implementation.

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

## Working on an Issue: One Worktree per Issue

Several agents work on this repository at the same time. The main checkout belongs to the maintainer and the planner (reviews, commits, releases). **Never implement an issue in the main checkout.** Each issue gets its own git worktree, so changes from different issues never mix in one working tree.

**When to ask:**
- **Explicitly asked to implement a specific GitHub issue** (e.g. "implement #53"): that order includes creating the issue's worktree and branch. Create them per the scheme below **without asking**, and say which ones you created.
- **Anything else** (no issue number, unclear which issue, work across several issues, or you think the scheme doesn't fit): **propose exactly this name, branch and base and wait for confirmation** — e.g. "I'll create `.worktrees/53-error-responses/` on branch `feat/53-error-responses` from `origin/main` — OK?".

Never invent other names or bases; deviate only if the maintainer says so.

**Naming scheme:**

| What | Pattern | Example |
|---|---|---|
| Worktree directory | `.worktrees/<issue>-<slug>/` in the repo root | `.worktrees/53-error-responses/` |
| Branch | `<type>/<issue>-<slug>` | `feat/53-error-responses` |
| Base | a freshly fetched `origin/main` | |

- `<issue>` is the GitHub issue number. `<slug>` is 2–4 lowercase words from the issue title, joined with `-`.
- `<type>` is `feat`, `fix`, `docs` or `chore`, matching the commit type the change will get.
- Create it from the repo root:
  ```bash
  git fetch origin
  git worktree add -b feat/53-error-responses .worktrees/53-error-responses origin/main
  cd .worktrees/53-error-responses
  ```
- If the worktree or branch already exists (an earlier session on the same issue), continue in it and say so — don't create a second one.
- Do all work for the issue inside its worktree: edits, tests, docs. `.worktrees/` is gitignored.
- Work that isn't tied to an issue still gets a worktree (proposed the same way); use a descriptive slug without a number (e.g. `.worktrees/release-gate-docs/`, branch `docs/release-gate-docs`).
- The usual git rules still apply inside the worktree: don't stage, don't commit, don't push unless the maintainer asks. Tell the maintainer the worktree path when the work is ready for review.
- Ports: to run a project's dev stack in a worktree while another checkout runs one, use `./schemaf.sh dev all --instance <issue>` (see INSTALL.md › Parallel dev instances). The test layers (`go test`, `e2e/db-test.sh`, the onboarding e2e) use their own ports and project names and can run in parallel.
- Clean-up after the branch is merged: `git worktree remove .worktrees/<name>` and `git branch -d <branch>`. Only the maintainer (or an agent the maintainer asks) removes worktrees.

## Repository Structure

```
api/             - API registry + OpenAPI generation
schemaf/         - App lifecycle (schemaf.New, app.Run)
schemaftest/     - Test harness: start the app in-process on its own database (schemaftest.Start)
internal/        - Framework-only hooks between packages; apps can't import them (e.g. internal/isolation: schemaftest's isolated server start)
cli/             - CLI framework (subcommands, config/state)
compose/         - Compose dependency resolver (x-schemaf metadata)
db/              - Database helpers + migrations
ai/              - AI provider integrations
log/             - Logging
cmd/schemaf/     - CLI entrypoint
e2e/             - From-scratch onboarding e2e + DB integration harness
```

## Normative Conventions

These are framework-wide conventions. Do not deviate.

**Generated files** use `.gen.` infix: `*.gen.go`, `*.gen.ts`

**Project layout** (enforced, not configurable):
- `db/migrations/` → input migrations
- `db/queries/` → sqlc input
- `db/migrations.gen.go` → generated `db.Provider`
- `db/db.gen.go`, `db/models.gen.go`, `db/<file>.sql.gen.go` → generated sqlc query code (removed and regenerated on every codegen)
- `main.go` → `schemaf.New` → `app.Wire` → `Run`, nothing else
- `app/app.go` → `func Wire(app *schemaf.App)`: every `Add*`/`On*`/`Set*` call, shared by `main.go` and the tests
- `app/constants.gen.go`, `app/frontend.gen.go` → generated project constants and `FrontendFS()` (package `app`)
- `api/*.go` → endpoint struct implementations
- `api/endpoints.gen.go` → generated `api.Provider`
- `frontend/src/api/generated/api.gen.ts` → generated TypeScript client
- `apiclient/client.gen.go` → generated Go API client (via oapi-codegen)
- `compose.gen.yml` → generated base compose (postgres + backend)
- `compose.dev.gen.yml` → generated dev overlay (exposed ports)
- `compose/*.yml` → project-specific compose extensions
- `compose.test.gen.yml` → generated test stack: project services only, no Postgres
- `api/*_test.go` → unit tests next to the file they test
- `tests/*_test.go` → integration tests that boot the app (`schemaftest.Start(t, app.Wire)`), package `tests`; `tests/*.test.ts` → TypeScript tests

**Port allocation** (base port configurable via `port` in schemaf.toml, default 8000):
- port — application server
- port+2 — frontend dev server
- port+3 — Postgres
- port+10+ — project-specific services

**Config file**: `schemaf.toml` (fields: `title`, `name`, optional `port` — default 8000)

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

There is **no committed example project**. Tests come in three layers:

- **Unit tests** — `go test ./...`. Real-Postgres tests auto-skip when `DATABASE_URL` is unset. They include the stubbed-`gh` tests of `scripts/merge.sh` and `scripts/release.sh` (`scripts/scripts_test.go`).
- **DB integration tests** — `e2e/db-test.sh`. Spins an ephemeral Postgres, exports `DATABASE_URL`, and runs the gated tests in `db/` (migration runner, `RunSet`, reset helper). Tears down automatically. Then runs `schemaftest/`'s own tests with `SCHEMAF_CONTAINER_TESTS=1` (they start their own Postgres container and skip without it).
- **From-scratch onboarding e2e** — `e2e/build-example.sh [<ref>]`. Builds a complete project in `/tmp` from a single `go run …/cmd/schemaf@<ref> init` call against an online ref, then compiles and tests it, runs two dev instances side by side (`e2e/instances.sh`), deploys it as two users (`e2e/deploy.sh`) and checks the runtime data-volume refusal with real volumes (`e2e/volumes.sh`). This is the real onboarding lifecycle and replaces the old in-repo example. It needs a pushed commit, so it runs in **the gate** (see below), not as part of an agent's verification: before hand-off, run the unit and DB layers.
  - `<ref>` is a release tag (`vX.Y.Z`; the default is the latest one), a full 40-character commit hash, or a moving ref (a branch or tag). Release tags and commit hashes are immutable, so everything comes through the Go module proxy, with no override. A moving ref could be served stale by the proxy, so for it the script sets `GOPRIVATE=github.com/flocko-motion/schemaf`: only schemaf bypasses the proxy and sumdb, all other dependencies still come through the proxy. An abbreviated hash, or a name that is neither a release tag, a full hash nor a branch or tag on the remote, is refused. The script prints which mode it uses (`ref '<ref>' is …`).

### The gate: CI is the reference

**The CI gate** (`.github/workflows/gate.yml`, check `gate`) is the reference environment: GitHub Actions on the pinned Linux amd64 runner `ubuntu-24.04`. Two parallel jobs, `podman` and `docker`, each with `SCHEMAF_RUNTIME` set to its runtime, each run the full sequence `make test all` against the commit under test: unit tests, `e2e/db-test.sh`, and the onboarding e2e, which includes the two-user deployment with **two real Unix users** created on the runner (`useradd`, each with its own rootless store under podman) and the data-volume check with real volumes in both runtimes. `gate` is green only if both jobs are. CI runs the oldest Go schemaf supports, the `go` directive in `go.mod`, with `GOTOOLCHAIN=local`, so a dependency that needs a newer Go fails the gate.

- **When**: on every pull request (opened, synchronized, reopened), and manually for any branch: `gh workflow run gate --ref <branch>`. Not on plain pushes.
- **What it tests**: the PR's head commit (for a manual run: the head of the dispatched branch), by its full hash — never GitHub's `refs/pull/<n>/merge` commit, which the Go module proxy can't fetch. `main` requires branches to be up to date, so the head commit is exactly what lands.
- **Caches**: only downloads — the Go module and build caches (keyed by `go.sum`) and the npm cache (keyed by the scaffold's `package.json` template). Never container volumes, images, podman/Docker storage or database state: every run starts from a clean slate.
- The e2e scripts are the single source of truth: the workflow provisions the runner (users, compose provider) and calls them. In CI it sets `E2E_REFERENCE=1`, so a check that can't run fails instead of being skipped, and `E2E_BUILD_USER` / `E2E_RUN_USER` for the two real users.

**Locally**, `make test all` runs the same suite, e.g. to debug on macOS; it is not the reference. There `deploy.sh` simulates the second user with its own `HOME`, and `volumes.sh` skips loudly when the other runtime isn't usable. `make release` runs it on macOS as the local gate (below), because CI doesn't cover macOS.

Container runtime: everything follows `SCHEMAF_RUNTIME` (podman by default, `docker` on request); CI covers both runtimes on every PR, so nothing has to be run by hand. To try the Docker path locally anyway, `SCHEMAF_RUNTIME=docker make test e2e`. Every container call goes through `cli.Container` (Go), `_schemaf_container` (generated `schemaf.sh`) or `rt` (`e2e/runtime.sh`) — never call `docker`/`podman` directly.

## Make Targets, Merging and Releasing

`Makefile` is the entrypoint for working **on** the framework (`schemaf.sh` is for projects that consume it). Read `scripts/` for the details. Merging and releasing are separate: not every merge is a release.

| Target | What it does |
|---|---|
| `make build` | `go build ./...` |
| `make test <unit\|db\|e2e\|all>` | the test layers above; `REF=<ref>` picks the ref for `e2e` |
| `make merge` | per PR, from the feature branch. Refuses on `main`, with a dirty tree, with no commits beyond `main`, or when the branch doesn't contain `origin/main` (`branch is behind main — run: git merge origin/main`). Runs `go build`, `go vet`, `go test ./...`, then pushes the branch, opens the PR (or reuses the open one) and **waits for the CI gate** on the PR's head commit (at most 45 minutes). Green → merges the PR (merge commit). Red, timeout, or no run found → merges nothing and prints the CI run URL; the PR stays open: push a fix and run `make merge` again. No local e2e, no tag |
| `make release <major\|minor\|patch>` | aliases `breaking\|feature\|fix`. Per batch, releases what is on `main`. Refuses unless HEAD equals `origin/main` (after a fetch), and when nothing landed since the last release tag. Runs `go build`, `go vet`, `go test ./...`; **on macOS** also the full local gate (`make test all` under podman against the HEAD commit, by its full hash). Green → tags `vX.Y.Z` and creates the GitHub release. Red → tags nothing; fix a macOS-only break with a PR (`make merge`), then `make release` again. On Linux the fast checks are enough locally: the commit already passed CI in its up-to-date PR. No PR, no merge |

- **`main` only accepts green, up-to-date PRs.** A branch protection rule on `main` requires the status check `gate` and requires branches to be up to date before merging (EXTEND.md › Testing the framework: the CI gate). After each merge, the other open branches must merge `origin/main` before they can merge.
- **A commit that finishes an issue says so in a `Closes #<n>` trailer**: a line of its own in the commit body, exactly that form, one line per issue. `make merge` reads these trailers from the commits it merges, prints the issues before pushing, and puts `Closes #<n>` into the PR body so GitHub closes them on merge; afterwards it checks that each issue really is closed and fails loudly if not. `make release` closes the issues of the released range that are still open (commits that reached `main` without a closing PR), with the comment `Released in vX.Y.Z.` The `(#<n>)` at the end of a subject is a reference only and closes nothing. Near-misses (`Fixes #12`, `closes #12`, `Closes #12, #13`) stop `make merge` before anything is pushed (`make release` warns): reword the commit.
- **The onboarding e2e runs in the CI gate of every PR.** When an issue's "done when" lists the onboarding e2e, the PR's `gate` check satisfies it. Hand off with the unit and DB layers green; a manual gate run (`gh workflow run gate --ref <branch>`) pushes the branch first, so only on the maintainer's order.
- `make merge` and `make release` push, merge and tag on GitHub: run them only on the maintainer's explicit order, with the bump the maintainer picked.
