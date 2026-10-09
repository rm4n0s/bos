# The three ways in

The web page, `bosctl` and the REST API are three front ends over the same core. What you do in one shows in the others at once.

## The web page

Open the address of the server (<http://127.0.0.1:8080> by default). The menu has **Servers, Generators, Fixers, Groups, Projects, REPL**.

- Tables page through long lists. Dialogs add and change things; a mistake appears in red inside the dialog and nothing is lost. Deleting asks once more before it acts.
- **Groups** has a tag cloud to filter by, an *Import from text* and an *Import from Civitai* dialog, and a tag panel to rename or merge tags. Open a group to edit its **items**; the item dialog shows only the sections the group's features allow.
- **Projects → a project** lists its commands. *Add command* opens the editor with the live preview: errors are marked in your text, and *Save command* is enabled only when the command is valid.
- **A command's jobs** page updates by itself (see [Running work](running.md)). Messages in the corner tell you what finished or failed anywhere.
- With an `auth_token` set, the page first shows a sign-in form.

The web page does not need the internet: its script is part of the program.

## `bosctl`

`bosctl` is a terminal program that talks to a running server. The full list of commands is the [bosctl reference](../cli.md); a few examples:

```sh
export BOS_SERVER=http://127.0.0.1:8080        # the default; add BOS_TOKEN=… if the server has one
bosctl server list
bosctl command preview 'gpu1 -$ anime: characters * poses'
bosctl command add 1 'gpu1 -$ anime: characters * poses' --name "all poses"
bosctl command run 3 --wait
bosctl command download 3 --output results.zip
bosctl events --follow
```

Lists print as tables; with `--json` the answer is printed exactly as the server sent it, so you can pipe it to `jq`. `bosctl help` lists everything; `bosctl help server create` shows one command's flags. Exit codes: 0 done, 1 the server answered with an error, 2 a mistake in how you called it (nothing was sent), 3 the server could not be reached.

Long texts and workflows are read from files with `@`: `--workflow @wf.json`, `--text @lines.txt`. `--file body.json` (or `--file -` for standard input) gives a whole JSON body. **Updates change only what you name**: `bosctl server update 1 --max-jobs 2` keeps everything else.

Put a command text in **single quotes**, so that the shell leaves `$`, `*`, `>` and `(` alone.

## The REST API

Everything is under `/api/v1`; the description is [openapi.json](../openapi.json), and the running server serves it at `/api/v1/openapi.json`. The rules are the same everywhere:

- JSON in and out, field names in `snake_case`; unknown fields in a request are refused.
- Lists take `page` and `page_size` (default 20, at most 200) and answer `{"items":[…],"total":N,"page":1,"page_size":20,"total_pages":N}`.
- `201` for something created (with its address in `Location`), `202` for work that goes on in the background (running a command, importing from Civitai), `204` for a deletion.
- With an `auth_token`, every request needs `Authorization: Bearer <token>`.
- An error is always `{"error": {"tag": "CommandValidationFailed", "message": "…", "metadata": {…}}}`. The `tag` is stable and is what a program should look at; [troubleshooting](../troubleshooting.md) explains each one. Problems with a command carry a list in `metadata.errors`, each with a `rule`, a `message` and the `position` (offset and length) in the text.

```sh
curl -s localhost:8080/api/v1/commands/preview \
     -H 'Content-Type: application/json' \
     -d '{"code": "gpu1 -$ anime: characters * poses"}'
```

`POST /commands/preview` answers 200 even for an invalid command (with `"valid": false` and the problems); adding the same text answers 422.

Uploads (group and item pictures) are `multipart/form-data` with the JSON in a part named `data` and the pictures in parts named `thumbnail`, `ip_adapter_image`, `mask_reference` and `controlnet_0`, `controlnet_1`, …. Results and uploads can be read back from `/api/v1/files/<kind>/<path>`.

## Events

The server keeps the last 100 events (more with `event_log_size`): a job started, finished or failed, a server came or went, a command finished, an import made progress. Ask for them with `GET /api/v1/events?after=<id>`, which answers `{"events":[…],"last_id":N,"gap":false}`; `gap` is true when you missed events because the log moved on, and the client should reload what it shows.

With `Accept: text/event-stream` the same URL is a live stream (`id:`, `event:` and a JSON `data:` line, a comment every 15 seconds). A client that reconnects sends `Last-Event-ID` and receives only what it missed. `bosctl events --follow` does exactly this.
