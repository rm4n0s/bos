# bosctl reference

This file is generated from the command table of `internal/cli`; do not edit it by hand. `go test ./internal/cli -update` rewrites it.

```
bosctl [--server URL] [--token TOKEN] [--json] <resource> <verb> [arguments] [flags]
```

`--server` (`BOS_SERVER`, default `http://127.0.0.1:8080`), `--token` (`BOS_TOKEN`) and `--json` can be given before or after the command. Exit codes: 0 done, 1 the server answered with an error, 2 a mistake in the call, 3 the server could not be reached.

## server

### `bosctl server list [flags]`

List servers with their status.

Calls `GET /api/v1/servers`.

| Flag | Meaning |
|---|---|
| `--page-size` | items on a page (at most 200) |
| `--page` | the page, from 1 |

### `bosctl server show ID [flags]`

Show a server.

Calls `GET /api/v1/servers/{id}`.

### `bosctl server create [flags]`

Add a server.

Calls `POST /api/v1/servers`.

| Flag | Meaning |
|---|---|
| `--code-name` | the code name used in commands |
| `--comfyui-path` | the folder of ComfyUI's installation, offered when LoRAs are imported |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--host` | the ComfyUI address, http://host:8188 |
| `--local` | ComfyUI reads this machine's files |
| `--max-jobs` | jobs it may run at once |
| `--name` | the name |

### `bosctl server update ID [flags]`

Change a server.

Calls `PUT /api/v1/servers/{id}`.

| Flag | Meaning |
|---|---|
| `--code-name` | the code name |
| `--comfyui-path` | the folder of ComfyUI's installation |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--host` | the ComfyUI address |
| `--local` | ComfyUI reads this machine's files |
| `--max-jobs` | jobs it may run at once |
| `--name` | the name |

### `bosctl server delete ID [flags]`

Delete a server.

Calls `DELETE /api/v1/servers/{id}`.

### `bosctl server loras ID [flags]`

List the LoRAs installed on a server.

Calls `GET /api/v1/servers/{id}/loras`.

## generator

### `bosctl generator list [flags]`

List generators.

Calls `GET /api/v1/generators`.

| Flag | Meaning |
|---|---|
| `--page-size` | items on a page (at most 200) |
| `--page` | the page, from 1 |

### `bosctl generator show ID [flags]`

Show a generator.

Calls `GET /api/v1/generators/{id}`.

### `bosctl generator analyze [flags]`

Infer what a workflow produces; nothing is stored.

Calls `POST /api/v1/generators/analyze`.

| Flag | Meaning |
|---|---|
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--workflow` | the ComfyUI workflow, @file |

### `bosctl generator create [flags]`

Add a generator from a workflow.

Calls `POST /api/v1/generators`.

| Flag | Meaning |
|---|---|
| `--attributes` | output attributes as JSON (or @file) |
| `--code-name` | the code name used in commands |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--name` | the name |
| `--negative-title` | title of the node that takes the negative prompt |
| `--output-node-class` | class of the output node |
| `--output-node-title` | title of the output node |
| `--output-type` | image, video, 3d or unknown (found from the workflow if left out) |
| `--positive-title` | title of the node that takes the positive prompt |
| `--random-seed` | give every job a new random seed |
| `--workflow` | the ComfyUI workflow in API format, @file |

### `bosctl generator update ID [flags]`

Change a generator.

Calls `PUT /api/v1/generators/{id}`.

| Flag | Meaning |
|---|---|
| `--attributes` | output attributes as JSON (or @file) |
| `--code-name` | the code name used in commands |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--name` | the name |
| `--negative-title` | title of the node that takes the negative prompt |
| `--output-node-class` | class of the output node |
| `--output-node-title` | title of the output node |
| `--output-type` | image, video, 3d or unknown (found from the workflow if left out) |
| `--positive-title` | title of the node that takes the positive prompt |
| `--random-seed` | give every job a new random seed |
| `--workflow` | the ComfyUI workflow in API format, @file |

### `bosctl generator delete ID [flags]`

Delete a generator.

Calls `DELETE /api/v1/generators/{id}`.

## fixer

### `bosctl fixer list [flags]`

List fixers.

Calls `GET /api/v1/fixers`.

| Flag | Meaning |
|---|---|
| `--page-size` | items on a page (at most 200) |
| `--page` | the page, from 1 |

### `bosctl fixer show ID [flags]`

Show a fixer.

Calls `GET /api/v1/fixers/{id}`.

### `bosctl fixer create [flags]`

Add a fixer from a workflow.

Calls `POST /api/v1/fixers`.

| Flag | Meaning |
|---|---|
| `--code-name` | the code name used in commands |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--load-title` | title of the image-input node |
| `--name` | the name |
| `--negative-title` | title of the negative prompt node |
| `--negative` | negative text |
| `--positive-title` | title of the positive prompt node |
| `--positive` | positive text |
| `--save-title` | title of the image-output node |
| `--workflow` | the ComfyUI workflow in API format, @file |

### `bosctl fixer update ID [flags]`

Change a fixer.

Calls `PUT /api/v1/fixers/{id}`.

| Flag | Meaning |
|---|---|
| `--code-name` | the code name used in commands |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--load-title` | title of the image-input node |
| `--name` | the name |
| `--negative-title` | title of the negative prompt node |
| `--negative` | negative text |
| `--positive-title` | title of the positive prompt node |
| `--positive` | positive text |
| `--save-title` | title of the image-output node |
| `--workflow` | the ComfyUI workflow in API format, @file |

### `bosctl fixer delete ID [flags]`

Delete a fixer.

Calls `DELETE /api/v1/fixers/{id}`.

## group

### `bosctl group list [flags]`

List groups.

Calls `GET /api/v1/groups`.

| Flag | Meaning |
|---|---|
| `--page-size` | items on a page (at most 200) |
| `--page` | the page, from 1 |
| `--tag` | only groups with this group tag |

### `bosctl group show ID [flags]`

Show a group.

Calls `GET /api/v1/groups/{id}`.

### `bosctl group create [flags]`

Add a group.

Calls `POST /api/v1/groups`.

| Flag | Meaning |
|---|---|
| `--code-name` | the code name used in commands |
| `--description` | a description |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--name` | the name |
| `--tags` | group tags, separated by commas |
| `--thumbnail` | a thumbnail image |
| `--use-controlnet` | its items can carry ControlNets |
| `--use-coordinates-region` | its items can carry box regions |
| `--use-ip-adapter` | its items can carry an IP-Adapter |
| `--use-lora` | its items can carry LoRAs |
| `--use-mask-region` | its items can carry mask regions |
| `--use-type-attributes` | its items can set output attributes |

### `bosctl group update ID [flags]`

Change a group.

Calls `PUT /api/v1/groups/{id}`.

| Flag | Meaning |
|---|---|
| `--code-name` | the code name used in commands |
| `--description` | a description |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--name` | the name |
| `--tags` | group tags, separated by commas |
| `--thumbnail` | a new thumbnail image |
| `--use-controlnet` | its items can carry ControlNets |
| `--use-coordinates-region` | its items can carry box regions |
| `--use-ip-adapter` | its items can carry an IP-Adapter |
| `--use-lora` | its items can carry LoRAs |
| `--use-mask-region` | its items can carry mask regions |
| `--use-type-attributes` | its items can set output attributes |

### `bosctl group delete ID [flags]`

Delete a group with its items and files.

Calls `DELETE /api/v1/groups/{id}`.

### `bosctl group import-text [flags]`

Make a group with one item for each line of a text.

Calls `POST /api/v1/groups/import-text`.

| Flag | Meaning |
|---|---|
| `--code-name` | the code name used in commands |
| `--description` | a description |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--name` | the name |
| `--tags` | group tags, separated by commas |
| `--text` | the text, one item for each line (or @file) |

### `bosctl group import-civitai [flags]`

Make a group and import LoRAs from Civitai into it.

Calls `POST /api/v1/groups/import-civitai`.

| Flag | Meaning |
|---|---|
| `--code-name` | the code name used in commands |
| `--comfyui-path` | the ComfyUI installation folder |
| `--description` | a description |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--lora-subfolder` | the sub-folder of models/loras |
| `--model` | a model as id[/version][:model strength[:clip strength]], repeatable (no version: the newest) |
| `--name` | the name |
| `--tags` | group tags, separated by commas |

## item

### `bosctl item list GROUP [flags]`

List the items of a group.

Calls `GET /api/v1/groups/{id}/items`.

| Flag | Meaning |
|---|---|
| `--page-size` | items on a page (at most 200) |
| `--page` | the page, from 1 |

### `bosctl item show ID [flags]`

Show an item.

Calls `GET /api/v1/items/{id}`.

### `bosctl item create GROUP [flags]`

Add an item to a group.

Calls `POST /api/v1/groups/{id}/items`.

| Flag | Meaning |
|---|---|
| `--code-name` | the code name used in commands |
| `--controlnet-image` | the image of a ControlNet, as N=path (repeatable) |
| `--controlnets` | the ControlNets as a JSON list (or @file) |
| `--coordinated-regions` | the box regions as a JSON list (or @file) |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--ip-adapter-image` | the IP-Adapter reference image |
| `--ip-adapter` | the IP-Adapter as JSON (or @file) |
| `--loras` | LoRAs as name:model:clip, repeatable |
| `--mask-reference` | the PNG that the mask regions are made from |
| `--mask-region` | the mask region as JSON (or @file) |
| `--name` | the name |
| `--negative-prompt` | the negative prompt (or @file) |
| `--output-attributes` | output attributes as JSON (or @file) |
| `--output-type` | image, video, 3d or unknown |
| `--positive-prompt` | the positive prompt (or @file) |
| `--tags` | item tags, separated by commas |
| `--thumbnail` | a thumbnail image |

### `bosctl item update ID [flags]`

Change an item.

Calls `PUT /api/v1/items/{id}`.

| Flag | Meaning |
|---|---|
| `--code-name` | the code name used in commands |
| `--controlnet-image` | the image of a ControlNet, as N=path (repeatable) |
| `--controlnets` | the ControlNets as a JSON list (or @file) |
| `--coordinated-regions` | the box regions as a JSON list (or @file) |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--ip-adapter-image` | the IP-Adapter reference image |
| `--ip-adapter` | the IP-Adapter as JSON (or @file) |
| `--loras` | LoRAs as name:model:clip, repeatable |
| `--mask-reference` | the PNG that the mask regions are made from |
| `--mask-region` | the mask region as JSON (or @file) |
| `--name` | the name |
| `--negative-prompt` | the negative prompt (or @file) |
| `--output-attributes` | output attributes as JSON (or @file) |
| `--output-type` | image, video, 3d or unknown |
| `--positive-prompt` | the positive prompt (or @file) |
| `--tags` | item tags, separated by commas |
| `--thumbnail` | a thumbnail image |

### `bosctl item delete ID [flags]`

Delete an item with its files.

Calls `DELETE /api/v1/items/{id}`.

### `bosctl item add-civitai GROUP [flags]`

Add one Civitai LoRA to a group as an item.

Calls `POST /api/v1/groups/{id}/items/civitai`.

| Flag | Meaning |
|---|---|
| `--clip-strength` | strength for the text encoder (default 1) |
| `--comfyui-path` | the ComfyUI installation folder |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--lora-subfolder` | the sub-folder of models/loras |
| `--model-strength` | strength for the model (default 1) |
| `--model` | the Civitai model id |
| `--version` | the version of the model (default: the newest) |

## tag

### `bosctl tag list [flags]`

List the tags in use.

Calls `GET /api/v1/tags`.

| Flag | Meaning |
|---|---|
| `--prefix` | only tags that start with this |
| `--scope` | group or item |

### `bosctl tag rename [flags]`

Rename or merge a tag.

Calls `POST /api/v1/tags/rename`.

| Flag | Meaning |
|---|---|
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--from` | the tag now |
| `--scope` | group or item |
| `--to` | the new name |

## project

### `bosctl project list [flags]`

List projects.

Calls `GET /api/v1/projects`.

| Flag | Meaning |
|---|---|
| `--page-size` | items on a page (at most 200) |
| `--page` | the page, from 1 |

### `bosctl project show ID [flags]`

Show a project.

Calls `GET /api/v1/projects/{id}`.

### `bosctl project create [flags]`

Add a project.

Calls `POST /api/v1/projects`.

| Flag | Meaning |
|---|---|
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--name` | the name |

### `bosctl project update ID [flags]`

Rename a project.

Calls `PUT /api/v1/projects/{id}`.

| Flag | Meaning |
|---|---|
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--name` | the name |

### `bosctl project delete ID [flags]`

Delete a project with its commands and jobs.

Calls `DELETE /api/v1/projects/{id}`.

## command

### `bosctl command list PROJECT [flags]`

List the commands of a project with their progress.

Calls `GET /api/v1/projects/{id}/commands`.

| Flag | Meaning |
|---|---|
| `--page-size` | items on a page (at most 200) |
| `--page` | the page, from 1 |

### `bosctl command show ID [flags]`

Show a command.

Calls `GET /api/v1/commands/{id}`.

### `bosctl command preview [COMMAND] [flags]`

Check a command text and see what it would make; nothing is stored.

Calls `POST /api/v1/commands/preview`.

| Flag | Meaning |
|---|---|
| `--code` | the command text (or @file) |
| `--file` | read the JSON body from this file (- for standard input); flags override it |

### `bosctl command add PROJECT [COMMAND] [flags]`

Add a command to a project.

Calls `POST /api/v1/projects/{id}/commands`.

| Flag | Meaning |
|---|---|
| `--at` | the position, from 1 (default: the end) |
| `--code` | the command text (or @file) |
| `--description` | a description |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--name` | the name |

### `bosctl command edit ID [COMMAND] [flags]`

Change a command; a new text makes its jobs again.

Calls `PUT /api/v1/commands/{id}`.

| Flag | Meaning |
|---|---|
| `--code` | the command text (or @file) |
| `--description` | a description |
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--name` | the name |

### `bosctl command delete ID [flags]`

Delete a command with its jobs and files.

Calls `DELETE /api/v1/commands/{id}`.

### `bosctl command move ID [flags]`

Move a command to a position in its project.

Calls `POST /api/v1/commands/{id}/move`.

| Flag | Meaning |
|---|---|
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--to` | the new position, from 1 |

### `bosctl command renumber PROJECT [flags]`

Number the commands of a project 1..N.

Calls `POST /api/v1/projects/{id}/commands/renumber`.

### `bosctl command run ID [flags]`

Queue the idle jobs of a command (--wait to follow it to the end).

Calls `POST /api/v1/commands/{id}/run`.

| Flag | Meaning |
|---|---|
| `--timeout` | with --wait: give up after this long, such as 30m |
| `--wait` | wait until the command is finished and say how it goes |

### `bosctl command stop ID [flags]`

Return the queued jobs of a command to idle.

Calls `POST /api/v1/commands/{id}/stop`.

### `bosctl command recreate ID [flags]`

Delete the jobs of a command and make them again.

Calls `POST /api/v1/commands/{id}/recreate`.

### `bosctl command download ID [flags]`

Save the ZIP of the results of a finished command.

Calls `GET /api/v1/commands/{id}/download`.

| Flag | Meaning |
|---|---|
| `--output` | where to save the file (- for standard output) |

## job

### `bosctl job list COMMAND [flags]`

List the jobs of a command.

Calls `GET /api/v1/commands/{id}/jobs`.

| Flag | Meaning |
|---|---|
| `--page-size` | items on a page (at most 200) |
| `--page` | the page, from 1 |
| `--status` | idle, queued, processing, error or finished |

### `bosctl job show ID [flags]`

Show a job with its error and trace.

Calls `GET /api/v1/jobs/{id}`.

### `bosctl job edit ID [flags]`

Change the prompts of a job; it starts over.

Calls `PUT /api/v1/jobs/{id}`.

| Flag | Meaning |
|---|---|
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--negative` | the negative prompt (or @file) |
| `--positive` | the positive prompt (or @file) |

### `bosctl job run ID [flags]`

Queue an idle job.

Calls `POST /api/v1/jobs/{id}/run`.

### `bosctl job stop ID [flags]`

Return a queued job to idle.

Calls `POST /api/v1/jobs/{id}/stop`.

## repl

### `bosctl repl run [flags]`

Run one ad-hoc job without a project.

Calls `POST /api/v1/repl/run`.

| Flag | Meaning |
|---|---|
| `--file` | read the JSON body from this file (- for standard input); flags override it |
| `--generator` | the generator's code name |
| `--items` | items as group(item),group(item) |
| `--loras` | LoRAs as name:model:clip, repeatable |
| `--negative` | text added to the negative prompt (or @file) |
| `--on` | the code name of the server to run on |
| `--positive` | text added to the positive prompt (or @file) |

### `bosctl repl last [flags]`

Show the previous ad-hoc job.

Calls `GET /api/v1/repl/last`.

## events

### `bosctl events [flags]`

Show the event log (--follow to keep watching).

Calls `GET /api/v1/events`.

| Flag | Meaning |
|---|---|
| `--after` | only events after this id |
| `--follow` | keep watching and print events as they happen |

## file

### `bosctl file get KIND NAME [flags]`

Save a stored file (results, thumbnails, controlnet, ipadapter, regions, downloads).

Calls `GET /api/v1/files/{kind}/{name...}`.

| Flag | Meaning |
|---|---|
| `--output` | where to save the file (- for standard output) |

## openapi

### `bosctl openapi [flags]`

Print the OpenAPI description of the API.

Calls `GET /api/v1/openapi.json`.

