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

**Task status is derived, never written:** `draft, approved, blocked, ready, running, in_review, done, deferred`.

**Decision lifecycle:** `raised → (questioned)* → decided | defaulted | superseded → applied`. **Applied** is the step that makes work move: amend affected briefs in §B, clear the blocks, notify live sessions, and let the dispatcher start tasks that are now ready.

**Decision priority** = tasks blocked transitively × urgency of the deadline. Computed in code.

## 4. Decision tiers

| Tier | Who decides | Mechanism |
|---|---|---|
| 0: pre-authorized | The session | `tier_grants` listed on the brief at approval (push own branch, open draft PR, reply to review, restack, a dev apply already shown as a dry run) |
| 1: in-contract choice | The session, with a veto window | Decision record with `default_if_no_answer`; applied automatically after `deadline` |
| 2: lead | The lead only | Decision record, no default; blocks until answered. Covers contract changes, production, outward actions, irreversible steps, releases. |

## 5. Invariants

1. **Single writer per section.** Ownership replaces locking.
2. **Freeze on approval.** No task starts until its brief is approved; after that §A never changes and §B amends it.
3. **Append-only.** §B, §C, decisions and events are never rewritten.
4. **Escalate, don't work around.** A session that thinks a contract is wrong raises a decision; it never redefines the contract locally.
5. **Detect and act are separate.** Auditors report, they never edit.
6. **Authoritative inputs only.** Truth is the remote default branch, PR merge state and deployed versions. Never a working tree or a ticket's status.
7. **Generated, not maintained.** Anything that summarizes other files is produced by code.
8. **External dependencies are gates** with an owner and dates.
9. **Code over tokens (D-007).** Anything deterministic (status, priority, unblocking, graph layout, queries, routing, validation) is software. Agents do judgment only: drafting context, recommending, answering follow-up questions, proposing spine edits. Agents read state through `ekumen … --json`, not by reasoning over raw files.
10. **Asking does not derail.** Questions about a decision go to a fork of the session that raised it. If the tool can't fork, they are queued to the original session as a side question.

## 6. Architecture

- **Core (Go, D-003):** domain, state machines and derivations as pure functions over the event log.
- **Event log:** the source of truth. Files in git, SQLite and Postgres are interchangeable stores; sync means exchanging events. Every record has a stable id and a version.
- **CLI:** `ekumen`, with every read command available as `--json`. Skills are thin `SKILL.md` wrappers around it.
- **Server and UI:** `ekumen serve` provides HTTP/JSON plus SSE and the React + TypeScript + Vite UI (D-004), built to static files embedded in the binary. Graph layout is computed by a layout library (ELK or dagre), not an agent.
- **Adapters (plugin points):** agent tools (operations: start, resume, send, fork, status; fallbacks when a tool lacks one), stores, notifiers, trackers.
- **Repo layout:** one repo; Go in `cmd/` and `internal/`, UI in `web/`.

The agent adapter design is open until R1 (Agent Client Protocol research) reports.

## 7. v0.1 scope

**In:**
- Event log and file store
- Decisions: raise, ask, answer, apply
- Task list with derived status and blocks
- `ekumen` CLI with `--json`
- `/blocked` and `/decide` skills
- `ekumen serve` with three views: **task graph** (decisions as gates), **decision inbox** (ranked), **decision screen** (context, evidence, Q&A, answer) (D-006)
- Claude Code adapter

**Out:** dispatcher, hooks, auditor, database stores, sync, other adapters, multiple approvers, hosted page.

## 8. How this repo is built (dogfooding)

Ekumen is built using Ekumen, starting before the tool exists:

- **Roles:** the lead approves and decides; the orchestrator session owns the spine, contracts, briefs and `registry.yaml`; task sessions work only from their brief, each in its own worktree.
- **Manual first, then switch.** Until the CLI exists, the orchestrator maintains these files by hand in the documented format. As each piece ships, the manual step it replaces stops immediately, even if the piece is rough.
- **Friction is data.** Every §C handoff includes a `FRICTION` section listing what was painful by hand and what was ambiguous in the format. It feeds the backlog.
- **Order for v0.1:** research (R1, R2) → contracts C1–C5, frozen → build tasks → switch to the tool.

## 9. Open questions

- Agent adapter shape and whether ACP is the base (R1).
- What session status and "needs input" signals are reliable for each tool (R1, R2).
- How a tier-1 choice proves it stayed inside its contract.
- How to measure value: time to decide, time blocked, hours spent reconciling.
