# Development

## Prerequisites

- Go 1.26+ (pinned in `go.mod`).
- Node 24 and npm 11, only for UI work and `make build`. Plain `go build ./...` and `go test ./...` do not need Node.
- `make`.

## Layout

| Path | What |
|---|---|
| `cmd/ekumen` | The `ekumen` binary (see [cli.md](cli.md)) |
| `internal/cli`, `internal/project`, `internal/core`, `internal/event`, `internal/store/files` | Commands, views and projections, domain rules, events, the file store |
| `internal/mapview` | `ekumen map`: the static build map page and its links to the git host |
| `scripts/ci-check.sh` | The CI `ekumen check` and `ekumen project --check` step |
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

## CI

`.github/workflows/ci.yml` runs on every push and pull request. The `Go (no Node)` job builds, vets, checks `gofmt`, runs `go test -race ./...`, and then runs **`ekumen check`** on the repository:

```
go build -o bin/ekumen ./cmd/ekumen
scripts/ci-check.sh .
```

`scripts/ci-check.sh [root]` runs `ekumen check --json` and then `ekumen project --check --json` at the root (binary from `$EKUMEN_BIN`, default `bin/ekumen`, with `EKUMEN_OBSERVE=off`), prints the JSON so a failure is readable in the log, and fails if either fails. So nothing merges with invalid records, lane violations or conflicts in `.ekumen/` (exit 2 or 4), or with a committed `.ekumen/decisions/<id>.yaml` that doesn't match the events (exit 2: run `ekumen project` and commit). `.ekumen/registry.yaml` and `.ekumen/state.json` are git-ignored and regenerated locally by every `ekumen` write (D-projections-in-git); a fresh clone needs neither. An `.ekumen/events/` folder with no events fails the step. The event log is append-only: against a base commit, a deleted, modified or renamed events file fails, and so does a missing `.ekumen/events/` that exists on the base. The base is `$EKUMEN_CI_BASE` (CI sets it to the pull request's base commit); otherwise the push's previous commit in GitHub Actions, otherwise the merge-base with `origin/main`. Only with no base, or when the folder is absent on the base too, is a missing `.ekumen/events/` skipped with a notice. Run it locally the same way before pushing. `cmd/ekumen/cicheck_test.go` checks the script against fixtures, including one with a lane violation and one with a stale decision file.

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
