# Configuration

The configuration is one JSON file, given with `-config`, plus environment variables (`BOS_` and the setting in capitals; they win over the file). The log level can also be given on the command line, `-log-level debug`, which wins over both. A key the program does not know is an error, and every problem in the file is reported together when the program starts.

**A missing file is created.** If the file after `-config` does not exist, the program writes it (and its folder) on start: the database and every folder go under a `data` folder next to the file, as full paths, and the address is `127.0.0.1:8080`. An existing file is never changed. If the file cannot be written, the program stops with `ConfigNotWritable`.

| Setting | Required | Default | Meaning |
|---|---|---|---|
| `database` | yes | | The SQLite file (`:memory:` keeps nothing between runs) |
| `results_dir` | yes | | Generated results |
| `controlnet_dir` | yes | | Uploaded ControlNet images |
| `ipadapter_dir` | yes | | Uploaded IP-Adapter images |
| `regions_dir` | yes | | Region reference images and the masks made from them |
| `thumbnails_dir` | yes | | Group and item thumbnails |
| `downloads_dir` | yes | | The ZIP archives of finished commands |
| `listen_addr` | no | `127.0.0.1:8080` | Where the server listens, `host:port` |
| `auth_token` | no | none | If set, the web page asks for it and the API needs it as a bearer token |
| `upload_limit_bytes` | no | 64 MiB | Largest accepted uploaded file |
| `cors_allow_list` | no | none | Origins that may call the API from a browser on another site |
| `grace_seconds` | no | 30 | How long a server may be unreachable before its waiting jobs go elsewhere |
| `max_job_attempts` | no | 3 | How many times a job is tried again after its server vanished |
| `preview_soft_limit` | no | 0 (none) | A preview warns above this many jobs |
| `event_log_size` | no | 100 | How many events are kept for clients that ask what they missed (at least 100) |
| `log_level` | no | `info` | `debug`, `info`, `warn` or `error`. At `info` the log has one line for every prompt sent to ComfyUI; at `debug` it also has the whole workflow |
| `civitai_token` | for Civitai | none | Your Civitai API key |
| `civitai_cache_dir` | for Civitai | none | Where downloaded models are kept |
| `civitai_domain` | no | `civitai.com` | Change it only if your account lives on another Civitai domain |
| `civitai_pause_seconds` | no | 15 | Wait between two downloads of one import |

A few of these in more words:

- **`auth_token`** protects the web page (cookie) and the REST API (bearer token). `/healthz` stays open. There are no user accounts: one token for everyone who knows it.
- **`grace_seconds`** and **`max_job_attempts`**: a server that cannot be reached is not given up at once, because a restart takes a moment. After the grace period, jobs that were waiting for it (and are not pinned) go to other servers. A job whose server vanishes *while it runs* is queued again and counted; after `max_job_attempts` it fails.
- **`cors_allow_list`** is only for web pages on other origins that call the API with a token. The built-in web page does not need it.
- **`upload_limit_bytes`** applies to each uploaded file; a request that carries much more than that in total is refused too.

**The log is printed on the console** (standard error), one JSON object per line. To see everything, including every request and the whole workflow sent to ComfyUI, start with `./bin/bos -config config.json -log-level debug`. The line `listening` says which level is in force.

**What was sent to ComfyUI.** For every prompt the program queues, the log has a line `sending a prompt to ComfyUI` with the job, the command, the server, the seed and, for each sampler in the workflow, the node its positive and negative prompt come from (id, class, title) and the text that node holds. The text is read back from the finished workflow, not from the job, so it shows what ComfyUI will encode. If the text there differs from the job's prompt, the generator's prompt node is not the one the sampler reads. Jobs of a generator without *new random seed for every job* show the same `seed`. If the generator names no prompt node, a `WARN` line says so for each job, because its prompt is then not sent at all. At `debug` a second line, `workflow sent to ComfyUI`, holds the whole workflow as JSON.

The web page, `bosctl` and the REST API all read the same running server; there is nothing to configure per interface.
