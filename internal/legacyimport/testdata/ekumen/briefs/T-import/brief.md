---
id: T-import
title: Import hand-written records into events
kind: build
contracts_consumed: [C-event-log, C-decision-record]
contracts_produced: []
blocked_by: [T-core, T-file-store]
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-import · Import hand-written records into events

*§A only. Amendments (§B) are files in `amendments/`, handoffs (§C) are files in `handoffs/` (C-event-log §8).*

**Goal:** Turn everything written by hand before the CLI existed into events, with readable ids and the old ids kept as aliases. After this and T-cli are merged, the hand-written files are replaced by generated ones and nobody edits `.ekumen/` by hand again.

**Context (to re-verify against origin/main when you start):** T-core and T-file-store are merged. T-cli is being built in parallel and owns `cmd/ekumen`, so this task ships its own small command, `cmd/ekumen-import`. The orchestrator keeps adding hand-written records until the switch, so the import must be **re-runnable** on the latest main.

**Inputs (hand-written, pre-contract shapes):**
- `.ekumen/decisions/*.yaml`: D-001 to D-019 plus any slug-named ones; some have `orchestrator_note`, `applied_on` and `applied`.
- `.ekumen/registry.yaml`: tasks (old and slug ids), `contracts:` section, `external`.
- Old single-file briefs `briefs/R1-acp.md`, `briefs/R2-landscape.md`, `briefs/T0-scaffold.md` with §A, §B and §C sections; new-layout briefs `briefs/<id>/brief.md` with `amendments/` and `handoffs/`.
- `contracts/*.md` front matter (status, frozen_on, version, aliases).

**Id mapping (decided by the orchestrator; use exactly these):**

| Old | New |
|---|---|
| R1 · R2 · T0 | T-acp-research · T-landscape-research · T-scaffold |
| D-001 · D-002 · D-003 · D-004 · D-005 | D-target-user · D-agent-agnostic · D-core-language · D-ui-stack · D-project-name |
| D-006 · D-007 · D-008 · D-009 · D-010 | D-v01-ui-scope · D-code-over-tokens · D-project-folder · D-license · D-plain-vocabulary |
| D-011 · D-012 · D-013 · D-014 · D-015 | D-research-location · D-control-loop · D-claude-status-source · D-decision-delivery · D-adapter-base |
| D-016 · D-017 · D-018 · D-019 | D-managed-observed-sessions · D-claude-auth · D-acp-version · D-id-scheme |

Contracts already have slug ids with `aliases: [C1…C5]`. Records already using slug ids keep them.

**Job:**
1. `internal/legacyimport`: parse every input above and produce the C-event-log events that describe its history: `TaskDrafted`/`TaskApproved`/`TaskAmended`/`TaskHandoff`, `DecisionRaised`/`DecisionDecided`/`DecisionDefaulted`/`DecisionApplied`, `ContractDrafted`/`ContractFrozen`. Record aliases. Keep `orchestrator_note` as a `QuestionAsked`/`QuestionAnswered` pair, or as extra data, whichever loses nothing; explain the choice in the handoff.
2. **Deterministic:** the same inputs give byte-identical events. Derive event ids from the content plus the record date, not from randomness, and use 12:00:00Z on the recorded date when only a date is known. Re-running on unchanged input writes nothing new.
3. **Old briefs:** split them into the new layout (`briefs/<new id>/brief.md`, `amendments/`, `handoffs/`). Keep the old file content reachable through git history only.
4. **Verify:** reduce the imported events with `core.Reduce` and compare against the hand-written records: every decision's status and answer, every task's status, every contract's status. Any mismatch fails the import with a report.
5. `cmd/ekumen-import`: `--dry-run` (report only) and the real run, which writes through `internal/store/files` and lists what it wrote.
6. Tests: fixtures copied from the real `.ekumen/`, golden events, determinism (run twice, compare), and the verification step.

**Touches:** `internal/legacyimport/`, `cmd/ekumen-import/`, its `testdata/`, and your handoff files. **Do not run the real import against main's `.ekumen/` in your PR.** The orchestrator runs it at the switch so it covers the latest records. Your PR contains code, tests and a dry-run report in the handoff.

**Definition of done:**
- [ ] Dry run on the current main reports every record mapped, 0 verification mismatches
- [ ] Determinism test passes
- [ ] `go test -race ./...`, vet and gofmt clean; CI green on the draft PR
- [ ] Handoff written, including the dry-run summary and FRICTION

**Out of scope:** the `ekumen` CLI itself (T-cli); writing projections (T-cli's `ekumen project` regenerates them after the import); editing any hand-written record to make the import pass. If a record is malformed, report it in the handoff.

**Escalate if:** a hand-written record can't be expressed in C-event-log v1 without losing information, which would mean a contract gap; or the id mapping is missing an item.

**Start prompt:**

    Read .ekumen/spine.md, then .ekumen/briefs/T-import/brief.md and its amendments/,
    then contracts C-event-log and C-decision-record.
    Work only inside the brief as amended. Never edit the spine, contracts, the brief, amendments, or any
    hand-written record. Report by adding a handoff file to .ekumen/briefs/T-import/handoffs/.
    Raise anything above your tier as a decision draft in that handoff, then carry on with what isn't blocked.
