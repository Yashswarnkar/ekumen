---
id: T-brief
title: "Brief authoring: /brief skill and ekumen brief new"
kind: build
contracts_consumed: [C-event-log, C-cli-json]
contracts_produced: []
blocked_by: []
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-brief · Brief authoring: `/brief` skill and `ekumen brief new`

**Goal:** v1 capability 3 (spine §7), GitHub **#42**. In the lead's workflow they ask an orchestrator session to write briefs. Today those briefs follow an informal format that lives only in the orchestrator's habits. Make brief writing a first-class part of Ekumen:
- **Code** does the mechanical parts: scaffolding, the task draft, and checking that required sections exist.
- **A skill** guides the judgment: scope, acceptance, what's out of scope, and the start prompt.

This follows spine rule 9: code over tokens.

**Context (verify against origin/main when you start):**
- Good examples of the de facto format: `.ekumen/briefs/T-inbox-v1/brief.md`, `T-start/brief.md` and `T-log-fixes/brief.md`. The sections are: front matter (id, title, kind, contracts consumed/produced, blocked_by, tier_grants); then **Goal**, **Context** (verified against main), **Job**, **Touches**, **Definition of done**, **Out of scope**, **Escalate if**, **Start prompt**.
- Drafting a task takes `ekumen task draft --file <t.yaml>` plus a hand-written `brief.md`. Approval hashes `brief.md` (`ekumen task approve`).
- Skills: `skills/blocked` and `skills/decide` are embedded in the binary (`skills/skills.go`) and installed with `ekumen skills install`. Follow their pattern exactly, including front matter and how they call `ekumen … --json`.
- Spine rule 12: the skill uses the cross-vendor `SKILL.md` format and only `ekumen` commands, with nothing specific to one agent tool.

**Job:**
1. **`ekumen brief new <id> --title t [--kind build|research|contract] [--blocked-by a,b] [--grants a,b] [--from-issue N]`**, orchestrator lane:
   - writes `.ekumen/briefs/<id>/brief.md` from a template: `.ekumen/templates/brief.md` if the project has one, otherwise a built-in template with the section headings above and one-line hints
   - records `TaskDrafted`, with branch, worktree and session defaulting from the id (`build/<id lower>`, `.worktrees/<id lower>`, `<id lower>`)
   - **`--from-issue N`** pre-fills the Goal with the issue's title, body and link, through `gh`. The observer already depends on `gh`; if it's missing, say so and continue without it.
   - refuses an id already in use
2. **`ekumen brief check <id>`** (also run by `ekumen check` for unapproved drafted tasks):
   - required sections are present and not left as template hints
   - the Start prompt sets an actor and names the handoff command
   - Definition of done has at least one checkbox
   - `blocked_by` ids exist

   It reports problems by section, and exits 2 when something is missing.
3. **The `/brief` skill** (`skills/brief/SKILL.md`, embedded and installable like the other two), for orchestrator sessions. It walks the orchestrator through:
   - reading the spine scope and the issue, and verifying context against main
   - running `ekumen brief new`, then filling each section, with guidance on what good looks like, taken from the examples
   - running `ekumen brief check`
   - telling the lead the task is ready for `ekumen task approve`

   The skill never approves tasks.
4. Add a `brief` row to the inbox's orchestrator actions: "Write the brief for X" already exists. Point it at `ekumen brief new` or the `/brief` skill.
5. Document it in `docs/cli.md` and `docs/skills.md`.

**Touches:** `internal/cli/` (new `brief.go`), `internal/core/` only if `brief check` needs a pure validator there, `skills/brief/`, `skills/skills.go`, the template, `docs/`, tests, your handoff files.

**Definition of done:**
- [ ] `ekumen brief new T-demo --title … --from-issue 40` on a temp project produces a draft task and a brief whose Goal links #40
- [ ] `ekumen brief check` passes on T-inbox-v1, T-start and this brief, and fails with clear messages on a template left unfilled
- [ ] `ekumen skills list` shows `brief`, and `skills install` writes it
- [ ] `go test -race ./...`, vet and gofmt clean; CI green (main is protected)
- [ ] Handoff via `ekumen task handoff T-brief`, including FRICTION

**Out of scope:** approving tasks; writing the brief's content with code; the web UI.

**Escalate if:** `brief check` running inside `ekumen check` would fail CI on existing records. Then it reports, but doesn't fail, for briefs approved before this task. Ask the lead in your terminal.

**Start prompt:**

    export EKUMEN_ACTOR=session:t-brief
    Read .ekumen/spine.md (§5 rules 9 and 12, §7, §8), then `bin/ekumen task show T-brief`, then GitHub issue #42,
    then skills/blocked/SKILL.md and skills/decide/SKILL.md (the pattern), then contract C-cli-json.
    Use `ekumen` for every .ekumen/ write. Report with `bin/ekumen task handoff T-brief`, push your branch, and open a draft PR
    titled "T-brief: …". Ask the lead in this terminal before anything outward-facing beyond that.
