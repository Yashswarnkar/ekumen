---
name: blocked
description: Use in an Ekumen task session when you hit a choice you may not make yourself (above your task's tier grants, a contract that looks wrong, anything touching production, outward actions, releases or irreversible steps). Turns it into a decision record for the lead with `ekumen decision raise` and lets you keep working on everything else. Also use when the lead says "answer your questions", or at a natural breakpoint, to answer questions about the decisions you raised and pick up applied decisions.
---

# /blocked

You are a task session. You turn a choice you can't make into a decision for the lead, keep working, and answer the lead's questions about it later. Ekumen does the bookkeeping; you supply judgment: the question, the options and their tradeoffs, a recommendation, a short context summary and evidence.

## Rules

- Read state only through `ekumen … --json`. Never read or search the files under `.ekumen/` that `ekumen` generates or appends to. Following an evidence link to a prose file (a brief, a contract, research) is fine.
- Every command below uses `--json`. Read the JSON; don't parse the human output.
- Never commit as part of this skill. A write prints `{"ok": true, "events": […], "paths": […]}`; report the `paths` so whoever owns the commit can add them.
- Exit codes: `0` ok · `1` usage · `2` refused · `3` not found · `4` conflict · `5` internal. Errors print `{"error": {"code", "message", "field"}}`.
- Exit 2 with `code: lane` means wrong actor or wrong task. Stop and report it. Never retry as a different role.

## 1. Find who you are

Your actor is `EKUMEN_ACTOR`, or the `--as session:<name>` given in your start prompt. If you have neither, stop and ask the lead. Never make one up. If you only have it from the start prompt, add `--as session:<name>` to every command below.

Read your task and its grants:

```
ekumen task show <task> --json
```

`task.tier_grants` lists what you may do without asking. `brief.amendments` are the §B changes to your brief.

## 2. Do you need a decision at all?

- **Covered by `tier_grants`** (e.g. push your own branch, open a draft PR, restack): just do it. No record is made.
- **Unsure whether a grant covers it:** treat it as tier 1 or 2 and raise.
- **A contract looks wrong:** raise a decision. Never work around the contract locally.
- **Already asked?** Look for a pending decision on the same question:

```
ekumen decision list --status pending --json
```

If one exists, use it: add your view as a question on it (`ekumen question ask <decision id> --text … --json`) instead of raising a duplicate.

## 3. Draft the record

Fill in this template. Leave out `raised_by`, `status` and every other lifecycle field; the CLI fills them in or refuses them.

```yaml
id: D-<slug>
title: <the choice, as a choice, at most 100 characters>
tier: 1
question: <answerable without opening this session>
options:
  - {id: <option-a>, tradeoff: <what you gain and what it costs>}
  - {id: <option-b>, tradeoff: <what you gain and what it costs>}
recommendation: <option-a>
default_if_no_answer: <option-a>
deadline: <YYYY-MM-DD>
blocks: [<your task>]
contract_impact: []
context:
  summary: <150 words or fewer>
  evidence: [<repo path or URL>]
  prior_decisions: []
```

- `id` is `D-` plus a short lowercase slug. It must not be in use.
- At least 2 options, each with an honest tradeoff. `recommendation` is one of the option ids.
- **Tier 2** if `contract_impact` is non-empty, or the choice touches production, outward actions (anything others see), irreversible steps or releases. Then set `default_if_no_answer: null`; `deadline` is optional.
- **Tier 1** otherwise. Both `default_if_no_answer` and `deadline` are required: the default is applied if the lead doesn't answer by the deadline.
- `blocks` holds your own task, and nothing you can't name with confidence. Use `[]` if nothing waits on it.
- `context.summary`: why it came up, what you tried, what the evidence shows. 150 words or fewer.
- `context.evidence`: repo paths (optionally `#anchor`) or URLs the lead can open.

## 4. Raise it

Pass the record on stdin:

```
ekumen decision raise --file - --json
```

- **Exit 0:** note the id and the `paths`.
- **Exit 2, `code: validation`:** fix the field the error names (`error.field`) and raise again.
- **Exit 2, `code: lane`:** stop and report. You are acting as the wrong session or for the wrong task.
- **Exit 3 or 5:** stop and report the error.

## 5. Keep working

Carry on with every part of your task that doesn't depend on the answer. Don't wait, and don't poll in a loop. Put the decision id under `DECISIONS RAISED` in your next handoff.

## 6. Answer questions and pick up answers

Do this at natural breakpoints, or whenever the lead says "answer your questions", "check your decisions" or similar.

List the questions waiting for you:

```
ekumen question list --for <session> --open --json
```

For each item in `questions`, read `subject` and `text`, then answer from what you know. Say so plainly if you don't know.

```
ekumen question answer <question id> --text … --json
```

Then check each decision you raised:

```
ekumen decision show <decision id> --json
```

- `decision.status` is `pending`, `decided`, `defaulted` or `superseded`.
- `decision.answer` (and `decision.answer_note` for `other`) is the choice.
- `decision.applied_on` set means the decision has been applied to your brief.

Once it is applied, read your task again and follow the new entry in `brief.amendments`:

```
ekumen task show <task> --json
```

A superseded decision no longer blocks you; its replacement (`decision.superseded_by`) does. Don't act on a decided answer before `applied_on` is set unless the lead tells you to.

## Report

Tell the lead, in a few lines: the decision id and title, its tier, what you will keep doing meanwhile, and the `paths` written.
