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
- **T-skills and T-reconcile are already approved** and building (PRs #17, #18 and the T-reconcile stack), so updates must also work on approved tasks. **Rule (lead, 2026-11-20): any change to a task is proposed by the orchestrator and approved by the lead.** Before approval there is nothing to re-approve: the lead approves the task as a whole. After approval, an update waits for the lead.
- `TaskDrafted` data (C-event-log v4 §6): `title, kind, brief, repo, base_branch, branch, worktree, blocked_by[], tier_grants[]`, optional `session`, optional `aliases[]`. Orchestrator lane: C-event-log §3.
- `C-cli-json` is v2. D-cli-json-v3 (decided `v3-after-merge`) already collects `reconcile`, `skills list` and `skills install` for v3; the two commands here join the same v3 text. `C-event-log` is v4; the new event type needs v5, covered by D-placeholder-task-fields (`contract_impact: [C-event-log, C-cli-json]`).
- `decision show C-http-api` today returns a bare not-found. Ids carry their kind letter (C-event-log §1).
- The T-reconcile session is changing `internal/core` and `internal/cli` in parallel (build/t-reconcile stack). Keep your edits to the reducer, lanes and status narrow; conflicts are resolved on rebase.

**Job:**

1. **Event type `TaskUpdated`** (orchestrator lane).
   - `data`: any subset of `repo, base_branch, branch, worktree, brief, title`, plus `reason`. At least one field besides `reason`. Fields not present are unchanged; an explicit empty string is refused.
   - **Proposal, then approval.** The orchestrator writes `TaskUpdated`. Same rule for every field:
     - **Task not yet approved:** the update takes effect at once. The lead's later `TaskApproved` covers it.
     - **Task approved:** the update is **pending**. Reads show the current values plus `pending_update` (the proposed fields, reason, event id). It takes effect only when the lead approves again with the existing `ekumen task approve <id>`: a `TaskApproved` after a pending `TaskUpdated` accepts every pending update before it. Grants and the §A hash are re-recorded as usual, so §A stays frozen.
     - **Task done or deferred:** refused.
   - Several pending updates merge field by field, newest wins; one re-approval accepts them all. A pending update does not change derived status.
   - `kind`, `blocked_by`, `tier_grants`, `session` and `aliases` are not updatable here: they already have their own events (`TaskBlockersChanged`, `task approve --grants`, `TaskSessionAttached`) or are fixed at creation.
   - Reducer: apply in log order. A `TaskUpdated` for a task with no `TaskDrafted` is an orphan step (existing rule). Validate the same formats as `TaskDrafted` (branch and path rules, `brief` resolved inside `.ekumen/`).
   - Lanes: `TaskUpdated` is orchestrator only (with the claim, like `TaskDrafted`); acceptance is the lead's `TaskApproved`, already in the lead's lane. An update to a done or deferred task is refused by the CLI and reported by `check` if it arrives through a merge.
   - **Inbox:** a task with a pending update gives the lead an action "Approve update to T-x: <fields> (<reason>)" whose command is `ekumen task approve T-x`. Use the existing lead-action mechanism.
2. **`ekumen task update <id> [--repo r] [--base-branch b] [--branch b] [--worktree w] [--brief p] [--title t] --reason t`** (orchestrator, with the claim). Prints the usual write result. `--json` errors name the field. A no-op (every given value equals the current one) prints a no-op result and writes nothing.
3. **`ekumen contract show <id> [--json]`** (read). Output: `{contract: {id, title, aliases, status, version, frozen_on, path, consumers, producers, blocked_by, freezes: [{version, text_sha256, at}], versions: [{version, by_decision, text_sha256, at}], pending}, text}` where `text` is the contract file's body. Plain-text output prints the front-matter fields, the freeze history, then the body. Accepts aliases (`C1`).
4. **Kind hint on not-found.** When `decision show`, `task show` or `contract show` gets a valid id of another kind (`C-…` to `decision show`), the not-found error message says which command to use. Exit code stays 3.
5. **Map and graph show what is really waiting on the lead** (lead, 2026-11-20). Today `ekumen graph` and `ekumen map` draw every decided-but-unapplied decision as a gate, so the lead saw 20 "pending" gates while the inbox, correctly, showed none.
   - In `graph --json`, give decision nodes a `waiting_on` field: `lead` for pending, `loop` for decided or defaulted but not applied.
   - In `ekumen map`, draw lead-waiting gates prominently and loop-waiting ones muted with an "apply pending" label; the legend says which is which. The map's inbox section and graph must agree on what is waiting on the lead.
   - Tests: golden graph JSON for a pending, a decided-unapplied and an applied decision; a map golden showing the two styles.
6. **Docs.** `docs/cli.md`: both commands and the hint. Handoff: the exact C-event-log v5 text (§3 lane row, §6 event row, any §5 rule) and the exact C-cli-json text for both commands, for the orchestrator to fold into the contract versions.
7. **Dogfood on a scratch copy** of the real `.ekumen/` (never the repo's): run `task update` for T-skills (`repo ekumen, base_branch main, branch build/t-skills, worktree .worktrees/t-skills, brief briefs/T-skills/brief.md`) and T-reconcile (its brief's values), show the pending update in `inbox --for lead`, run `task approve` as the lead, then show `task list` derives `ready` and, with observation on, that T-skills links to PR #17. Put the commands and output in the handoff. After merge, the orchestrator proposes the real updates and the lead approves them.

**Touches:** `internal/event` (type and validation), `internal/core` (reducer, lanes, conflict rule), `internal/cli` (both commands, the hint, tests), `internal/project` (contract view, graph `waiting_on`), `internal/mapview` (gate styles), `docs/cli.md`. Target: one PR of about 600 changed lines or fewer; stack `contract show` as a second PR if it goes past that. Don't edit contract files or the spine.

**Definition of done:**
- [ ] `TaskUpdated` applies at once on an unapproved task, stays pending on an approved one until the lead's next `task approve`, merges several pending updates, is refused after done, and `check` reports a violating event that arrived by merge. Golden, reducer and lane tests.
- [ ] The lead inbox shows "Approve update to T-x" for a pending update, and it disappears after `task approve`.
- [ ] `task update` golden `--json` tests: applied (unapproved task), pending (approved task), no-op, refused empty value, refused after done, refused without the claim.
- [ ] `contract show` golden `--json` and text tests for a draft, a frozen and a versioned contract; alias lookup works.
- [ ] Not-found hint test for each of the three `show` commands.
- [ ] Graph `waiting_on` and the two map gate styles, with goldens; map inbox and graph agree.
- [ ] Scratch-copy dogfood in the handoff: `task update` on T-skills and T-reconcile shows a pending update in the lead's inbox; after `task approve`, both derive `ready` and T-skills links to its PR.
- [ ] `go test -race ./...`, vet and gofmt clean; `scripts/ci-check.sh` passes; CI green on the draft PR.
- [ ] Handoff recorded with `ekumen task handoff T-task-update`, with the exact contract texts and a `FRICTION` section.

**Out of scope:**
- Running the real updates on the repo's `.ekumen/` (orchestrator, after merge)
- Writing the contract versions (orchestrator, after merge)
- `contract list`, `external show`, or other new reads
- Changing `blocked_by`, `tier_grants`, `session` or `aliases` through `task update`

**Escalate if:**
- Accepting updates through a repeated `TaskApproved` conflicts with C-event-log's freeze rules (`frozen-text-changed`), the first-wins handling of `TaskApproved`, or D-task-status-approved. If so, propose a separate lead event (`TaskUpdateApproved`) in a decision instead.
- The reducer change collides with the T-reconcile stack in a way a rebase can't settle.

**Start prompt:**

    You are the task session t-task-update. Read .ekumen/spine.md, then this brief, then C-event-log §3, §5, §6 and C-cli-json.
    Work only inside §A as amended by §B. Never edit the spine, contracts, §A or §B, or another brief.
    Act as --as session:t-task-update. Report only through `ekumen task handoff T-task-update`.
    Raise anything above your tier with `ekumen decision raise`; never work around a contract.
