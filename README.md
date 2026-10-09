# Bowl of Scenes

Bowl of Scenes makes images (or video, or 3D models) in bulk. You keep reusable pieces of prompt in **groups** (characters, poses, places, styles), write a one-line **command** that combines them, and the server runs every combination as a **job** on one or more [ComfyUI](https://github.com/comfyanonymous/ComfyUI) servers.

```
gpu1, gpu2 -$ anime: characters * poses(~jumping) * "masterpiece" where not #acrobatic limit 20 > upscale
```

That line says: on the servers `gpu1` and `gpu2`, with the generator `anime`, make every character in every pose except `jumping`, add the text “masterpiece” to each prompt, leave out anything tagged `acrobatic`, keep the first 20, and run each result through the fixer `upscale`.

You work in a web page, from a terminal (`bosctl`) or through a REST API. All three do the same things.

## What it does

- **Combine** groups of prompt pieces with a small language: products, merges, pairs, filters by name, pattern or tag, templates, regions, random samples, repeats, setting overrides. See the [command language](docs/guide/commands.md).
- **Preview** a command before you add it: how many jobs, how they split across servers, what is missing.
- **Run on several ComfyUI servers at once**, split evenly, in blocks, by item, or dynamically as servers become free. A server that goes away does not lose work: its jobs run elsewhere.
- **Improve results** with fixers (upscale, face repair, background removal), also on another server.
- **Keep everything**: every job records its prompts, seed and result. Edit a prompt and run the one job again; download a finished command as a ZIP.
- **Import LoRAs from Civitai** as a ready-made group.
- **Stay yours**: it runs on your machine, contacts only your ComfyUI servers (and Civitai if you ask), and has no telemetry.

## What you need

- **Go 1.26.3 or newer** and a **C compiler** (SQLite is built from source, so `CGO_ENABLED=1`; on Fedora `gcc`, on Debian or Ubuntu `build-essential`).
- One or more **ComfyUI** servers, reachable over HTTP, with the models your workflows use.

## Install and run

```sh
go build -o bin/ ./cmd/bos ./cmd/bosctl
cp config.example.json config.json
./bin/bos -config config.json
```

Open <http://127.0.0.1:8080>. The first build compiles SQLite and takes a minute; later builds are quick. The data (database, results, uploads) goes where `config.json` says.

Then follow the [tutorial](docs/tutorial.md): from an empty program to a finished batch, in the web page and with `bosctl`.

## Documentation

| | |
|---|---|
| [Tutorial](docs/tutorial.md) | A first batch, step by step |
| [User guide](docs/guide/README.md) | Every concept and screen |
| [Command language](docs/guide/commands.md) | The one-line language, with examples |
| [`bosctl` reference](docs/cli.md) | Every terminal command (generated) |
| [Guide for AI agents](docs/agents.md) | Rules and a tested worked session for a program that drives `bosctl` |
| [REST API](docs/openapi.json) | The OpenAPI description; the running server serves it at `/api/v1/openapi.json` |
| [Troubleshooting](docs/troubleshooting.md) | Every error the program can give, and what to do |
| [Specification](docs/SPEC.md) | The exact behaviour, for people who build on it or check it |
| [Acceptance tests](docs/ACCEPTANCE.md) | Which test checks each promise of the specification |

## Safety

By default the server listens only on `127.0.0.1`. If you let other machines reach it, set `auth_token` in the configuration: the web page then asks for the token once, and the API wants it as a bearer token. Server addresses are whatever you enter, so the program will connect to them; do not run it where untrusted people can add servers. See [the configuration](docs/guide/configuration.md).

## For developers

```sh
go build ./...
go vet ./...
golangci-lint run
go test -race ./...
```

All four must pass before a change is done. `internal/domain` and `internal/command` are pure (no I/O); `internal/service` holds the use cases; the REST API (`internal/api`), the web page (`internal/ui`) and `bosctl` (`internal/cli`) are thin layers over them. [CLAUDE.md](CLAUDE.md) has the project rules, [docs/IMPLEMENTATION.md](docs/IMPLEMENTATION.md) the build plan and [docs/IMPLEMENTED.md](docs/IMPLEMENTED.md) the log of what was built and why.
