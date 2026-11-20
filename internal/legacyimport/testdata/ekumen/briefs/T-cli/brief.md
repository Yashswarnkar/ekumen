---
id: T-cli
title: "ekumen CLI with --json, incl. ekumen map"
kind: build
contracts_consumed: [C-cli-json, C-event-log, C-decision-record]
contracts_produced: []
blocked_by: [C-cli-json, T-core, T-file-store, D-yaml-library]
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-cli · ekumen CLI with --json, incl. ekumen map

*§A only. Amendments (§B) are files in `amendments/`, handoffs (§C) are files in `handoffs/` (C-event-log §8).*

**Goal:** Every command in C-cli-json, so that the agents and the orchestrator stop editing `.ekumen/` by hand. This is the **switch point** in spine §8. Thin glue: all logic is in `internal/core`, all file access in `internal/store/files`.

**Context (to re-verify against origin/main when you start):** T-core (`internal/event`, `internal/core`) and T-file-store (`internal/store/files`) are merged. The T0 scaffold has `cmd/ekumen` with `version` and `serve`. `tools/map-prototype/` is the hand-run build map this task replaces; its look and data are the reference for `ekumen map`. YAML library: per D-yaml-library.

**Job:**
1. `internal/cli`: argument parsing (standard library `flag`), resolving the project root, the actor (`--as` / `EKUMEN_ACTOR`), `--json` output, the exit codes and error shape from C-cli-json §1.
2. Every read and write command in C-cli-json §2–§3, built on `core` and `files`. A write checks lanes and role claims through `core`, then appends through `files`, all or nothing.
3. **Projections:** after every successful write, regenerate `.ekumen/decisions/<id>.yaml`, `.ekumen/registry.yaml` and `.ekumen/state.json` from events (C-event-log §8, C-decision-record §4). Also `ekumen project` to regenerate them on demand.
4. **Observed state:** `status`, `task` and `graph` fill `observed.pr` from `gh pr list --json` when `gh` is available. If it isn't, `observed` is `null` and a warning goes to stderr. Put this behind a small interface so tests use a fake. `gh` is a portable git-host tool, not an agent vendor; keep it out of `core`.
5. **`ekumen map`:** port the prototype to Go, rendering from the same data as `graph` and `inbox`. It produces one self-contained HTML file that works offline: vendor dagre 0.8.5 (MIT) into the binary with its license file, rather than loading it from a CDN.
6. **`ekumen check`:** C-cli-json §2.
7. **Tests:**
   - golden tests for every command's `--json` output against a fixture `.ekumen/`
   - exit codes
   - lane refusals
   - an end-to-end script test: raise → question → answer → amend → handoff → status, run against a temp project

**Touches:** `cmd/ekumen/`, `internal/cli/`, `internal/project/` (projections), `internal/observe/`, `internal/mapview/`, `go.mod` (the YAML library only), `docs/cli.md`, your handoff files. Don't modify `internal/core`, `internal/event` or `internal/store/files`. If they lack something, record it as a FOLLOW-UP and work around it in `internal/cli`.

**Definition of done:**
- [ ] Every C-cli-json command implemented, with `--json` output matching §4 (golden tests)
- [ ] Projections regenerate after writes and match the shapes of today's hand-written files
- [ ] `ekumen map` output opens offline and shows inbox, graph and decided list
- [ ] `docs/cli.md` with an example per command
- [ ] `go test -race ./...`, vet and gofmt clean; CI green on the draft PR
- [ ] Handoff written **using `ekumen task handoff`** from your own branch, plus FRICTION about using the CLI on itself

**Out of scope:** importing today's hand-written records (T-import); `/blocked` and `/decide` skills; the control loop; `serve` and its UI; agent adapters.

**Escalate if:** a C-cli-json shape can't be produced from what `core` exposes; or any dependency beyond the YAML library and vendored dagre seems needed.

**Start prompt:**

    Read .ekumen/spine.md, then .ekumen/briefs/T-cli/brief.md and its amendments/,
    then contracts C-cli-json, C-event-log and C-decision-record.
    Work only inside the brief as amended. Never edit the spine, contracts, the brief or amendments.
    Report by adding a handoff to .ekumen/briefs/T-cli/handoffs/ (use `ekumen task handoff` once it works).
    Raise anything above your tier as a decision draft in that handoff, then carry on with what isn't blocked.
