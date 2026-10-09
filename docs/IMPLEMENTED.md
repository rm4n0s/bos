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

## Step 2 – Command language, pure part
- Branch: feat/command-parser
- Last code commit: 199b2c8
- Date: 2026-10-09
- Delivered: `internal/command` with the lexer (SPEC §6.1; positions count runes), the recursive-descent parser and AST (§6.2), the restriction rules S1–S8 and T1–T5 (§6.3) each with its own message and token position, the canonical formatter `Format`, and the parsed JSON form `ToJSON`/`FromJSON` (§6.10, key order preserved, 64-bit integers exact, restrictions re-checked on read). `Parse` reports the first lexical error, else the first syntax error, else the restriction violation that comes first in the text; errors carry `rule`, `position` and `token` metadata and `ProblemOf` extracts them for the API. Tests: the valid and invalid tables of §6.12 (the rows that need no catalogue), about 100 more invalid cases covering every rule, spacing equivalence, JSON shapes against the §6.10 example, rejection of malformed JSON, and a fuzz test (parse → format → parse and JSON round trips are stable and the parser never panics; 15 million executions clean). Route tests for `CommandLexError`, `CommandSyntaxError`, `CommandRestrictionViolated` and `CommandJSONInvalid`.
- Checks: gofmt ✔  go vet ✔  golangci-lint ✔  go test -race ✔
- Deviations from the spec:
  - Rule R1 and the semantic rows of the §6.12 invalid table (`characters(nobody)`, `poses(#nosuchtag)`, `where emotions = sad`, `with colour=3`) need the generator or the catalogue; they parse here and are rejected by the validator in step 3.
  - `limit n` is stored as `"limit": n` in the parsed form; §6.10 shows only `sample` and `repeat`. Add `limit` to §6.10.
  - Absent false flags (`is_merged`, `is_zipped`, `is_template`, `is_regioned`) are omitted from the JSON rather than written as `false`. §6.10 should say so.
  - `sample.seed` stays absent in the parsed form until the service stores a generated seed (step 7); the parser is deterministic and does not invent one.
  - A command that repeats a `with` key, template tag or region keyword has no JSON form (a JSON object cannot repeat a key); `ToJSON` fails for it with `CommandJSONInvalid`. The duplicate is a semantic error that the validator reports (step 3, via `DuplicateKey`).
  - `gpu1 - g: …` (missing `$`) is reported as a lexical error (`LEX`) at the `-`; §6.12 gives only the reason "missing `-$`".
  - New error tag `CommandJSONInvalid` (a parsed command that is malformed or violates a restriction when read back). Add it to the Command-language row of §17.2 (HTTP 422).
