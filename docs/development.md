# Development

## Prerequisites

- Go 1.26+ (pinned in `go.mod`).
- Node 24 and npm 11, only for UI work and `make build`. Plain `go build ./...` and `go test ./...` do not need Node.
- `make`.

## Layout

| Path | What |
|---|---|
| `cmd/ekumen` | The `ekumen` binary: `version` and `serve` |
| `internal/server` | HTTP server: `GET /api/health` and the UI at `/` |
| `internal/buildinfo` | Version, commit and build date, set with `-ldflags` |
| `web/` | Vite + React + TypeScript UI, plus the Go package that embeds it |

## Commands

| Command | What it does |
|---|---|
| `make dev` | Runs `ekumen serve` on `127.0.0.1:7477` and the Vite dev server on `http://127.0.0.1:5173` together. Open the Vite URL: UI edits hot-reload, and `/api` is proxied to the Go server. Ctrl-C stops both. Go changes need a restart. |
| `make build` | Builds the UI (`web/dist`), then `bin/ekumen` with the UI embedded and version info injected. |
| `make test` | `go test ./...` and the UI typecheck. |
| `make lint` | `go vet`, a `gofmt` check and `tsc --noEmit`. |
| `make clean` | Removes `bin/` and `web/dist/`. |

Make targets that need Node run `npm ci` in `web/` when `node_modules` is missing or older than the lockfile.

Run the binary with `bin/ekumen serve [--addr 127.0.0.1:7477]` and open `http://127.0.0.1:7477`. It binds to localhost by default. `bin/ekumen version` prints the injected version, commit and build date (`dev` for a plain `go build`).

## How embedding works

The UI is embedded with `//go:embed` in package `github.com/Yashswarnkar/ekumen/web` (`web/embed*.go`). A build tag chooses what is embedded:

| Build | Embedded | Used by |
|---|---|---|
| default (no tag) | `web/placeholder/`, a committed one-page placeholder | `go build ./...`, `go test ./...`, `go run`, `make dev` |
| `-tags embedui` | `web/dist/`, the Vite build output | `make build`, CI |

Why a build tag: `//go:embed` fails to compile when its pattern matches nothing, and `web/dist/` is build output (git-ignored, and emptied by Vite on every build). The tag keeps a fresh clone buildable without Node, and makes a release build fail loudly if the UI was not built, instead of silently shipping the placeholder. `ekumen serve` logs which one it serves (`ui embedded: true|false`).

To build with the UI by hand: `(cd web && npm ci && npm run build) && go build -tags embedui ./cmd/ekumen`.
