# Running work

## Projects and commands

A **project** is an ordered list of commands. Make one (*Projects → Add project*), then open it to add commands.

**Adding a command** parses and checks the text, then creates its jobs right away (nothing runs yet), so you can look at them first. You give it an optional name and description, and a position (the end by default). If anything is wrong you get *every* problem at once and nothing is stored.

What you can do with a command:

| Action | What happens |
|---|---|
| **Run** | Idle jobs become queued and are handed to the servers. A command that is already queued or running cannot be run again |
| **Stop** | Queued jobs go back to idle. A job that is already running finishes |
| **Recreate** | Deletes the jobs and makes them again from the stored text (a `sample` without a seed gives the same ones, because its seed was stored). Handy after you changed groups |
| **Edit** | Changing the name or description is immediate. Changing the **text** deletes the old jobs and results and makes new ones |
| **Move / renumber** | Commands are numbered 1, 2, 3…; moving one shifts the others |
| **Download** | Only for a **finished** command: a ZIP of every result plus `summary.json` with each job's prompts and LoRAs |
| **Delete** | Deletes its jobs and files. A command that is queued or running must be stopped first |

Deleting a project deletes all its commands, jobs and result files, and is refused while any of its commands is queued or running.

## Preview

Before you add (or while you type), the preview tells you:

- whether the text is valid, and every problem with its place in the text;
- how many combinations there are, how many remain after `where`, how many are kept by `limit`/`sample`, and how many **jobs** that makes;
- for a static spread, how many jobs go to each server; for `dynamic`, that the servers share them;
- warnings: a target is offline, a LoRA is missing on a server, `sample` is larger than the list, a server would get no job, or the jobs exceed `preview_soft_limit`.

The preview and *Add* agree: the same text gives the same number of jobs.

## Jobs

The jobs of a command are listed in the order the command defines. Each shows its status, the server that ran it, a small picture of the result, the seed, the prompt and the error.

**Looking at the pictures.** Click a small picture to see it large, over the same page. The `‹` and `›` buttons, or the left and right arrow keys (or a swipe on a touch screen), move to the previous and the next picture of the list; `Esc`, the `×` or a click beside the picture closes it. It goes through **all** the pictures of the command, not only the ones on the page you are looking at (the status filter you chose applies), so it can go on past the last picture of a page; the number in the corner is the place in the whole list. At the last picture it looks again, so pictures that finished while it was open are reached too. A click with `Ctrl`, `Shift` or the middle button still opens the picture alone in a new tab.

Open a job to see everything it carries: the prompts, the generator, the LoRAs, the warnings ComfyUI gave about nodes, and for a failed job the error and its trace.

- **Edit the prompts** of a job and it starts over: its result is deleted and it goes back to idle. (Not while it runs.)
- **Run** an idle job by itself, or **Stop** a queued one.
- **Warnings** appear when ComfyUI accepted the job but complained about a node; the job still runs.

### Seeds

Every job records the seed it ran with, so you can reproduce a result. `with seed=42` gives 42 to every job (42, 43, 44… to the repeats of one combination); a generator marked *new random seed for every job* gives each job its own; otherwise the workflow's own seed is used.

### When things go wrong

| What happened | What the program does |
|---|---|
| A server cannot be reached **before** a job starts | The job waits for another free target (dynamic), or for the grace period and then another target (static, not pinned), or for the server (pinned) |
| A server vanishes **during** a job | The job is queued again; the attempt is counted. After `max_job_attempts` the job fails (the command carries on) |
| ComfyUI rejects the workflow, a node fails, or no result file comes | The job fails with the error and trace, and the **queued** jobs of the same command return to idle (they would fail the same way). Jobs already running finish |
| You stop `bos` while jobs run | They stay *processing*. After the next start the program asks ComfyUI: jobs it still knows are collected, others are queued again |

A fixer step that is waiting for a server that is gone is moved to another target once the grace period passes, unless the command named that server with `on`.

### Order and capacity

The queue is the database: jobs run in the order commands were run, and within a command in job order. Each server runs at most its *jobs at once*; different servers run in parallel. Several commands can be queued; they take turns in the order you ran them.

## Progress

The command list shows `finished/total` and a status for every command; the job page of a command updates **live**, without reloading, and a message appears whenever jobs start, finish or fail, or a server goes away or returns. From a terminal, `bosctl command run 3 --wait` follows a command to its end and `bosctl events --follow` prints the events as they come.

## The REPL

*REPL* makes one picture right now, without a project: pick a server and a generator, write extra prompt text, name items as `group(item),group(item)`, and add LoRAs if you like. The items' prompts and LoRAs are joined in the order written and your text goes last. The result always has the same name and replaces the previous one; the page shows it as soon as the job ends. Only one REPL job exists at a time, and a new run is refused (`JobBusy`) while the previous one is still running.

The REPL job always runs on the server you chose (it is pinned to it).
