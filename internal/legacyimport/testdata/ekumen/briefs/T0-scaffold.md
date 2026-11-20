---
id: T0
title: Repo scaffold
kind: build
contracts_consumed: []
contracts_produced: []
blocked_by: []
tier_grants: [push_own_branch, open_draft_pr, restack, add_listed_dependencies]
---

# T0 · Repo scaffold

## §A Brief (frozen on approval)

**Goal:** A working skeleton for the one-repo layout in spine §6: a Go binary that serves an embedded React + TypeScript UI, a one-command dev loop, and CI. No domain logic.

**Context (verified on 2026-11-20 against origin/main):** the repo holds only docs and `.ekumen/`. Local toolchain: Go 1.26.0, Node 24.12.0, npm 11.6.2. Module path: `github.com/Yashswarnkar/ekumen`.

**Job:**
1. `go.mod` for `github.com/Yashswarnkar/ekumen`.
2. `cmd/ekumen/main.go` with two commands only:
   - `ekumen version`: version, commit and build date, injected with `-ldflags`, defaulting to `dev`.
   - `ekumen serve [--addr 127.0.0.1:7477]`: serves the embedded UI at `/` and `GET /api/health` → `{"status":"ok","version":"…"}`. Binds to localhost by default.
3. `internal/` with only what the two commands need (e.g. `internal/server`, `internal/buildinfo`). No placeholder domain packages.
4. `web/`: Vite + React + TypeScript. One page showing "Ekumen" and the result of `/api/health`. In dev mode, Vite proxies `/api` to the Go server.
5. Embedding: `web/dist` is embedded with `//go:embed`. **Plain `go build ./...` must succeed on a fresh clone without Node**, serving a small placeholder page when the UI hasn't been built. Pick and document the mechanism (e.g. a committed placeholder `index.html`, or a build tag).
6. `Makefile`: `dev` (Go server + Vite dev server together), `build` (UI then binary to `bin/ekumen`), `test` (Go tests + UI typecheck), `lint` (`go vet`, `gofmt` check, `tsc --noEmit`).
7. Tests: a Go test for `/api/health` and for serving the placeholder or embedded index.
8. GitHub Actions CI on push and PR: Go build, vet, gofmt check and test; UI install, typecheck and build; then build the binary with the UI embedded.
9. `docs/development.md`: prerequisites, `make dev`, `make build`, and how embedding works. Do not edit README.md.
10. `.gitignore` for `bin/`, `web/node_modules/`, `web/dist/` (except anything the placeholder mechanism needs).

**Dependencies allowed:** Go standard library only. Web: `react`, `react-dom`, `vite`, `typescript`, `@vitejs/plugin-react`, `@types/react`, `@types/react-dom`, `@types/node`. Anything else (a CLI framework, router, UI kit, lint tooling) → tier-1 decision with a default, in your handoff.

**Touches:** `go.mod`, `cmd/`, `internal/`, `web/`, `Makefile`, `.github/workflows/`, `docs/development.md`, `.gitignore`.

**Definition of done:**
- [ ] `go build ./...` and `go test ./...` pass on a fresh clone without Node
- [ ] `make build` produces `bin/ekumen`; `bin/ekumen serve` shows the React page with health status
- [ ] `make dev` works with live reload for the UI
- [ ] CI is green on the draft PR
- [ ] Handoff written in §C, including FRICTION

**Out of scope:** events, decisions, tasks or any other domain code; CLI commands beyond `version` and `serve`; UI design; release packaging (goreleaser, Homebrew).

**Escalate if:** embedding without Node forces an awkward tradeoff; Go 1.26 or Node 24 cause CI problems that need a version decision; any dependency outside the list seems necessary.

**Start prompt:**

    Read .ekumen/spine.md, then this brief (.ekumen/briefs/T0-scaffold.md).
    Work only inside §A as amended by §B. Never edit the spine, contracts, §A or §B.
    Report only by appending to §C. Raise anything above your tier as a decision record draft in §C, then stop.

## §B Amendments (orchestrator; append-only, dated)
- 2026-11-20: approved by lead.

## §C Handoff (task session; append-only, dated)

<!-- Format:
### YYYY-MM-DD
STATUS: …
OUTPUT: …
DECISIONS RAISED: …
FOLLOW-UPS: …
DEVIATIONS: …
FRICTION: what was painful by hand; what was ambiguous in the Ekumen format
-->

### 2026-11-20
STATUS: in_review. All §A job items done; draft PR #1 (https://github.com/Yashswarnkar/ekumen/pull/1), CI green. Not merged.

OUTPUT:
- `go.mod` (stdlib only), `cmd/ekumen/main.go` (`version`, `serve [--addr 127.0.0.1:7477]`, graceful shutdown on SIGINT/SIGTERM), `internal/buildinfo`, `internal/server` (`GET /api/health` → `{"status":"ok","version":…}`, other `/api/*` → 404, UI at `/`).
- `web/`: Vite 8 + React 19 + TypeScript 7 page showing "Ekumen" and the health result; Vite proxies `/api` to the Go server (`EKUMEN_API` overrides the target).
- Embedding: build tag `embedui`. Default builds embed the committed `web/placeholder/index.html`; `-tags embedui` embeds `web/dist` (`all:dist`) and fails to compile if the UI was not built. Code in `web/embed*.go` (Go package `github.com/Yashswarnkar/ekumen/web`). `serve` logs `ui embedded: true|false`.
- `Makefile`: `dev`, `build`, `test`, `lint`, `clean`; Node targets run `npm ci` when `node_modules` is stale.
- Tests: `internal/server` (health, index served from an fs, unknown API route 404) and `web` (embedded FS has `index.html`; runs under both tags).
- CI (`.github/workflows/ci.yml`): Go job with no Node (build, vet, gofmt, test); UI job (npm ci, typecheck, `make build`, `go test -tags embedui`, smoke test of `bin/ekumen`).
- `docs/development.md`, `.gitignore`.
- Verified locally: fresh clone with Node off PATH and no `web/dist` → `go build`, `go vet`, `go test` pass; `make lint`, `make test`, `make build` pass; `bin/ekumen serve` returns health JSON and the built index, JS asset 200; `make dev` → both ports listen (7477, 5173), `/api/health` works through the Vite proxy, HMR client served, Ctrl-C stops both with nothing left listening.

DECISIONS RAISED: none. No dependency outside the §A list was needed; Go 1.26 and Node 24 work in CI as-is.

FOLLOW-UPS:
- CI runs twice per PR push (`push` + `pull_request` triggers, as §A asks). Consider limiting `push` to `main` later.
- The React page was verified over HTTP (index + bundle served, health JSON), not in a real browser. A browser smoke test would need a dependency outside the list (e.g. Playwright).
- No SPA fallback for client-side routes yet; add when the UI gets a router.

DEVIATIONS: none from §A. Added `make clean` and a CI smoke test beyond the list; both are within the touched files.

FRICTION:
- npm lockfile: installing deps in two steps (`npm install` then `npm install -D`) produced a lockfile missing optional `@emnapi/*` entries, so `npm ci` failed only on Linux CI. Fixed by regenerating from a clean install. Worth a note in future briefs that add web deps: regenerate the lockfile from scratch and run `npm ci`.
- §A said "embed `web/dist`" and "Plain `go build` must succeed without Node"; `//go:embed` cannot reference a missing directory, so some mechanism choice was unavoidable. The brief anticipated this, which helped.
- Format ambiguity: §A's "Touches" didn't list `.ekumen/briefs/T0-scaffold.md` §C, though the start prompt requires writing it; implicitly fine but worth stating in the brief template.
- Ambiguous whether `make dev` must reload Go code too ("live reload for the UI" read as UI only; Go changes need a restart, documented).
