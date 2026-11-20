---
id: R2
title: Landscape of agent-supervisor tools
contracts_consumed: []
contracts_produced: []
blocked_by: []
tier_grants: [read_web, clone_public_repos_to_scratch, write_own_output, push_own_branch, open_draft_pr]
---

# R2 · Landscape of agent-supervisor tools

## §A Brief (frozen on approval)

**Goal:** Learn how existing tools for supervising many coding agents solve the problems Ekumen shares with them (session status, "needs input" detection, launching and steering sessions, UI), so we reuse proven patterns. Also confirm what none of them do, so Ekumen's difference is real.

**Context (as of 2026-11-19):** a name search turned up dozens of projects in this space, most created in Nov 2026. Starting set:
- `dwmkerr/signalbox`: jump to any agent session across Cursor, Claude Code, OpenCode
- `quanticstudios/pitwall`: desktop terminal; see which agent is waiting; worktrees; phone approvals
- `chekrd/pitwall`: decision board for parallel Claude Code sessions (**the closest to us; read closely**)
- `leggetter/pitwall`: local web UI to watch, steer and start Claude Code sessions
- `aidarkazybekov/pitwall`: board across Claude Code, Codex, Cursor, Gemini CLI
- `g4m35/yardmaster`: many agents at once without collisions
- `ApostPap/yardmaster`: kanban, one agent thread per worktree, human merge
- `nickcox/docket`: records agent work: tasks, attempts, evidence, approvals
- `photostructure/gavel`: review changes with a local agent before commit

Add any others with real traction (stars or activity) that you find.

**Job:**
1. For each project: what it is, stack, license, activity, and stars.
2. Extract how it does each of these, with file or doc links:
   - session discovery and status
   - "needs input" detection (hooks? transcript parsing? process state?)
   - starting, resuming and messaging sessions
   - worktree handling
   - notifications
   - UI layout, and graph or board rendering
   - data model and where state is stored
3. Pay special attention to anything resembling **decisions, approvals, permission tiers, contracts or a task graph with dependencies**. How close does any project come to Ekumen's core (spine §1, §4, §5)?
4. Write a list of patterns to **adopt**, patterns to **avoid** (with why), and a **differentiation check**: what Ekumen does that none of them do, or a plain statement if one already does.
5. Read code and docs only. **Do not copy code** into Ekumen; note licenses so later tasks know what may be reused.

**Touches:** `docs/research/R2-landscape.md` only.

**Definition of done:**
- [ ] Comparison table covering every project in the starting set (plus additions)
- [ ] A "how they detect needs-input / status" section, per agent tool
- [ ] Adopt / avoid / differentiation lists
- [ ] Choices needing the lead drafted as decision records in the report
- [ ] Handoff written in §C, including FRICTION

**Out of scope:** building anything; contributing to these projects; designing Ekumen's contracts.

**Escalate if:** a project already does Ekumen's core (decisions as gates with background, tier grants, contracts) well enough that building it ourselves should be questioned. Raise that as a tier-2 decision immediately.

**Start prompt:**

    Read .ekumen/spine.md, then this brief (.ekumen/briefs/R2-landscape.md).
    Work only inside §A as amended by §B. Never edit the spine, contracts, §A or §B.
    Write your findings to docs/research/R2-landscape.md. Report only by appending to §C.
    Anything above your tier: draft it as a decision record in your report, then stop.

## §B Amendments (orchestrator; append-only, dated)
- 2026-11-20: approved by lead.
- 2026-11-20: output path changed from `docs/research/R2-landscape.md` to `.ekumen/research/R2-landscape.md` (D-011). Read every `docs/research/` in §A as `.ekumen/research/`.

## §C Handoff (task session; append-only, dated)

<!-- Format:
### YYYY-MM-DD
STATUS: …
OUTPUT: …
DECISIONS RAISED: …
FOLLOW-UPS: …
DEVIATIONS: …
FRICTION: what was painful by hand; what was ambiguous in the Ekumen format
-->

### 2026-11-20
STATUS: done; awaiting review (draft PR).
OUTPUT: `.ekumen/research/R2-landscape.md`: comparison of 9 starting-set + 8 added projects (stack, license, stars, activity, pinned commits); how each tool detects status and needs-input; adopt/avoid lists; differentiation check. Verdict: no project does Ekumen's core (decisions as gates on a task graph, the applied step, tier grants on a frozen brief, frozen contracts). The closest pieces are chekrd/pitwall (decision cards with background and forked side questions; no license) and nickcox/docket (permissions per objective, person-only actions). No §A escalation.
DECISIONS RAISED: two tier-2 drafts in report §6, for the orchestrator to number: R2-DR1 (Claude Code status and needs-input signal sources: hooks plus `claude agents --json`); R2-DR2 (raise via an MCP tool that only acknowledges, deliver via the session inbox, side questions in a `--fork-session` fork).
FOLLOW-UPS: (1) R1 or a spike should confirm against Claude Code docs: `claude agents --json --all`, cross-session messaging / `CLAUDE_CODE_MESSAGING_SOCKET`, the `PermissionRequest` hook. (2) Consider a notifier adapter that posts to signalbox or quanticstudios boards instead of building every view. (3) Event and decision schema contracts should adopt: a version field, seq + SSE since, first-answer-wins with typed refusals, answer separate from delivery, an `uncertain` effect state, brief revision counters. (4) chekrd/pitwall has no license, so later tasks must not reuse its code or text. claude-squad and opcode are AGPL.
DEVIATIONS: The report includes a drafted-decisions section (§6) although no project triggered the escalation, because §A's definition of done asks for "choices needing the lead". The slopus/happy clone failed to check out fully, so its row uses the README read through the GitHub API.
FRICTION: (a) The brief's start prompt and §A "Touches" still say `docs/research/`. The §B amendment fixes this, but each session has to merge it in its head. A rendered "§A as amended" view would help. (b) The decision-record id for a task-raised draft is undefined (who assigns D-0xx? collisions between parallel R1/R2). I used `R2-DRn` placeholders. (c) It is unclear whether drafts live only in the report or also as files in `.ekumen/decisions/` (outside my write set). The format needs a "proposed" location the task may write to. (d) Tier for adapter-shaping choices is ambiguous before any contract exists: "contract change" (tier 2) vs "in-contract" (tier 1). I chose tier 2. (e) There is no tool to check the §C entry against the format; the comment template is the only spec.
