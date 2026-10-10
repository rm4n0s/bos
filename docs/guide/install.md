# Install, configure, run

## Build

You need Go 1.26.3 or newer and a C compiler (the SQLite driver is built from C source; Go's `CGO_ENABLED` must be `1`, which is the default when a compiler is installed).

```sh
go build -o bin/ ./cmd/bos ./cmd/bosctl
```

This makes two programs: `bos`, the server, and `bosctl`, the terminal client. The first build compiles SQLite, which takes about a minute and may print a harmless compiler warning. Later builds are fast.

If the build works but the program says it cannot open the database, the program was built without cgo: check `go env CGO_ENABLED` and that `gcc` (or `clang`) is on your path.

## Run

```sh
./bin/bos
```

The program keeps everything in one SQLite database, `data/bos.db` by default (`-db path/to/bos.db` for another place). If the file does not exist it is created, with its folder, and the folders for results, uploaded images and downloads are made next to it. Nothing needs to be written by hand: **the settings are in the database**, and the *Settings* page of the web interface edits them. See [Settings](configuration.md).

The server writes JSON log lines to standard error and serves on `127.0.0.1:8080`. Open <http://127.0.0.1:8080> for the web page. `curl http://127.0.0.1:8080/healthz` answers `{"status":"ok"}` when it is up.

Stop it with Ctrl-C (or `SIGTERM`). It finishes the requests in progress, stops its workers and closes the database. Jobs that were running stay *processing* in the database; when you start again the program finds them and waits for their results from ComfyUI if it still remembers them, or queues them again.

For more detail in the log, start it with `-log-level debug` (or set the *Log level* in Settings): every request is then logged (method, path, status, time).

## Letting other machines in

By default only this machine can reach the server. To open it:

1. In *Settings*, set the **Access token** to a long secret.
2. Set the **Listen address** to `0.0.0.0:8080` (or the address of one network card), save and press *Restart now*.

The web page then asks for the token once and remembers it in a cookie; `bosctl` and other clients send it as `Authorization: Bearer <token>`. If you listen on a non-local address without a token the program logs a warning. The connection is plain HTTP: put a reverse proxy with TLS in front of it if the network is not yours.

## Back up

Copy the database file (it holds the settings too) and the data folders next to it while the server is stopped. Everything the program knows is in them.
