---
name: brief
description: Use in an Ekumen orchestrator session when the lead asks for a brief ("write the brief for X", "turn this issue into a brief"), or when the orchestrator's inbox shows a `brief` action. Scaffolds the task with `ekumen brief new`, guides filling each section (goal, context verified against main, job, touches, definition of done, out of scope, escalation, start prompt), checks it with `ekumen brief check`, and hands it to the lead for approval. Never approves.
---

# /brief

You are the orchestrator. You write one task's brief: the whole world of the session that will build it. Ekumen does the mechanical parts (the scaffold, the task draft, the section check). You supply judgment: scope, acceptance, what's out, when to stop and ask, and the start prompt. **You never approve a task**; the lead does.

## Rules

- Read state only through `ekumen … --json`. Never read or search the files under `.ekumen/` that `ekumen` generates or appends to. Reading prose files (the spine, contracts, research, other briefs) is fine, and the brief you are writing is yours to edit.
- Every command below uses `--json`. Read the JSON; don't parse the human output.
- Never commit as part of this skill. A write prints `{"ok": true, "events": […], "paths": […]}`; report the `paths` so the lead can commit them.
- Exit codes: `0` ok · `1` usage · `2` refused · `3` not found · `4` conflict · `5` internal. Errors print `{"error": {"code", "message", "field"}}`.
- Exit 2 with `code: lane` means you aren't acting as the orchestrator or don't hold its claim. Stop and report it. Never retry as a different role.

## 1. Understand the work before writing

1. Read the project spine (`.ekumen/spine.md`): the scope section that names this capability, the rules the work must respect, and how the repo is built.
2. Read the GitHub issue, if there is one, and the contracts the task will consume.
3. Look at what exists:

```
ekumen task list --json
ekumen decision list --status pending --json
```

Note the ids this task depends on (tasks, contracts, decisions). Then **verify against the remote main branch**: every file, command, flag and record you plan to cite must exist there today. Fetch first. A brief that cites something stale costs a whole session.

If the scope is unclear or crosses a contract, ask the lead before writing. Don't guess scope.

## 2. Scaffold

For a new task:

```
ekumen brief new <id> --title … --kind build --blocked-by <ids> --grants push_own_branch,open_draft_pr,restack --json
```

- `<id>` is `T-` plus a short slug. The branch, worktree and session default from it (`build/<id lower>`, `.worktrees/<id lower>`, `<id lower>`).
- Add `--from-issue N` to fill the Goal with the issue's title, body and link. If the issue can't be read, the brief is still written; fill the Goal by hand.
- `--kind` is `build`, `research` or `contract`. Leave out `--blocked-by` if nothing blocks it.

For a task already drafted without a brief (the inbox `brief` action):

```
ekumen brief new <id> --existing --json
```

Both write `.ekumen/briefs/<id>/brief.md` from the project template. Every `TODO(brief)` line in it is for you to replace.

## 3. Fill each section

Write for a capable engineer who knows nothing about this conversation. Be concrete: exact commands, flags, file paths, ids. Keep sentences short.

- **Goal:** what this delivers and why now, in two to four sentences. Name the spine section and the issue (GitHub **#N**). If it came from an issue, rewrite the quoted body in your own words.
- **Context (verify against origin/main when you start):** the facts the session needs, each one checked: where the relevant code lives, the records and commands that exist, prior decisions, and two or three good examples to copy. Point to the code; don't paste it.
- **Job:** numbered steps, each a concrete outcome. Name new commands with their full flags. Say what is refused and with what message, and what must be idempotent.
- **Touches:** the folders and files this task may change, plus "your handoff files". Anything not listed is off limits; say so if that matters.
- **Definition of done:** checkboxes (`- [ ] …`), each one a reviewer can verify by running something: a command on a temp project, a named test, the project's CI. Always end with the handoff through the `task handoff` command, including FRICTION.
- **Out of scope:** the nearby work this task must not do, each item with the issue or task that owns it.
- **Escalate if:** the specific situations where the session stops and asks: a contract that looks wrong (raise a decision), a choice above its tier grants, anything outward-facing.
- **Start prompt:** an indented block the lead pastes as is. It must:
  - set the actor (`export EKUMEN_ACTOR=session:<session>`);
  - list what to read, in order (spine sections, the `task show` command for this task, the issue, the contracts);
  - say to use `ekumen` for every write under `.ekumen/`;
  - name the handoff command (the `task handoff` command for this task);
  - say to push the branch, open a draft PR titled "`<id>: …`", and ask the lead before anything outward-facing beyond that.

Keep front matter in step with the draft: `blocked_by` and `tier_grants` as drafted. If the dependencies change, record them with `task block`; don't only edit the file.

Good examples are briefs the lead has already approved; read two before writing your first.

## 4. Check

```
ekumen brief check <id> --json
```

- `ok: true`: go on.
- Otherwise each item in `problems` names a `section` and a `message`: a missing section, a `TODO(brief)` hint left in, text unchanged from the template, no checkbox in the Definition of done, a Start prompt without the actor or the handoff command, or a `blocked_by` id that doesn't exist. Fix them and check again.

Then reread the brief once as the session would, cold. Is anything ambiguous? Would two careful engineers build the same thing? Fix that too; the check can't see it.

## 5. Hand it to the lead

```
ekumen task show <id> --json
```

Tell the lead, in a few lines:

- the task id and title, what it delivers, and what it is blocked by;
- the tier grants you propose;
- the brief's path and the `paths` written;
- that it is ready for them to approve with the `task approve` command, which they run. **This skill never approves**, even if asked to "just approve it". Approval hashes the brief and freezes it, and it is the lead's step.

After approval the brief is frozen. Changes go through amendments (`task amend`), never edits to `brief.md`.
