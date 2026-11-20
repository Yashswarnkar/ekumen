---
id: T-loop-hardening
title: Harden the control loop (lane history, atomic writes, role claims)
kind: build
contracts_consumed: [C-event-log, C-cli-json]
contracts_produced: []
blocked_by: [D-lane-check-history, D-write-atomicity, D-claim-takeover]
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-loop-hardening · Harden the control loop

**Goal:** The control loop (`ekumen reconcile`) now runs for real, writes many events per pass, and turns every conflict into a tier-2 decision for the lead. Three known weaknesses would make it unsafe or noisy. Fix them as decided. Each decision is the specification: read it with `ekumen decision show <id>`. Each also links its GitHub issue.

**Job:**
1. **D-lane-check-history (GitHub #14).** Lane conflicts for past events are judged against the state *as it was just before each event*, reduced incrementally in time order, not against the final state.
   - Re-attaching a task ends the previous session's ownership from that point on, including the drafting session's.
   - Questions with an empty `to` resolve their addressee at the time they were asked.
   - Tests:
     - attach s1 → s1 hands off → re-attach s2: no conflict on s1's handoff
     - s1 hands off *after* the re-attach: conflict
     - the drafting session after a re-attach: conflict
   - Keep `check` fast on the real log. Report its timing before and after in the handoff.
2. **D-write-atomicity (GitHub #12).**
   - **Lock:** take an exclusive lock on `.ekumen/.lock` around load → validate → append for every write command, including `reconcile`. Use flock on Unix, with a clear fallback elsewhere. If the lock is held, wait up to 10 s, then exit with a clear error naming the holder's pid.
   - **Commit last:** each multi-event write orders its events so the one that makes the write meaningful comes **last**, e.g. `DecisionApplied` after its amendments. Entry files are written before their events, as now.
   - **`check` reports partial writes** as a new conflict rule, `partial-write`: amendments with `by_decision` but no `DecisionApplied`, and an entry file without its event, which is already `orphan-entry`.
   - Tests: two concurrent `claim orchestrator` runs → exactly one succeeds. A simulated crash mid-write → `check` reports it, and a re-run completes it.
3. **D-claim-takeover (GitHub #13).**
   - **Takeover:** add `ekumen claim --takeover <role>`, lead lane. It writes `RoleReleased` for the current holder, recorded as written by the lead, then `RoleClaimed` for the new holder.
   - **Unique ids:** refuse claims whose session id is a generic word (`main`, `default`, `orchestrator`, `session`, `local`), and make `check` warn on past ones.
   - **Visibility:** `ekumen status` shows who holds each role and since when.
   - Document all three in `docs/cli.md`.
4. **Contract text:** your handoff lists the exact lines for C-event-log (lanes, the `partial-write` rule, takeover) and C-cli-json (`claim --takeover`, the lock behaviour). The orchestrator applies them with `contract version` after merge.

**Touches:** `internal/core/`, `internal/store/files/`, `internal/cli/`, `docs/cli.md`, tests, and your handoff files.

**Definition of done:**
- [ ] A test per item, named after its decision id
- [ ] `ekumen check` and `ekumen reconcile --dry-run` on a copy of the real log: clean, and 0 actions after one real pass
- [ ] `go test -race ./...`, vet and gofmt clean; CI green on the draft PR (main is protected: CI must pass to merge)
- [ ] Handoff via `ekumen task handoff T-loop-hardening`, with the contract text for item 4

**Out of scope:** PR observation (GitHub #15, D-pr-observation), adapter work, and roles and rules.

**Start prompt:**

    Read .ekumen/spine.md, then `ekumen task show T-loop-hardening --json`, then the three decisions with
    `ekumen decision show` and their GitHub issues (#14, #12, #13), then contracts C-event-log and C-cli-json.
    Use `ekumen` for every .ekumen/ write. Report with `ekumen task handoff T-loop-hardening`.
