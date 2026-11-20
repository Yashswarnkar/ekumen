---
id: T-log-fixes
title: Event log fixes from the first audit
kind: build
contracts_consumed: [C-event-log, C-decision-record, C-cli-json]
contracts_produced: []
blocked_by: []
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-log-fixes · Event log fixes from the first audit

**Goal:** Fix what the first audit (T-audit, run 2026-11-20) and T-cli-gaps found once the tool ran on its own project. The decisions below are decided and are the specification. Read each with `ekumen decision show <id>`. Contract text is updated by the orchestrator **after** this merges, using the `contract version` command this task builds. That is the first real use of the contract-change protocol.

**Job:**
1. **D-imported-event-time-order (causal-within-subject).**
   - The reducer orders events within one subject by lifecycle step first and time second. The steps are: (1) raised/drafted; (2) asked/approved/frozen; (3) answered/decided/defaulted/superseded/amended/handoff; (4) applied. Across subjects, order by time as now.
   - New conflict rule `orphan-step`: a step whose prerequisite never exists (e.g. applied with no decided) is reported by `check`, never silently dropped.
   - Test: an apply stamped earlier than its decide is applied.
2. **D-projections-in-git (per-item-only).**
   - Stop committing `.ekumen/registry.yaml` and `.ekumen/state.json`: add them to `.gitignore` and remove them from the index. Every command regenerates them when needed.
   - Keep `decisions/<id>.yaml` committed, and add `ekumen project --check`, which fails if a committed per-item file doesn't match the events. Run it in `scripts/ci-check.sh`.
3. **D-contract-version-recording (versioned-command).**
   - Add `ekumen contract version <id> --by <decision>` (orchestrator). It records `ContractVersioned {version, by_decision, text_sha256}`, with the version taken from the contract's front matter.
   - Refuse unless the decision is decided and names the contract in `contract_impact`, and the version is higher than the current one.
   - `contract freeze` then freezes the new text. `frozen-text-changed` compares against the latest frozen version.
4. **D-post-switch-hand-edits (prose-carve-out).**
   - Owner-edited prose files are the spine, contract bodies, `brief.md` before approval, and research reports. Document this list in `docs/cli.md`.
   - Add `ekumen spine approve` (lead), recording `SpineApproved {text_sha256}`. `check` reports `spine-unapproved` when `spine.md` doesn't match the latest approval.
5. **D-apply-amend-lane (loop-lane).** `TaskAmended` with `data.by_decision` set is in the loop's lane. `decision apply` then writes everything as `loop:<id>`, with no orchestrator claim needed. Other amendments stay with the orchestrator.
6. **Audit findings:**
   - `check` reports `orphan-entry` for any amendment or handoff markdown file with no matching event.
   - `ekumen inbox --for lead` adds `kind: action` items for the lead's next moves:
     - tasks drafted with a brief, no open blockers, awaiting approval
     - draft contracts with no open blockers, awaiting freeze
     - decided-but-unapplied decisions older than a day

     Rank these after decisions.
7. `ekumen map --fragment`: write the page without `<!doctype>`, `<html>`, `<head>` and `<body>` wrappers, for embedding.

**Touches:** `internal/core/`, `internal/event/`, `internal/cli/`, `internal/project/`, `internal/mapview/`, `cmd/ekumen/`, `scripts/ci-check.sh`, `.gitignore`, `docs/cli.md`, `docs/development.md`, tests and golden files. Don't edit contracts or the spine: the orchestrator applies those with the new commands after merge.

**Definition of done:**
- [ ] A test per item above, named after its decision id
- [ ] On a copy of the real `.ekumen/`: `decision apply D-lane-gaps` takes effect, and `check` is clean
- [ ] `registry.yaml` and `state.json` untracked; `project --check` in CI
- [ ] `go test -race ./...`, vet and gofmt clean; CI green on the draft PR
- [ ] Handoff recorded with `ekumen task handoff T-log-fixes`, including the exact contract text changes to apply for C-event-log v5 and C-cli-json v3, so the orchestrator can apply them

**Escalate if:** any decision can't be implemented as decided without changing its meaning. Raise it with `ekumen decision raise`.

**Start prompt:**

    Read .ekumen/spine.md, then `ekumen task show T-log-fixes --json`, then the six decisions with
    `ekumen decision show`, then contracts C-event-log, C-decision-record and C-cli-json.
    Use `ekumen` for every .ekumen/ write. Report with `ekumen task handoff T-log-fixes`.
