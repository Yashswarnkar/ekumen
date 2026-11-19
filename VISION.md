# Ekumen: vision

*Draft v0.1, 2026-11-19. Name: Ekumen (D-005), after Ursula K. Le Guin's federation that coordinates autonomous worlds without ruling them.*

## One line

**Coordination for one engineer leading many coding agents.** Agents do the work. Ekumen turns what they need from you into decisions with their background attached, and moves the work forward once you decide.

## Who it is for

A solo engineer running 5–20 coding-agent sessions on one project, in parallel worktrees, across one or more agent tools (Claude Code, Codex, Gemini CLI and others).

Not for now: teams with several human approvers, or enterprise governance. The format should not rule these out, but we do not build for them.

## What success looks like

1. **Daily use.** The author runs their own projects through it every day, starting with Ekumen itself.
2. **Open-source traction.** Other solo engineers adopt the format and the tool.
3. **A developer-experience showcase.** Fast, deterministic, a single binary, a UI you'd want to keep open.

## The core idea

When you run many agents, the human is the bottleneck, and most of that waiting is for **permission, not judgment**. Ekumen:

- **Removes permission waits** with tier grants: actions each task is pre-approved to take.
- **Turns real judgment calls into decisions:** gates in the task graph, each with its background attached, ranked by how much work it is holding up.
- **Moves work forward automatically** once a decision is applied: amend the briefs, clear the blocks, notify or start the sessions.

A decision moves through `raised → questioned* → decided → applied → tasks unblocked`. The step that makes work move is **applied**.

## Design principles

1. **Code over tokens.** Anything deterministic is software: status, priority, unblocking, graph layout, queries, routing, validation. Agents are only for judgment: drafting a decision's context, recommending an option, answering the lead's follow-up questions, proposing design edits. An agent reaches Ekumen state by calling `ekumen … --json`, never by reading and reasoning over raw files.
2. **Agent-agnostic.** Ekumen integrates with *agent tools*, not models, through an adapter interface. Skills are thin `SKILL.md` wrappers (the cross-vendor Agent Skills format) around CLI commands. A tool without skills can still call the CLI.
3. **Event log as truth.** Every change is an append-only event. Files, a database and the UI are all views built from it. Sync means exchanging events.
4. **Single writer per record.** Ownership replaces locking, and it makes sync conflicts rare and detectable.
5. **Gradual adoption.** Day one is `/blocked` and `/decide` plus the map. Contracts, briefs, tiers, the dispatcher and the auditor come in as a project grows.
6. **Dogfood.** Ekumen is built using Ekumen. Its own `.ekumen/` folder is the main example.

## Architecture

```
            ┌─────────── UI (React + TS, embedded) ───────────┐
            │ task graph · decision inbox · decision screen    │
            └───────────────▲──────────────────────────────────┘
                            │ HTTP/JSON + SSE  (ekumen serve)
  agents ──► ekumen CLI ──► core (Go) ──► event log ──► views (state, graph, inbox)
  (skills)                 │                 │
                           │                 ├── store: files/git | SQLite | Postgres
                           │                 └── sync
                           ├── agent adapters: claude-code | codex | gemini-cli | opencode
                           ├── notifiers: desktop | push | slack
                           └── trackers: github | linear
```

**Core domain:** Task, Decision, Contract, Brief, External dependency, Session. The state machines and derivations (status, priority = tasks blocked transitively × urgency of the deadline) are pure functions over the event log, so they are fully unit-testable.

**Agent adapter interface:**

| Operation | Purpose | Fallback when the tool lacks it |
|---|---|---|
| `start(task, prompt, worktree)` | Launch a session for an approved task | — (required) |
| `resume(session, msg)` | Continue a stopped session | — (required) |
| `send(session, msg)` | Deliver a message to a *running* session | Queue it; deliver on the next resume |
| `fork(session, msg)` | Side session with full background, original untouched | Send to the original as a side question |
| `status(session)` | running / idle / needs-input / done | Infer from the transcript and process |

**Asking a question about a decision:** a question from the decision screen goes to a **fork** of the session that raised the decision. The fork has the background and the original stays undisturbed. The answer is attached to the decision record as an event.

**Support order:** (1) Claude Code and Codex CLI, (2) Gemini CLI and OpenCode, (3) Cursor and Copilot.

## UI for v0.1

1. **Task graph.** Tasks as nodes, decisions as gate nodes on the edges, colored by derived status. The layout is computed in code (ELK or dagre), not by an agent.
2. **Decision inbox.** Open decisions ranked by priority; external dependencies with owner and dates.
3. **Decision screen.** Question, options and tradeoffs, recommendation, ≤150-word context, evidence links, Q&A thread (ask → fork), and the answer action. Answering triggers apply.

## Roadmap

| Version | Scope |
|---|---|
| v0.1 | Event log + file store; decisions (raise, ask, answer, apply); `ekumen` CLI with `--json`; `/blocked` and `/decide` skills; `ekumen serve` with graph, inbox and decision screen; Claude Code adapter |
| v0.2 | Task list and derived status; `/reconcile`; tier-1 defaults; external dependencies; Codex adapter |
| v0.3 | Dispatcher (auto-start ready tasks, notify sessions on decisions) |
| v0.4 | Hooks: write protection, append-only checks, fetch on start, PR spec validator, notifications |
| v0.5 | SQLite store + sync; read-only auditor |
| later | Postgres, hosted page, tracker adapters, more agent adapters, multiple approvers |

## Open

- how a tier-1 choice proves it stayed inside its contract · how to measure value (time to decide, time blocked).
