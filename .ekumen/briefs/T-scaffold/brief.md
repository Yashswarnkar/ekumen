---
id: T-scaffold
aliases: [T0]
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
