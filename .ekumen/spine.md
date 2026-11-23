# Ekumen: spine

*The design and its reasons. Owned by the orchestrator, changed only with the lead's approval. History is in git; this file keeps no change log.*

**Precedence (the only rule of this kind):** merged code > contracts (`.ekumen/contracts/`) > this spine > older design docs (`VISION.md`, `SPEC.md`).

---

## 1. Purpose

Ekumen coordinates **one engineer (the lead) leading many coding-agent sessions** on one or more large projects. Agents do the work. Ekumen turns what they need from the lead into **decisions** with their background attached, and moves the work forward once the lead decides.

It solves four problems:

1. **Visibility.** Which sessions are waiting on the lead, and why.
2. **Lost background.** A decision needs background, but asking for it in the session that raised it derails that session.
3. **Idle waiting.** Work that depends on a decision waits for a human to notice the decision was made.
4. **Drift.** Status written by hand goes stale, and agents reason over stale state.

Most waiting is for **permission, not judgment**. Removing permission waits is as important as speeding up judgment.

## 2. Users and scope

- **Primary user:** a solo engineer running 5–20 sessions across one or more projects, using one or more agent tools (D-001).
- **Agent tools:** agent-agnostic through adapters (D-002). Support order: Claude Code and Codex CLI, then Gemini CLI and OpenCode, then Cursor and Copilot.
- **Not in scope yet:** several human approvers, hosted or multi-tenant service, enterprise governance. The format must not rule these out.

## 3. Domain

| Concept | What it is | Writer |
|---|---|---|
| **Project** | One body of work with its own `.ekumen/` folder in its repo | — |
| **Spine** | The design and its reasons (this file, for each project) | Orchestrator, with lead approval |
| **Contract** | An interface two or more tasks depend on. `draft → frozen`; a frozen contract changes only through a tier-2 decision. | Orchestrator drafts, lead freezes |
| **Task** | One unit of work with a brief, an owner session, a branch and a worktree | — |
| **Brief** | A task's whole world. §A scope (frozen on approval) · §B amendments (orchestrator, append-only) · §C handoff (task session, append-only) | See sections |
| **Decision** | A choice above a session's tier; a **gate** on the tasks it blocks | Session raises, lead answers |
| **External dependency** | Something owned outside the lead's control, with owner, asked-on and needed-by dates. It is a gate. | Orchestrator |
| **Session** | One running or resumable agent conversation, through an adapter | — |
| **Event** | One append-only fact; all state is derived from events | Ekumen CLI only |

**Task kinds:** `research` (output in `.ekumen/research/<id>-<slug>.md`, cited as evidence by the spine and decisions; research is the phase before a project, D-011) · `contract` · `build`.

**Task status is derived, never written:** `draft, approved, blocked, ready, running, in_review, done, deferred`. **Approved** means approved and unblocked, but missing what it needs to start (branch or worktree); **ready** means it can start now (D-task-status-approved).

**Decision lifecycle:** `raised → (questioned)* → decided | defaulted | superseded → applied`. **Applied** is the step that makes work move: amend affected briefs in §B, clear the blocks, notify live sessions, and let the dispatcher start tasks that are now ready.

**Decision priority** = tasks blocked transitively × urgency of the deadline. Computed in code.

**Roles and rules (D-roles-and-rules, C-roles).** A *role* is a reusable kind of agent work that a project declares: what the agent is told, what it may do, and where its output goes (e.g. reviewer, auditor, tester). A *rule* says when the control loop starts a role and what its verdict holds back (e.g. "every build PR gets a reviewer; merge waits for a pass"). Both are plain files the lead approves. Ekumen coordinates them and never does the work itself, so a project grows its workflow by adding roles, not by changing Ekumen.

## 4. Decision tiers

| Tier | Who decides | Mechanism |
|---|---|---|
| 0: pre-authorized | The session | `tier_grants` listed on the brief at approval (push own branch, open draft PR, reply to review, restack, a dev apply already shown as a dry run) |
| 1: in-contract choice | The session, with a veto window | Decision record with `default_if_no_answer`; applied automatically after `deadline` |
| 2: lead | The lead only | Decision record, no default; blocks until answered. Covers contract changes, production, outward actions, irreversible steps, releases. |

## 5. Invariants

1. **One owner per document; everyone else writes to their own lane.** Every document and record has exactly one owner role. Others never edit it. They append timestamped entries to their own lane (a handoff, a raised decision, a question), and the control loop routes entries that need action to the owner's inbox. The owner folds them into its documents. One live session holds a role at a time. This is the concurrency model: write conflicts become routing (C-event-log §3–§5).
2. **Freeze on approval.** No task starts until its brief is approved; after that §A never changes and §B amends it.
3. **Append-only.** §B, §C, decisions and events are never rewritten.
4. **Escalate, don't work around.** A session that thinks a contract is wrong raises a decision; it never redefines the contract locally.
5. **Detect and act are separate.** Auditors report, they never edit.
6. **Authoritative inputs only.** Truth is the remote default branch, PR merge state and deployed versions. Never a working tree or a ticket's status.
7. **Generated, not maintained.** Anything that summarizes other files is produced by code.
8. **External dependencies are gates** with an owner and dates.
9. **Code over tokens (D-007).** Anything deterministic (status, priority, unblocking, graph layout, queries, routing, validation) is software. Agents do judgment only: drafting context, recommending, answering follow-up questions, proposing spine edits. Agents read state through `ekumen … --json`, not by reasoning over raw files.
10. **Asking does not derail.** Questions about a decision go to a fork of the session that raised it. If the tool can't fork, they are queued to the original session as a side question.
11. **CLI first.** Everything can be done from the CLI and files; the UI is one view and never has a capability the CLI lacks.
12. **Vendor specifics live in adapters.** The core depends on capabilities, never on a particular agent tool, model provider or vendor feature. Prefer plain, portable mechanisms (files, git, CLI, stdio, HTTP) over tool-specific ones; use a vendor feature only inside its adapter and only with a fallback.

## 6. Architecture

- **Core (Go, D-003):** domain, state machines and derivations as pure functions over the event log.
- **Engine: one control loop (D-012).** Each pass compares *desired* state (spine, frozen contracts, approved briefs, decided decisions) with *observed* state (remote default branch, PR state, session status). It takes only pre-authorized actions: apply decisions, apply tier-1 defaults past their deadline, start ready tasks, message sessions. Anything else becomes a new decision. Every action is idempotent, so passes are safe to repeat or restart. Observations are re-read each pass and are not stored as events; only the loop's own actions are. **Audit** is a pass in dry-run mode; the **dispatcher** is the loop on a timer.
- **Event log:** the source of truth. Files in git, SQLite and Postgres are interchangeable stores; sync means exchanging events. Every record has a stable id and a version.
- **CLI:** `ekumen`, with every read command available as `--json`. Skills are thin `SKILL.md` wrappers around it.
- **Server and UI:** `ekumen serve` provides HTTP/JSON plus SSE and the React + TypeScript + Vite UI (D-004), built to static files embedded in the binary. Graph layout is computed by a layout library (ELK or dagre), not an agent.
- **Adapters (plugin points):** agent tools (operations: start, resume, send, fork, status; fallbacks when a tool lacks one), stores, notifiers, trackers.
- **Repo layout:** one repo; Go in `cmd/` and `internal/`, UI in `web/`.

**Agent layer (D-013 to D-018, from the ACP research R1 and the landscape research R2).** Three layers, and nothing tool-specific leaks upward:

1. **Core and the contracts C-event-log, C-decision-record, C-cli-json and C-http-api** know only generic concepts: session, status (`running`, `idle`, `needs_input`, `done`, `unknown`), deliver a message, fork, and *capabilities*. No tool or vendor name appears in them.
2. **C-adapter, the adapter interface**, defines operations (`list, status, start, resume, send, fork, events`), a capability set per adapter, and a fallback for each missing capability, chosen in code: e.g. no `send` to a running session → a queued side question. Sessions are either *managed* (Ekumen started them) or *observed* (the lead started them); observed sessions get whatever the adapter can see (D-016).
3. **Adapters** hold every tool-specific choice. A generic ACP adapter covers any agent that speaks ACP v1 (D-015, D-018). Native adapters add what ACP can't do, starting with **Claude Code** for v1: status from hooks plus `claude agents --json` (D-013); answers delivered on the session's input stream for managed sessions (D-014); it runs the lead's own installed `claude` under the lead's login and never handles credentials (D-017). Codex, Gemini CLI and OpenCode adapters follow the same interface.

A decision about one tool is recorded against its adapter, never written into the core.

- **Rules are evaluated by the control loop in code** (no tokens), and start roles through agent adapters. Role runs are ordinary tasks whose handoffs carry a verdict; gates hold a task's `done` until the verdict passes (C-roles).

## 7. v1 scope

**The first complete release (set by the lead, 2026-11-23).** v1 is done when the lead can run a project the way they work: one terminal per brief, many sessions at once, and one control plane that says what needs them. Tracked on GitHub under the **v1** milestone.

**Already built (v0.1):** the event log and file store; decisions (raise, ask, answer, apply); the task list with derived status and blockers; the `ekumen` CLI with `--json`; the `/blocked` and `/decide` skills; one pass of the control loop (`ekumen reconcile`); the static build map; `ekumen check` gating CI.

**v1 capabilities:**

| # | Capability | What v1 needs | Issues |
|---|---|---|---|
| 1 | **Inbox** | Honest ranking (unfinished work only, real paths) and plain output: what to do, why, what it unblocks, the command. Decisions raised on task branches are visible. Statuses aren't stale. | #37, #38, #39 |
| 2 | **Control loop** | Runs continuously (watch or timer), with a write lock so the loop and background sessions can write at once, and PRs observed once per pass | #41, #12 (lock), #15 (once per pass) |
| 3 | **Briefs written** | A `/brief` skill for the orchestrator, and `ekumen brief new` to scaffold and draft a task | #42 |
| 4 | **Environment setup** | `ekumen init` for a new project; `ekumen task start <id>` creates the worktree and branch, attaches the session and prints or launches the command | #40 |
| 5 | **Agents in the background** | The Claude Code adapter (after its safety fixes), then the loop starts ready tasks through it, in a visible terminal where possible, with a paste-a-command fallback | #32, #30, #31, #44 |
| 6 | **Roles and personas in the lifecycle** | C-roles frozen and built: role and rule files, rules evaluated by the loop, runs started through the adapter. First roles: reviewer and auditor. | #43 |
| 7 | **Several projects at once** | A project registry, `inbox --all` and `status --all`, and the loop per project | #45 |

**Order:** start 1, 3, 4 and the adapter fix-up (#32) in parallel. Then 2, then 6 and 5 (both need the loop, and 5 needs the adapter). 7 comes last, because it touches the whole CLI.

**Deferred to v1.1** (GitHub milestone **v1.1**):
- goals and `ekumen next` (#46)
- the web UI (#47)
- lane checks against history (#14)
- role-claim takeover (#13)
- crash-atomic writes (the rest of #12)
- cycle reporting (#48)
- a linear priority walk (#49)
- "main is broken" alerting (#50)
- CI noise (#51)
- the stale-binary warning (#52)
- multi-decision contract versions (#53)
- recurring audits until roles exist (#54)

**Out of v1:** database stores and sync, adapters other than Claude Code (ACP comes after), multiple approvers, a hosted page.

## 8. How this repo is built (dogfooding)

Ekumen is built using Ekumen, starting before the tool exists:

- **Roles:** the lead approves and decides; the orchestrator session owns the spine, contracts, briefs and `registry.yaml`; task sessions work only from their brief, each in its own worktree.
- **Every task session runs in a terminal the lead opens (2026-11-23).** The orchestrator prepares the worktree and the exact command, and never starts hidden worker sessions of its own. The lead can open, watch or talk to any session at any time; Ekumen (`ekumen inbox`, the map) is how the lead sees which ones need attention.
- **Manual first, then switch.** Until the CLI exists, the orchestrator maintains these files by hand in the documented format. As each piece ships, the manual step it replaces stops immediately, even if the piece is rough.
- **Friction is data.** Every §C handoff includes a `FRICTION` section listing what was painful by hand and what was ambiguous in the format. It feeds the backlog.
- **Self-hosting first.** Work that lets us *use* Ekumen on itself comes before everything else. The order that took v0.1 to self-hosting (done; v1 order is in §7):
  1. **Foundations:** T-core and T-file-store.
  2. **Self-hosting slice:** C-cli-json, then T-cli (decisions, questions, tasks, handoffs, status, inbox, graph, and `ekumen map`, a static build map) and T-import (today's hand-written records become events). **Switch point:** from here on, nobody edits `.ekumen/` by hand. Agents and the orchestrator use `ekumen`.
  3. **Moving work by itself:** T-skills (`/blocked`, `/decide`), then T-reconcile, at first observing only git and pull requests.
  4. **Sessions and the UI:** C-adapter → T-claude-adapter (session status joins the loop); C-http-api → T-server → T-graph-view and T-decision-views.
- **Use what exists.** Every brief written after a piece ships tells its session to use that piece instead of the manual way, and to report what was missing as FRICTION.

## 9. Open questions

- How a tier-1 choice proves it stayed inside its contract.
- How to measure value: time to decide, time blocked, hours spent reconciling.
