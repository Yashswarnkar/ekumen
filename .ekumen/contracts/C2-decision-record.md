---
id: C2
title: Decision record
status: draft          # draft | frozen
frozen_on: null
version: 0
consumers: []
producers: []
---

# C2 · Decision record

A decision is a choice above a session's tier and a gate on the tasks it blocks (spine §3, §4). This contract fixes its fields, validation rules, lifecycle and the generated YAML file. The events that create and change a decision are in C1 §4.

## 1. Fields

| Field | Type | Required | Rule |
|---|---|---|---|
| `id` | string | yes | See C1 "Open before freeze" for the scheme. |
| `title` | string | yes | ≤ 100 characters, phrased as the choice. |
| `tier` | 1 \| 2 | yes | Tier 0 never creates a record. |
| `raised_by` | `{task, session, on}` | yes | `on` is a date. `task` may be `null` for decisions about the project itself. |
| `question` | string | yes | Answerable without opening the raising session. |
| `options` | list of `{id, tradeoff}` | yes | ≥ 2 options, or ≥ 1 with an explicit `other` allowed. `id` is a short slug, unique within the record. |
| `recommendation` | option id \| null | no | Must be one of `options[].id`. |
| `default_if_no_answer` | option id \| null | tier 1: **required**. Tier 2: **must be null**. | Applied by the control loop after `deadline`. |
| `deadline` | date \| null | tier 1: **required**. Tier 2: optional. | Drives priority. |
| `blocks` | list of task ids | yes (may be empty) | These tasks are blocked until the decision is applied. |
| `contract_impact` | list of contract ids | yes (may be empty) | Non-empty → must be tier 2; applying it starts the contract-change protocol. |
| `context.summary` | string | yes | ≤ 150 words. Why it came up, what was tried, what evidence exists. |
| `context.evidence` | list of strings | no | Repo paths (optionally `#anchor`) or URLs. |
| `context.prior_decisions` | list of decision ids | no | |

**Lifecycle fields** are set by events only, never by the raiser:

| Field | Set by |
|---|---|
| `status` | `pending` → `decided` \| `defaulted` \| `superseded` |
| `questions` | list of `{id, at, by, text, route, answer, answered_by, answered_at}` |
| `answer`, `answer_note`, `decided_by`, `decided_on` | `DecisionDecided` or `DecisionDefaulted` |
| `superseded_by` | `DecisionSuperseded` |
| `applied_on`, `applied` (`amended_tasks`, `unblocked_tasks`, `notified_sessions`) | `DecisionApplied` |

**Not stored:** `priority`. It is computed on read (tasks blocked transitively × deadline urgency) and appears only in `state.json`, the CLI and the UI.

## 2. Validation

The CLI rejects a `DecisionRaised` that breaks any rule in §1, with a message naming the field. Specifically:

1. Tier 1 without `default_if_no_answer` or `deadline` → reject.
2. Tier 2 with `default_if_no_answer` → reject.
3. `contract_impact` non-empty and tier ≠ 2 → reject.
4. `recommendation` or `default_if_no_answer` not in `options` → reject.
5. `context.summary` over 150 words → reject.
6. `blocks` naming a task that does not exist → reject.

## 3. Lifecycle

```
pending ──(lead answers)──────────────► decided ─┐
   │ ──(tier 1, deadline passed)──────► defaulted ┼─(loop applies)─► applied
   │ ──(another decision replaces it)─► superseded
   └─ questions may be asked and answered any time while pending
```

- **Applied** is not a fourth status. It is recorded as `applied_on`, because a decided record stays decided after it is applied.
- **Applying** is done only by the control loop, and is idempotent. It adds a §B amendment to every task in `blocks`, removes the decision from their `blocked_by`, and messages live sessions.
- A **superseded** decision is never applied.

## 4. Generated file

`.ekumen/decisions/<id>.yaml` is generated from events: all §1 fields, then lifecycle fields, in the order listed. The shape matches the hand-written D-001 to D-012, plus `questions` and `applied_on`.

## Open before freeze

- Whether a decision can be **reopened** after it is decided (proposal: no. Raise a new decision that supersedes it).
- Whether `options` should allow free-text answers outside the listed ids (proposal: yes, recorded as `answer: other` plus `answer_note`).
