---
id: T-core
title: "Core domain: events, lanes, reducer, conflicts"
kind: build
contracts_consumed: [C-event-log, C-decision-record]
contracts_produced: []
blocked_by: [C-event-log, C-decision-record]
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-core · Core domain: events, lanes, reducer, conflicts

*§A only. Amendments (§B) are files in `amendments/`, handoffs (§C) are files in `handoffs/` (C-event-log §8).*

**Goal:** The pure heart of Ekumen: typed events, validation, and the functions that turn an event log into state. Everything here is deterministic code with no file, network or process access (spine invariant 9). Every later task builds on it.

**Context (verified on 2026-11-20 against origin/main):** the repo has the T0 scaffold (`cmd/ekumen`, `internal/server`, `internal/buildinfo`, `web/`), Go 1.26, standard library only. Contracts C-event-log v1 and C-decision-record v1 are frozen. The hand-written records in `.ekumen/decisions/` and `.ekumen/registry.yaml` show real examples of the shapes, but they predate the contracts (old ids, no events). Use them as reading material, not as input.

**Job:**
1. `internal/event`: the envelope (C-event-log §2), a typed `data` struct for every v1 event type (§6), JSON encode and decode that keeps unknown fields, ULID generation (standard library only), and id validation (§1, including aliases).
2. `internal/core/lanes.go`: the lane table (§3) as data, and `CheckLane(event) error`.
3. `internal/core/decision.go`: decision validation exactly as C-decision-record §2, with errors that name the field.
4. `internal/core/state.go`: `Reduce(events) State`. State holds tasks, decisions (with lifecycle fields, questions and applied info), contracts, externals, open routes and role claims. Reducing the same events in any input order gives the same state (order by event id).
5. Derivations, all pure, all on `State`:
   - task status (`draft, approved, blocked, ready, running, in_review, done, deferred`) from events **plus** an `Observed` input (PR and session facts) passed in by the caller. Observed facts are never events (C-event-log §7 rule 2).
   - decision priority: number of tasks blocked, directly or through contracts, × deadline urgency. Document the urgency formula in code comments and tests.
   - `Inbox(State, owner)`: open routed items and, for the lead, pending decisions ranked by priority.
6. `internal/core/route.go`: `Route(state, newEvents) []RoutedToOwner` implementing C-event-log §4.
7. `internal/core/conflict.go`: `Conflicts(state, events) []Conflict`, one check per rule in C-event-log §5. Hash checks take the current text hashes as an input; the core never reads files.
8. Tests: table-driven unit tests for every lane, validation rule, routing rule and conflict rule, plus golden tests that reduce a fixture log (`testdata/`) to an expected state. Include an order-independence test: shuffle the events and get identical state.

**Touches:** `internal/event/`, `internal/core/`, their `testdata/`. Your handoff files under `.ekumen/briefs/T-core/handoffs/`.

**Definition of done:**
- [ ] Every rule in C-event-log §1–§6 and C-decision-record §1–§3 has a function and a test that names the rule
- [ ] No package under `internal/event` or `internal/core` imports `os`, `io/fs`, `net` or `os/exec`
- [ ] `go test ./...`, `go vet ./...` and `gofmt` clean; CI green on the draft PR
- [ ] Exported API documented with Go doc comments, plus a short `internal/core/README.md` mapping contract sections to functions
- [ ] Handoff written, including FRICTION

**Out of scope:** reading or writing files (T-file-store does that, in parallel); the CLI; YAML projections; the control loop itself (T-reconcile); session events (they come with C-adapter); anything vendor-specific (spine rule 12).

**Escalate if:** a contract rule is ambiguous, contradictory or impossible to implement as written. Don't interpret it creatively; raise a decision (tier 2, since it touches a frozen contract) and carry on with the rest. Also escalate if you need any dependency outside the standard library.

**Start prompt:**

    Read .ekumen/spine.md, then .ekumen/briefs/T-core/brief.md and any files in its amendments/ folder,
    then contracts C-event-log and C-decision-record.
    Work only inside the brief as amended. Never edit the spine, contracts, the brief or amendments.
    Report by adding a new file to .ekumen/briefs/T-core/handoffs/ named <UTC YYYYMMDDTHHMMSSZ>-t-core.md,
    with STATUS, OUTPUT, DECISIONS RAISED, FOLLOW-UPS, DEVIATIONS, FRICTION.
    Raise anything above your tier as a decision draft in that handoff, then carry on with what isn't blocked.
