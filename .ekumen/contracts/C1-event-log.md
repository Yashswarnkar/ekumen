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

The event log is Ekumen's source of truth (spine §6). Everything else (task status, decision files, `state.json`, the UI) is computed from it. This contract fixes the event format, how events are stored in the file store, and the v1 event types.

## 1. Envelope

Every event is one JSON object:

```json
{
  "schema": 1,
  "id": "01JD8Z3K6Q4M2V7X9T1B5N0RCE",
  "type": "DecisionRaised",
  "at": "2026-11-20T09:14:03Z",
  "actor": {"kind": "session", "id": "r1-acp"},
  "subject": {"kind": "decision", "id": "D-R1-1"},
  "seq": 1,
  "data": { }
}
```

| Field | Rule |
|---|---|
| `schema` | Envelope version. Integer, currently `1`. |
| `id` | ULID. Globally unique, sorts by creation time. |
| `type` | One of the types in §4. |
| `at` | RFC 3339, UTC, second precision or finer. |
| `actor.kind` | `lead` · `orchestrator` · `session` · `system` (the control loop). |
| `actor.id` | Lead's handle, session name, or `loop`. |
| `subject.kind` | `task` · `decision` · `contract` · `external` · `project`. |
| `subject.id` | The subject's stable id (`T3`, `D-012`, `C1`, `X1`). |
| `seq` | Per-subject sequence number: 1 for the first event about a subject, then +1 each time. |
| `data` | Type-specific payload (§4). Unknown fields are kept and ignored. |

## 2. Rules

1. **Append-only.** An event file is never edited or deleted. Corrections are new events.
2. **Ordering.** Events about one subject are ordered by `seq`. Across subjects, order by `id`.
3. **Single writer.** Two events with the same `subject` and `seq` mean two writers edited the same thing. Reading still succeeds; the control loop reports the conflict and raises a decision. It never picks a winner silently.
4. **Observed state is not stored.** PR state, branch heads and session status are read fresh on every loop pass (spine invariant 6). Only facts Ekumen creates are events: lead and session acts, and the loop's own actions.
5. **Prose stays in markdown.** Brief §A, §B, §C text and contract bodies live in their markdown files. The matching event (`TaskAmended`, `TaskHandoff`, `ContractFrozen`) records who, when, and a `sha256` of the text it added, so edits to that text can be detected.

## 3. File store layout

```
.ekumen/events/<subject.kind>/<subject.id>/<seq, zero-padded to 6>-<type>-<id>.json
```

Example: `.ekumen/events/decision/D-012/000003-DecisionDecided-01JD8Z….json`

One file per event means two branches can add events about different subjects, or the same subject, without git merge conflicts. Duplicate `seq` values after a merge are caught by rule 3.

**Projections** are generated from events and committed so they read well on GitHub. They are never hand-edited once the CLI exists:
- `.ekumen/decisions/<id>.yaml` (shape in C2)
- `.ekumen/registry.yaml` (task list)
- `.ekumen/state.json`

## 4. Event types, v1

**Task**

| Type | `data` |
|---|---|
| `TaskDrafted` | `title, kind (research\|contract\|build), brief, repo, base_branch, branch, worktree, blocked_by[], tier_grants[]` |
| `TaskApproved` | `tier_grants[]` (final list) |
| `TaskAmended` | `text_sha256, note` (one §B entry) |
| `TaskBlockersChanged` | `added[], removed[], reason` |
| `TaskSessionAttached` | `session, adapter` |
| `TaskHandoff` | `text_sha256, status_line` (one §C entry) |
| `TaskDeferred` | `reason` |

**Decision**

| Type | `data` |
|---|---|
| `DecisionRaised` | The full record as in C2 §1, minus the lifecycle fields |
| `DecisionQuestionAsked` | `question_id, text, route (fork\|queue\|original)` |
| `DecisionQuestionAnswered` | `question_id, text, answered_by` |
| `DecisionDecided` | `answer, note` |
| `DecisionDefaulted` | `answer` (= `default_if_no_answer`), `deadline` |
| `DecisionSuperseded` | `by` (decision id), `note` |
| `DecisionApplied` | `amended_tasks[], unblocked_tasks[], notified_sessions[]` |

**Contract:** `ContractDrafted {title, path}` · `ContractFrozen {version, text_sha256}` · `ContractVersioned {version, by_decision, text_sha256}`

**External dependency:** `ExternalAdded {what, owner_team, asked_on, needed_by, blocks[]}` · `ExternalUpdated {fields}` · `ExternalResolved {note}`

**Session events:** reserved. They will be defined after R1 reports, in a minor version of this contract.

## 5. Versioning

Adding an event type or an optional field is a minor change: readers must ignore what they don't know. Renaming or removing anything, or changing a meaning, needs a tier-2 decision, a new `schema` number, and a migration.

## Open before freeze

- **Decision ids.** Sequential `D-013` ids collide when two branches each raise a decision. Proposal: decisions raised by a task session use `D-<task>-<n>` (e.g. `D-R1-1`), unique without coordination because each task has one writer. Decisions raised on `main` by the lead or orchestrator keep `D-<nnn>`. The same scheme applies to task ids created by sessions.
- **Projections in git.** Committing projections makes them readable on GitHub but creates churn in diffs. Alternative: commit events only and generate projections locally. Proposal: commit them.
- **Migration.** The hand-written D-001 to D-012 and `registry.yaml` become events through a one-off import, which needs its own build task.
