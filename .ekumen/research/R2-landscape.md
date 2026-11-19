# R2 · Landscape of agent-supervisor tools

*Task R2 research output (D-011). Written 2026-11-20 from code and docs read at the commits pinned below. Nothing was run and no code was copied into Ekumen.*

## 0. Verdict first

**No project does Ekumen's core.** No tool makes a decision a gate on a task graph, with background attached, answered once and then applied: briefs amended, blocks cleared, dependent work dispatched. None has tier grants on a brief or frozen contracts that only a decision can change. So **no escalation under §A "Escalate if"**, and no tier-2 decision questions building Ekumen.

The parts do exist, spread across several projects. Ekumen should borrow them, not reinvent them:

| Ekumen core idea | Closest project | How close | What it lacks |
|---|---|---|---|
| Decisions with background (spine §1 problem 2) | **chekrd/pitwall** | Very close for one session: a decision card restated in plain words, an "Elaborate" read-only run, a second opinion from a fresh model, LLM ranking, and "Ask on the side" in a forked session (our invariant 10) | Decisions belong to one session's question. They are not gates on other tasks, have no tiers or defaults, and leave no durable record or applied step. **No license (all rights reserved)** |
| Tier grants and authority (spine §4) | **nickcox/docket** | Close in spirit: a fixed list of permissions for each objective, person-only actions, trusted caller binding, "answers are guidance, never new authority" | No tier-1 veto window or default-on-deadline. No decision objects with options. Supervision is by an LLM, and the host must be bb. 0 stars, spec stage |
| Task graph with dependencies | g4m35/yardmaster, ApostPap/yardmaster, docket | Task DAGs (`--after`, `dependsOn`, `deps` table) with readiness derived in code | Edges run only from task to task. Nothing models a decision or an external dependency as a gate node |
| Approvals as policy | g4m35/yardmaster | `policies[].set.requireApproval` matched by files or wording | A boolean hold on acceptance, with no background or answer record |
| Contracts | none | — | No project has frozen interface contracts between tasks. (docket's "contracts" are its own CLI and config schemas) |
| Event log as truth | dwmkerr/signalbox, quanticstudios/pitwall | `events.jsonl` replayed into state, SSE `?since=seq`; quanticstudios uses an event → delta → snapshot reducer | Used for session status only, not coordination state |

**Differentiation holds.** The landscape is crowded with "which session needs me" boards. It has no "what decision is the bottleneck for the project, and what moves once I answer it". That gap is decision-gated planning with tiers and contracts across many sessions.

## 1. Projects compared

Stars, dates and licenses come from `gh api repos/<repo>` on 2026-11-20. "Active" is the last push.

### Starting set

| Project | What it is | Stack | License | Stars | Created → last push | Pinned commit |
|---|---|---|---|---|---|---|
| [dwmkerr/signalbox](https://github.com/dwmkerr/signalbox) | Hook-fed hub plus menu-bar/jumplist/iOS views; jump to any agent session | TS (Bun) CLI/hub, Swift app, iOS | MIT | 22 | 2026-07-15 → 2026-11-15 | `3c059e0` |
| [quanticstudios/pitwall](https://github.com/quanticstudios/pitwall) | Desktop terminal with a daemon; sidebar/inbox/board of agent states; answer approvals from a row or phone; worker fleets | Go (daemon, Gio GUI), Android app | MIT | 22 | 2026-10-02 → 2026-11-19 | `713abd7` |
| [chekrd/pitwall](https://github.com/chekrd/pitwall) | **Decision board** for parallel Claude Code sessions: queue of decision cards, Wall, task pages | TS: Node server, React/Vite, SQLite, tmux, Ghostty | **none** | 0 | 2026-10-24 → 2026-11-18 | `f404975` |
| [leggetter/pitwall](https://github.com/leggetter/pitwall) | Local web UI listing every Claude Code session; recaps; a supervisor that proposes messages you approve | TS: Node, Hono, ws, React/Vite, node-pty, xterm.js | Apache-2.0 | 0 | 2026-10-16 → 2026-11-18 | `8038145` |
| [aidarkazybekov/pitwall](https://github.com/aidarkazybekov/pitwall) | Native Mac kanban: your asks to 5 agents become tickets beside planned tasks; worklog | Swift 6 | MIT | 0 | 2026-11-13 → 2026-11-18 | `0cf99ae` |
| [g4m35/yardmaster](https://github.com/g4m35/yardmaster) | Hub that queues tasks into worktrees with file reservations, checks, cross-model review, budgets, policies, merge trains | TS (Node 22), SQLite, MCP | MIT | 0 | 2026-11-03 → 2026-11-17 | `c8f1260` |
| [ApostPap/yardmaster](https://github.com/ApostPap/yardmaster) | Kanban companion for T3 Code: plan → tasks with deps → threads in worktrees → AI review → human merge | TS: Node, Drizzle, React | MIT | 14 | 2026-11-13 → 2026-11-13 | `7d2b010` |
| [nickcox/docket](https://github.com/nickcox/docket) | CLI that records tasks, attempts, evidence, approvals; enforces authority; host = bb | TS (Node), SQLite | MIT | 0 | 2026-11-08 → 2026-11-19 | `f93c30b` |
| [photostructure/gavel](https://github.com/photostructure/gavel) | Local diff review with the agent that wrote it; verdict gates the commit | TS (Node, Vite) | MIT | 14 | 2026-11-07 → 2026-11-18 | `210adee` |

### Added for traction

| Project | What it is | Stack | License | Stars | Created → last push |
|---|---|---|---|---|---|
| [smtg-ai/claude-squad](https://github.com/smtg-ai/claude-squad) | TUI managing many agents in tmux + worktrees | Go | **AGPL-3.0** | 8,837 | 2025-03 → 2026-11-13 |
| [BloopAI/vibe-kanban](https://github.com/BloopAI/vibe-kanban) | Kanban issues → agent workspaces, diff review, PRs. **Announced sunsetting** (README) | Rust + TS | Apache-2.0 | 28,749 | 2025-06 → 2026-11-15 |
| [kbwo/ccmanager](https://github.com/kbwo/ccmanager) | Session manager for 8+ CLIs; per-tool screen state detectors; LLM auto-approval | TS (Ink) | MIT | 1,279 | 2025-06 → 2026-10-27 |
| [asheshgoplani/agent-deck](https://github.com/asheshgoplani/agent-deck) | tmux TUI; hooks; "conductor" LLM session that answers routine asks and escalates others to phone | Go | MIT | 1,279 | 2025-12 → 2026-11-19 |
| [agent-of-empires/agent-of-empires](https://github.com/agent-of-empires/agent-of-empires) | TUI + web dashboard; tmux, worktrees, containers | Rust | MIT | 4,609 | 2026-01 → 2026-11-19 |
| [humanlayer/humanlayer](https://github.com/humanlayer/humanlayer) | Team platform: daemon runs agents; research → design → plan → implement phases with human approval | TS (Bun), Postgres/Electric, AWS | Apache-2.0 | 12,918 | 2024-08 → 2026-11-16 |
| [coder/agentapi](https://github.com/coder/agentapi) | HTTP API over a terminal agent: `/status` stable/running, `/message`, SSE `/events` | Go | MIT | 1,494 | 2025-04 → 2026-09-13 |
| [slopus/happy](https://github.com/slopus/happy) | Mobile/desktop client, E2E-encrypted relay, multiplayer sessions | TS/Swift | MIT | 24,712 | 2025-07 → 2026-11-19 |

Noted, not examined: [pingdotgg/t3code](https://github.com/pingdotgg/t3code) (66k stars; ApostPap/yardmaster's host), [winfunc/opcode](https://github.com/winfunc/opcode) (AGPL-3.0, 22k), [stravu/crystal](https://github.com/stravu/crystal) (now Nimbalyst, last push Feb 2026), [imbue-ai/sculptor](https://github.com/imbue-ai/sculptor).

**Reuse by license:** MIT and Apache-2.0 ideas and specs may be cited freely. Code reuse would need attribution and a decision. **chekrd/pitwall has no license, so nothing from it may be reused, not even text.** Learn from its behavior only. claude-squad and opcode are AGPL-3.0, so copy nothing from them.

## 2. How each one works

Links use `https://github.com/<repo>/blob/<commit>/<path>` at the commits above (short SHAs resolve on GitHub).

### chekrd/pitwall: the closest one

- **Discovery and status:** reads Claude Code's undocumented `~/.claude/sessions/<pid>.json` for live status (working, your turn, permission prompt) and the JSONL transcripts. It polls every 1.5 s, and hooks (`SessionStart`, `UserPromptSubmit`, `Stop`, `Notification`) only post to trigger an immediate sync ([README](https://github.com/chekrd/pitwall/blob/f404975/README.md), [server/ingest.ts](https://github.com/chekrd/pitwall/blob/f404975/server/ingest.ts), [scripts/claude.ts](https://github.com/chekrd/pitwall/blob/f404975/scripts/claude.ts)). It pins **fixtures for each Claude Code version** (`server/fixtures/claude-2.1.314/`) and shows a drift banner when a field it relies on disappears.
- **Needs input:** comes from four sources, all merged into one "open decision": the `ask_human` / `ask_questions` MCP tools (user-scope MCP at `127.0.0.1:7420/mcp`; the tool only acknowledges, and the board reads the call from the transcript), a `PreToolUse` hook on `AskUserQuestion` that posts and never answers, a question at the end of a turn detected by a Sonnet summary, and "Needs focus", a terminal-only prompt ([server/mcp.ts](https://github.com/chekrd/pitwall/blob/f404975/server/mcp.ts), [server/decision.ts](https://github.com/chekrd/pitwall/blob/f404975/server/decision.ts)).
- **Steering:** sessions run in detached tmux on their own socket. Answers are typed via `paste-buffer`/`send-keys`. Before each paste and before Enter it re-checks that the same process, conversation and screen are still there. Every send is held 5 s with Undo. The **first answer wins**, and a stale page is refused with codes `stale|answered|out-of-date|advice-changed` (`Refusal` in decision.ts).
- **Decision background:** a briefing per task (summary, "Worth knowing") and **Elaborate**, a read-only `claude -p` with Read/Grep/Glob in the worktree that is stored once per decision. A **second opinion** comes from a fresh Opus with no conversation, which writes only to a scratch APFS clone, and an MCP guard blocks write tools ([server/mcpGuard.ts](https://github.com/chekrd/pitwall/blob/f404975/server/mcpGuard.ts)). **Ask on the side** answers in `claude -p --resume <id> --fork-session` read-only, so the task never sees the question. That is spine invariant 10, built.
- **Priority:** LLM-ranked by `claude -p --model sonnet` against an editable rubric ([config/priority-rubric.md](https://github.com/chekrd/pitwall/blob/f404975/config/priority-rubric.md)). "A question that blocks more work ranks higher" is a judgment by the model, since there is no graph.
- **Worktrees:** `scripts/git-wt`. It fetches origin's default branch first and refuses to start from stale state. It copies `.worktreeinclude`, and tasks can span a folder of repos via an `open_worktree` MCP tool.
- **UI:** Queue (one card at a time, keyboard-first), Wall (tiles), task page (Overview/Conversation/Diff/Artifacts). Subtasks come from "findings".
- **State:** SQLite `~/Library/Application Support/pitwall/state.db` (repos, sessions, schedules, ux_events) ([server/migrations.ts](https://github.com/chekrd/pitwall/blob/f404975/server/migrations.ts)). A UX log measures **decision wait time and how decisions end** ([scripts/ux/](https://github.com/chekrd/pitwall/tree/f404975/scripts/ux)), which matches our open question on measuring value.

### leggetter/pitwall

- **Discovery:** `claude agents --json --all` every 5 s, documented and about 0.2 s for 20 sessions. Ended sessions come from transcripts ([docs/decisions.md D1, D17](https://github.com/leggetter/pitwall/blob/8038145/docs/decisions.md)).
- **Needs input:** five ordered reasons, computed in the index and not the UI ([D18](https://github.com/leggetter/pitwall/blob/8038145/docs/decisions.md), [docs/states.md](https://github.com/leggetter/pitwall/blob/8038145/docs/states.md), `src/shared/needsYou.ts`):
  1. A prompt on screen (`waiting`).
  2. A background job's `needs` line in `~/.claude/jobs/<id>/state.json`.
  3. The last turn asked you something.
  4. You resumed it and have sent no prompt since.
  5. Blocked but unreadable, so it fails toward showing the question.

  A recap may **add** a session to Needs you, never remove it. A PR needing attention also counts.
- **Messaging:** via Claude Code's cross-session messaging through a headless relay session (D2, D8). The receiver sees "not typed by your user", so **a relayed message cannot approve a permission prompt**. Background sessions run in a PTY with `claude attach <id>` (D3).
- **Supervisor:** a `claude -p` stream-json session with only read-only MCP tools. `spawn_session`, `resume_session` and `SendMessage` sit under `ask` rules with `--permission-prompt-tool mcp__pitwall__approve`, so **every proposal waits as a card for a click**. The checks run again at send time, and a one-time pass is keyed on the exact input (D4).
- **Process:** a numbered decision log with lead, date, reasoning and rejected options (D1–D28) plus `PLAN.md` for open items. This is the same practice as Ekumen's `.ekumen/decisions`, kept by hand.

### quanticstudios/pitwall

- **Status:** hooks installed for 7 agents with backup, dry-run and no duplicates ([docs/hooks.md](https://github.com/quanticstudios/pitwall/blob/713abd7/docs/hooks.md)). Amp and Aider have no hooks, so their screens are read. States are Working, Background, Input, Approval, Plan, PR open, Done, Error ([docs/usage.md#watching-agents](https://github.com/quanticstudios/pitwall/blob/713abd7/docs/usage.md)).
- **Answering:** Allow/Deny/Always from the sidebar row. A button presses only if the screen still offers it and still shows the command the hook reported. Anything else is refused.
- **Managed sessions:** Claude and Codex drivers, an ACP v1 driver ([docs/acp.md](https://github.com/quanticstudios/pitwall/blob/713abd7/docs/acp.md)), and **driver Event → reducer → Delta(seq) → eventlog + Snapshot**. Answers are first-wins with `conflict`, and "pitwall never answers for the user" ([docs/managed-sessions.md](https://github.com/quanticstudios/pitwall/blob/713abd7/docs/managed-sessions.md), sections "Event model" and "Who answers approvals").
- **Fleet:** `pitwall worktree new <name> -- claude "…"`, with `pitwall wait --children --until done` for an orchestrator agent.
- **State:** `~/.local/state/pitwall/state.json` with rolling snapshots. A `decisions.jsonl` existed in older versions and was dropped ([docs/state.md](https://github.com/quanticstudios/pitwall/blob/713abd7/docs/state.md)).
- **Notifications:** desktop notifications plus a ring on the pane. Ctrl+Shift+U jumps to the most recent waiter. The phone pairs by QR code over your own network.

### dwmkerr/signalbox

- **Data model:** the cleanest event spec in the set ([components/specs/events.md](https://github.com/dwmkerr/signalbox/blob/3c059e0/components/specs/events.md)). A versioned event (`v`, `id`, `ts`, `machine`, `agent`, `event` ∈ busy/attention/done/error/ended, `reason`) is appended to `events.jsonl`. State is rebuilt by a reducer on boot. `GET /stream?since=N` is SSE replay-then-live. User actions (seen, pin, hide) are events too.
- **Needs input mapping:** set per tool in [specs/adapters.md](https://github.com/dwmkerr/signalbox/blob/3c059e0/components/specs/adapters.md), with dedup rules ("an enriched ask is not clobbered by its bare twin").
- **Install:** `signalbox init` edits agent configs with consent, a backup and an exact reversal. `signalbox fire` lets any script or CI post an event.

### aidarkazybekov/pitwall

- **Status:** pure transcript and history parsing per tool, with no hooks ([Sources/Pitwall/Ingest/](https://github.com/aidarkazybekov/pitwall/tree/0cf99ae/Sources/Pitwall/Ingest)). Codex: `request_user_input`/`exec_approval_request`/`apply_patch_approval_request` events in `CodexReader.swift`. Cursor: tool status `pending|awaiting_approval` or `hasBlockingPendingActions` in `CursorReader.swift`. Gemini: `~/.gemini/tmp/<project>/chats/`. Claude: `AskUserQuestion`/`ExitPlanMode` waiting for a result in `TranscriptParser.swift`. Fallback: the reply ends with "?".
- **Tasks:** stored either locally or as **markdown files with front matter in `backlog/` in the repo** (Backlog.md layout). Mentioning `T-015` in a prompt links that session.

### g4m35/yardmaster

- **Data model:** SQLite `tasks`, `deps(task_id, depends_on)`, `locks` (file-pattern leases), `runs`, `events`, `messages` ([src/db.ts](https://github.com/g4m35/yardmaster/blob/c8f1260/src/db.ts)). The critical path starts first, and an import graph serializes coupled tasks.
- **Approvals:** `policies` matched by files or text set `requireApproval`, tier, review and guidance ([README](https://github.com/g4m35/yardmaster/blob/c8f1260/README.md), [src/policies.ts](https://github.com/g4m35/yardmaster/blob/c8f1260/src/policies.ts)). `ym approve <id>`.
- **Needs input:** detected by code, not tokens. A watchdog flags the last text matching a question regex plus no tool since plus idle more than N minutes as `awaiting_input`, along with loops, repeated errors and stalls ([src/watchdog.ts](https://github.com/g4m35/yardmaster/blob/c8f1260/src/watchdog.ts)). Every state change is tied to an attempt, so a stale run can't overwrite it.
- **In-agent:** MCP tools (`yard_add_tasks`, `yard_wait`, `yard_claim_files`, `yard_control`) and hooks that brief at session start.

### ApostPap/yardmaster

- **Columns:** derived by one pure function from stored stages, with `attention` overriding them. One transition table is the only place stages change. A reconciler polls `t3 thread show --json` and sets attention when `status == waiting || pendingRequestCount > 0` ([SPEC.md §6, §8](https://github.com/ApostPap/yardmaster/blob/7d2b010/SPEC.md)). This is a direct precedent for spine invariant 9 and D-012.
- **Limits:** it never runs agents itself, and it cannot answer questions (you jump to T3). The planner proposes tasks with `dependsOn`, and a human accepts the plan first.

### nickcox/docket

- **Specification:** normative, R01–R40 with acceptance scenarios A01–A60 ([docs/specification/](https://github.com/nickcox/docket/tree/f93c30b/docs/specification)). Seven tables: objectives, tasks, dependencies, attempts, events, requests, claims ([schema.sql](https://github.com/nickcox/docket/blob/f93c30b/docs/specification/schema.sql)).
- **Authority (R17, R18):** the caller comes from trusted host code. JSON input never carries a caller, a permission or an approval. Each objective has a fixed list of permissions (`dispatch`, `answerQuestions`, `acceptOutput`, `integrate`…), and some actions are person-only ([domain.md](https://github.com/nickcox/docket/blob/f93c30b/docs/specification/domain.md)).
- **Effects (R09):** each effect is pending → claimed → succeeded | rejected | withdrawn | **uncertain** ("inspect it, never repeat it").
- **Revisions (R03):** a `definitionRevision` and a `decisionRevision`. Evidence for an old definition never satisfies a newer one, which is the same idea as "frozen §A, amended only via §B".
- **Questions (R21, R22):** questions are durable, and answers are recorded once and delivered separately. "Recording an answer does not prove it was delivered". Notifications are ordered at-least-once with handled acknowledgements.

### photostructure/gavel

- **Review flow:** the agent writes a "Why this change" brief, and the human gives each part a verdict that **gates the commit**.
- **Feedback:** sent to the owning Claude session through `CLAUDE_CODE_MESSAGING_SOCKET` + `CLAUDE_CODE_SESSION_ID` (the session's inbox), or `codex queue` for Codex. The skill forbids the agent from polling or waiting, and says "the notification itself is not approval; read a fresh snapshot" ([skills/gavel/SKILL.md](https://github.com/photostructure/gavel/blob/210adee/skills/gavel/SKILL.md), "Wait for feedback").

### The additions, briefly

- **claude-squad:** tmux `capture-pane`, a hash change means busy, and a `strings.Contains` check for the permission text means a prompt ([session/tmux/tmux.go `HasUpdated`](https://github.com/smtg-ai/claude-squad/blob/070eb93/session/tmux/tmux.go)). It has an `--autoyes` flag. State is a JSON list of instances.
- **ccmanager:** screen-regex detectors per tool with a 1.5 s idle debounce ([src/services/stateDetector/](https://github.com/kbwo/ccmanager/tree/b0948dd/src/services/stateDetector)). An **LLM auto-approval verifier** decides whether a prompt is safe ([autoApprovalVerifier.ts](https://github.com/kbwo/ccmanager/blob/b0948dd/src/services/autoApprovalVerifier.ts)).
- **vibe-kanban:** runs Claude via stream-json `control_request`/`can_use_tool` and routes tool approvals and questions to its own approval service ([crates/executors/src/executors/claude/protocol.rs](https://github.com/BloopAI/vibe-kanban/blob/3f095f0/crates/executors/src/executors/claude/protocol.rs), [crates/executors/src/approvals.rs](https://github.com/BloopAI/vibe-kanban/blob/3f095f0/crates/executors/src/approvals.rs)). It has an ACP executor and is sunsetting.
- **agent-deck:** hooks plus polling, with Running, Waiting, Idle and Error states, and the tmux status bar jumps to a waiting session. A **conductor** LLM session auto-answers when confident and escalates to Telegram or Slack ([docs/guide/conductor.md](https://github.com/asheshgoplani/agent-deck/blob/80494e5/docs/guide/conductor.md)).
- **agent-of-empires:** tmux, worktrees, optional containers, and a web dashboard for the phone.
- **agentapi:** a terminal emulator diffs the screen to decide `stable` or `running`, behind an HTTP + SSE API ([README](https://github.com/coder/agentapi/blob/7c468d5/README.md), `lib/screentracker`).
- **humanlayer:** phases (research → design/PRD/TDD → structure outline → implement), each approved by a person: "the agent can propose a choice, but the user owns the decision" ([apps/docs/explanation/workflow-phases.md](https://github.com/humanlayer/humanlayer/blob/2f5b58d/apps/docs/explanation/workflow-phases.md)). A task groups sessions and artifacts. It is hosted and built for teams.
- **happy:** a wrapper CLI with an E2E relay to the phone; multiplayer. Not a decision tool.

## 3. How they detect needs-input and status, per agent tool

Ranked by reliability: **structured hook or protocol** > **documented CLI listing** > **transcript parsing** > **screen scraping**.

| Agent tool | Best signal seen | Who uses it | Fallbacks seen |
|---|---|---|---|
| **Claude Code** | Hooks: `PermissionRequest` (the real ask in `tool_name`/`tool_input`), `Notification` (permission or idle; type often missing), `PreToolUse` matcher `AskUserQuestion`, `Stop`, `SessionStart`, `UserPromptSubmit`. In-process alternative: stream-json `control_request can_use_tool` / `--permission-prompt-tool` for sessions Ekumen launches | signalbox, quanticstudios, chekrd, agent-deck; vibe-kanban and leggetter (supervisor) for stream-json | `claude agents --json --all` (documented; `waiting` = prompt on screen) — leggetter. `~/.claude/sessions/<pid>.json` (undocumented) — chekrd. `~/.claude/jobs/<id>/state.json` `needs` line (internal) — leggetter. Transcript tail (`AskUserQuestion` without a result, a turn ending with a question) — chekrd, aidarkazybekov. Screen text "No, and tell Claude what to do differently" — claude-squad, ccmanager |
| **Codex CLI** | `~/.codex/hooks.json` `PermissionRequest` (needs `[features] hooks = true`; Codex asks for `/hooks` trust once) | signalbox, quanticstudios | Session log events `request_user_input`, `exec_approval_request`, `apply_patch_approval_request` — aidarkazybekov. Legacy `notify` (turn complete only). Screen regex — ccmanager |
| **Gemini CLI** | Hooks in `~/.gemini/settings.json` (a settings file with comments is skipped) | quanticstudios | `~/.gemini/tmp/<project>/chats/*.json` — aidarkazybekov. Screen "Yes, allow once" — claude-squad |
| **OpenCode** | Plugin events `permission.asked`/`permission.updated`, `session.idle`, `session.status` | signalbox, quanticstudios (v2 needs a TUI plugin to map windows to panes) | Screen regex — ccmanager |
| **Cursor (CLI/agent)** | `~/.cursor/hooks.json` `beforeShellExecution` / `beforeMCPExecution` (only blocking signals) | signalbox, quanticstudios | Cursor's store: tool `status in {pending, awaiting_approval}`, `hasBlockingPendingActions` — aidarkazybekov |
| **Copilot CLI** | No hook path found in the set | — | Screen regex ("confirm with … enter", "│ do you want", "esc to cancel") — ccmanager `github-copilot.ts` |
| **Any ACP agent** | `session/request_permission` in the protocol | quanticstudios (ACP v1 driver), vibe-kanban, happy | — (R1 covers ACP) |
| **Tool-agnostic** | Code heuristic: a question-shaped last message, no tool call since, idle > N min | g4m35 watchdog | An LLM recap saying "waiting on you" (leggetter; it may only add) |

**Lessons:**

1. Every serious tool moved from screen scraping to hooks. Scraping survives only for tools that have no hooks.
2. One blocked dialog can fire two hooks: dedupe, and keep the richer one (signalbox).
3. Missing data must **fail toward "needs you"** (leggetter reason 5, signalbox's attention default).
4. Undocumented files drift: pin fixtures for each version and show a drift banner (chekrd).
5. A message from another session **cannot approve a permission prompt** in Claude Code (leggetter D2). That is good for Ekumen, because a decision answer is guidance and never a covert approval.

## 4. Adopt / Avoid

### Adopt

1. **A hook-first adapter with a capability table for each tool.** Install hooks with backup, dry-run, no duplicates and an exact reversal (quanticstudios `hooks install`, signalbox `init`). Record for each tool which of start/resume/send/fork/status/needs-input it supports, and its fallback. This fits spine §6 "fallbacks when a tool lacks one".
2. **An event spec with a version field and a reducer** (signalbox events.md, quanticstudios Event→Delta→Snapshot). Ekumen already plans an event log. Copy the ideas: `seq` per store, SSE `?since=seq` replay-then-live, user actions as events, an explicit dedup/no-clobber rule.
3. **Derive needs-you in one shared function with ordered reasons, failing toward attention** (leggetter `needsYou.ts`/states.md; ApostPap's pure column function). The CLI `--json` and the UI read the same derivation (invariants 9 and 11).
4. **First answer wins, with typed refusals** (`stale`, `answered`, `out-of-date`) and a re-check of the target just before acting (chekrd decision.ts, quanticstudios "Who answers approvals"). Ekumen's `decide` must refuse an answer to a superseded decision version.
5. **Answering is separate from delivery** (docket R21/R22). `decided` is recorded once. Delivery to a session is its own effect with pending, delivered and uncertain states. This maps to Ekumen's `decided → applied`.
6. **Effect requests with an `uncertain` state** for outward actions such as push, PR and message (docket R09). "Inspect, never repeat" is the right default for the reconcile loop in D-012.
7. **Revision counters on briefs** (docket R03 `definitionRevision`): an attempt records the brief revision it started from, so a §B amendment marks running work stale and does not silently apply.
8. **Fork for side questions:** `claude -p --resume <id> --fork-session`, read-only tools, the fork transcript deleted on close (chekrd "Ask on the side"). This shows invariant 10 can be built on Claude Code today.
9. **Deliver answers through the session inbox** (`CLAUDE_CODE_MESSAGING_SOCKET`/`SESSION_ID`, as gavel and leggetter do; `codex queue` for Codex). Avoid tmux keystroke injection where possible. The skill tells the agent to end its turn, not to poll.
10. **A raise-decision MCP tool that only acknowledges**, read back from the transcript or event (chekrd `ask_human`), so raising a decision never blocks or derails the session.
11. **Proposals wait for a click, and the checks run again at send time** (leggetter D4 one-time passes keyed on the exact input). This is useful for Ekumen's tier-2 actions run on the lead's behalf.
12. **Measure the decision loop** (chekrd `scripts/ux/01-wait-time.sql`, `02-decision-endings.sql`): log events with no content, then compute time to decide and time blocked. This answers spine §9 "how to measure value".
13. **Decision records in the repo** (leggetter `docs/decisions.md`, and aidarkazybekov's `backlog/` markdown with front matter). This confirms Ekumen's file-first format is a known-good practice.

### Avoid

1. **Screen scraping as the main signal** (claude-squad, ccmanager, agentapi). It breaks with each UI change and with keyboard layouts (chekrd sends named keys for this reason). Keep it as a last fallback and label it "unreliable".
2. **LLM-ranked priority and LLM-judged status as truth** (chekrd's ranker, leggetter recaps, agent-deck conductor). It costs tokens, isn't deterministic, and can hide work. Spine invariant 9 already says to compute priority in code. Let a model add attention, never remove it.
3. **LLM auto-approval or a conductor that answers for the lead** (ccmanager auto-approval, agent-deck conductor, claude-squad `--autoyes`). This bypasses tiers by model judgment. Ekumen's tier 0 and 1 grants are explicit, written on the brief, and auditable.
4. **Depending on undocumented internal files without version fixtures** (`~/.claude/sessions/<pid>.json`, `~/.claude/jobs/`). If we use them, put them behind an adapter that fails soft (leggetter) and pin a fixture for each version (chekrd).
5. **Lock-in to one host or OS** (docket needs bb; ApostPap needs T3; chekrd needs macOS, Ghostty and Brave Beta). Spine D-002 is agent-agnostic, so keep the core free of terminal or host specifics.
6. **A huge single-page UI spec in prose** (chekrd's README is about 45 KB of behavior in one file). Ekumen's spine and contracts split should stay.
7. **Rewriting decision records in place** (leggetter: "a decision that's revised keeps its ID and is updated in place"). Ekumen's append-only plus superseded is better for audit.
8. **Building the product on a fast-moving host feature with no fallback.** vibe-kanban is sunsetting, and many of these projects are under a month old. Keep Ekumen's value in files plus the CLI, not in one tool's UI.

## 5. Differentiation check

What Ekumen does that **none** of the 17 projects do:

1. **Decisions as gates in a task graph.** Elsewhere, task edges run from task to task (yardmaster, docket) or decisions are questions per session (chekrd, quanticstudios). No project ranks a decision by *tasks blocked transitively × deadline*, or draws decisions as gate nodes.
2. **The applied step.** No project, once an answer lands, amends the briefs of other affected tasks, clears their blocks and dispatches the tasks that are now ready. Elsewhere an answer goes back to the one session that asked.
3. **Tier 1 with a veto window and a default after a deadline.** Nothing like this exists. Approvals are either required (yardmaster `requireApproval`, docket person-only) or automated by an LLM (ccmanager, agent-deck).
4. **Tier grants listed on a frozen brief.** docket's permissions per objective come nearest, but they are global flags for the objective, not grants per task frozen at approval.
5. **Contracts frozen between tasks, changed only through a tier-2 decision.** No project has these.
6. **External dependencies as gates with an owner and dates.** None found.
7. **Spine plus brief §A/§B/§C with a single writer for each section.** The nearest is docket's revision counters and leggetter's PLAN.md and decision log, both kept by hand.

**Where Ekumen is not different:** session discovery, needs-input detection, jump-to-session, worktree creation, phone notifications and diff review are solved many times over. Ekumen should treat them as commodity (spine v0.1 already scopes them down), adopt the hook mappings above, and not compete on terminal UX.

**Interop option (not a decision yet):** signalbox's `fire` event API and quanticstudios' `notify`/hooks are open (MIT). An Ekumen notifier adapter could post "decision raised" to whichever board the lead already uses, instead of Ekumen owning every view.

## 6. Decisions drafted for the lead

None of these is the §A escalation, since no project does Ekumen's core. They are adapter choices that R2's evidence bears on, drafted in the `.ekumen/decisions` format for the orchestrator to number and file. Each is tier 2 because it shapes the agent-adapter contract.

```yaml
id: R2-DR1   # orchestrator to assign D-0xx
title: Needs-input and status signal sources for the Claude Code adapter
tier: 2
raised_by: {task: R2, session: r2-landscape, on: 2026-11-20}
question: >
  Which signals does the Claude Code adapter treat as authoritative for session status and "needs input"?
options:
  - id: hooks-plus-listing
    tradeoff: >
      Hooks (PermissionRequest, Notification, PreToolUse:AskUserQuestion, Stop, SessionStart,
      UserPromptSubmit) post events; `claude agents --json --all` (documented) reconciles liveness;
      transcript tail only for the ask's text. Proven by signalbox, quanticstudios, leggetter. Needs a
      hooks install step (backup, dry-run, reversal) and dedup of twin events.
  - id: transcript-only
    tradeoff: No install; read-only (aidarkazybekov). Misses live permission prompts until written; slower; parses undocumented JSONL.
  - id: screen-scrape
    tradeoff: Works for any tool in tmux (claude-squad, ccmanager). Brittle across versions and layouts; rejected by most mature tools.
recommendation: hooks-plus-listing
context:
  summary: >
    Every mature supervisor converged on hooks. Missing data should fail toward "needs you". Undocumented
    files (~/.claude/sessions, ~/.claude/jobs) stay behind a fail-soft adapter with version fixtures.
  evidence: [.ekumen/research/R2-landscape.md#3-how-they-detect-needs-input-and-status-per-agent-tool]
blocks: []
contract_impact: [agent-adapter contract (C-series, not yet drafted)]
status: pending
answer: null
```

```yaml
id: R2-DR2   # orchestrator to assign D-0xx
title: How Ekumen raises decisions from, and delivers answers to, a live Claude Code session
tier: 2
raised_by: {task: R2, session: r2-landscape, on: 2026-11-20}
question: >
  What channel carries a decision from a session to Ekumen, and the applied answer back, and where do
  side questions run?
options:
  - id: mcp-raise-inbox-deliver-fork-ask
    tradeoff: >
      Raise via an Ekumen MCP tool that only acknowledges (or `ekumen decision raise --json` from the
      skill); deliver via Claude Code's session inbox (CLAUDE_CODE_MESSAGING_SOCKET / relay, as gavel
      and leggetter), which cannot approve permission prompts (good: answers are guidance); questions in
      `claude -p --resume <id> --fork-session` read-only (chekrd), satisfying invariant 10. Depends on
      Claude Code messaging (>= 2.1.295 per leggetter) and needs a queued fallback.
  - id: tmux-injection
    tradeoff: Works with any CLI (chekrd, claude-squad). Requires owning the terminal; fragile; risks answering the wrong prompt without the pre-send checks.
  - id: blocking-tool
    tradeoff: Session blocks on a tool call until answered. Simple, but holds a turn open for hours and derails the session — contrary to spine §1.
recommendation: mcp-raise-inbox-deliver-fork-ask
context:
  summary: >
    Delivery is a separate effect from the answer (docket R21): record `decided` once, then deliver with
    pending/delivered/uncertain state, re-checking the target decision version just before sending
    (chekrd/quanticstudios first-wins with typed refusals).
  evidence: [.ekumen/research/R2-landscape.md#4-adopt--avoid]
blocks: []
contract_impact: [agent-adapter contract, decision/event schema]
status: pending
answer: null
```

## 7. Method and limits

- 17 repos were shallow-cloned read-only into `/private/tmp/claude-501/ekumen-r2-scratch`. Nothing was installed or run.
- The `slopus/happy` clone did not finish checking out, so its row relies on the README read through the GitHub API.
- Stars move fast, and most of the starting set is under 6 weeks old with 0–22 stars. **Activity, not traction, is what makes them worth reading.** The traction is in older, broader tools (vibe-kanban, happy, claude-squad, humanlayer), none of which targets decision gating.
- Claude Code features cited (`claude agents --json`, cross-session messaging, `~/.claude/jobs`) are as the projects describe them at these commits. R1 or a spike should confirm them against Claude Code's own docs.
