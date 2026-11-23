---
id: C-decision-record
aliases: [C2]
title: Decision record
status: frozen         # draft | frozen
frozen_on: 2026-11-20
version: 3
consumers: [T-core, T-file-store, T-cli, T-server, T-decision-views, T-import]
producers: [T-core]
---

# C-decision-record · Decision record

A decision is a choice above a session's tier and a gate on the tasks it blocks (spine §3, §4). This contract fixes its fields, validation rules, lifecycle and the generated YAML file. The events that create and change a decision are in C-event-log §6.

**Tier 0 never creates a record.** Tier-0 actions (push own branch, open a draft PR, reply to review, restack) were approved in advance as the task's `tier_grants`, which `TaskApproved` records. Nobody waits on them, and their effects are observed directly by the loop. Recording each one would add noise and no information. A session that is unsure whether an action is covered by its grants treats it as tier 1 or 2 and raises a decision.

**Questions are not decisions.** A question that asks for information rather than a choice is a `QuestionAsked` event (C-event-log §6), which can be about a task, a decision or a contract. Questions about a decision are shown in its `questions` thread (§1).

## 1. Fields

| Field | Type | Required | Rule |
|---|---|---|---|
| `id` | string | yes | `D-<slug>` (C-event-log §1). |
| `aliases` | list of strings | no | Older ids, e.g. `D-012`. |
| `title` | string | yes | ≤ 100 characters, phrased as the choice. |
| `tier` | 1 \| 2 | yes | |
| `raised_by` | `{task, session, on}` | yes | `on` is a date. `task` may be `null` for decisions about the project itself. |
| `question` | string | yes | Answerable without opening the raising session. |
| `options` | list of `{id, tradeoff}` | yes | At least 2. `id` is a short slug, unique within the record. |
| `recommendation` | option id \| null | no | Must be one of `options[].id`. |
| `default_if_no_answer` | option id \| null | tier 1: **required**. Tier 2: **must be null**. | Applied by the control loop after `deadline`. |
| `deadline` | date \| null | tier 1: **required**. Tier 2: optional. | Drives priority. |
| `blocks` | list of task ids | yes (may be empty) | These tasks are blocked until the decision is applied. |
| `contract_impact` | list of contract ids | yes (may be empty) | Non-empty → must be tier 2; applying it starts the contract-change protocol (§3). |
| `context.summary` | string | yes | ≤ 150 words. Why it came up, what was tried, what evidence exists. |
| `context.evidence` | list of strings | no | Repo paths (optionally `#anchor`) or URLs. |
| `context.prior_decisions` | list of decision ids | no | |

**Lifecycle fields** are set by events only, never by the raiser:

| Field | Set by |
|---|---|
| `status` | `pending` → `decided` \| `defaulted` \| `superseded` |
| `questions` | `QuestionAsked` / `QuestionAnswered` on this decision: list of `{id, at, by, to, text, answer, answered_by, answered_at}` |
| `answer`, `answer_note`, `decided_by`, `decided_on` | `DecisionDecided` or `DecisionDefaulted`. `answer` is an option id, or `other` with the choice in `answer_note`. |
| `superseded_by` | `DecisionSuperseded` |
| `applied_on`, `applied` (`amended_tasks`, `unblocked_tasks`, `notified_sessions`) | `DecisionApplied` |

**Not stored:** `priority`. It is computed when read and appears only in `state.json`, the CLI and the UI (D-blocker-semantics, D-urgency-formula):

- **Tasks held up:** the tasks reached from the decision by following `blocked_by` links downstream, transitively, including through contracts in `contract_impact`. Deferred tasks are not counted.
- **Urgency** (UTC dates): no deadline or 8+ days away 1 · 4–7 days 2 · 2–3 days 3 · tomorrow 4 · today 5 · overdue 6.
- **Priority** = tasks held up × urgency. A decision that holds up nothing has priority 0. Ties break by earliest deadline, then id.

## 2. Validation

The CLI rejects a `DecisionRaised` that breaks any rule in §1, naming the field:

1. Tier 1 without `default_if_no_answer` or `deadline`.
2. Tier 2 with `default_if_no_answer`.
3. `contract_impact` non-empty and tier ≠ 2.
4. `recommendation` or `default_if_no_answer` not in `options`.
5. `context.summary` over 150 words.
6. `blocks` or `contract_impact` naming an item that doesn't exist.
7. An `id` that doesn't match the id format, or is already in use.

## 3. Lifecycle

```
pending ──(lead answers)──────────────► decided ─┐
   │ ──(tier 1, deadline passed)──────► defaulted ┼─(loop applies)─► applied_on set
   │ ──(another decision replaces it)─► superseded
   └─ questions may be asked and answered at any time
```

- **Applied** is not a status. It is recorded as `applied_on`, because a decided record stays decided after it is applied.
- **Applying** is done only by the control loop (`ekumen reconcile`, or `ekumen decision apply` run as the loop), and is idempotent. It adds a §B amendment to every task in `blocks` (written by the loop with `by_decision`; D-apply-amend-lane), removes the decision from their blockers, and messages live sessions.
- **Contract-change protocol** (D-contract-version-recording). A decided decision with `contract_impact` changes each contract it names in four steps:
  1. The orchestrator edits the contract body and bumps its front-matter `version`.
  2. The orchestrator records `ContractVersioned {version, by_decision, text_sha256}` with `ekumen contract version <id> --by <decision>`. This is refused unless the decision is decided or defaulted and names the contract, and the version is higher than the frozen one.
  3. The lead freezes the new text with `ekumen contract freeze <id>`. Freezing a new version of a frozen contract that wasn't recorded this way is refused.
  4. Until the freeze, `check` accepts the recorded text.

  Draft contracts change freely until their first freeze.
- A **superseded** decision is never applied, and it **stops blocking**. Its replacement carries the gate and should list the same `blocks` (D-blocker-semantics).
- A decided decision is **never reopened**. To change course, raise a new decision that supersedes it.

## 4. Generated file

`.ekumen/decisions/<id>.yaml` is generated from events: all §1 fields, then lifecycle fields, in the order listed. Its shape matches the hand-written records made before the CLI existed, plus `questions` and `applied_on`.

## Settled at freeze (2026-11-20)

- No reopening; supersede instead.
- Answers outside the listed options are `answer: other` plus `answer_note`.
- Questions are a general mechanism, not a decision-only field.

## Version 2 (2026-11-20)

- Superseded decisions stop blocking; priority's reach and the urgency formula are defined (D-blocker-semantics, D-urgency-formula).

## Version 3 (2026-11-23)

Consolidated from handoff proposals by the orchestrator (GitHub #57).

- Defined the contract-change protocol (D-contract-version-recording).
- Amendments added by applying a decision are written by the loop (D-apply-amend-lane).
