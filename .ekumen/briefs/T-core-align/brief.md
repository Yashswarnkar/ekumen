---
id: T-core-align
title: Align core and file store with C-event-log v3
kind: build
contracts_consumed: [C-event-log, C-decision-record]
contracts_produced: []
blocked_by: [T-import, D-contract-blockers]
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-core-align · Align core and file store with C-event-log v3

*§A only. Amendments (§B) are files in `amendments/`, handoffs (§C) are files in `handoffs/` (C-event-log §8).*

**Goal:** `internal/core`, `internal/event` and `internal/store/files` were built against C-event-log v1/v2 and C-decision-record v1. Bring them up to the current versions, so that the real import at the switch point produces **zero** validation failures and zero lane conflicts, and `ekumen check` is clean.

**Context (to re-verify against origin/main when you start):** T-import's dry run (handoff `20261120T053847Z-t-import.md`, FOLLOW-UPS 1–3 and FRICTION) found the gaps listed below. T-import is merged before this starts. T-cli may be merged or in flight; don't touch its packages.

**Job:**
1. **Aliases:** `ValidateAlias` accepts pre-contract ids (`R1`, `T0`, `C1`, `D-001`…). An alias is any non-empty string without `/` or whitespace that isn't already an id. Add optional `aliases[]` to `TaskDrafted`, `ContractDrafted` and `ExternalAdded` (v3 §1).
2. **Lanes v3** (§3): `DecisionRaised` by lead, orchestrator, session, integrator or loop. A session or integrator raises only from its own task; others may use `raised_by.task: null`. `TaskSessionAttached` by orchestrator or loop. `RouteResolved` by the route's owner. "Own task" comes from `TaskDrafted.session` (new optional field) or the latest `TaskSessionAttached`.
3. **Contract blockers:** implement what D-contract-blockers decides. If it decides `optional-fields`, that means `blocked_by[]` on `ContractDrafted`, a `ContractBlockersChanged` event (orchestrator lane), and contract blockers counted in task status and priority.
4. **Ordering of events written at the same instant:** when several events share a timestamp (as in imports and replays), the reducer must still apply lifecycle steps in order: draft before approve, raise before decide, decide before apply. Implement this as a stable tie-break on a per-type rank, not by relying on ULID entropy. Document it in `internal/core/README.md`, and draft a one-line addition to C-event-log §2 in your handoff for the orchestrator.
5. **File store:** `WriteBrief(task, body)` (create only, never overwrite) and `WriteEntryAt(task, kind, author, at time.Time, body)` for historical entries, with the same naming and clash rules as `WriteEntry`.
6. **Prove it:** run `ekumen-import --dry-run` against the latest main, and report 0 validation failures and 0 lane conflicts in the handoff. Don't run the real import.

**Touches:** `internal/core/`, `internal/event/`, `internal/store/files/`, their tests and `testdata/`, `internal/legacyimport/` (only to drop workarounds the new APIs make unnecessary), and your handoff files.

**Definition of done:**
- [ ] Dry run on latest main: 0 mismatches, 0 validation failures, 0 lane conflicts
- [ ] A test for every changed rule, named after its contract section
- [ ] `go test -race ./...`, vet and gofmt clean; CI green on the draft PR
- [ ] Handoff written, including FRICTION

**Out of scope:** CLI commands and projections (T-cli); session events from C-adapter (still draft); editing hand-written records. Report any malformed record instead.

**Escalate if:** a v3 rule conflicts with existing tests in a way that suggests the contract is wrong rather than the code.

**Start prompt:**

    Read .ekumen/spine.md, then .ekumen/briefs/T-core-align/brief.md and its amendments/,
    then contracts C-event-log and C-decision-record, then .ekumen/briefs/T-import/handoffs/.
    Work only inside the brief as amended. Never edit the spine, contracts, the brief, amendments or hand-written records.
    Report by adding a handoff file to .ekumen/briefs/T-core-align/handoffs/ (use `ekumen task handoff` if it works).
    Raise anything above your tier as a decision draft in that handoff, then carry on with what isn't blocked.
