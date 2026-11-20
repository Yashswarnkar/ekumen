---
id: C-roles
title: Roles and rules
status: draft          # draft | frozen
frozen_on: null
version: 0
consumers: [T-reconcile, T-cli]
producers: []
---

# C-roles · Roles and rules

How a project declares **kinds of agent work** (roles) and **when they run** (rules), so recurring work happens without someone remembering, and its results come back as records. Depends on D-roles-and-rules.

**Layering (spine §6, rule 12):** Ekumen coordinates; it never does the work. A role says what an agent is told, what it may do and where its output goes. A rule says when the control loop starts it. The agent tool that runs it is an adapter detail (C-adapter). No tool or vendor name appears in a role or rule, except inside a `prompt` the project writes itself.

## 1. Role

`.ekumen/roles/<role id>.yaml`. The role id is a slug, e.g. `reviewer`, `auditor`, `tester`.

```yaml
id: reviewer
title: Code reviewer
prompt: roles/reviewer.md      # prose, owner-edited; may say "run your code-review skill on PR {pr}"
kind: research                 # task kind its runs get: research | build
grants: [read, comment_on_pr]  # tier-0 grants for every run
output: handoff                # handoff | report  (report = a file under research/)
verdict: [pass, changes, block]  # optional: the STATUS values a run must end with
escalate:                      # optional: findings that become records, evaluated in code
  - if: verdict == block
    raise: decision            # tier-2 decision for the lead, carrying the run's summary
  - if: verdict == changes
    ask: task-session          # a question to the reviewed task's session
agent: null                    # optional adapter preference; null = project default
```

- **Placeholders** in the prompt are filled from the triggering subject: `{task}`, `{pr}`, `{branch}`, `{decision}`, `{contract}`.
- **Each run is a task.** It gets an id `T-<role>-<subject>-<n>` (e.g. `T-reviewer-t-log-fixes-1`), drafted and approved automatically under the role's grants, because a rule is a standing approval (§3). Its handoff is an ordinary `TaskHandoff`, with `verdict` parsed from its STATUS line.

## 2. Rule

`.ekumen/rules/<rule id>.yaml`.

```yaml
id: review-build-prs
when: pr_ready                 # trigger (§2.1)
if: task.kind == build         # optional filter (§2.2)
start: reviewer                # role id
gate: merge                    # optional: what the run's verdict holds back (§2.3)
pass_when: verdict == pass
once_per: [task, pr_head]      # rerun when the PR head changes
```

### 2.1 Triggers (v1)

| `when` | Fires on |
|---|---|
| `pr_ready` | Observed: a task's PR is open and not a draft |
| `pr_updated` | Observed: the PR head commit changed |
| `ci_failed` | Observed: a task's PR checks failed |
| `task_done` | A task becomes `done` |
| `decision_decided` | A decision is decided |
| `contract_frozen` | A contract is frozen or versioned |
| `every` | A schedule: `every: 7d`, or `every: 3 merges` |
| `manual` | Only `ekumen role run <role> <subject>` |

### 2.2 Filters

A small expression over the subject's fields: `==`, `!=`, `in`, `and`, `or`, `not`. For example `task.kind == build and task.id != T-docs`. There are no function calls and no loops. Rules are evaluated in code (spine rule 9).

### 2.3 Gates

`gate: merge` adds the run as a blocker on the subject task's `in_review → done` step. The task can't count as done, and the inbox shows "waiting on reviewer", until the latest run's verdict satisfies `pass_when`. A failed or missing run keeps the gate closed. **Gates never merge anything themselves.** They inform the lead and the loop.

## 3. Approval

- **Roles and rules are prose the project owner edits** (D-post-switch-hand-edits). They take effect only after the lead approves them: `ekumen rules approve` records `RulesApproved {text_sha256}` over all role and rule files, like `spine approve`.
- `check` reports `rules-unapproved` if the files changed since the last approval, and the loop ignores unapproved changes.
- An approved rule is a **standing approval** for the runs it starts, within the role's grants. Anything beyond those grants still needs a decision.

## 4. Events (additions to C-event-log §6, a minor change)

| Type | Lane | `data` |
|---|---|---|
| `RulesApproved` | lead | `text_sha256, roles[], rules[]` |
| `RoleRunStarted` | loop | `rule, role, subject, run_task, key` |
| `GateOpened` / `GateClosed` | loop | `rule, subject, run_task, verdict` |

## 5. CLI (additions to C-cli-json, a minor change)

- `ekumen role list` and `ekumen rule list` (`--json`)
- `ekumen role run <role> <subject>`: a manual run, honouring grants
- `ekumen rules approve` (lead)
- `ekumen rules test <rule> <subject>`: a dry run that shows whether the rule fires and what it would start

## Open before freeze

- **Who may edit roles and rules:** proposal, the orchestrator, with lead approval as above.
- **Run task ids:** generated (`T-reviewer-t-log-fixes-1`) or one recurring task per role, like T-audit today. Proposal: generated, so each run has its own handoff and gate.
- **First roles to declare:** `reviewer` (rule: every build PR, gate on merge) and `auditor` (rule: every 3 merges or 7 days). These replace the manual `/code-review` habit and T-audit's manual scheduling.
