---
id: T-task-update
title: task update, contract show
kind: build
repo: ekumen
base_branch: main
branch: build/t-task-update
worktree: .worktrees/t-task-update
session: t-task-update
contracts_consumed: [C-event-log, C-cli-json]
contracts_produced: []
blocked_by: []
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-task-update · task update, contract show

*§A only. Amendments (§B) are files in `amendments/`, handoffs (§C) are files in `handoffs/` (C-event-log §8).*

**Goal:** Implement D-placeholder-task-fields (decided `task-update`): give the orchestrator a way to fill a task's location fields after it was drafted, so imported placeholder tasks can derive `ready`, link to their pull requests, and be started. In the same PR, add `ekumen contract show`, because the lead's inbox asks the lead to freeze contracts and nothing in the CLI shows one (lead, 2026-11-20).

**Context (verified against `main` at `23e3040`; re-verify against `origin/main` when you start):**
- Six tasks were imported as placeholders with empty `repo`, `base_branch`, `branch`, `worktree` and `brief`: T-skills, T-reconcile, T-claude-adapter, T-server, T-graph-view, T-decision-views. `task draft` refuses an id already in use, and no command sets these fields.
- Consequences today: status rule 7 in `internal/core/status.go` keeps them `approved` instead of `ready`; the observer links pull requests to tasks by `branch`, so a placeholder task's merged PR never makes it `in_review` or `done`, and tasks blocked by it stay blocked.
- **T-skills and T-reconcile are already approved** and building (PRs #17, #18 and the T-reconcile stack). The decision's option text said "allowed only before approval". That would leave both broken, so this brief splits the fields (Job 1). This split is the one point the lead confirms when approving this brief.
- `TaskDrafted` data (C-event-log v4 §6): `title, kind, brief, repo, base_branch, branch, worktree, blocked_by[], tier_grants[]`, optional `session`, optional `aliases[]`. Orchestrator lane: C-event-log §3.
- `C-cli-json` is v2. D-cli-json-v3 (decided `v3-after-merge`) already collects `reconcile`, `skills list` and `skills install` for v3; the two commands here join the same v3 text. `C-event-log` is v4; the new event type needs v5, covered by D-placeholder-task-fields (`contract_impact: [C-event-log, C-cli-json]`).
- `decision show C-http-api` today returns a bare not-found. Ids carry their kind letter (C-event-log §1).
- The T-reconcile session is changing `internal/core` and `internal/cli` in parallel (build/t-reconcile stack). Keep your edits to the reducer, lanes and status narrow; conflicts are resolved on rebase.

**Job:**

1. **Event type `TaskUpdated`** (orchestrator lane).
   - `data`: any subset of `repo, base_branch, branch, worktree, brief, title`, plus `reason`. At least one field besides `reason`. Fields not present are unchanged; an explicit empty string is refused.
   - **Location fields** (`repo, base_branch, branch, worktree, brief`) may be updated at any time before the task is `done` or deferred. They say where the work happens, not what it is, so they don't touch the frozen §A.
   - **`title`** may be updated only before `TaskApproved`. After approval the task's meaning is frozen with §A.
   - `kind`, `blocked_by`, `tier_grants`, `session` and `aliases` are not updatable here: they already have their own events (`TaskBlockersChanged`, `task approve --grants`, `TaskSessionAttached`) or are fixed at creation.
   - Reducer: apply in log order; newest value wins per field. A `TaskUpdated` for a task with no `TaskDrafted` is an orphan step (existing rule). Validate the same formats as `TaskDrafted` (branch and path rules, `brief` resolved inside `.ekumen/`).
   - Lanes: orchestrator only (with the claim, like `TaskDrafted`). A `title` update after approval, or any update after `done`, is refused by the CLI and reported by `check` as a `lane`-style violation if it arrives through a merge. Add the rule in the same place as the other §3/§5 checks.
2. **`ekumen task update <id> [--repo r] [--base-branch b] [--branch b] [--worktree w] [--brief p] [--title t] --reason t`** (orchestrator, with the claim). Prints the usual write result. `--json` errors name the field. A no-op (every given value equals the current one) prints a no-op result and writes nothing.
3. **`ekumen contract show <id> [--json]`** (read). Output: `{contract: {id, title, aliases, status, version, frozen_on, path, consumers, producers, blocked_by, freezes: [{version, text_sha256, at}], versions: [{version, by_decision, text_sha256, at}], pending}, text}` where `text` is the contract file's body. Plain-text output prints the front-matter fields, the freeze history, then the body. Accepts aliases (`C1`).
4. **Kind hint on not-found.** When `decision show`, `task show` or `contract show` gets a valid id of another kind (`C-…` to `decision show`), the not-found error message says which command to use. Exit code stays 3.
5. **Docs.** `docs/cli.md`: both commands and the hint. Handoff: the exact C-event-log v5 text (§3 lane row, §6 event row, any §5 rule) and the exact C-cli-json text for both commands, for the orchestrator to fold into the contract versions.
6. **Dogfood on a scratch copy** of the real `.ekumen/` (never the repo's): run `task update` for T-skills (`repo ekumen, base_branch main, branch build/t-skills, worktree .worktrees/t-skills, brief briefs/T-skills/brief.md`) and T-reconcile (its brief's values), then show `task list` derives `ready`, and with observation on, that T-skills links to PR #17. Put the commands and output in the handoff. The orchestrator runs the real updates after merge.

**Touches:** `internal/event` (type and validation), `internal/core` (reducer, lanes, conflict rule), `internal/cli` (both commands, the hint, tests), `internal/project` if the contract view needs it, `docs/cli.md`. Target: one PR of about 600 changed lines or fewer; stack `contract show` as a second PR if it goes past that. Don't edit contract files or the spine.

**Definition of done:**
- [ ] `TaskUpdated` reduces per field, is orchestrator-lane only, refuses `title` after approval and any update after done, and `check` reports a violating event that arrived by merge. Golden and lane tests.
- [ ] `task update` golden `--json` tests: success, no-op, refused title after approval, refused empty value, refused without the claim.
- [ ] `contract show` golden `--json` and text tests for a draft, a frozen and a versioned contract; alias lookup works.
- [ ] Not-found hint test for each of the three `show` commands.
- [ ] Scratch-copy dogfood in the handoff shows T-skills and T-reconcile deriving `ready`, and T-skills linked to its PR.
- [ ] `go test -race ./...`, vet and gofmt clean; `scripts/ci-check.sh` passes; CI green on the draft PR.
- [ ] Handoff recorded with `ekumen task handoff T-task-update`, with the exact contract texts and a `FRICTION` section.

**Out of scope:**
- Running the real updates on the repo's `.ekumen/` (orchestrator, after merge)
- Writing the contract versions (orchestrator, after merge)
- `contract list`, `external show`, or other new reads
- Changing `blocked_by`, `tier_grants`, `session` or `aliases` through `task update`

**Escalate if:**
- Splitting location fields from `title` turns out to conflict with C-event-log's freeze rules (`frozen-text-changed`) or with D-task-status-approved.
- The reducer change collides with the T-reconcile stack in a way a rebase can't settle.

**Start prompt:**

    You are the task session t-task-update. Read .ekumen/spine.md, then this brief, then C-event-log §3, §5, §6 and C-cli-json.
    Work only inside §A as amended by §B. Never edit the spine, contracts, §A or §B, or another brief.
    Act as --as session:t-task-update. Report only through `ekumen task handoff T-task-update`.
    Raise anything above your tier with `ekumen decision raise`; never work around a contract.
