# Using `bosctl` from an AI agent

This page is for a program, or a person writing instructions for one, that drives Bowl of Scenes through `bosctl`. It is short on purpose: rules first, then one worked session. Put it (or a link to it) in your agent's instructions. Everything else is in the [command language](guide/commands.md), the [`bosctl` reference](cli.md) and the [troubleshooting guide](troubleshooting.md).

## Setup

```
export BOS_SERVER=http://127.0.0.1:8080   # where bos runs (this is the default)
export BOS_TOKEN=...                      # only if the server has auth_token set
```

`bosctl` never asks a question and never reads the terminal unless you pass `-` as a file, so it is safe to run unattended. Every `bosctl` command is one process that exits; there is nothing to keep open.

## Rules

1. **Always pass `--json`** (before or after the command). The answer is then exactly what the server sent: one JSON document on standard output. Without it the output is for people and may change.
2. **Read the exit code first.**

   | Code | Meaning | What to do |
   |---|---|---|
   | 0 | Done | Read the answer |
   | 1 | The server answered with an error, **or** `--wait` ended without every job finished | Read the error (below) |
   | 2 | A mistake in the call; nothing was sent | Fix the arguments; `bosctl help <resource>` says how |
   | 3 | The server cannot be reached | Check `BOS_SERVER`; try again later, do not guess |

3. **An error is `{"error":{"tag":…,"message":…,"metadata":…}}`** on standard output with `--json` (a line `Tag: message` on standard error without it). Decide by the `tag`, not the message; [troubleshooting](troubleshooting.md) has a section for every tag. A command that is not valid carries `metadata.errors`: a list of `{rule, tag, message, position}` with the offset and length in the command text.
4. **`command preview` exits 0 even when the command is not valid.** Check `"valid": true` in the answer, and look at `jobs` (how many jobs it will make) before you add or run it. `warnings` (for example `ServerOffline`) do not make it invalid.
5. **Preview before you add, add before you run.** `command add` refuses an invalid command with exit 1, but only `preview` tells you the number of jobs. A command can multiply into thousands of jobs; if `jobs` is far larger than you meant, change the command (`limit`, `sample`, a filter) instead of running it.
6. **Quote command text in single quotes**: `'gpu1 -$ anime: characters * poses'`. `$`, `*`, `>`, `#` and `(` mean something to a shell.
7. **Keep the ids** that `create` returns (`.id` in the answer) and use them in later commands; item, group and command numbers are the database ids, not positions. List commands answer with `{"items":[…],…}` and are paged (`--page-size` up to 200, `--page`).
8. **`command run --wait --timeout 30m`** blocks until the command ends. Exit 0 means every job finished. Without `--wait`, `command run` returns at once, and `command show ID` gives the `status` later (`idle`, `queued`, `processing`, `error` or `finished`).
   - Exit 1 with `stopped waiting` on standard error: the timeout passed. **The jobs keep running on the server**; check `command show ID` later instead of running it again (`CommandNotRunnable` means it is already queued or running, or has no idle job).
   - Exit 1 with `the command ended with errors`: a job failed, and the batch **halts**: the jobs that were still queued go back to `idle`, those already running finish. List the failed jobs with `job list ID --status error`; `job show` has the reason in `error` and `error_trace`. Fix the cause (a missing model on the server, a bad workflow), then continue the rest with `command run ID` again, which queues the idle jobs. A failed job itself stays `error` until its prompt is edited (`job edit`, with a changed prompt) and `job run` queues it, or until `command recreate` makes every job again (finished results are discarded).
9. **Destructive calls cannot be undone and have no confirmation**: `group delete` deletes the items and their files; `command delete` deletes its jobs and result files; `project delete` deletes all its commands, jobs and files; changing a command's text (`command edit --code`) or `command recreate` throws away its jobs and results; `job edit` resets that job. Do these only when the task says to.
10. **Do not run two writers at once on the same command** (two runs, a run and an edit). Stop it first (`command stop ID`).
11. **Generated files** come out through `command download ID --output FILE.zip` (a ZIP of the results of the finished jobs, even of a command that still runs, and `summary.json` with all their information) or `file get KIND NAME --output FILE`; a job's `result_path` and the file kinds are in `job show`. Use `--output -` for standard output.

## The command language in ten lines

```
SERVER[, SERVER] -$ GENERATOR: TERM [* TERM ...] [where ...] [limit N | sample N [seed S]] [repeat N] [with key=value, ...] [spread ...] [> FIXER [on SERVER]]
```

- `characters` every item of the group; `characters(alice, bob)` those; `characters(~alice)` all but; `poses(run%)` by pattern (`%` any, `?` one character); `poses(#acrobatic)` by tag.
- `"text"` adds positive text, `!"text"` adds negative text, as an item of its own.
- `*` makes every combination (last term varies fastest); `&&` joins lists; `||` pairs them.
- `where not #acrobatic`, `where characters = alice and poses in (standing, sitting)` filters the combinations.
- `repeat N` needs `with seed=…` or a generator with a random seed.
- Every name in a filter must exist and match something; an unknown name is an error, never a smaller batch.
- Clause order is fixed: `where`, `limit`/`sample`, `repeat`, `with`, `spread`, fixers.

The full grammar with examples and the common mistakes is [the command language guide](guide/commands.md).

## A worked session

These blocks are run by the project's tests against a stand-in ComfyUI, so they work as written. Lines beginning `bosctl` are commands; `# exit 1` marks one that is expected to fail. The server address is whatever your ComfyUI is.

**1. Check that the program is up and see what exists.**

```bosctl
bosctl --json server list
bosctl --json generator list
```

**2. Create the pieces and keep their ids** (`id` is 1 for each in a fresh database).

```bosctl
bosctl --json server create --name "My GPU" --host http://127.0.0.1:8188 --code-name gpu1
bosctl --json generator create --name Anime --code-name anime --workflow @examples/workflows/creators/illustrious_xl.json --positive-title Positive --negative-title Negative
bosctl --json group create --name Characters --code-name characters
bosctl --json item create 1 --code-name alice --name Alice --positive-prompt "1girl, alice, long blond hair"
bosctl --json item create 1 --code-name bob --name Bob --positive-prompt "1boy, bob, short brown hair"
bosctl --json project create --name Batch
```

**3. Preview, and check `valid` and `jobs` in the answer.**

```bosctl
bosctl --json command preview 'gpu1 -$ anime: characters'
bosctl --json command preview 'gpu1 -$ anime: nobody'
```

The first answer is `{"valid":true,"jobs":2,…}`. The second is `{"valid":false,"errors":[{"rule":"SEM","tag":"GroupNotFound","message":"group \"nobody\" does not exist","position":{"offset":15,"length":6}}],…}` and still exits 0.

**4. Add, and see what an invalid command does.**

```bosctl
bosctl --json command add 1 'gpu1 -$ anime: nobody' --name Broken   # exit 1
bosctl --json command add 1 'gpu1 -$ anime: characters' --name Portraits
```

The failing call prints `{"error":{"tag":"CommandValidationFailed","message":"command is not valid","metadata":{"errors":[…]}}}`.

**5. Run and wait.**

```bosctl
bosctl --json command run 1 --wait --timeout 10m
```

With `--json` the first document on standard output is the command as it was queued; the lines after it (`processing: 1 queued, 1 processing of 2 jobs`, then `finished: 2 finished of 2 jobs`) are progress. Trust the exit code, not the text.

**6. Read the results.**

```bosctl
bosctl --json command show 1
bosctl --json job list 1 --status finished
bosctl --json job show 1
bosctl command download 1 --output agent-results.zip
```

**7. A one-off picture without a project.**

```bosctl
bosctl --json repl run --on gpu1 --generator anime --positive "a red fox" --items 'characters(alice)'
bosctl --json repl last
```

`repl run` returns at once; poll `repl last` until its `status` is `finished` or `error`. Only one REPL job exists: a new run replaces the last.

**8. Clean up what this session made.**

```bosctl
bosctl command delete 1
bosctl project delete 1
```

## What to do when something fails

| Symptom | Likely cause | Action |
|---|---|---|
| exit 3 | The server is not running or `BOS_SERVER` is wrong | Stop and report; do not retry in a tight loop |
| exit 1, tag `*NotFound` | A wrong id or code name | List the resource and use what exists |
| exit 1, tag `CommandValidationFailed` | The command text is wrong | Read `metadata.errors` (rule, position, message), fix the text, preview again |
| exit 1, tag `*CodeNameTaken` | The thing exists already | Use its id (`list`) instead of creating it again |
| `ServerOffline` warning, jobs stay `queued` | ComfyUI is down or unreachable | Report it; the jobs wait and go on when it comes back |
| `--wait` ends with errors | A job failed in ComfyUI and the batch halted | `job list ID --status error`, `job show` for `error`; fix the cause, then `command run ID` again for the rest (rule 8) |
| a tag you do not know | | Look it up in [troubleshooting](troubleshooting.md): it says what it means and what to do |

To learn the exact request behind any command, `bosctl openapi` prints the API description, and [docs/cli.md](cli.md) says which endpoint each command calls.
