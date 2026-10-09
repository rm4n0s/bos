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

## Step 3 – Command language, evaluation
- Branch: feat/command-expand
- Last code commit: 4b395c9
- Date: 2026-10-09
- Delivered: in `internal/command`: the read-only `Catalogue` interface and its view types (the package stays free of project imports, so the service maps domain entities to them in step 7); the semantic validator `Validate` (SPEC §6.11) that collects every problem with its span and tag; the evaluator for item selectors (names, `%`/`?` patterns, case-insensitive tags, include and exclude), literals, merge, zip (fused items), templates (placeholder replacement, LoRA merge, de-duplication) and regions (prompt sets); the product with the last term varying fastest; `where` compiled to per-dimension truth tables; `limit`, `sample` (own SplitMix64 + Floyd sampling, so a stored seed reproduces the selection on any Go version) and `repeat`; seed variation from `with seed`; static spread assignment (`even`, `blocks`, `by`, `pinned`) with per-server counts; `PreviewOf`, `Expand`, `EffectiveOutput` and `EnsureSampleSeed`. Tests: every valid row of §6.12 with its job count and the preview/expansion parity; the semantic invalid rows; collected errors at every nesting depth; region diff messages; the `with` table for each output type; order tests; golden `sample` answer cross-checked with an independent Python implementation; randomised property tests against a brute-force oracle (counts, order, `where`, sampling, spread), checked by mutation. Route tests for every semantic tag.
- Checks: gofmt ✔  go vet ✔  golangci-lint ✔  go test -race ✔
- Deviations from the spec (decisions where §6 is silent; update the named sections):
  - §6.4 / §6.6: the region-prompt set (`R`) is the **last, fastest-varying** dimension of the product, after all terms, matching `N = |T1| × … × |Tn| × R`.
  - §6.5: a `<tmpl:tag>` marker in the base item's **negative** prompt is replaced by the fragment's **negative** text (positive by positive), consistent with the text import of §3.6 that puts one marker per tag in the negative prompt. The template item's code name is `base-label1-label2`, a label being the code names of the fragment's items joined with `+`.
  - §6.9 `by g`: the position `k` of an item is its index in the **term's own list** (after the item filter), not in the whole group.
  - §6.11: an empty operand of a merge is allowed (the merge is checked as a whole); an empty zip, group, template, region body or term is `CommandSelectionEmpty`. `limit n` with `n` not smaller than the available combinations gives a `LimitNotSmaller` warning like `sample` does.
  - §6.8: random seeds are not drawn at expansion; `Job.Seed` is set only for `with seed=S` (S + r). A generator with `has_random_seed` gets its seed when the job runs (§8.2) and records it then. A whole number given for a text setting (`file_type=123`) is accepted and becomes text.
  - §6.11 / §17.2: the aggregated `CommandValidationFailed` entries carry these tags: `ServerNotFound`, `GeneratorNotFound`, `FixerNotFound`, `GroupNotFound`, `ItemNotFound` (existing), `CommandSelectionEmpty`, `CommandWhereEmpty` (existing), and new `CommandRegionMismatch`, `CommandWhereGroupMissing`, `CommandSettingInvalid`, `CommandKeyDuplicated`, `CommandTooLarge`; rule R1 is `CommandRestrictionViolated` with rule `R1`. Add the new tags to the Command-language row of §17.2 (HTTP 422).
  - Hard limits against runaway commands (new, §6.11): `CommandTooLarge` above 5,000,000 combinations or 1,000,000 jobs (`Options.MaxCombinations`, `Options.MaxJobs`); these are separate from the soft preview limit of §2.
  - A region keyword that the chosen base item does not itself define (another item of the group does) yields a region with neither mask nor rectangle; the region injector reports `RegionInvalid` when the job runs (§8.2).
