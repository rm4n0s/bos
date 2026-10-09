# Install, configure, run

## Build

You need Go 1.26.3 or newer and a C compiler (the SQLite driver is built from C source; Go's `CGO_ENABLED` must be `1`, which is the default when a compiler is installed).

```sh
go build -o bin/ ./cmd/bos ./cmd/bosctl
```

This makes two programs: `bos`, the server, and `bosctl`, the terminal client. The first build compiles SQLite, which takes about a minute and may print a harmless compiler warning. Later builds are fast.

If the build works but the program says it cannot open the database, the program was built without cgo: check `go env CGO_ENABLED` and that `gcc` (or `clang`) is on your path.

## Configure

Copy the example and edit it:

```sh
cp config.example.json config.json
```

```json
{
  "database": "data/bos.db",
  "results_dir": "data/results",
  "controlnet_dir": "data/controlnet",
  "ipadapter_dir": "data/ipadapter",
  "regions_dir": "data/regions",
  "thumbnails_dir": "data/thumbnails",
  "downloads_dir": "data/downloads",
  "listen_addr": "127.0.0.1:8080"
}
```

The first seven keys are required. Paths are relative to the folder you start `bos` from. The directories are made if they do not exist. Everything else is optional; see [Configuration](configuration.md).

Any setting can also come from an environment variable named `BOS_` plus the setting in capitals: `BOS_LISTEN_ADDR=0.0.0.0:8080`, `BOS_AUTH_TOKEN=secret`. The variable wins over the file.

## Run

```sh
./bin/bos -config config.json
```

The server writes JSON log lines to standard error and serves on `listen_addr`. Open <http://127.0.0.1:8080> for the web page. `curl http://127.0.0.1:8080/healthz` answers `{"status":"ok"}` when it is up.

Stop it with Ctrl-C (or `SIGTERM`). It finishes the requests in progress, stops its workers and closes the database. Jobs that were running stay *processing* in the database; when you start again the program finds them and waits for their results from ComfyUI if it still remembers them, or queues them again.

For more detail in the log, set `"log_level": "debug"`: every request is then logged (method, path, status, time).

## Letting other machines in

By default only this machine can reach the server. To open it:

1. Set `"auth_token": "a long secret"`.
2. Set `"listen_addr": "0.0.0.0:8080"` (or the address of one network card).

The web page then asks for the token once and remembers it in a cookie; `bosctl` and other clients send it as `Authorization: Bearer <token>`. If you listen on a non-local address without a token the program logs a warning. The connection is plain HTTP: put a reverse proxy with TLS in front of it if the network is not yours.

## Back up

Copy the database file and the data directories while the server is stopped. Everything the program knows is in them.
