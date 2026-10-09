# Implemented steps

Log of finished steps from [IMPLEMENTATION.md](IMPLEMENTATION.md), oldest first. A step is added here, on its own branch, when its four checks pass and just before it is published to `main`.

Entry format:

```
## Step N – Title
- Branch: feat/…
- Last code commit: <hash of the commit before this entry>
- Date: YYYY-MM-DD
- Delivered: …
- Checks: gofmt ✔  go vet ✔  golangci-lint ✔  go test -race ✔
- Deviations from the spec: none | … (spec section to update)
```

## Step 1 – Project skeleton
- Branch: feat/skeleton
- Last code commit: 4826b83
- Date: 2026-10-09
- Delivered: `go.mod` (module `github.com/rm4n0s/bos`, Go 1.26.3) and the package layout of SPEC §14.2 (`doc.go` placeholders for packages that get code in later steps). `internal/config`: JSON configuration with `BOS_<FIELD>` environment overrides, defaults and validation that reports every problem at once; `config.example.json`. `internal/files.EnsureDirs`. `internal/store.Open`: `modernc.org/sqlite`, busy timeout, foreign keys, WAL for files, private in-memory databases. `internal/api`: `/healthz`. `cmd/bos`: thin `main`, `serve` (config, directories, store, listener) and `runServer` (graceful shutdown on SIGINT/SIGTERM). `.golangci.yml` (also forbids the standard `errors` package and `fmt.Errorf`) and the GitHub Actions workflow running the four checks. Route tests for `ConfigNotReadable`, `ConfigInvalid`, `FileStoreFailure`, `StoreFailure`, `ListenFailed`, `ServeFailed`, `ShutdownFailed`.
- Checks: gofmt ✔  go vet ✔  golangci-lint ✔  go test -race ✔
- Deviations from the spec:
  - Only `modernc.org/sqlite` and `rm4n0s/errors` are in `go.mod`. The other approved dependencies are added in the step that first imports them (6, 10, 11), because `go mod tidy` removes unused requirements. No spec change needed.
  - New error tags not in SPEC §17.2: `ConfigNotReadable` (configuration file cannot be read), `ListenFailed`, `ServeFailed`, `ShutdownFailed` (HTTP server lifecycle). Add them to the Infrastructure row of §17.2.
  - `CLAUDE.md` already contained the project notes (no Qt/miqt text left), so nothing was replaced.
