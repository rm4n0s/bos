# CLAUDE.md

Guidance for Claude Code when working in this repository (Go, hosted on GitHub).

## Commands
- Build: `go build ./...`
- Test all: `go test -race ./...`
- Test one package: `go test ./internal/foo -run TestName -v`
- Vet: `go vet ./...`
- Lint: `golangci-lint run`
- Format: `gofmt -w .` (or `goimports -w .`)
- After changing dependencies: `go mod tidy`

## Definition of done
Before reporting a task as finished, run `gofmt`, `go vet ./...`, `golangci-lint run` and `go test -race ./...`, and fix anything they report. Say so explicitly if any step was skipped or failed.

## Layout
- `cmd/<app>/` holds entry points; keep `main.go` thin
- `internal/` holds application code
- Tests live next to the code (`foo_test.go`)

## Go conventions
- Never ignore a returned error; handle it or return it
- Pass `context.Context` as the first parameter of anything that does I/O
- Tests are table-driven
- No global mutable state; inject dependencies through constructors
- No hand-written SQL: queries go through gorm's builder and the schema through numbered Go migrations (`internal/store`)
- Prefer the standard library. Ask before adding a new dependency (exception: `github.com/rm4n0s/errors`, see below)

## Error handling
Use `github.com/rm4n0s/errors` for all errors. Never use `fmt.Errorf`, and never import the standard library `errors` package: this package re-exports `errors.Is`, `errors.As` and `errors.Unwrap`, so a single import covers everything.

**Creating errors**
- New failure with a fixed message: `errors.New("Tag", "message", "key", value, ...)`
  - Tag: short PascalCase name of the failure, e.g. `UserAlreadyExists`
  - Trailing arguments are key/value metadata pairs, e.g. `"email", user.Email`
- New failure with a formatted message: `errors.NewErrf("Tag", "format %s", arg)`
- Wrapping an error that is NOT already an `*errors.Error` (stdlib, database driver, third-party): `errors.NewErr("Tag", err)`. Add metadata by chaining `.SetMetadata("key", value)`
- Every `New`, `NewErr` and `NewErrf` call captures the stack at that point

**Propagating errors**
- If the error already came from this package, return it untouched: `return err`. Re-wrapping it would replace the Tag and the stack and break route tests
- Functions return the plain `error` interface, never `*errors.Error` (avoids the non-nil-interface-holding-nil-pointer bug)
- `errors.FromError(err)` is a plain type assertion. It fails if something else wrapped the error, so don't wrap these errors with anything

**Inspecting errors**
- `appErr, ok := errors.FromError(err)`, then use `appErr.Tag`, `appErr.Metadata`, `appErr.Message`
- Use `errors.Is` / `errors.As` / `errors.Unwrap` for the underlying cause stored in `OriginalErr`

**Testing errors**
- Every new failure case gets its own unique Tag and a test that asserts the route, not just that an error occurred:
  ```go
  appErr, ok := errors.FromError(err)
  if !ok {
      t.Fatalf("expected *errors.Error, got %T", err)
  }
  if !appErr.HasRoute("UserService.CreateUser->UserRepository.Save.UserAlreadyExists") {
      t.Errorf("unexpected failure path: %s", appErr.Route())
  }
  ```
- A route lists the call chain from outermost to innermost (`Type.Method` or `Func`, joined by `->`) and ends with the Tag. `HasRoute` matches by substring
- Log errors with `appErr.ToJson()` so they can be replayed in a test via `errors.FromJson`

## Git and GitHub
- Never commit or push directly to `main`; work on a branch named `feat/...` or `fix/...`
- **Exception:** if `docs/IMPLEMENTATION.md` exists, follow its Rules section for the steps it lists. Each step is done on its own branch, one at a time, and a finished step (all four Definition-of-done checks passing) is published to `main` by a fast-forward merge and push, without a PR, after its entry is added to `docs/IMPLEMENTED.md` on that branch. Never commit work for a step directly on `main`. Everything not covered by that file (other changes, fixes outside the listed steps) still follows the rule above
- Commit messages: imperative mood, e.g. "Add retry to client"
- Use the `gh` CLI for PRs, issues and CI: `gh pr create`, `gh pr view`, `gh run list`
- PRs: short description, link the issue (`Closes #123`), keep them small
- CI runs in GitHub Actions (`.github/workflows/`); make sure it passes before marking a PR ready

## Don't
- Don't edit generated files (`*.pb.go`, `*_gen.go`); regenerate them with `make generate`
- Don't commit secrets or `.env` files

## Project notes
Bowl of Scenes generates images (or video / 3D) in bulk: the user combines reusable prompt items from groups with a one-line command language, and the server runs every combination as a job on one or more ComfyUI servers.

**Documents**
- `docs/SPEC.md`: the specification. Part I is behaviour, Part II is the Go / REST / Datastar binding. If code and spec disagree, stop and ask which is right, then fix the wrong one
- `docs/IMPLEMENTATION.md`: build order and working rules. `docs/IMPLEMENTED.md`: log of finished steps. Always check the log to know where the project stands

**Stack** (approved dependencies only): Go, `net/http`, SQLite via `gorm.io/gorm` with `github.com/glebarez/sqlite` (pure Go, no cgo), `html/template`, Datastar with `github.com/starfederation/datastar-go`, JSON configuration, `rm4n0s/errors`, `rm4n0s/comfyui-client-go`, `rm4n0s/gocivitailib`. Everything else is the standard library

**Architecture**
- `internal/service` holds the use cases. `internal/api` (REST) and `internal/ui` (Datastar) are thin front ends over it; the UI never calls the REST API
- `internal/command` (lexer, parser, validator, expander) and `internal/domain` are pure: no I/O, no project imports. Keep them that way so they test without a database
- ComfyUI, Civitai and the store sit behind small interfaces defined next to their users; tests use fakes
- Item order is creation order (ascending id) and the last term of a product varies fastest, so job order is deterministic

**Libraries**
- `comfyui-client-go` and `gocivitailib` use standard-library errors. Wrap their errors with `errors.NewErr` at the adapter boundary so no foreign error crosses a port
- ComfyUI: one WebSocket per client id, and a prompt's events go only to the client that queued it. One `Client` per server, with exactly one `Listen` consumer (the event hub). The library never reconnects; recover prompts after a reconnect by polling history and queue
- `QueuePrompt` can return a prompt id together with a `*NodeErrorsError`: the prompt is queued, record the errors as warnings. With no id, it was rejected
- Workflow node titles the app addresses must be unique; the library also matches a title against `class_type`
- Uploaded workflow input files are named by content hash and referenced with `UploadResult.InputValue()`
- Datastar answers are server-sent events (`datastar-patch-elements`, `datastar-patch-signals`). The Datastar script is embedded, never loaded from a CDN. State-changing `/ui/` routes require the `Datastar-Request: true` header

**Out of scope on purpose**: person segmentation for masks (colour masks and coordinates only), a category entity (use tags), Civitai as a generation backend (proposal only, SPEC §19.5)

**Testing**: fake ComfyUI with `httptest` plus a WebSocket endpoint; in-memory SQLite for the store; table tests from SPEC §6.12 and a fuzz test for the parser; every error tag in SPEC §17.2 has a route test

