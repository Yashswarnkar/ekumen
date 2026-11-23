---
id: C-event-log
aliases: [C1]
title: Event log
status: frozen         # draft | frozen
frozen_on: 2026-11-20
version: 5
consumers: [T-core, T-file-store, T-cli, T-server, T-import, T-reconcile]
producers: [T-core]
---

# C-event-log · Event log

The event log is Ekumen's source of truth (spine §6). Everything else (task status, decision files, `state.json`, the UI) is computed from it. This contract fixes the event format, the id format, who may write which events, routing, how conflicts are found, how events and prose are stored, and the v1 event types.

## 1. Ids

Every task, decision, contract and external dependency has a readable **slug id** (D-019):

```
<kind letter>-<slug>      kind letter: T task · D decision · C contract · X external dependency
slug: lowercase a–z, 0–9, words joined by "-", 1–5 words, the slug itself ≤ 40 characters (so an id is ≤ 42; D-slug-length-scope)
```

Examples: `T-core`, `D-control-loop`, `C-event-log`, `X-sandbox-creds`. The regex is `^[TDCX]-[a-z0-9]+(-[a-z0-9]+){0,4}$`.

- The CLI refuses an id already in use. Two branches picking the same id is caught by the `id-collision` rule (§5).
- **Aliases.** Items created before this contract keep their old ids (`R1`, `T0`, `D-012`, `C1`) as `aliases`. Readers resolve an alias to its id everywhere. The import task (T-import) assigns slugs and aliases to them. Aliases are carried by the optional `aliases[]` field of the event that creates the item: `TaskDrafted`, `ContractDrafted`, `ExternalAdded`, and the record in `DecisionRaised` (D-alias-field).
- **Events** use a ULID: unique, and sorting by creation time.

## 2. Envelope

Every event is one JSON object:

```json
{
  "schema": 1,
  "id": "01JD8Z3K6Q4M2V7X9T1B5N0RCE",
  "type": "DecisionRaised",
  "at": "2026-11-20T09:14:03Z",
  "actor": {"role": "session", "id": "t-core"},
  "subject": {"kind": "decision", "id": "D-event-ids"},
  "data": { }
}
```

| Field | Rule |
|---|---|
| `schema` | Envelope version. Integer, currently `1`. |
| `id` | ULID. Also part of the file name, so two events never share a file. |
| `type` | One of the types in §6. |
| `at` | RFC 3339, UTC. Every date in Ekumen (`on`, `deadline`, `decided_on`, file-name timestamps) is a **UTC** date. |
| `actor.role` | `lead` · `orchestrator` · `session` · `loop` (the control loop) · `integrator` (an end-to-end session that owns integration docs). |
| `actor.id` | The lead's handle, a session name, or `loop`. |
| `subject.kind` | `task` · `decision` · `contract` · `external` · `project`. |
| `subject.id` | The subject's id (§1). `project` for project-wide events. |
| `data` | Type-specific payload (§6). Unknown fields are kept and ignored. |

**Log order** (D-imported-event-time-order). There is no per-subject counter: two events written at the same moment by different writers both stand. Readers order the log causally within each subject, and by time across subjects:

- **Within one subject** (`subject.kind` and `subject.id`), events keep time order (the ULID's timestamp), except that an event stamped before its own prerequisite is placed right after it: `DecisionApplied` after the `DecisionDecided` or `DecisionDefaulted`; `QuestionAnswered` after its `QuestionAsked`; any other event about an item after the event that creates it (`TaskDrafted`, `DecisionRaised`, `ContractDrafted`, `ExternalAdded`). This covers backdated imports and clock skew.
- **No prerequisite:** routing, conflict and project-wide events (`subject.kind: project`: role claims and releases, spine approvals, conflicts).
- **Aliases:** an event addressed to an alias belongs to the sequence of the item of the same kind that declares the alias.
- **Across subjects,** events are ordered by time: readers merge the subjects' sequences, always taking the earliest next event. Remaining ties break by `id`.

A step whose prerequisite never exists is reported (`orphan-step`, §5), never dropped silently.

## 3. Lanes: who may write what

Every event type has one role allowed to write it: its **lane**. The CLI refuses to write an event outside the writer's lane. Anyone who needs an owner to act appends to their own lane, and the loop routes it (§4).

| Role | May write |
|---|---|
| **lead** | `TaskApproved`, `DecisionDecided`, `DecisionSuperseded`, `ContractFrozen`, `SpineApproved` |
| **orchestrator** | `TaskDrafted`, `TaskAmended` (§B; with `by_decision`, the loop), `TaskBlockersChanged`, `TaskDeferred`, `TaskUpdated`, `TaskSessionAttached`, `ContractDrafted`, `ContractBlockersChanged`, `ContractVersioned`, `External*` |
| **session** (its own task only) | `TaskHandoff` (§C) |
| **integrator** (its own task only) | `TaskHandoff` |
| **lead, orchestrator, session, integrator, loop** | `DecisionRaised`. A session or integrator raises only from its own task; the others may raise with `raised_by.task: null` (D-lane-gaps). |
| **anyone** | `QuestionAsked`. `QuestionAnswered` only by the question's addressee. |
| **the route's owner** | `RouteResolved`, for items routed to them (D-lane-gaps). |
| **the claiming session** | `RoleClaimed`, `RoleReleased` (D-claim-lane) |
| **loop** | `DecisionDefaulted`, `DecisionApplied`, `TaskSessionAttached`, `RoutedToOwner`, `ConflictDetected`, and `TaskAmended` with `data.by_decision` naming a decided or defaulted decision that blocks the task (in its `blocks`, or in the task's `blocked_by`) (D-apply-amend-lane) |

The orchestrator may run `ekumen decision apply` acting as `loop:<its session>`. That writes `DecisionApplied` and the §B amendments (`TaskAmended` with `by_decision`) in the loop's lane, with no orchestrator claim (D-apply-before-loop, D-apply-amend-lane). `ekumen reconcile` does the same for every decided decision in one pass.

**A session's own task** is the task whose `TaskDrafted.session` names it, or the latest `TaskSessionAttached` for that session (written by the orchestrator or the loop) (D-lane-gaps).

**One live holder per role.** A session takes a role with `RoleClaimed` (`ekumen claim orchestrator`) and gives it up with `RoleReleased`. A claim lasts until released or until its session is no longer alive. The CLI refuses a second claim while one is held.

## 4. Routing

The loop reads new lane entries and decides, in code, whether someone needs to act:

- A `TaskHandoff` with non-empty FOLLOW-UPS, DECISIONS RAISED or DEVIATIONS → `RoutedToOwner {to: orchestrator}`.
- A `DecisionRaised` → appears in the lead's inbox. The decision itself is the item.
- A `QuestionAsked` → routed to its `to`. If `to` is empty: on a decision, a fork of the session that raised it; on a task, that task's session; on a contract, the orchestrator.

A routed item stays open in its owner's inbox until the owner answers it (`QuestionAnswered`) or resolves it (`RouteResolved {route_id, note}`). This replaces the lead telling the orchestrator to go and look at a handoff.

## 5. Conflicts

Lanes prevent most contradictions. What's left is **detected, never resolved silently**: the loop writes `ConflictDetected {rule, events[]}` and raises a tier-2 decision for the lead.

| Rule | Meaning |
|---|---|
| `lane` | An event written by a role not allowed for its type (it can arrive through a git merge, bypassing the CLI). |
| `role` | Two live claims on the same role. |
| `decided-twice` | Two `DecisionDecided` with different answers for one decision. |
| `frozen-twice` | Two `ContractFrozen` for one version with different text. |
| `frozen-text-changed` | The text of an approved §A, or of a frozen contract, no longer matches the hash recorded at approval or at the contract's latest freeze (the `ContractFrozen` of its highest version). A contract text that matches a newer version recorded by `ContractVersioned` and not frozen yet is not a conflict. |
| `orphan-step` | A lifecycle step whose prerequisite is nowhere in the log: an event about a task, decision, contract or external dependency that was never created, a `DecisionApplied` with no `DecisionDecided` or `DecisionDefaulted`, a `QuestionAnswered` with no `QuestionAsked`, or an event addressed to an alias nothing declares (D-imported-event-time-order). |
| `spine-unapproved` | `spine.md` does not match the latest `SpineApproved`. Not reported before the first approval (D-post-switch-hand-edits). |
| `update-after-close` | A `TaskUpdated` that comes after the task's `TaskDeferred` in log order. The CLI refuses it; it arrives through a git merge. Done is observed, not logged, so a pending update on a done task is not a conflict, but it can no longer be approved (D-placeholder-task-fields). |
| `id-collision` | Two `DecisionRaised` or `TaskDrafted` for the same id from different writers. |

Events that don't contradict are never conflicts. Two questions on one decision at the same time both stand.

## 6. Event types, v1

**Task**

| Type | `data` |
|---|---|
| `TaskDrafted` | `title, kind (research\|contract\|build), brief, repo, base_branch, branch, worktree, blocked_by[], tier_grants[]`, optional `session`, optional `aliases[]` |
| `TaskApproved` | `tier_grants[]` (final list), `text_sha256` (§A as approved) |
| `TaskAmended` | `file, text_sha256, note` (one §B entry), optional `by_decision` (set when the amendment is made by applying that decision; then written by the loop) |
| `TaskBlockersChanged` | `added[], removed[], reason` |
| `TaskSessionAttached` | `session, adapter` |
| `TaskHandoff` | `file, text_sha256, status_line, has_followups, has_decisions, has_deviations` (one §C entry) |
| `TaskDeferred` | `reason` |
| `TaskUpdated` | Any subset of `repo, base_branch, branch, worktree, brief, title`, plus `reason`. At least one field besides `reason`. A present field is never empty; `branch` and `base_branch` are git branch names; `worktree` is a relative path inside the repository; `brief` is a relative path inside `.ekumen/`. An event that breaks these is invalid. Absent fields are unchanged. `TaskUpdated` and `TaskApproved` apply in log order (§2): an update before the task's first `TaskApproved` takes effect at once; one after it is pending until a later `TaskApproved`, which accepts every pending update before it (several merge field by field, newest wins). A pending update does not change derived status. On acceptance the task's `tier_grants` are the new approval's. Its §A hash is the new approval's only when the update moves the brief to another file; otherwise the first approval's hash stands. A `TaskApproved` with nothing pending changes nothing (D-placeholder-task-fields). |

**Decision**

| Type | `data` |
|---|---|
| `DecisionRaised` | The record as in C-decision-record §1, minus lifecycle fields |
| `DecisionDecided` | `answer, note` |
| `DecisionDefaulted` | `answer` (= `default_if_no_answer`), `deadline` |
| `DecisionSuperseded` | `by` (decision id), `note` |
| `DecisionApplied` | `amended_tasks[], unblocked_tasks[], notified_sessions[]` |

**Questions** (subject may be a task, decision or contract)

| Type | `data` |
|---|---|
| `QuestionAsked` | `question_id, text, to` (role or session; may be empty, see §4) |
| `QuestionAnswered` | `question_id, text` |

**Contract:** `ContractDrafted {title, path, aliases[]?, blocked_by[]?}` · `ContractBlockersChanged {added[], removed[], reason}` · `ContractFrozen {version, text_sha256}` · `ContractVersioned {version, by_decision, text_sha256}`. `ContractVersioned` records a new version of a frozen contract, made by a decided decision whose `contract_impact` names it. It does not freeze the version: the lead's `ContractFrozen` for the same version and text does (D-contract-version-recording, C-decision-record §3).

**Project:** `SpineApproved {text_sha256}` (subject `project`): the lead's approval of `spine.md` (D-post-switch-hand-edits).

**External dependency:** `ExternalAdded {what, owner_team, asked_on, needed_by, blocks[], aliases[]?}` · `ExternalUpdated {fields}` · `ExternalResolved {note}`

**Routing, roles, conflicts:** `RoutedToOwner {route_id, to, from_event, reason}` · `RouteResolved {route_id, note}` · `RoleClaimed {role, session}` · `RoleReleased {role, session}` · `ConflictDetected {rule, events[]}`

**Session events:** reserved, defined with C-adapter (the agent adapter contract).

## 7. Rules

1. **Append-only.** An event file is never edited or deleted. Corrections are new events.
2. **Observed state is not stored.** PR state, branch heads and session status are read fresh on every loop pass (spine invariant 6). Only facts Ekumen creates are events.
3. **Prose stays in markdown, one file per entry.** §A, each §B amendment, each §C handoff and each contract body are separate markdown files (§8). The matching event records who, when, the file, and a `sha256` of its text, so later edits are detectable.
4. **Owner-edited prose.** After the switch point nobody edits `.ekumen/` by hand except these prose files, each edited by its owner only (D-post-switch-hand-edits):
   - `spine.md`: the orchestrator; approved by the lead with `SpineApproved`
   - contract bodies: free to edit while draft; once frozen, changed only through the contract-change protocol (C-decision-record §3)
   - `brief.md` (§A) before approval: the orchestrator
   - `research/*.md`: the research task's session

## 8. File layout

```
.ekumen/
  events/<subject.kind>/<subject.id>/<event id>.<type>.json
  briefs/<task id>/
    brief.md                         # §A only; owner: orchestrator; frozen on approval
    amendments/<timestamp>-<slug>.md # one §B entry each; owner: orchestrator
    handoffs/<timestamp>-<session>.md# one §C entry each; owner: the task's session
  contracts/<contract id>.md
  research/<task id>.md
  spine.md                           # owner: orchestrator; approved by the lead (SpineApproved)
  config.yaml                        # project settings (project, lead, …); owner-edited
  decisions/<decision id>.yaml       # projection, committed
  registry.yaml                      # projection, local (git-ignored)
  state.json                         # projection, local (git-ignored)
```

`<timestamp>` is UTC `YYYYMMDDTHHMMSSZ`, so files sort by time and two writers never pick the same name.

Every change is a new file with a unique name, so git never sees two branches editing the same file and never reports a merge conflict. One known exception, accepted (D-entry-name-same-author-branches): the *same* author writing an entry for the *same* task on two branches within the same second produces two files with one name, and git reports a conflict at merge. It needs one role active on two branches at once, which role claims make rare, and git surfaces it loudly.

The file store checks that an event's `type` is a safe name (`^[A-Z][A-Za-z0-9]{0,63}$`), not that it is in §6; the core enforces the list (D-store-event-type-check). Contradictions are found by §5 instead.

**Projections** are generated from events and never hand-edited (D-projections-in-git).

- Per-item projections (`decisions/<id>.yaml`) are committed, so they read well on the git host. CI checks that they match the events (`ekumen project --check`). Two branches that change the same decision regenerate its file; to resolve the conflict, take either side and run `ekumen project`.
- Aggregate projections (`registry.yaml`, `state.json`) summarize the whole project, so two branches that each add one event would always conflict on them. They are git-ignored, and every command regenerates them when needed. A brief "view" (§A, then amendments, then handoffs, in time order) is generated on demand and not stored.

## 9. Versioning

Adding an event type or an optional field is a minor change: readers ignore what they don't know. Renaming or removing anything, or changing a meaning, needs a tier-2 decision, a new `schema` number, and a migration.

## Settled at freeze (2026-11-20)

- Ids are slugs with aliases for older items (D-019).
- §B and §C entries are separate files (§8).
- Projections are committed.
- Questions are recorded on any subject, not only decisions.
- The hand-written records made before the CLI existed become events through a one-off import (T-import).

## Version 3 (2026-11-20)

- Aliases on creating events (D-alias-field).
- Lanes widened: who may raise decisions, a session's own task, who resolves routes (D-lane-gaps).
- All dates are UTC dates.
- Documented the same-author entry-name case (D-entry-name-same-author-branches) and the store's type check (D-store-event-type-check).

## Version 4 (2026-11-20)

- Contracts can be blocked: `blocked_by[]` on `ContractDrafted` and `ContractBlockersChanged`. A task blocked by a contract stays blocked while that contract has open blockers (D-contract-blockers).
- `RoleClaimed`/`RoleReleased` are written by the claiming session (D-claim-lane).
- Until the loop exists, `DecisionApplied` is written through `ekumen decision apply` acting as the loop (D-apply-before-loop).
- Lifecycle order for events that share a timestamp.

## Version 5 (2026-11-23)

Consolidated from handoff proposals by the orchestrator (GitHub #57).

- Log order is causal within a subject: an event stamped before its prerequisite is placed after it; aliases join their item's sequence; time order across subjects. New rule `orphan-step` (D-imported-event-time-order).
- Only per-item projections are committed; `registry.yaml` and `state.json` are local (D-projections-in-git).
- `ContractVersioned` records a decided change to a frozen contract, which `ContractFrozen` then freezes; `frozen-text-changed` compares against the latest frozen version (D-contract-version-recording).
- New event `SpineApproved` (lead) and rule `spine-unapproved`; the owner-edited prose files are named in §7 (D-post-switch-hand-edits).
- `TaskAmended.by_decision`; amendments made by applying a decision are in the loop's lane (D-apply-amend-lane).
- New event `TaskUpdated` (orchestrator) and rule `update-after-close` (D-placeholder-task-fields).
- `config.yaml` added to the layout.
