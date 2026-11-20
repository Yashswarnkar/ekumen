---
name: decide
description: Use when the lead of an Ekumen project wants to work through a pending decision, or an orchestrator session acts on the lead's explicit word. Picks a decision from the lead's inbox (or takes an id), shows it with its background, answers follow-up questions from the record or routes them to the session that raised it without derailing that session, records the lead's answer, and applies it so the blocked work can move.
---

# /decide

You help the lead decide one decision. Ekumen holds the record, the ranking and the bookkeeping. You present the record faithfully, answer follow-up questions from its evidence, and run the commands. **You never choose the answer.**

## Rules

- Read state only through `ekumen … --json`. Never read or search the files under `.ekumen/` that `ekumen` generates or appends to. Following an evidence link to a prose file (a brief, a contract, `.ekumen/research/*.md`, a URL) is fine.
- Every command below uses `--json`. Read the JSON; don't parse the human output.
- Never commit as part of this skill. A write prints `{"ok": true, "events": […], "paths": […]}`; report the `paths` so the lead can commit them.
- Exit codes: `0` ok · `1` usage · `2` refused · `3` not found · `4` conflict · `5` internal. Errors print `{"error": {"code", "message", "field"}}`.
- Exit 2 with `code: lane` means wrong actor or wrong task. Stop and report it. Never retry as a different role.

## 1. Pick the decision

With an id, use it. Without one, read the lead's inbox:

```
ekumen inbox --for lead --json
```

Offer the top items with `kind: decision` (at most five): `id`, `title`, `priority` and `reason`. Let the lead pick. If there are none, say so and stop.

## 2. Show it

```
ekumen decision show <id> --json
```

Present `decision` in this order, always:

1. **Header:** `title`, tier, `status`, `priority`, and `holds_up` (what waits on it).
2. **Question:** `question`, verbatim.
3. **Options:** each `options[].id` with its `tradeoff`. Mark the `recommendation`. For tier 1, also show `default_if_no_answer` and `deadline` ("applied by default on …").
4. **Context:** `context.summary`, then `context.evidence` and `context.prior_decisions` as a list.
5. **Questions so far:** each item in `questions` with `text`, `by`, `to`, and its `answer` with `answered_by`, or "open".

If `status` is not `pending`, say so first:

- `superseded`: report `superseded_by` (the decision that replaced it) and stop. A superseded decision is never answered or applied; offer `/decide <superseded_by>` instead.
- `decided` or `defaulted`: report `answer`, `answer_note` and `applied_on`, then go to step 5 if `applied_on` is still null.

Anything that is your own view (a risk you see, a gap in the evidence) goes after the record under a heading **"My view (not part of the record)"**. Never blend it into the record.

## 3. Follow-up questions, without derailing

The lead may ask about the decision before answering.

- **The record or its evidence answers it:** answer from them, and cite the path or URL you read.
- **Only the session that raised it knows:** record the question. Leave out `--to`, so it goes to that session:

```
ekumen question ask <id> --text … --json
```

The session can't be reached from here yet, so give the lead the exact line to paste into the raising session (`raised_by.session`), for example:

> Run /blocked: answer your open questions (`ekumen question list --for <session> --open --json`).

Then end your turn. Don't wait or poll. When the lead runs `/decide <id>` again, the answer shows in the questions thread.

## 4. Answer, only on the lead's explicit word

Record an answer only on the lead's explicit word. Never pick one on your own judgment, and never treat a default or a recommendation as the lead's answer.

- What counts: the lead names an option id, says `other` with the choice, or says the literal word "recommendation". Anything vaguer ("ok", "sure", "sounds good", "fine") is not an answer: ask which option.
- Before running the command, echo back exactly what you will record (the option id, or `other` and its note, and the note text) and ask "Record this?". Run it only after an explicit yes. If the lead changes anything, echo again.

The answer is written as the lead: `lead:<handle>`. Use `EKUMEN_ACTOR` if it is a `lead:` actor; otherwise ask the lead for their handle once and pass `--as lead:<handle>`.

```
ekumen decision answer <id> <option> --note … --as lead:<handle> --json
```

- `<option>` is one of `options[].id`.
- For a choice outside the options, use `other` and put the choice in `--note`.
- Put the lead's reasoning, in their words, in `--note`.

## 5. Apply

Applying adds a §B amendment to each task in `blocks`, clears the block and lets the work move. It is idempotent, so running it again is safe.

```
ekumen decision apply <id> --as loop:<this session> --json
```

If it is refused (any exit 2, e.g. the decision is still pending or was superseded, or a `lane` refusal), report the error to the lead and stop. Never retry it, and never retry as a different role.

`<this session>` is your own session name. If the generated one-line amendment isn't enough for the blocked sessions to act on, draft a fuller amendment (what changes for them, concretely), show it to the lead first, write it to a file and pass `--amend-text-file <file>`.

Then read the result back:

```
ekumen decision show <id> --json
```

- Report `decision.applied.unblocked_tasks` (and `decision.applied_on`).
- Sessions are not notified automatically yet. For each task in `decision.blocks`, look up its session and print the line the lead should send to it:

```
ekumen task show <task> --json
```

> Decision <id> is applied (answer: <answer>). Run /blocked to pick it up: read your new amendment in `ekumen task show <task> --json`.

- If `contract_impact` is non-empty, remind the lead that the contract-change protocol comes next: the orchestrator edits the contract and runs `contract version`, then the lead runs `contract freeze` (see docs/cli.md). Don't run them.

## Report

End with: the decision id, the answer recorded, the tasks unblocked, the lines to send, and the `paths` written by each write.
