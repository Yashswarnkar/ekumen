---
id: C1
title: Event log
status: draft          # draft | frozen
frozen_on: null
version: 0
consumers: []          # filled when build briefs are written
producers: []
---

# C1 · Event log

The event log is Ekumen's source of truth (spine §6). Everything else (task status, decision files, `state.json`, the UI) is computed from it. This contract fixes the event format, who may write which events, how conflicts are found, how events are stored, and the v1 event types.

## 1. Envelope

Every event is one JSON object:

```json
{
  "schema": 1,
  "id": "01JD8Z3K6Q4M2V7X9T1B5N0RCE",
  "type": "DecisionRaised",
  "at": "2026-11-20T09:14:03Z",
  "actor": {"role": "session", "id": "r1-acp"},
  "subject": {"kind": "decision", "id": "D-control-loop"},
  "data": { }
}
```

| Field | Rule |
|---|---|
| `schema` | Envelope version. Integer, currently `1`. |
| `id` | ULID: unique, and sorts by creation time. Also the file name, so two events never share a file. |
| `type` | One of the types in §5. |
| `at` | RFC 3339, UTC. |
| `actor.role` | `lead` · `orchestrator` · `session` · `loop` (the control loop) · `integrator` (an end-to-end session that owns integration docs). |
| `actor.id` | The lead's handle, a session name, or `loop`. |
| `subject.kind` | `task` · `decision` · `contract` · `external` · `project`. |
| `subject.id` | The subject's stable id (scheme: D-019). |
| `data` | Type-specific payload (§5). Unknown fields are kept and ignored. |

There is no per-subject counter. Events are ordered by `id` (time). Two events written at the same moment by different writers are both kept.

## 2. Lanes: who may write what

Every event type has exactly one role allowed to write it: its **lane**. The CLI refuses to write an event outside the writer's lane. Anyone who needs an owner to act appends to their own lane, and the loop routes it to the owner (§3).

| Owner | May write |
|---|---|
| **lead** | `TaskApproved`, `DecisionDecided`, `DecisionSuperseded`, `DecisionQuestionAsked`, `ContractFrozen` |
| **orchestrator** | `TaskDrafted`, `TaskAmended` (§B), `TaskBlockersChanged`, `TaskDeferred`, `ContractDrafted`, `ContractVersioned`, `External*`, `RouteResolved` (for items routed to it) |
| **session** (only its own task) | `TaskHandoff` (§C), `DecisionRaised`, `DecisionQuestionAnswered` (only for questions routed to it) |
| **integrator** (only docs it owns) | `TaskHandoff` on its own integration task, `DecisionRaised` |
| **loop** | `DecisionDefaulted`, `DecisionApplied`, `TaskSessionAttached`, `RoutedToOwner`, `ConflictDetected`, `RoleClaimed`, `RoleReleased` |

**One live holder per role.** A session takes a role with `RoleClaimed` (through `ekumen claim orchestrator`) and gives it up with `RoleReleased`. A claim lasts until it is released or its session is no longer alive. The CLI refuses a second claim while one is held. This is the main defence against two orchestrator sessions writing at once.

## 3. Routing

The loop reads new lane entries and decides, in code, whether an owner needs to act:

- A `TaskHandoff` whose FOLLOW-UPS, DECISIONS RAISED or DEVIATIONS are non-empty → `RoutedToOwner {to: orchestrator, from_event, reason}`.
- A `DecisionRaised` → appears in the lead's inbox (the decision itself is the item; no routing event needed).
- A `DecisionQuestionAsked` → `RoutedToOwner {to: <fork of the raising session>, from_event}`.

An item stays open in the owner's inbox until the owner writes `RouteResolved {route_id, note}`. This replaces "the lead goes and tells the orchestrator to look at a handoff".

## 4. Conflicts

Lanes prevent most contradictions. What's left is **detected, never resolved silently**: the loop writes `ConflictDetected {rule, events[]}` and raises a tier-2 decision for the lead.

| Rule | Meaning |
|---|---|
| `lane` | An event written by a role not allowed for its type (it can arrive through a git merge, bypassing the CLI). |
| `role` | Two live claims on the same role. |
| `decided-twice` | Two `DecisionDecided` with different answers for one decision. |
| `frozen-twice` | Two `ContractFrozen` for one version with different text. |
| `frozen-text-changed` | The text of an approved §A or a frozen contract no longer matches the hash recorded when it was approved or frozen. |
| `id-collision` | Two `DecisionRaised` (or `TaskDrafted`) for the same id from different writers. |

Events that don't contradict are never conflicts. Two questions asked on one decision at the same time, for example, both stand.

## 5. Event types, v1

**Task**

| Type | `data` |
|---|---|
| `TaskDrafted` | `title, kind (research\|contract\|build), brief, repo, base_branch, branch, worktree, blocked_by[], tier_grants[]` |
| `TaskApproved` | `tier_grants[]` (final list), `text_sha256` (§A as approved) |
| `TaskAmended` | `text_sha256, note` (one §B entry) |
| `TaskBlockersChanged` | `added[], removed[], reason` |
| `TaskSessionAttached` | `session, adapter` |
| `TaskHandoff` | `text_sha256, status_line, has_followups, has_decisions, has_deviations` (one §C entry) |
| `TaskDeferred` | `reason` |

**Decision**

| Type | `data` |
|---|---|
| `DecisionRaised` | The record as in C2 §1, minus the lifecycle fields |
| `DecisionQuestionAsked` | `question_id, text` |
| `DecisionQuestionAnswered` | `question_id, text` |
| `DecisionDecided` | `answer, note` |
| `DecisionDefaulted` | `answer` (= `default_if_no_answer`), `deadline` |
| `DecisionSuperseded` | `by` (decision id), `note` |
| `DecisionApplied` | `amended_tasks[], unblocked_tasks[], notified_sessions[]` |

**Contract:** `ContractDrafted {title, path}` · `ContractFrozen {version, text_sha256}` · `ContractVersioned {version, by_decision, text_sha256}`

**External dependency:** `ExternalAdded {what, owner_team, asked_on, needed_by, blocks[]}` · `ExternalUpdated {fields}` · `ExternalResolved {note}`

**Routing, roles, conflicts:** `RoutedToOwner {route_id, to, from_event, reason}` · `RouteResolved {route_id, note}` · `RoleClaimed {role, session}` · `RoleReleased {role, session}` · `ConflictDetected {rule, events[]}`

**Session events:** reserved, defined with C4 (the agent adapter contract).

## 6. Rules

1. **Append-only.** An event file is never edited or deleted. Corrections are new events.
2. **Observed state is not stored.** PR state, branch heads and session status are read fresh on every loop pass (spine invariant 6). Only facts Ekumen creates are events.
3. **Prose stays in markdown.** §A, §B and §C text and contract bodies live in markdown. The matching event records who, when, and a `sha256` of the text, so later edits are detectable.

## 7. File store layout

```
.ekumen/events/<subject.kind>/<subject.id>/<id>.<type>.json
```

Example: `.ekumen/events/decision/D-control-loop/01JD8Z3K6Q4M2V7X9T1B5N0RCE.DecisionDecided.json`

Every change is a new file with a unique name, so git never sees two branches editing the same file and never reports a merge conflict. Contradictions are found by §4 instead.

**Projections** are generated from events and committed so they read well on GitHub. Once the CLI exists they are never hand-edited:
- `.ekumen/decisions/<id>.yaml` (shape in C2)
- `.ekumen/registry.yaml` (task list)
- `.ekumen/state.json`

## 8. Versioning

Adding an event type or an optional field is a minor change: readers ignore what they don't know. Renaming or removing anything, or changing a meaning, needs a tier-2 decision, a new `schema` number, and a migration.

## Open before freeze

- **Id scheme:** D-019.
- **Handoff files:** §B and §C entries currently live inside the brief file, which two writers then append to. Proposal: store each entry as its own file (`.ekumen/briefs/<task>/handoffs/<ulid>.md`, likewise for amendments). The brief view shows them together, and git conflicts become impossible there too.
- **Projections in git:** committing them makes them readable on GitHub but adds noise to diffs. Proposal: commit them.
- **Migration:** the hand-written D-001 to D-019 and `registry.yaml` become events through a one-off import (task T9).
