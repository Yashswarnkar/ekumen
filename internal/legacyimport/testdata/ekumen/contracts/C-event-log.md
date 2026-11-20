---
id: C-event-log
aliases: [C1]
title: Event log
status: frozen         # draft | frozen
frozen_on: 2026-11-20
version: 3
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

Events are ordered by `id`. There is no per-subject counter: two events written at the same moment by different writers both stand.

## 3. Lanes: who may write what

Every event type has one role allowed to write it: its **lane**. The CLI refuses to write an event outside the writer's lane. Anyone who needs an owner to act appends to their own lane, and the loop routes it (§4).

| Role | May write |
|---|---|
| **lead** | `TaskApproved`, `DecisionDecided`, `DecisionSuperseded`, `ContractFrozen` |
| **orchestrator** | `TaskDrafted`, `TaskAmended` (§B), `TaskBlockersChanged`, `TaskDeferred`, `TaskSessionAttached`, `ContractDrafted`, `ContractVersioned`, `External*` |
| **session** (its own task only) | `TaskHandoff` (§C) |
| **integrator** (its own task only) | `TaskHandoff` |
| **lead, orchestrator, session, integrator, loop** | `DecisionRaised`. A session or integrator raises only from its own task; the others may raise with `raised_by.task: null` (D-lane-gaps). |
| **anyone** | `QuestionAsked`. `QuestionAnswered` only by the question's addressee. |
| **the route's owner** | `RouteResolved`, for items routed to them (D-lane-gaps). |
| **loop** | `DecisionDefaulted`, `DecisionApplied`, `TaskSessionAttached`, `RoutedToOwner`, `ConflictDetected`, `RoleClaimed`, `RoleReleased` |

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
| `frozen-text-changed` | The text of an approved §A or a frozen contract no longer matches the hash recorded at approval or freeze. |
| `id-collision` | Two `DecisionRaised` or `TaskDrafted` for the same id from different writers. |

Events that don't contradict are never conflicts. Two questions on one decision at the same time both stand.

## 6. Event types, v1

**Task**

| Type | `data` |
|---|---|
| `TaskDrafted` | `title, kind (research\|contract\|build), brief, repo, base_branch, branch, worktree, blocked_by[], tier_grants[]`, optional `session`, optional `aliases[]` |
| `TaskApproved` | `tier_grants[]` (final list), `text_sha256` (§A as approved) |
| `TaskAmended` | `file, text_sha256, note` (one §B entry) |
| `TaskBlockersChanged` | `added[], removed[], reason` |
| `TaskSessionAttached` | `session, adapter` |
| `TaskHandoff` | `file, text_sha256, status_line, has_followups, has_decisions, has_deviations` (one §C entry) |
| `TaskDeferred` | `reason` |

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

**Contract:** `ContractDrafted {title, path, aliases[]?}` · `ContractFrozen {version, text_sha256}` · `ContractVersioned {version, by_decision, text_sha256}`

**External dependency:** `ExternalAdded {what, owner_team, asked_on, needed_by, blocks[], aliases[]?}` · `ExternalUpdated {fields}` · `ExternalResolved {note}`

**Routing, roles, conflicts:** `RoutedToOwner {route_id, to, from_event, reason}` · `RouteResolved {route_id, note}` · `RoleClaimed {role, session}` · `RoleReleased {role, session}` · `ConflictDetected {rule, events[]}`

**Session events:** reserved, defined with C-adapter (the agent adapter contract).

## 7. Rules

1. **Append-only.** An event file is never edited or deleted. Corrections are new events.
2. **Observed state is not stored.** PR state, branch heads and session status are read fresh on every loop pass (spine invariant 6). Only facts Ekumen creates are events.
3. **Prose stays in markdown, one file per entry.** §A, each §B amendment, each §C handoff and each contract body are separate markdown files (§8). The matching event records who, when, the file, and a `sha256` of its text, so later edits are detectable.

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
  decisions/<decision id>.yaml       # projection
  registry.yaml                      # projection
  state.json                         # projection
```

`<timestamp>` is UTC `YYYYMMDDTHHMMSSZ`, so files sort by time and two writers never pick the same name.

Every change is a new file with a unique name, so git never sees two branches editing the same file and never reports a merge conflict. One known exception, accepted (D-entry-name-same-author-branches): the *same* author writing an entry for the *same* task on two branches within the same second produces two files with one name, and git reports a conflict at merge. It needs one role active on two branches at once, which role claims make rare, and git surfaces it loudly.

The file store checks that an event's `type` is a safe name (`^[A-Z][A-Za-z0-9]{0,63}$`), not that it is in §6; the core enforces the list (D-store-event-type-check). Contradictions are found by §5 instead.

**Projections** are generated from events and committed, so they read well on GitHub. Once the CLI exists they are never hand-edited. A brief "view" (§A, then amendments, then handoffs, in time order) is generated on demand and not stored.

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
