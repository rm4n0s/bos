# Settings

The settings are kept **in the database**, not in a file. Open **Settings** in the web page, change what you need, press *Save* and then *Restart now*: the program stops as it does for Ctrl-C and starts again by itself, reading the new settings. A field left empty uses its default.

The only things given on the command line are the database file, `-db` (default `data/bos.db`), and `-log-level`. If the database file does not exist, the program creates it, with its folder and its schema, and gives it default settings: every folder is **next to the database file** (`results`, `controlnet`, `ipadapter`, `regions`, `thumbnails`, `downloads`, `civitai`), as full paths. So `./bin/bos` with nothing else keeps everything in `./data`, and `./bin/bos -db /srv/bos/bos.db` keeps it in `/srv/bos`.

A setting can also come from an environment variable named `BOS_` plus the setting in capitals: `BOS_LISTEN_ADDR=0.0.0.0:8080`, `BOS_AUTH_TOKEN=secret`. The variable wins for that run, and the Settings page says so next to the field; what you save there applies again when the variable is not set. A value that does not parse is refused when you save it, and when the program starts with a wrong variable, with every problem listed together.

| Setting | Default | Meaning |
|---|---|---|
| `results_dir` | `results` next to the database | Generated results |
| `controlnet_dir` | `controlnet` next to the database | Uploaded ControlNet images |
| `ipadapter_dir` | `ipadapter` next to the database | Uploaded IP-Adapter images |
| `regions_dir` | `regions` next to the database | Region reference images and the masks made from them |
| `thumbnails_dir` | `thumbnails` next to the database | Group and item thumbnails |
| `downloads_dir` | `downloads` next to the database | The ZIP archives of finished commands |
| `civitai_cache_dir` | `civitai` next to the database | Where downloaded models are kept |
| `civitai_token` | none | Your Civitai API key; needed to import LoRAs. Never shown again after it is saved |
| `civitai_domain` | `civitai.com` | Change it only if your account lives on another Civitai domain |
| `civitai_pause_seconds` | 15 | Wait between two downloads of one import |
| `delete_comfy_outputs` | `true` | After a result is saved here, delete it from the `output` folder of its ComfyUI (`true` or `false`), through the [comfyui-model-crud](catalogue.md#servers) extension that every server runs. A file that cannot be deleted is only logged |
| `listen_addr` | `127.0.0.1:8080` | Where the server listens, `host:port` |
| `auth_token` | none | If set, the web page asks for it and the API needs it as a bearer token. Never shown again after it is saved |
| `cors_allow_list` | none | Origins that may call the API from a browser on another site, separated by commas |
| `upload_limit_bytes` | 64 MiB | Largest accepted uploaded file |
| `grace_seconds` | 30 | How long a server may be unreachable before its waiting jobs go elsewhere |
| `max_job_attempts` | 3 | How many times a job is tried again after its server vanished |
| `preview_soft_limit` | 0 (none) | A preview warns above this many jobs |
| `log_level` | `info` | `debug`, `info`, `warn` or `error`. At `info` the log has one line for every prompt sent to ComfyUI; at `debug` it also has the whole workflow |
| `event_log_size` | 100 | How many events are kept for clients that ask what they missed (at least 100) |

A few of these in more words:

- **Credentials.** `civitai_token` and `auth_token` are not written into the page: it says whether a value is set. Leave the field empty to keep it, type a new one to replace it, or tick *remove it*.
- **`auth_token`** protects the web page (cookie) and the REST API (bearer token), including the Settings page. `/healthz` stays open. There are no user accounts: one token for everyone who knows it.
- **`grace_seconds`** and **`max_job_attempts`**: a server that cannot be reached is not given up at once, because a restart takes a moment. After the grace period, jobs that were waiting for it (and are not pinned) go to other servers. A job whose server vanishes *while it runs* is queued again and counted; after `max_job_attempts` it fails.
- **`cors_allow_list`** is only for web pages on other origins that call the API with a token. The built-in web page does not need it.
- **`upload_limit_bytes`** applies to each uploaded file; a request that carries much more than that in total is refused too.
- **The restart** also keeps the jobs that were running: they are picked up again, as after Ctrl-C. If you changed `listen_addr`, the page comes back at the new address, not the old one.
- **Moving the database.** The folders are stored as full paths, so a copied database keeps pointing at the old ones. Change them in Settings, or clear a field to get the folder next to the database again.

**The log is printed on the console** (standard error), one JSON object per line. To see everything, including every request and the whole workflow sent to ComfyUI, start with `./bin/bos -log-level debug`. The line `listening` says which level is in force.

**What was sent to ComfyUI.** For every prompt the program queues, the log has a line `sending a prompt to ComfyUI` with the job, the command, the server, the seed and, for each sampler in the workflow, the node its positive and negative prompt come from (id, class, title) and the text that node holds. The text is read back from the finished workflow, not from the job, so it shows what ComfyUI will encode. If the text there differs from the job's prompt, the generator's prompt node is not the one the sampler reads. Jobs of a generator without *new random seed for every job* show the same `seed`. If the generator names no prompt node, a `WARN` line says so for each job, because its prompt is then not sent at all. The line also has `loras`: the LoRA loaders that are in the finished workflow, each as file, model strength and text-encoder strength. At `debug` a second line, `workflow sent to ComfyUI`, holds the whole workflow as a JSON object (not as an escaped string), so `jq .workflow` prints it and a log viewer shows all of it.

