---
id: T-skills
title: /blocked and /decide skills
kind: build
repo: ekumen
base_branch: main
branch: build/t-skills
worktree: .worktrees/t-skills
session: t-skills
contracts_consumed: [C-cli-json, C-decision-record, C-event-log]
contracts_produced: []
blocked_by: []
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-skills · /blocked and /decide skills

*§A only. Amendments (§B) are files in `amendments/`, handoffs (§C) are files in `handoffs/` (C-event-log §8).*

**Goal:** Ship the two skills that make Ekumen usable day to day (VISION "gradual adoption": day one is `/blocked` and `/decide`). `/blocked` lets a task session turn something it can't decide into a decision without stopping its other work. `/decide` lets the lead work through one decision: see it with its background, ask follow-up questions without derailing the session that raised it, answer it, and apply it so the blocked work can move. Both are thin `SKILL.md` files in the cross-vendor Agent Skills format around `ekumen … --json` (spine §6, invariant 9, D-code-over-tokens). The agent supplies judgment only: the question, the options and their tradeoffs, a recommendation, a context summary of 150 words or fewer, and evidence. Everything else is a CLI call.

**Context (verified against `main` at `476ad0b`; re-verify against `origin/main` when you start):**
- Every command the skills need already exists, and its `--json` shape is frozen in C-cli-json v2 and documented in `docs/cli.md`: `decision raise --file -`, `decision list|show`, `decision answer`, `decision apply`, `question ask|list|answer`, `inbox --for`, `task show`.
- The whole flow was smoke-tested on a scratch copy of the real `.ekumen/`:
  1. A session raises a decision that blocks its own task.
  2. The lead asks a question about it with no `--to`. The question goes to `session:<raiser>` and shows in `inbox --for <session>` as `kind: question`.
  3. The session answers. The answer appears in `decision show --json` under `decision.questions[]`.
  4. The lead answers the decision.
  5. `--as loop:<id> decision apply` records the amendment. It warns on stderr "no session messaging exists yet; tell the sessions of … yourself".
- **A session can raise only for its own task.** Before you attach, `decision raise` as `session:t-skills` is refused with `lane: … a session may raise decisions only for its own task`. The orchestrator runs `ekumen task attach T-skills --session t-skills` before you start.
- **This task's registry record is an imported placeholder.** Its `TaskDrafted` has empty `repo`, `branch`, `worktree` and `brief`, and `tier_grants: []`. No CLI command updates those fields yet. The branch `build/t-skills` and worktree `.worktrees/t-skills` named in this front matter apply anyway. `ekumen task show` will keep showing them as null, and the status as `approved` rather than `ready` (D-task-status-approved), until a CLI fix lands. The grants come from `task approve --grants`.
- `-h` on any subcommand prints `Usage: ekumen <cmd> …` and exits 1 without an error message. An unknown command or flag exits 1 with a message (`unknown command` / `flag provided but not defined`). The test below relies on that difference.
- The CLI test harness (`newHarness`, `buildFixture`, `h.run`, `h.must` in `internal/cli/harness_test.go`) lives in package `cli` and isn't exported. Tests that need it go in `internal/cli/`.
- C-adapter is **frozen at version 1** (2026-11-20) and T-claude-adapter hasn't started. Nothing can fork a session or deliver a message yet. The skills use only the fallbacks in C-adapter §3: questions are queued for the raising session, and the lead is shown what to tell it. They must not depend on any adapter.
- No `skills/` directory exists yet.

**Job:**

1. **Where the skills live.**
   - Put them in `skills/blocked/SKILL.md` and `skills/decide/SKILL.md` at the repo root. The folder name is the skill name, so the commands are `/blocked` and `/decide`.
   - Front matter follows the Agent Skills format: `name` (equal to the folder, lowercase letters, digits and hyphens, ≤ 64 characters) and `description` (≤ 1024 characters, saying when to use the skill). Add no tool-specific keys.
   - The body names no agent tool or vendor (invariant 12).
   - Add short reference files beside a `SKILL.md` only if it would otherwise go past about 150 lines.

2. **`/blocked`, used by a task session about its own task.** The skill tells the agent to work in this order:
   1. **Find who it is.** The actor is `EKUMEN_ACTOR`, or the `--as session:<name>` given in its start prompt. If it has neither, stop and ask the lead; never make one up. Read its task with `ekumen task show <task> --json`.
   2. **Check whether it needs a decision at all.**
      - If the action is covered by the task's `tier_grants`, just do it. No record is made (C-decision-record, "Tier 0 never creates a record").
      - If the session thinks a contract is wrong, it raises a decision. It never works around the contract (invariant 4).
      - Run `ekumen decision list --status pending --json` and look for an existing decision on the same question. If there is one, use it.
   3. **Draft the record** to C-decision-record §1:
      - `id` is `D-<slug>`.
      - The `title` is phrased as the choice.
      - The `question` can be answered without opening the session.
      - Give at least 2 options, each with its tradeoff, plus a `recommendation`.
      - **Tier:** 2 if `contract_impact` is non-empty or the choice touches production, outward actions, irreversible steps or releases. Otherwise tier 1, with `default_if_no_answer` and `deadline` both set.
      - `blocks` holds the session's own task (and nothing it can't name with confidence).
      - `context.summary` is 150 words or fewer: why it came up, what was tried, what the evidence shows.
      - `context.evidence` lists repo paths or URLs.
      - Leave out `raised_by`; the CLI fills it in. Include a YAML template in the skill.
   4. **Raise it** with `ekumen decision raise --file - --json`, passing the record on stdin. If it is refused with exit 2, fix the field the error names and retry. On exit 3 or 5, stop and report.
   5. **Keep working.** Carry on with any part of the task that doesn't depend on the answer. Don't wait or poll in a loop. Record the decision under `DECISIONS RAISED` in the next handoff.
   6. **Answer questions about its decisions.** At natural breakpoints, or when the lead says so:
      - Run `ekumen question list --for <session> --open --json` and answer each question with `ekumen question answer <qid> --text … --json`.
      - Check `ekumen decision show <id> --json` for `status` and `applied_on`.
      - Once a decision is applied, read the new §B entry in `ekumen task show <task> --json` (`brief.amendments`) and follow it.

   The skill also covers the case where the lead says "answer your questions" or similar, so a session can be pointed back to it.

3. **`/decide`, used by the lead, or by an orchestrator session acting on the lead's explicit word.**
   1. **Pick the decision.** With no id, run `ekumen inbox --for lead --json` and offer the top `kind: decision` items (id, title, priority, reason). With an id, use that one.
   2. **Show it.** Run `ekumen decision show <id> --json` and present, in a fixed order:
      - title, tier, priority and `holds_up`
      - question
      - options with tradeoffs, marking the recommendation (and the default and deadline for tier 1)
      - context summary, evidence and prior decisions
      - the `questions` thread, with answers or "open"

      Any view of the agent's own is labelled as its own and kept apart from the record.
   3. **Handle follow-up questions without derailing (invariant 10).**
      - If the record or its evidence (repo files, `.ekumen/research/*.md`, URLs) answers the lead, answer from them and cite them.
      - If only the raising session knows, record the question with `ekumen question ask <id> --text … --json`, with no `--to`, so it goes to the session that raised the decision.
      - Until an adapter can fork or deliver, use the C-adapter §3 fallback. Print the exact line the lead should paste into the raising session (for example "Run /blocked: answer your open questions (`ekumen question list --for <session> --open --json`)"). Then end the turn. Don't wait. Running `/decide <id>` again later shows the answer in the thread.
   4. **Answer, only on the lead's explicit word.**
      - Run `ekumen decision answer <id> <option|other> --note … --json` as `lead:<handle>` (from `EKUMEN_ACTOR`, or ask the lead once).
      - For `other`, the choice goes in `--note`.
      - The skill never picks an answer on its own judgment.
   5. **Apply.** Run `ekumen decision apply <id> --as loop:<this session's name> --json`. It is idempotent.
      - The agent may draft the §B amendment text and pass it with `--amend-text-file` when the generated one-liner isn't enough. That is judgment; the lead sees it first.
      - Report `unblocked_tasks`.
      - For each task in `blocks`, print the line the lead should send to that task's session (the fallback for "notify live sessions").
      - If `contract_impact` is non-empty, remind the lead of the contract-change protocol (`docs/cli.md`, `contract version` then `contract freeze`). Don't run it.

4. **Rules for both skills.**
   - Read state only through `ekumen … --json`. Never read or grep `.ekumen/events/`, `decisions/*.yaml`, `registry.yaml` or `state.json`. Following an evidence link to a prose file is fine.
   - Every read command in a skill uses `--json`.
   - Skills never commit. They report the `paths` a write printed, and the caller commits (C-cli-json "the CLI never commits").
   - Exit codes are handled as C-cli-json §1 defines them. Exit 2 with `code: lane` means "wrong actor or wrong task". Stop and report; never retry as a different role.

5. **Install command and docs** (lead, 2026-11-20: "approve + install command").
   - Embed `skills/` in the binary (`//go:embed`, from a small package next to the folder) so a release binary carries the skills that match its CLI.
   - Add `ekumen skills list [--json]` and `ekumen skills install --dir <path> [--force] [--json]`. `install` writes each `skills/<name>/` under `<dir>/<name>/`, refuses to overwrite a differing existing file without `--force` (exit 2, naming the file), is a no-op for identical files, and prints the written paths in the usual write result shape. It never guesses a tool-specific folder: the caller passes `--dir`. It needs no actor and writes nothing under `.ekumen/`.
   - This adds commands to C-cli-json v2. The orchestrator raises the contract-change decision (it does not block this task); put the exact C-cli-json text for both commands in the handoff, and the orchestrator versions the contract after merge.
   - Write `docs/skills.md`: what each skill does and the actor it needs; installing with `ekumen skills install --dir …` and, as a table in this doc only, the skills folder of each supported agent tool at user and project level; using the steps by hand with the CLI when a tool has no skills support.

   Link it from `README.md` and `docs/cli.md`.

6. **Tests** (code over tokens: the skills can't drift from the CLI). Add `internal/cli/skills_test.go`, which reads `../../skills/*/SKILL.md`:
   - **Front matter:** valid YAML, `name` equal to the folder and matching `^[a-z0-9]+(-[a-z0-9]+)*$` with ≤ 64 characters, a non-empty `description` of ≤ 1024 characters, and no keys outside the Agent Skills set (`name, description, license, compatibility, metadata, allowed-tools`).
   - **Every command a skill names exists and its flags parse.** Take every line in a fenced block that starts with `ekumen `. Replace `<…>` placeholders with a dummy value, append `-h`, and run it in-process. Expect `Usage: ekumen <cmd> <sub>` on stdout or stderr, not an error. Also assert that every read command line has `--json`.
   - **Forbidden content:** no `.ekumen/events`, `registry.yaml`, `state.json` or `decisions/` path, and no agent tool or vendor name (case-insensitive list: claude, codex, gemini, opencode, cursor, copilot, anthropic, openai).
   - **Flow test on the harness fixture:**
     1. The YAML template from `skills/blocked/SKILL.md`, filled in, raises successfully as a session for its own task.
     2. A lead question with no `--to` reaches that session's `question list --for … --open`.
     3. The session answers it.
     4. `decision show --json` has the answer in `decision.questions[]`.
     5. The lead's answer, then `decision apply` as the loop, reports the task in `unblocked_tasks`.
   - Assert the JSON fields the skills tell the agent to read: `decision.status`, `decision.applied_on`, `decision.questions[].answer`, `items[].kind`, `brief.amendments`, `unblocked_tasks`.

**Touches:** `skills/blocked/SKILL.md`, `skills/decide/SKILL.md`, the embed package for `skills/`, `internal/cli` (the `skills` command and its tests, `internal/cli/skills_test.go`), `docs/skills.md`, `docs/cli.md` (the two commands and a link), `README.md` (one line). Target: one PR of about 600 changed lines or fewer; if the install command pushes it past that, stack it as a second PR. Don't change other CLI behaviour, the contract files or the spine. If a skill needs something the CLI lacks, see "Escalate if".

**Definition of done:**
- [ ] `skills/blocked/SKILL.md` and `skills/decide/SKILL.md` cover every step above, and each is about 150 lines or fewer.
- [ ] `internal/cli/skills_test.go` passes: front matter, command existence and flag parsing, `--json` on reads, forbidden content, and the end-to-end flow.
- [ ] A deliberately broken command line in a `SKILL.md` (e.g. `ekumen decision anwser`) fails the test, shown in the PR description.
- [ ] `ekumen skills list` and `skills install --dir` work from the built binary: golden `--json` tests for list and install, refusal without `--force` on a differing file, no-op on identical files, and a test that the embedded skills equal `skills/` on disk.
- [ ] `docs/skills.md` explains installing and use without skills. It is linked from `README.md` and `docs/cli.md`.
- [ ] The handoff carries the exact C-cli-json text for `skills list` and `skills install`.
- [ ] **Dogfooded on a scratch copy of the real `.ekumen/` (not the repo's).**
  - Load `/blocked` in a session and raise one decision.
  - Load `/decide` as the lead: show it, ask one question, answer it from the `/blocked` side, answer the decision, apply it.
  - Paste the transcript excerpt (commands and JSON only) into the handoff.
- [ ] `go test -race ./...`, vet and gofmt clean. CI is green on the draft PR. `scripts/ci-check.sh` passes.
- [ ] Handoff recorded with `ekumen task handoff T-skills`. Its `FRICTION` section lists every point where a skill had to make up for a missing CLI read or field. Expected candidates: finding the lead's handle, the placeholder task record, and session notification with no adapter.

**Out of scope:**
- The Claude Code adapter, forks, message delivery and `MessageQueued`
- The control loop (`T-reconcile`)
- `ekumen serve` and any UI
- New or changed CLI commands other than `skills list` and `skills install`
- Tool-specific plugin manifests or hooks
- Any other skill (handoff, reconcile, …)

**Escalate if** (raise with `/blocked`, which is this task's own first real use):
- A skill can't do its job without a CLI command, flag or field that doesn't exist. If it can't be worked around in the skill text, raise a decision (tier 2 if it changes C-cli-json); otherwise record it as FRICTION.
- The Agent Skills format conflicts with invariant 12 (keeping the body vendor-neutral).
- Keeping to ~600 lines would mean dropping a DoD item.

**Start prompt:**

    export EKUMEN_ACTOR=session:t-skills
    Read .ekumen/spine.md (§3, §5 rules 9–12, §6), then run `ekumen task show T-skills --json` (the brief, as amended).
    Read contracts C-cli-json, C-decision-record and C-event-log §3–§4, the draft C-adapter §3 (fallbacks only),
    and docs/cli.md. Work in .worktrees/t-skills on branch build/t-skills from main (the task record shows them empty;
    the brief's front matter is authoritative). Use `ekumen` for every .ekumen/ write; never edit those files by hand.
    Report with `ekumen task handoff T-skills`, including a FRICTION section. Raise anything above your tier with
    `ekumen decision raise` (or your own /blocked skill once it works).

**FRICTION reporting:** The handoff's `FRICTION` section covers:
- what was painful about doing this through `ekumen`
- anything ambiguous in C-cli-json, C-decision-record or the Agent Skills format
- every place the skill text had to make up for missing code

Each item names the command or field involved, so the orchestrator can turn it into a backlog task.
