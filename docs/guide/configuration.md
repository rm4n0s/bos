# Configuration

The configuration is one JSON file, given with `-config`, plus environment variables (`BOS_` and the setting in capitals; they win over the file). A key the program does not know is an error, and every problem in the file is reported together when the program starts.

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
| `log_level` | no | `info` | `debug`, `info`, `warn` or `error` |
| `civitai_token` | for Civitai | none | Your Civitai API key |
| `civitai_cache_dir` | for Civitai | none | Where downloaded models are kept |
| `civitai_domain` | no | `civitai.com` | Change it only if your account lives on another Civitai domain |
| `civitai_pause_seconds` | no | 15 | Wait between two downloads of one import |

A few of these in more words:

- **`auth_token`** protects the web page (cookie) and the REST API (bearer token). `/healthz` stays open. There are no user accounts: one token for everyone who knows it.
- **`grace_seconds`** and **`max_job_attempts`**: a server that cannot be reached is not given up at once, because a restart takes a moment. After the grace period, jobs that were waiting for it (and are not pinned) go to other servers. A job whose server vanishes *while it runs* is queued again and counted; after `max_job_attempts` it fails.
- **`cors_allow_list`** is only for web pages on other origins that call the API with a token. The built-in web page does not need it.
- **`upload_limit_bytes`** applies to each uploaded file; a request that carries much more than that in total is refused too.

The web page, `bosctl` and the REST API all read the same running server; there is nothing to configure per interface.
