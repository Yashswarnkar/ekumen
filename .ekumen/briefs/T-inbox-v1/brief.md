---
id: T-inbox-v1
title: An inbox the lead can act on
kind: build
contracts_consumed: [C-event-log, C-decision-record, C-cli-json]
contracts_produced: []
blocked_by: []
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-inbox-v1 · An inbox the lead can act on

**Goal:** v1 capability 1 (spine §7). `ekumen inbox --for lead` must tell the lead, in plain words, **what to do next, why, and what it unblocks**, with numbers they can trust. Today it ranks by a `p=` score nobody can read, that score counts finished work, decisions waiting on task branches are invisible, and some task statuses are stale. GitHub issues: **#37, #38, #39, #63**.

**Context (verify against origin/main when you start):**
- Inbox: `core.Inbox` (`internal/core/priority.go:170`), `core.LeadActions` (`internal/core/actions.go:52`), `project.Snapshot.Inbox` and `actionItems` (`internal/project/views.go:476–534`), and the printer `printInbox` (`internal/cli/read.go:74`).
- Priority: `core.BlockedByDecision` and `core.Priority` (`internal/core/priority.go`). The doc comment admits done tasks are counted, because the function takes no observations. Example from the real log: D-write-atomicity listed 14 held-up items, including T-core, T-cli and T-import, which are done; really it blocked only T-loop-hardening.
- PR observation: `internal/observe`, where `ByBranch` links PRs to tasks by exact branch name. Stacked branches (`build/t-reconcile-2`, `-3`) and tasks with empty branch fields never link.

**Job:**
1. **Honest impact (#37).**
   - "Held up" counts only tasks that aren't done or deferred, using derived status **with** observations.
   - Separate **directly blocks** (`blocks`, or a task's `blocked_by` names the item) from **touches a contract** (`contract_impact`). Only direct and transitive *task* blocking counts toward rank. Contract impact is shown as context.
   - Keep the urgency steps (C-decision-record v2).
2. **Plain output (#37).** The default (non-JSON) inbox prints, per item:
   - one line saying **what to do**, e.g. "Decide D-write-atomicity: how writes stay all-or-nothing"
   - **why it matters**, e.g. "T-loop-hardening can't start until this is decided"
   - **what it unblocks**, as a path: `D-write-atomicity → T-loop-hardening`
   - **the command to run**

   No unexplained letters or bare scores. `--json` adds `why`, `unblocks` (task ids), `path`, `direct` and `contract_context`, while keeping the existing fields (C-cli-json §5, minor change).
3. **`ekumen why <id>`.** For any task, decision or contract: what it waits on (open blockers, with *their* blockers), and what waits on it (unfinished only). Plain text by default, plus `--json`.
4. **Decisions waiting on branches (#38).** Decisions raised on an open task branch (on a PR not yet merged) must appear in the lead's inbox, marked as "raised on branch X, in PR #N". Read them from the remote branch with read-only git, the same `gh` and `git` tools the observer already uses. Don't merge or check anything out.

   **Answering one is the hard part:** an answer on main would refer to a decision that isn't on main yet. Propose the mechanism in your handoff and raise it as a decision with `ekumen decision raise` before building it. Show such decisions in the inbox, but don't let them be answered until that decision is made.
5. **Stale statuses (#39).** A PR also links to a task when:
   - its head branch is the recorded branch plus a `-<n>` suffix (stacks), or
   - its title starts with `<task id>:` (the convention every PR so far follows).

   Recorded branch first, then stack suffix, then title. `task show` says which rule linked it.

6. **One-sided gates (#63).** A decision gates a task if *either* the decision's `blocks` lists the task *or* the task's `blocked_by` lists the decision. Impact, ranking and `why` use that union, so D-pr-observation-style decisions don't show up at 0. `decision apply` amends every task in the union. Today it only amends `blocks`, so T-loop-hardening's decisions amend nobody. That amend change is in `internal/core` (`ApplyPlan`); check it against C-decision-record §3 and note any contract text in your handoff.

**Touches:** `internal/core/` (priority, actions, a new why/impact file), `internal/project/views.go`, `internal/observe/`, `internal/cli/` (`read.go`, the new `why` command, help text), `docs/cli.md`, tests and golden files, your handoff files.

**Definition of done:**
- [ ] On the real log: D-write-atomicity shows exactly one held-up task (T-loop-hardening). No done task appears in any "held up" or "unblocks" list.
- [ ] T-skills and T-reconcile show `done` from their merged PRs (title rule).
- [ ] The plain inbox has a "what / why / unblocks / command" block per item. Paste the real output into the handoff.
- [ ] `ekumen why D-write-atomicity` and `ekumen why T-claude-adapter` are correct on the real log.
- [ ] Applying a decision named only in a task's `blocked_by` amends that task (test).
- [ ] Golden tests for the inbox, `why` and the linking rules. `go test -race ./...`, vet and gofmt are clean, and CI is green (main is protected).
- [ ] Handoff via `ekumen task handoff T-inbox-v1`, including FRICTION about reading the inbox as the lead would.

**Out of scope:** goals and `ekumen next` (#46, v1.1); the web UI; changing what the loop does.

**Escalate if:** making branch decisions answerable needs a contract change (it probably does). Raise it with `ekumen decision raise`, and ask the lead in your terminal.

**Start prompt:**

    export EKUMEN_ACTOR=session:t-inbox-v1
    Read .ekumen/spine.md (§3, §5, §7), then `bin/ekumen task show T-inbox-v1`, then GitHub issues #37, #38 and #39
    (`gh issue view`), then contracts C-decision-record and C-cli-json. Run `bin/ekumen inbox --for lead` and
    `bin/ekumen decision show D-write-atomicity --json` first, to see the problem for yourself.
    Use `ekumen` for every .ekumen/ write. Report with `bin/ekumen task handoff T-inbox-v1`, push your branch, and open a draft PR
    titled "T-inbox-v1: …". Ask the lead in this terminal before anything outward-facing beyond that.
