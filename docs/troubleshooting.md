# Troubleshooting

Every error the program gives has a **tag**: a stable name such as `CommandValidationFailed`. The web page shows its message in the dialog, `bosctl` prints `<Tag>: <message>`, and the REST API sends it in the `error` object. Find the tag below.

Errors that are the program's fault or the machine's (a full disk, a broken database) are not described to the client for safety: look in the server's log, where each is written once with its tag and details (set `"log_level": "debug"` for more).

A job that fails keeps its error and trace; open the job (web page: *Open*; terminal: `bosctl job show ID`).

Not an error but often asked:

- **The page says a server is offline.** The program probes every second; check the address, that ComfyUI is running with `--listen` if it is on another machine, and any firewall.

- **A command is stuck queued.** No server that can take its jobs is online, or each is busy with earlier commands. Check *Servers* and the other commands' status.

- **A job fails at once with a ComfyUI error about a missing model or node.** The server lacks something the workflow needs. Open the same workflow in that ComfyUI and run it by hand.

- **The program starts but the web page is empty or says sign in.** `auth_token` is set; use the token, or remove it for local use.

- **Nothing happens when I run a command.** Run needs idle jobs: a finished command has none. Use *Recreate*, then *Run*.


## Requests


### `InvalidRequest`

**Why.** The program could not understand what was sent: a malformed or empty body, an unknown field, a number that is not a number, a bad page, an id that is not positive, a file of the wrong kind, a path that points outside, a bad sub-folder for an import, or no server to import to.

**What to do.** Read the message; it names the field or part. With `bosctl`, nothing is sent for such a mistake (exit code 2). For files, check the type (thumbnails are PNG, JPEG or GIF; the mask picture is PNG).


### `UploadTooLarge`

**Why.** A file, or the JSON body (over 8 MiB), is bigger than allowed.

**What to do.** Make the file smaller, or raise *Largest upload* in Settings (`upload_limit_bytes`).


## Not found


### `ServerNotFound`

**Why.** There is no server with that id or code name (it may have been deleted).

**What to do.** List the servers and use an existing one. A command that names a server that does not exist is refused with the same name inside `CommandValidationFailed`.


### `GeneratorNotFound`

**Why.** No such generator.

**What to do.** List the generators; check the code name in the command.


### `FixerNotFound`

**Why.** No such fixer.

**What to do.** List the fixers; check the name after `>`.


### `GroupNotFound`

**Why.** No such group.

**What to do.** List the groups. Importing from Civitai into a group that was deleted meanwhile reports this as a message.


### `ItemNotFound`

**Why.** No such item, in the group or by id.

**What to do.** Check the code name in `group(item)`; `bosctl item list GROUP` shows them.


### `ProjectNotFound`

**Why.** No such project.

**What to do.** List the projects.


### `CommandNotFound`

**Why.** No such command.

**What to do.** List the commands of the project; ids are not reused.


### `JobNotFound`

**Why.** No such job; or, for the REPL, nothing has been run yet.

**What to do.** List the jobs of the command. Recreating or editing a command deletes its jobs, so earlier job ids stop existing.


### `FileNotFound`

**Why.** The file does not exist, or it is a folder.

**What to do.** A result is only there once its job has finished. Use the paths the server gives (`result_path`, `thumbnail`, …).


### `LoRANotFound`

**Why.** No item carries that LoRA and no server that answered has it. The name may be spelled differently from the file (`style` and `style.safetensors` are the same file; the folder matters), or the LoRA was already deleted.

**What to do.** Look for it on the LoRAs page. If a server is offline its LoRAs are not listed; bring it back first.


### `TagNotFound`

**Why.** You tried to rename a tag that no group or item carries.

**What to do.** List the tags of that kind (group or item) and use one of them.


## Names and tags


### `CodeNameInvalid`

**Why.** A code name has a character other than letters, digits and `_`, is empty, or is longer than 100 characters.

**What to do.** Choose a plain name such as `gpu1` or `alice_2`.


### `CodeNameReserved`

**Why.** The code name is a reserved word of the command language (`where repeat limit sample with spread on and or not in`).

**What to do.** Choose another name; otherwise a command could not refer to it.


### `TagInvalid`

**Why.** A tag has a space or a symbol, is a reserved word, is empty or is too long.

**What to do.** Tags are lower case letters, digits and `_`, at most 50 characters. Upper case is lower-cased for you.


### `NameInvalid`

**Why.** A name is empty or too long.

**What to do.** Give a name that is not empty and not longer than 300 characters (100 for a server).


## Names already used


### `ServerCodeNameTaken`

**Why.** Another server has this code name.

**What to do.** Pick a different code name, or edit the existing server.


### `GeneratorCodeNameTaken`

**Why.** Another generator has this code name.

**What to do.** Pick another.


### `FixerCodeNameTaken`

**Why.** Another fixer has this code name.

**What to do.** Pick another.


### `GroupCodeNameTaken`

**Why.** Another group has this code name.

**What to do.** Pick another. (Importing from text or Civitai creates the group first, so this is reported before any download.)


### `ItemCodeNameTaken`

**Why.** Another item **of the same group** has this code name.

**What to do.** Code names are unique inside a group only. Importing a Civitai model that is already in the group gives this as a message.


## Values that are not allowed


### `OutputTypeInvalid`

**Why.** The output type is not `image`, `video`, `3d` or `unknown`.

**What to do.** Choose one of the four, or leave it empty so the program finds it from the workflow.


### `ServerMaxJobsInvalid`

**Why.** *Jobs at once* is less than 1.

**What to do.** Use 1 or more.


### `ServerHostInvalid`

**Why.** The server address is not an `http://` or `https://` address with a host, or is longer than 100 characters.

**What to do.** Write it as `http://192.168.1.20:8188`.


### `GeneratorTitleRequired`

**Why.** A generator was saved without the title of its positive prompt node, and none of the nodes of the workflow has a title that starts with “positive”. Without that node the program would have nowhere to put the prompt of a job, and ComfyUI would run the text already in the workflow for every job.

**What to do.** In ComfyUI rename the node that takes the positive prompt to `Positive` (double-click its title) and save the workflow again, or type the title of that node in *Positive prompt node*. The dialog lists the candidates once the workflow is in the box; the message lists them too.


### `FixerTitleRequired`

**Why.** A fixer has no title for its image-input or image-output node.

**What to do.** Choose the titles from the list the dialog offers once the workflow is in the box, or type them.


### `ItemControlNetInvalid`

**Why.** A ControlNet of an item has a type that does not exist.

**What to do.** Use `openpose`, `dwpose`, `tile`, `canny`, `midas`, `depth`, `lineart`, `softedge`, `scribble` or `normal`.


### `ItemRegionInvalid`

**Why.** A region of an item is wrong: an empty or repeated keyword, a keyword that is not a plain word, or a box with no size or a negative position.

**What to do.** Fix the box (`keyword width height x y`) or the mask keywords; they must be plain words and different.


## The command language


### `CommandLexError`

**Why.** The text contains something that is not part of the language: a stray character, an unclosed quote.

**What to do.** The message and position point at it. Only words, numbers, quoted text and the symbols of the language are allowed (see the table of tokens in the specification, §6.1).


### `CommandSyntaxError`

**Why.** The words are in an order the grammar does not allow: a missing `-$`, a missing selection, clauses in the wrong order, a missing value.

**What to do.** The clauses go: `where`, `limit`/`sample`, `repeat`, `with`, `spread`, fixers. See [the language](guide/commands.md).


### `CommandRestrictionViolated`

**Why.** The text is grammatical but breaks a rule: mixing `&&` and `||`, mixing include and exclude in a filter, a region inside a merge or template, two regions, a filter on a region group, an empty literal, a count of 0, a duplicate server, weights without a static spread, `pinned` with `dynamic`, `spread by` on something that is not one plain group, `repeat` without a varying seed.

**What to do.** The `rule` in the message (`S1`…`S8`, `T1`…`T5`, `R1`) names it; [the language](guide/commands.md#common-mistakes) explains the common ones.


### `CommandValidationFailed`

**Why.** The command parsed, but it refers to things that do not exist or do not fit: servers, generators, fixers, groups, items, patterns, tags, region names, `where` groups, `with` keys, or it makes no job at all. The answer lists **every** problem, each with a position.

**What to do.** Read each entry in `metadata.errors` (the web page marks them in your text). The entries below are the kinds you will see.


### `CommandSelectionEmpty`

**Why.** One of the terms picks nothing.

**What to do.** Check the filter: a pattern or tag that matches no item, or an empty group.


### `CommandWhereEmpty`

**Why.** The `where` condition removes every combination.

**What to do.** Loosen the condition; check spelling of the groups and items in it.


### `CommandRegionMismatch`

**Why.** The region names in `{…}` are not exactly those the group's items define.

**What to do.** The message lists both differences: names missing in the command and names missing in the group.


### `CommandWhereGroupMissing`

**Why.** A `where` mentions a group that is not part of the command.

**What to do.** Add the group to the selections, or remove it from the `where`.


### `CommandSettingInvalid`

**Why.** A `with` key does not exist for the generator's output type, or its value has the wrong type or range.

**What to do.** See the table of keys in [the language](guide/commands.md#settings).


### `CommandKeyDuplicated`

**Why.** A `with` key is given twice.

**What to do.** Write each key once.


### `CommandTooLarge`

**Why.** The command would make an unreasonable number of combinations or jobs.

**What to do.** Narrow the selections, or use `limit` / `sample`.


### `CommandJSONInvalid`

**Why.** The stored parsed form of a command is damaged or unreadable. It cannot happen through normal use.

**What to do.** Edit the command (the text is kept) or recreate it. If it repeats, keep the database and report it.


## State


### `CommandNotRunnable`

**Why.** You ran a command that is already queued or running, or that has no idle job.

**What to do.** Wait for it, stop it, or recreate its jobs to make new idle ones.


### `CommandNoResults`

**Why.** You asked to download a command that has no finished job yet, so there is nothing to put in the archive.

**What to do.** Wait until at least one job is finished. A command does not have to be finished: the archive holds the jobs that are finished when you ask.


### `CommandBusy`

**Why.** The command (or one of the project's commands) is queued or running and cannot be edited or deleted now.

**What to do.** Stop it first; a job that is already running still finishes.


### `JobNotRunnable`

**Why.** You ran a job that is not idle, or stopped a job that is not queued.

**What to do.** Only idle jobs can be run and only queued jobs can be stopped.


### `JobBusy`

**Why.** The job is running and its prompts cannot be changed, or the previous REPL job is still running.

**What to do.** Wait for it to end.


### `ReplInvalid`

**Why.** The REPL input is wrong: the items are not written `group(item),group(item)`, or a LoRA has no name.

**What to do.** Write the items exactly like `characters(alice),poses(standing)`.


## Workflows


### `WorkflowInvalid`

**Why.** The workflow is not a ComfyUI workflow in API format: it is not JSON, has no nodes, or its nodes have no `class_type`.

**What to do.** In ComfyUI turn on *Dev mode options* and use *Save (API Format)*.


### `WorkflowTitleDuplicated`

**Why.** Two nodes of the workflow have the title you gave (for example two nodes called `Positive`).

**What to do.** Rename nodes in ComfyUI so the titles the program uses are unique, and save the workflow again.


### `WorkflowTitleNotFound`

**Why.** No node has the title you gave.

**What to do.** Check spelling and capital letters; the dialog offers the titles the workflow has.


### `OutputNodeNotFound`

**Why.** The program cannot find a node that saves the result (`SaveImage`, `SaveAnimatedWEBP`, `VHS_VideoCombine`, `SaveGLB`, …).

**What to do.** Add a save node to the workflow.


### `NoSamplerInWorkflow`

**Why.** A job needs to write a seed or settings, but the workflow has no `KSampler`, `KSamplerAdvanced` or `SamplerCustomAdvanced`.

**What to do.** Use a workflow with a sampler, or do not set attributes on this generator.


### `InjectorFailed`

**Why.** The program could not add something (a LoRA, a ControlNet, an IP-Adapter, a region) to the workflow for a job.

**What to do.** The job's error says which and why: usually a node the workflow does not have, a text node that has no input for the prompt (it needs `text`, or `prompt`, or `value` for a string primitive; a region prompt needs `text`), or custom nodes missing on the server. The job fails; fix the workflow or the item.


### `RegionInvalid`

**Why.** A region of a job cannot be applied to the workflow; the message says why.

**What to do.** Check the item's regions; the job's error says which keyword.


### `MaskGenerationFailed`

**Why.** The mask picture could not be turned into regions: it is not readable, has no colour other than white, has more than 10 colours, or is not a PNG.

**What to do.** Paint flat colours on white, with no anti-aliasing, and save as PNG.


## ComfyUI


### `ServerUnreachable`

**Why.** The server does not answer.

**What to do.** Check that ComfyUI is running and that the address is right and reachable from this machine. A job that was running is queued again and tried on another server.


### `ComfyUIRejected`

**Why.** ComfyUI refused the workflow (it reported errors for it and queued nothing).

**What to do.** The job's error lists the nodes: a missing model, a missing custom node, a wrong value. Fix the workflow or install what is missing on that server.


### `ComfyUIExecutionFailed`

**Why.** ComfyUI started the job but a node failed while running.

**What to do.** The job's trace has ComfyUI's own traceback. Out of memory is common: lower the size or the batch.


### `ComfyUIUploadFailed`

**Why.** An input image could not be uploaded to the server.

**What to do.** Check the server, its disk, and that the image file still exists.


### `ComfyUIResultMissing`

**Why.** The job ended but the output node produced no file the program could read.

**What to do.** Check the workflow's save node; a `PreviewImage` makes a temporary image that is not kept. Use a `SaveImage`.


### `ComfyUIInterrupted`

**Why.** Someone cancelled the job in ComfyUI.

**What to do.** Run the job again.


### `ComfyUIPromptLost`

**Why.** ComfyUI no longer knows the job: it was restarted and forgot it.

**What to do.** The job is queued again automatically (it counts as an attempt).


### `ComfyUICallCancelled`

**Why.** The program stopped waiting for ComfyUI because it is shutting down or the request ended.

**What to do.** Nothing to do; the job continues after the next start.


### `ModelCrudMissing`

**Why.** The server answered, but it has no `/model_crud` routes: the [comfyui-model-crud](https://github.com/rm4n0s/comfyui-model-crud) extension is not installed in that ComfyUI. The program needs it to delete results from the `output` folder and to upload LoRAs.

**What to do.** Install the extension in the `custom_nodes` folder of that ComfyUI and restart ComfyUI. Open `/model_crud/config` on the server to check that it answers.


### `ModelCrudDisabled`

**Why.** The extension is installed but switched off for this action: it was started with `MODEL_CRUD_READ_ONLY=1`, or with `MODEL_CRUD_ALLOW_UPLOAD` or `MODEL_CRUD_ALLOW_DELETE` set to 0.

**What to do.** Start ComfyUI without the setting that forbids it. Until then a result stays in the `output` folder of the server (the job is fine, it has its copy) and a Civitai import to that server fails.


### `ModelCrudRejected`

**Why.** The extension refused the call. The log has its error code (`code`) and message: for example `extension_not_allowed` (the file type is not on its list; see `MODEL_CRUD_EXTRA_EXTENSIONS`), `too_large`, `insufficient_storage`, `too_many_uploads` or `path_outside_root`.

**What to do.** Read the code in the log and the extension's documentation, and change the server or the request. A failed upload is cancelled on the server.


### `LoRADeleteFailed`

**Why.** A LoRA is deleted from every server before anything else, and at least one server could not do it: it is offline or does not answer, or the extension refused (`ModelCrudDisabled`, `ModelCrudMissing` or `ModelCrudRejected`; the log has the reason for each server). The message and the metadata `servers` name them. `deleted_from` lists the servers that already deleted the file. **No item was deleted and the Civitai cache was not touched.**

**What to do.** Start the servers named, or install or enable the extension on them, and delete the LoRA again. Servers that already deleted the file are not a problem the second time. If a server is gone for good, delete it on the Servers page.


## Civitai


### `CivitaiNotConfigured`

**Why.** Civitai needs an API key (and a cache folder, which has a default), and the key is missing.

**What to do.** Open Settings, paste your Civitai API key into *Civitai → API key*, save and press *Restart now*.


### `CivitaiModelNotFound`

**Why.** Civitai has no model with that id.

**What to do.** Check the number in the model's page address.


### `CivitaiRateLimited`

**Why.** Civitai asked the program to slow down.

**What to do.** Wait the `retry_after` it gives, or raise `civitai_pause_seconds` and try again.


### `CivitaiDownloadFailed`

**Why.** The download failed: the network, a refusal (check your key and whether the model needs you to be signed in), a file that does not match its checksum, or a model with no `.safetensors` file.

**What to do.** The message and `reason` say which. Try again later.


### `CivitaiMetadataInvalid`

**Why.** Civitai answered with something the program cannot read.

**What to do.** Try again later; if it repeats, Civitai may have changed its API.


## The program itself


### `StoreFailure`

**Why.** The database failed: the disk is full or read-only, the file is damaged, or something else holds it.

**What to do.** The log has the details (the web page and API only say "internal error"). Free space, check permissions, restore from your backup.


### `MigrationFailed`

**Why.** The database could not be brought to the current version when the program started.

**What to do.** The log says which step failed. Restore a backup and report the problem.


### `FileStoreFailure`

**Why.** A file or a folder could not be read, written or removed.

**What to do.** Check permissions and free space of the configured directories. A result that cannot be removed from ComfyUI is not this error: see `ModelCrudMissing`, `ModelCrudDisabled` and `ModelCrudRejected`.


### `FileOutsideStorage`

**Why.** A stored path points outside the configured directories. The program refuses to touch it.

**What to do.** This should not happen; it means the database was edited by hand or moved from another installation. Check the folders in Settings.


### `ConfigInvalid`

**Why.** A setting has a mistake: a value that is not a number, an address that is not `host:port`, a log level that is not one of the four, or the like. It is refused when you save it in the settings page, and also when the program starts with an environment variable (`BOS_…`) that is wrong.

**What to do.** The message lists **every** problem; fix them all. In the settings page nothing is saved until all are right. For an environment variable, correct it or unset it and start again.


### `ListenFailed`

**Why.** The program cannot listen on `listen_addr`: the port is in use or the address is not this machine's.

**What to do.** Pick another port, or stop the other program.


### `ServeFailed`

**Why.** The web server stopped with an error.

**What to do.** The log says why. Start again.


### `ShutdownFailed`

**Why.** Stopping took longer than the 10 seconds allowed.

**What to do.** Nothing is lost: jobs that were running are found again at the next start.


### `TemplateInvalid`

**Why.** One of the program's own page templates is broken.

**What to do.** This is a bug in the build. Report it with the log line.

