---
id: T-file-store
title: File store for events and entries
kind: build
contracts_consumed: [C-event-log]
contracts_produced: []
blocked_by: [C-event-log]
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-file-store · File store for events and entries

*§A only. Amendments (§B) are files in `amendments/`, handoffs (§C) are files in `handoffs/` (C-event-log §8).*

**Goal:** Read and write Ekumen's files exactly as C-event-log §7–§8 lays them out: append-only event files, and one markdown file per amendment and handoff. It must be safe for several writers on several branches. This task runs **in parallel with T-core**, so it works from the contract alone and treats event payloads as opaque.

**Context (verified on 2026-11-20 against origin/main):** the repo has the T0 scaffold; Go 1.26, standard library only. C-event-log v1 is frozen. T-core is building the typed events at the same time in `internal/event` and `internal/core`, which you must not touch or import. Parallel work is possible because both follow the same contract; that is what contracts are for.

**Job:**
1. `internal/store/files`: a `Store` rooted at a project's `.ekumen/` folder.
2. **Events:**
   - `Append(raw []byte) (path, error)`: parse only the envelope fields the store needs (`id`, `type`, `subject.kind`, `subject.id`) with a small private struct, validate them (ULID shape, id format from C-event-log §1, known subject kind), and write `events/<kind>/<subject id>/<event id>.<type>.json`.
   - Write atomically (temp file + rename in the same directory) and **never overwrite**: an existing path is an error.
   - `ReadAll() ([][]byte, error)` and `ReadSubject(kind, id)`, returning raw events sorted by event id. Ignore files that don't match the layout, but report them as warnings rather than silently skipping.
3. **Entries:**
   - `WriteEntry(task, kind, author, body) (path, sha256, error)` for kind `amendments` or `handoffs`, named `<UTC YYYYMMDDTHHMMSSZ>-<author slug>.md`. Never overwrite: on a name clash within the same second, add a `-2`, `-3` suffix.
   - `ReadBrief(task)` returns §A text, then amendments and handoffs, each sorted by name, each with its path and sha256.
   - `HashFile(path)` for the conflict checks T-core runs.
4. **Safety:** every id and kind is validated before it becomes a path, with no path traversal possible. Create directories with 0755 and files with 0644.
5. **Tests** (with `t.TempDir()`):
   - round trips
   - refuses to overwrite
   - sorted reads
   - many goroutines appending at once with no lost or corrupted files
   - an injected failure between temp write and rename leaves no partial event
   - two simulated branches (two directories) each appending, then merged by copying files, with the reader seeing the union

**Touches:** `internal/store/files/` and its `testdata/`. Your handoff files under `.ekumen/briefs/T-file-store/handoffs/`.

**Definition of done:**
- [ ] Layout matches C-event-log §8 exactly, with a test per path rule
- [ ] No event or entry file is ever modified or deleted by the store
- [ ] `go test -race ./...`, `go vet ./...` and `gofmt` clean; CI green on the draft PR
- [ ] Exported API documented with Go doc comments
- [ ] Handoff written, including FRICTION

**Out of scope:** typed events and their validation beyond the envelope fields above (T-core); projections such as `decisions/*.yaml`, `registry.yaml` and `state.json` (T-cli); git operations; database stores; anything vendor-specific.

**Escalate if:** the layout in C-event-log §8 can't be implemented safely as written (e.g. filesystem limits on names); or you need any dependency outside the standard library. Raise a decision (tier 2 for anything touching the contract) and carry on with the rest.

**Start prompt:**

    Read .ekumen/spine.md, then .ekumen/briefs/T-file-store/brief.md and any files in its amendments/ folder,
    then contract C-event-log.
    Work only inside the brief as amended. Never edit the spine, contracts, the brief or amendments.
    Report by adding a new file to .ekumen/briefs/T-file-store/handoffs/ named <UTC YYYYMMDDTHHMMSSZ>-t-file-store.md,
    with STATUS, OUTPUT, DECISIONS RAISED, FOLLOW-UPS, DEVIATIONS, FRICTION.
    Raise anything above your tier as a decision draft in that handoff, then carry on with what isn't blocked.
