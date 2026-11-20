---
id: T-reconcile
title: Control loop, one pass (ekumen reconcile)
kind: build
repo: ekumen
base_branch: main
branch: build/t-reconcile
worktree: .worktrees/t-reconcile
session: t-reconcile
contracts_consumed: [C-event-log, C-decision-record, C-cli-json]
contracts_produced: []
blocked_by: [T-core, T-file-store, T-cli]
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-reconcile · Control loop, one pass (ekumen reconcile)

*§A only. Amendments (§B) are files in `amendments/`, handoffs (§C) are files in `handoffs/` (C-event-log §8).*

**Goal:** Ship the control loop of D-control-loop as one idempotent pass you run when you want: `ekumen reconcile [--dry-run] [--json]`. A pass compares desired state (the events) with observed state (git and pull requests, through the existing observer), takes only the actions the loop is allowed to take, and reports the rest. This is spine §7's "the control loop, run one pass on demand" and spine §8 step 3, "T-reconcile, at first observing only git and pull requests". With it, decided work moves without the orchestrator running `decision apply` by hand (D-apply-before-loop ends here).

**Branch and worktree:** the task's `TaskDrafted` event was imported with empty `repo`, `base_branch`, `branch`, `worktree` and `session`, so the registry shows them empty until D-placeholder-task-fields is decided and built. The values in this brief's front matter still apply. Two consequences to expect:
- The observer matches PRs to tasks by recorded branch, so this task's own PR will **not** show in its `observed` status (it will read `draft` or `approved`, not `in_review`), until the fields are filled in. Don't treat that as a reconcile bug.
- Before you start, the orchestrator runs `ekumen task attach T-reconcile --session t-reconcile`, so that `task handoff` accepts you as the task's session.

**Context (verified against origin/main `a8af023`, 2026-11-20):**
- The loop's lane (C-event-log v4 §3) is `DecisionDefaulted`, `DecisionApplied`, `TaskSessionAttached`, `RoutedToOwner`, `ConflictDetected`. The loop may also raise decisions with `raised_by.task: null` (D-lane-gaps), and write `TaskAmended` with `data.by_decision` (D-apply-amend-lane). No new event type is needed.
- Most of the logic exists already. Reuse it and don't duplicate it:
  - routing: `core.Route` and `core.RouteID`. The CLI computes the routes the loop *would* write: `project.Snapshot.Routes`, `Pending: true`.
  - conflicts: `core.Conflicts`, which already skips conflicts recorded by `ConflictDetected`.
  - status and blockers: `core.DeriveTaskStatus` and `core.OpenBlockers`.
  - lead actions: `core.LeadActions`.
  - apply: `(*run).decisionApply` in `internal/cli/write_more.go`, which is idempotent and records `DecisionApplied` last.
- Observation is `internal/observe`, through `gh pr list --state all --limit 200`. It is loaded once in `project.Load`. `commit` (`internal/cli/write.go`) reloads the snapshot after every write, so `gh` runs again (issue #15).
- Real log today: **20** decided or defaulted decisions are not applied, all with empty `blocks`; **13** handoffs and **2** questions, and **0** `RoutedToOwner`; 0 conflicts. A first real pass is therefore about 20 `DecisionApplied` plus one `RoutedToOwner` per routable entry. Most of those routes close at once, through the early `RouteResolved`s already in the log.

**Job:**
1. **Pure planner in `internal/core`** (code over tokens, invariant 9).
   - `Plan(s *State, obs Observed, now time.Time, in ConflictInputs) Plan` returns the ordered actions of one pass and a report. It does no I/O.
   - It must be closed: after the planned events are written, `Plan` on the new state returns no actions. A tier-1 default and its apply belong to the same pass: plan the apply against the state with the default reduced in.
   - Actions, in this order:
     1. **Default**: a pending tier-1 decision whose `deadline` (a UTC date) is before today's UTC date gets `DecisionDefaulted {answer: default_if_no_answer, deadline}`. The deadline day itself is still answerable, matching `Urgency` (C-decision-record §1, "overdue").
     2. **Apply**: every decided or defaulted decision that is not applied and not superseded. Do what `decision apply` does today: one generated §B amendment per drafted task in `blocks` (`TaskAmended` with `by_decision`, skipping tasks already amended for it), then `DecisionApplied {amended_tasks, unblocked_tasks, notified_sessions: []}` last. Move the "what applying D writes" computation (tasks to amend, `unblocked_tasks`, the generated line) out of `internal/cli/write_more.go` into core. Then `decision apply` and `reconcile` share one implementation, and `decision apply` keeps its current behaviour and tests.
     3. **Route**: write `RoutedToOwner` for every entry `core.Route` returns over the whole log.
     4. **Conflict**: for each conflict `core.Conflicts` returns:
        - raise a tier-2 decision with a deterministic id, e.g. `D-conflict-<rule>-<first 6 chars of the lowest event id, lowercased>`. It must pass `event.ValidateID` (≤ 5 words, slug ≤ 40).
        - options `fix` (correct it with new events or restore the file) and `accept` (record it and change nothing); `recommendation: null`, `blocks: []`, `contract_impact: []`, a generated `context.summary` naming the rule, events and detail, and `raised_by {task: null, session: <loop id>, on: today}`.
        - then write `ConflictDetected {rule, events}` **after** the decision. That order makes a crash between the two self-healing: the next pass finds the decision id in use, skips the raise and writes only `ConflictDetected`.
   - **Report, not action:**
     - `ready`: every task whose derived status is `ready`, with branch, worktree, session and brief path.
     - `not_startable`: tasks that are `approved` only because branch or worktree is empty, citing D-placeholder-task-fields.
     - `unblocked`: tasks this pass unblocked.
     - `notify`: sessions of amended tasks, because no session messaging exists.
     - The loop **never starts sessions**: no adapter exists yet. It never writes `TaskSessionAttached` in this task.
2. **CLI executor `ekumen reconcile [--dry-run] [--json]`** in `internal/cli`.
   - **Actor:** the actor's role must be `loop` (`--as loop:<session>`, like `decision apply`); any other role is refused (exit 2). `--dry-run` needs no actor.
   - **One pass:**
     - Load once and observe once.
     - Write each action as its own commit, in plan order, admitting each event against the state *including* the events already written in this pass. For example, `DecisionApplied` must be admitted after its `DecisionDefaulted`.
     - Regenerate the projections once at the end, from that same observation. Do not reload with `gh` per action: that part of #15 is in scope.
   - **`--dry-run`** writes nothing: no events, no entry files, no projections. It prints the same report, with `dry_run: true` and the planned events.
   - **JSON:** propose and document one shape in `docs/cli.md`. Suggested: `{ok, dry_run, observed: {prs: "ok|unavailable|truncated"}, actions: [{kind: default|apply|route|conflict, subject, events: [ids], paths: [...]}], ready: [...], not_startable: [...], unblocked: [...], notify: [...], events: [...], paths: [...]}`.
   - **Exit codes:** 0 when the pass completed, including when it recorded conflicts (they are now decisions in the lead's inbox), and C-cli-json codes otherwise. A failure partway through exits 5 and names the events already recorded.
3. **Observation limits (#15), the part the loop needs:**
   - When `gh` returns exactly `Limit` PRs, warn on stderr and set `observed.prs: "truncated"`.
   - When observation is unavailable or truncated, the pass still applies, defaults, routes and records conflicts, since none of those depend on PR state except `unblocked_tasks`. The report says so, and `ready` is marked unknown rather than listed from events alone.
   - Changing the `gh` query strategy (per-branch, pagination, caching) is **out of scope** and needs the decision #15 asks for.
4. **Lead and orchestrator inbox gaps found 2026-11-20.** Drafted tasks with no `brief.md` (today T-skills, T-claude-adapter, and this task until this file lands) appear in no inbox. `LeadActions` lists `approve` only for tasks that *have* a brief and no open blockers. These are derived items, not lane entries, so they are computed like `LeadActions`, not routed or stored:
   - add `core.OrchestratorActions` with action `brief`: a drafted task that is not approved or deferred and has no `brief.md`. Command: write `.ekumen/briefs/<id>/brief.md`. It is shown in `inbox --for orchestrator` and in the reconcile report.
   - change the lead's `apply` action command to `ekumen reconcile --as loop:<session>`. Keep the one-day threshold.
   - A brief that exists but whose task still has open blockers stays out of the lead's inbox, as today. See the open question to the lead.
5. **Docs and contract map:**
   - `docs/cli.md`: a `reconcile` section, and a note under `decision apply` that the loop now does this.
   - `internal/core/README.md`: contract map rows for the planner.
   - Your handoff carries the **exact C-cli-json text** for the `reconcile` command and the `brief` action. The orchestrator records it after merge with `contract version` (tier-2 decision, see Escalate).

**Shape:** a short stack, each PR coherent and ≤ ~600 changed lines:
1. core planner, plus the apply logic moved into core;
2. the CLI executor, the observe-once change, and docs;
3. the inbox actions.

Stack with `restack`.

**Touches:** `internal/core/` (new planner file, `actions.go`, `README.md`), `internal/cli/` (new `reconcile` command, `write_more.go` apply refactor, inbox output), `internal/project/` (inbox, a snapshot that can absorb events in memory), `internal/observe/` (truncation signal), `docs/cli.md`, tests and golden files. Don't edit `.ekumen/` contracts or the spine.

**Definition of done:**
- [ ] Planner tests, one per action kind, named after the rule or decision they enforce, each also proving the plan is empty on the resulting state:
  - default only after the deadline day (`…_DecisionRecord3`)
  - default then apply in one pass
  - apply idempotent and identical to `decision apply` output (`…_DApplyBeforeLoop`)
  - routes (`…_EventLog4`)
  - conflict decision + `ConflictDetected`, deterministic id (`…_EventLog5`)
  - readiness and `not_startable`
- [ ] Executor tests:
  - `--dry-run` writes nothing, compared byte for byte on the `.ekumen/` tree
  - a second pass right after a pass records nothing
  - a pass interrupted after k actions (injected failure) is completed by the next pass with no duplicate effects
  - running as a non-loop role is refused
  - `gh` is called once per pass (counting fake observer)
- [ ] Golden `--json` output for `reconcile` and `reconcile --dry-run`, and for the new inbox actions.
- [ ] On a **copy** of the real `.ekumen/` at the base commit:
  - `reconcile --dry-run` plans about 20 applies plus the routes and nothing unexpected; paste the plan in your handoff
  - a real pass, then `check`, is clean
  - a second `--dry-run` plans nothing

  Never run a write pass against the real `.ekumen/` in the repo: the orchestrator does that after merge.
- [ ] `go test -race ./...`, vet and gofmt clean; CI green on each draft PR.
- [ ] Handoff recorded with `ekumen task handoff T-reconcile`. Include the C-cli-json text, and FRICTION.

**Out of scope:**
- Starting or messaging sessions, and `TaskSessionAttached` (need T-claude-adapter / C-adapter).
- The dispatcher: running the loop on a timer (spine §7 "Out").
- Roles and rules (C-roles is a draft and not frozen). Keep the planner's stages plain functions, so rule evaluation can later be one more stage; build no rule hooks now.
- The contract-change follow-through for decisions with `contract_impact`: applying them is in scope, versioning the contract stays with the orchestrator.
- The write lock and crash-atomic batches (#12): see below.
- Dead-claim expiry (#13): the loop writes as `loop`, needs no claim, and is unaffected.
- Historical lane-check accuracy (#14).
- Filling the placeholder task fields (D-placeholder-task-fields).

**How the open issues are handled:**
- **#15 (observation capped at 200, `gh` twice per write):** partly in scope. Reconcile observes once per pass and reports truncation (Job 3). The query strategy, and the double load in other write commands, are out of scope pending #15's decision. This repo has far fewer than 200 PRs, so it doesn't block v0.1.
- **#12 (non-atomic multi-event writes, no lock):** this task depends on its decision but is not blocked by it. Reconcile is level-triggered, which already gives the property that matters for the loop. Every action is idempotent and ordered so its last event is the marker that it finished: `DecisionApplied` after the amendments, `ConflictDetected` after the decision. A crash mid-pass is repaired by the next pass, which the interrupted-pass test proves. Two concurrent passes are *not* made safe here: duplicate `RoutedToOwner`, `DecisionDefaulted` and `DecisionApplied` are ignored by the reducer, but two loop actors raising the same conflict decision would trip `id-collision`. Document "run one pass at a time" in `docs/cli.md`. The lock itself waits for #12's tier-2 decision.
- **#14:** a lane conflict reported against a re-attached session's earlier handoff would become a tier-2 decision. None exist on the real log today. If the dry run on the copy shows one, see Escalate.

**Escalate if:**
- Adding the `reconcile` command or the `brief` inbox action can't be a minor C-cli-json change. The orchestrator raises D-reconcile-cli (tier 2, `contract_impact: [C-cli-json]`) for the text you hand off; raise your own decision if the shape you need differs.
- Any action needs an event, field or lane the contracts don't have.
- The real-log dry run plans an action you can't explain, such as a conflict caused by #14 or an apply of a decision that should stay unapplied. Stop and raise it, and don't add special cases to skip it.
- Moving apply into core would change `decision apply`'s recorded output.

Raise with `ekumen decision raise`.

**Start prompt:**

    Read .ekumen/spine.md (§3, §5, §6, §7, §8), then `ekumen task show T-reconcile --json`
    (the brief, as amended), then contracts C-event-log (§3–§7), C-decision-record (§1, §3)
    and C-cli-json, and decisions D-control-loop, D-apply-before-loop, D-apply-amend-lane,
    D-projection-observed, D-blocker-semantics and D-placeholder-task-fields with
    `ekumen decision show`. Read GitHub issues #12 and #15. Then read internal/core
    (route.go, conflict.go, status.go, actions.go, README.md), internal/observe, and
    internal/cli/write_more.go (decisionApply).
    Work in .worktrees/t-reconcile on branch build/t-reconcile from main.
    Use `ekumen` for every .ekumen/ write; never edit those files by hand.
    Never run a write pass of `ekumen reconcile` against the repo's own .ekumen/; use a copy.
    Report with `ekumen task handoff T-reconcile`. Raise anything above your tier with
    `ekumen decision raise`.

**FRICTION reporting:** your handoff's `FRICTION` section lists:
- what was painful or missing in `ekumen` while you used it on this task: attach, handoff, and reading your own task with its empty placeholder fields;
- where the contracts were ambiguous for the loop: the deadline boundary, the conflict-decision shape, and the observed-state rules;
- anything you had to do by hand that the loop or the CLI should have done.
