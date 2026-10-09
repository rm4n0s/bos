# Implementation plan

Companion to [SPEC.md](SPEC.md). Each step is one `feat/…` branch and one small pull request. A step is done when `gofmt`, `go vet ./...`, `golangci-lint run` and `go test -race ./...` pass. Every step that adds a failure case also adds its error tag (SPEC §17.2) and a test that asserts the route. Dependencies are limited to the approved set in SPEC §14.4: `gorm.io/gorm`, `github.com/glebarez/sqlite`, `github.com/starfederation/datastar-go`, `rm4n0s/errors`, `rm4n0s/comfyui-client-go`, `rm4n0s/gocivitailib`. Anything else needs approval first.

## Rules

1. **One step at a time.** Steps are done strictly in order, never in parallel. The next step starts only after the previous one has been published to `main`.
2. **One branch per step.** All work for a step happens on its own branch (named in the step heading). Nothing for a step is committed to `main` directly.
3. **Publish when finished.** A step is finished when `gofmt`, `go vet ./...`, `golangci-lint run` and `go test -race ./...` all pass. Only then is the branch published to `main` (fast-forward merge, then push). If any check fails or is skipped, the step is not published; the failure is reported and fixed first.
4. **Record every finished step** in [IMPLEMENTED.md](IMPLEMENTED.md). The entry is committed on the step's branch, so it reaches `main` together with the code it describes.
5. **Tests first.** Read the relevant spec section, write the tests, then the code. Every failure case added gets its own error tag and a route test (SPEC §17.1).
6. **Dependencies** are limited to the approved set above; anything else needs approval first.

A step entry in `IMPLEMENTED.md` contains: the step number and title, the branch, the last code commit (the commit just before the entry, because an entry cannot contain the hash of its own commit), the date, what was delivered, the result of the four checks, and any deviation from the spec (with the reason, and the spec section that should be updated).

## Step 1 – Project skeleton (`feat/skeleton`)

- `go.mod` for `github.com/rm4n0s/bos` on Go 1.26.3; the `cmd/bos` and `internal/…` layout of SPEC §14.2.
- `golangci-lint` configuration and a GitHub Actions workflow running the four checks.
- A thin `main`: load the JSON configuration (SPEC §2), open SQLite, serve `/healthz`, shut down cleanly.
- Replace the Qt/miqt project notes in `CLAUDE.md` with notes that describe this project.
- Add the approved dependencies.

## Step 2 – Command language, pure part (`feat/command-parser`)

No database needed.

- Lexer, EBNF parser and AST (SPEC §6.1–6.2).
- Restriction rules S1–S8, T1–T5, R1 (§6.3), each with its own error and token position.
- Parsed JSON form and round-trip (§6.10).
- Tests: the valid and invalid tables of §6.12; a fuzz test that the parser never panics and that parse → format → parse is stable.

## Step 3 – Command language, evaluation (`feat/command-expand`)

- A read-only catalogue interface (groups, items, tags) so no database is needed yet.
- Semantic validator collecting all errors (§6.11).
- Item selectors (names, patterns, tags), literals, merge, zip, templates, regions, the product, `where`, `limit`/`sample`, `repeat`, and `spread` assignment (§6.4–6.9).
- Tests: job counts and ordering for every example in §6.12; property tests for the count formulas.
- This is the riskiest logic, so it comes before storage and UI.

## Step 4 – Domain and store (`feat/store`)

- Domain types and numbered Go migrations (gorm) with a version table.
- Repositories for servers, generators, fixers, groups, items, tags, projects, commands and jobs.
- Atomic job claiming, the delete cascades of SPEC §7.4, and the uniqueness rules.
- Tests: in-memory SQLite, including concurrent claiming under `-race`.

## Step 5 – Workflow package (`feat/workflow`)

- Output-type analysis (§3.2).
- Attribute and seed edits (§6.8, §8.2), working on a copy of the graph.
- LoRA, ControlNet, IP-Adapter and region injectors (§8.2).
- Flat-colour mask generation (§7.3).
- Tests: golden-file tests using the eleven example workflows from the original repository.

## Step 6 – ComfyUI adapter and event hub (`feat/comfy`)

- The `comfy.Client` port and its adapter over `comfyui-client-go` (SPEC §18.2).
- One event hub per server connection: routing by prompt id, buffering early events, reconnecting with a new client, and recovering prompts by polling history and queue (§18.3).
- Content-hash uploads and handling of node errors that still queue a prompt (§18.6, §18.7).
- Tests: an `httptest` fake ComfyUI with a WebSocket endpoint.
- Second riskiest step.

## Step 7 – Services (`feat/services`)

- CRUD for servers, generators, fixers, groups, items and projects.
- Commands: add, edit, preview and recreate, creating jobs from step 3.
- Tags: autocomplete, rename and merge (§3.4).
- The error tag catalogue (§17.2) takes shape here, with a route test for each tag.
- Tests: fake store, fake `comfy.Client`, fake clock.

## Step 8 – Scheduler (`feat/scheduler`)

- Ordered event log (§8.5).
- Server monitor, per-server runners, and `dynamic`, `even`, `blocks` and `by` assignment (§6.9, §8.7).
- Grace period, retries after server loss, workflow-failure handling, fixer steps including `on <server>`, and recovery of jobs after a restart.
- Tests: fake clients with controllable latency and failures.

### Milestone A

One command runs end to end against a real ComfyUI, driven by a temporary test harness. Stop here and verify before building the interfaces.

## Step 9 – REST API (`feat/api`)

- Conventions, error envelope and tag-to-status table, pagination, uploads, safe file serving, optional bearer token, and the events endpoint (SPEC §15).
- Routes added in slices: catalogue; then commands and jobs; then REPL and downloads.
- OpenAPI document and a test that every registered route is listed in it.
- Tests: `httptest` round trips for every route.

## Step 10 – Web UI (`feat/ui-*`, one branch per slice)

- Page shell with the embedded Datastar script, CSRF checks, escaping, and security headers (SPEC §16).
- Catalogue screens (servers, generators, fixers, groups, items, projects).
- Command editor with live preview, and the command list.
- Job grid with live updates over server-sent events.
- REPL and toasts.
- Tests: handlers against `httptest`, asserting the emitted events.

## Step 11 – Civitai (`feat/civitai`)

- The `civitai.Models` adapter over `gocivitailib` and the error mapping of SPEC §19.4.
- Copying LoRAs into ComfyUI, and group and item import.

## Step 12 – Hardening and acceptance (`feat/hardening`)

- Graceful shutdown, structured logging, and the tag-by-tag error scenarios still missing.
- A full run of acceptance tests 1–41 against the fake ComfyUI, then a smoke test against a real ComfyUI.
- README with setup and first-run guide.
