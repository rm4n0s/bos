# How it fits together

## The picture

```
 ┌─ catalogue ─────────────────────────────────────────┐
 │ servers     where ComfyUI runs                      │
 │ generators  workflows that make the main picture    │
 │ fixers      workflows that improve a picture        │
 │ groups      named lists of reusable prompt items    │
 └─────────────────────────────────────────────────────┘
                     │ used by
 ┌─ work ──────────────▼───────────────────────────────┐
 │ project  →  commands (one line each)  →  jobs       │
 └─────────────────────────────────────────────────────┘
```

You set up the **catalogue** once, then write **commands**. A command is one line of text. When you add it, the program works out every combination it describes and makes one **job** for each. Running the command sends the jobs to your servers.

## The words

| Word | Meaning |
|---|---|
| **Server** | A ComfyUI installation, reached by its address (for example `http://192.168.1.20:8188`). Has a *code name* (`gpu1`) and a limit on how many jobs it runs at once. |
| **Generator** | A ComfyUI workflow (exported in API format) that produces the main result: an image, a video or a 3D model. The program writes the prompts, the seed and the settings into it for every job. |
| **Fixer** | A workflow that takes a finished picture and returns a better one: an upscale, a face repair, a background removal. |
| **Group** | A named list of **items**: all your characters, all your poses. A group says which features its items may carry (LoRAs, ControlNets, an IP-Adapter, regions, output settings). |
| **Item** | One choice inside a group: “alice”, with a positive prompt, a negative prompt and perhaps LoRAs or images. |
| **Tag** | A label on an item (`#acrobatic`) that commands can select by, or a label on a group that only organises the page. |
| **Code name** | The short name used in commands: letters, digits and `_`, not one of the command language's reserved words. |
| **Project** | A container of commands, in order. |
| **Command** | One line of the command language. Adding it makes its jobs; you can look at them before running anything. |
| **Job** | One concrete picture to make. A job is a *snapshot*: it copies the prompts and settings it needs, so changing a group later does not change a job that exists. |
| **Preview** | A dry run of a command text: it tells you what adding it would make, and changes nothing. |
| **REPL** | One quick picture without a project. |

## How a job lives

```
idle ──run──▶ queued ──a server is free──▶ processing ──▶ finished
  ▲              │                              │
  └────stop──────┘                              └──▶ error
```

- A job is **idle** until you run its command (or the job).
- **Queued** jobs wait for a server with a free place. The first command you run goes first.
- **Processing** means ComfyUI is working on it. With fixers the job goes through *queued* and *processing* again for each fixer step.
- **Finished** jobs have a result file; **error** jobs say why, with the trace.
- A command is finished only when **all** its jobs are, whatever order the servers finish them in.

If a server becomes unreachable in the middle of a job, the job goes back to the queue and another server takes it (after a grace period, unless the command pins the job to its server). After too many such attempts the job fails. A failure of the workflow itself (a node error, a missing model) stops the rest of the command's queued jobs, because the next job would fail the same way, but does not interrupt jobs already running.

## What is kept where

Everything you create is stored in one SQLite database. Uploaded and generated files live in the directories named in the configuration (results, thumbnails, reference images, masks, downloads). A job's result is written under `results/<command id>/`.

## The three ways in

The same actions are available in the **web page**, in **`bosctl`** (a terminal program) and in the **REST API**. They share one core, so they never disagree. See [the three ways in](interfaces.md).
