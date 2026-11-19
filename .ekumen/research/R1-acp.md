# R1 · Agent Client Protocol (ACP) as the agent adapter base

*Task R1, research. Written 2026-11-20 by the R1 task session. Evidence for spine §6 ("agent adapter design is open until R1 reports") and §9 open questions.*

## 0. Summary

- **Recommendation: ACP plus native fallbacks.** Build one Go adapter interface that reports what each tool can do. Back it with a generic ACP driver for sessions Ekumen starts, plus a small native driver per tool for what ACP can't do: discovering and reading the status of sessions **the lead started in a terminal**, and (for Claude Code) reading `claude agents --json`. **v0.1 ships only the native Claude Code adapter.** v0.1 has no dispatcher, and the native CLI already covers start, resume, fork, structured output and status (verified hands-on below). ACP comes in when Ekumen starts sessions itself (dispatcher, v0.3) and when the Codex, Gemini and OpenCode adapters arrive.
- **Why not ACP-only:** In ACP, the client starts the agent as a child process and owns its sessions over that connection. For Claude Code and Codex, the ACP adapter starts its *own private* agent process. ACP therefore can't attach to, report on, or message a session the lead already has open in a terminal. Seeing those sessions is Ekumen's problem #1 (visibility). Also, `session/fork` and `session/status` are still **unstable** in ACP v1, and the running/idle/requires_action state exists only in the **v2 draft**.
- **Why not native-only:** Four (eventually six) bespoke drivers, each with its own event format. Two of those tools (Gemini CLI, OpenCode) speak ACP natively, Cursor and Copilot ship ACP servers, and the Claude and Codex adapters are actively maintained. One ACP driver gives Ekumen a uniform event stream and permission flow (`session/request_permission`, which is how "needs input" is detected) for every tool.
- **ACP health:** Not abandoned. Schema 1.19.0 released 2026-11-18, roughly weekly releases, Rust and TS SDKs at 1.0. It is not locked to one vendor, but it is governed by **two editor vendors (Zed, JetBrains)**, with a foundation promised but not yet created. **No agent vendor (Anthropic, OpenAI) maintains the Claude or Codex adapter.** The escalation trigger in §A is *not* hit. This is a dependency risk, recorded in §5.
- **Terms:** No tool's terms forbid driving it programmatically. One constraint does need the lead (§5, D-R1-3): Anthropic does not allow third-party products to offer claude.ai login. Ekumen must drive the lead's *own installed* `claude` under the lead's own login, and must never bundle a Claude Agent SDK app that signs users in.

## 1. Evidence markers and method

- ***verified (ran)***: I ran it on this machine on 2026-11-20 (Claude Code 2.1.328, macOS). These are the only tools installed. `codex`, `gemini`, `opencode`, `cursor-agent` and `copilot` are not installed (*verified (ran)*: `command -v` found none).
- ***verified (source)***: I read the code at a pinned commit, shallow-cloned into `/private/tmp/claude-501/ekumen-r1-scratch` (cloned code was never built or run):
  - `agentclientprotocol/agent-client-protocol` @ `bc03f00`
  - `agentclientprotocol/claude-agent-acp` @ `eb4c16f` (v0.109.0)
  - `agentclientprotocol/codex-acp` @ `ad09e0e` (v2.8.0)
  - `coder/acp-go-sdk` @ `6217b27` (v0.20.0)
  - `openai/codex` @ `65454c7`
  - `google-gemini/gemini-cli` @ `808199f` (0.71.0-nightly.20261118)
  - `sst/opencode` @ `847de27` (2.3.0)

  Source shows what the code at HEAD does, not what a released binary does.
- ***from docs***: a linked page. "(3rd party)" marks a non-official source.
- **Not done:** a live ACP client run. No ACP-speaking agent is installed. Installing an adapter (`npx @agentclientprotocol/claude-agent-acp`) means running downloaded third-party code, which my brief does not allow. Claude Code itself has no `--acp` flag (*verified (ran)*: `claude --help`). Recorded as a follow-up.

## 2. ACP itself

| Item | Finding | Evidence |
|---|---|---|
| Version | Protocol **v1** (`protocolVersion: 1`) is stable. **v2 is in Draft** (announced 2026-07-20). Schema crate **1.19.0**, released 2026-11-18. | *verified (source)* `schema/v1/meta.json` `"version": 1`, `CHANGELOG.md`. *from docs* [updates](https://agentclientprotocol.com/updates) |
| Maintainers | Jointly governed by **Zed and JetBrains** as an "interim model while working toward transitioning to an independent foundation". Lead maintainers: Ben Brandt (Zed) and Sergey Ignatov (JetBrains, since 2026-02-04). | *from docs* [governance](https://agentclientprotocol.com/community/governance) |
| License | **Apache-2.0**, with no CLA. The spec repo, claude-agent-acp, codex-acp, acp-go-sdk, Codex and Gemini CLI are all Apache-2.0. OpenCode is MIT. | *verified (source)* LICENSE files |
| Activity | Stabilizations in 2026: session/list (Feb), session_info_update (Mar), session/resume (Apr 22), session/close (Apr 23), session/delete (Jun 5), request cancellation (Jun 29), elicitation (Jul 22), client system prompt (Nov 6). | *from docs* [updates](https://agentclientprotocol.com/updates) |
| Transport | JSON-RPC over stdio. Remote transports (HTTP, WebSocket) are WIP in a working group. | *from docs* [intro](https://agentclientprotocol.com/overview/introduction) |
| Go SDK | `github.com/coder/acp-go-sdk` v0.20.0 (Coder, Apache-2.0, generated from the schema, includes unstable types). No official Go SDK; official SDKs are Kotlin, Java, Python, Rust and TS. | *verified (source)*; *from docs* [community libs](https://agentclientprotocol.com/libraries/community) |

**Stable v1 agent methods** (*verified (source)*, `schema/v1/meta.json`): `initialize, authenticate, session/new, session/load, session/set_mode, session/set_config_option, session/prompt, session/cancel, session/list, session/delete, session/resume, session/close, logout`. Client-side: `session/request_permission, session/update, fs/*, terminal/*, elicitation/*`.
**Unstable v1 additions** (`meta.unstable.json`): `session/fork, session/status, session/archive, session/options, session/set_title, providers/*, auth/status, mcp/message`.

### 2.1 ACP methods mapped to Ekumen's five operations

| Ekumen op | ACP mapping | Support |
|---|---|---|
| **start** | `initialize` → `session/new {cwd, additionalDirectories?, systemPrompt?}` → `session/prompt` | **Supported** (stable). |
| **resume** | `session/load` (replays history as `session/update`; capability `loadSession`) or `session/resume` (no replay; `sessionCapabilities.resume`) | **Supported** (stable). Only for sessions *this connection's agent process* can open. |
| **send** | Between turns: another `session/prompt`. **Mid-turn: v1 doesn't define it.** The v2 draft lets clients send `session/prompt` in any state; the agent inserts it or holds it ([v2 prompt lifecycle](https://agentclientprotocol.com/protocol/v2/prompt-lifecycle)). Today's mid-turn path is the pre-standard extension `_session/steering`, shipped by claude-agent-acp, codex-acp and opencode. | **Partly.** Only to a session the client owns. |
| **fork** | `session/fork {sessionId, cwd, mcpServers, cursorId?}`, gated by `sessionCapabilities.fork` | **Partly.** Still `unstable_session_fork`. RFD last revised 2026-11-16 ([RFD](https://agentclientprotocol.com/rfds/session-fork)). |
| **status** | v1 has none. Unstable `session/status` returns only `open`/`saved`. The v2 draft adds a `state_update` notification and a `state` with values `running`/`idle`/`requires_action`/`unknown`. "Needs input" can also be inferred from an outstanding `session/request_permission` or `elicitation/create`. | **Partly.** Fine for sessions you own (you see every request). Nothing for sessions you don't. |

## 3. Capability matrix

Legend: ✅ yes · ◐ partial (see note) · ❌ no · ? unknown. Each cell gives its evidence marker.

### 3.1 How each tool speaks ACP

| Tool | ACP route | Evidence |
|---|---|---|
| Claude Code | **Adapter**: `@agentclientprotocol/claude-agent-acp` v0.109.0. Built on `@anthropic-ai/claude-agent-sdk`. Started at Zed, now in the ACP org, **not Anthropic**. No native `--acp`. | *verified (source)* package.json; *verified (ran)* `claude --help` |
| Codex CLI | **Adapter**: `@agentclientprotocol/codex-acp` v2.8.0 (JetBrains copyright, ACP org). Spawns its own **private** `codex app-server` per adapter process. | *verified (source)* `CodexJsonRpcConnection.ts` |
| Gemini CLI | **Native**: `gemini --acp` (`--experimental-acp` is deprecated) | *verified (source)* `cli/src/config/config.ts` |
| OpenCode | **Native**: `opencode acp`. The ACP service is a client of the OpenCode server, so sessions are server-owned. | *verified (source)* `cli/src/acp/service.ts` |
| Cursor | **Native**: `agent acp` | *from docs* [cursor.com/docs/cli/acp](https://cursor.com/docs/cli/acp) |
| Copilot CLI | **Native, public preview**: `copilot --acp [--stdio\|--port N]` | *from docs* [GitHub docs](https://docs.github.com/en/copilot/reference/copilot-cli-reference/acp-server) |

### 3.2 Capabilities each ACP agent advertises (`initialize` response)

| Capability | Claude (adapter) | Codex (adapter) | Gemini | OpenCode |
|---|---|---|---|---|
| loadSession | ✅ | ✅ | ✅ | ✅ |
| resume / list / close / delete | ✅ ✅ ✅ ✅ | ✅ ✅ ✅ ✅ | ❌ ❌ ❌ ❌ | ✅ ✅ ✅ ✅ |
| fork (unstable) | ✅ | ✅ | ❌ | ✅ |
| status (unstable) | ✅ (`open`+v2 `state` / `saved`) | ❌ | ❌ | ❌ |
| v2 `state_update` | ✅ | ✅ | ❌ | ❌ |
| `_session/steering` (mid-turn send) | ✅ | ✅ | ❌ | ✅ |

All cells are *verified (source)*: claude-agent-acp `src/v1/acp-agent.ts` L1833–1882 and `src/v2/initialize.ts`; codex-acp `src/CodexAcpServer.ts` L850–875 and `AcpV2ForegroundState.ts`; gemini-cli `cli/src/acp/acpRpcDispatcher.ts` L91–102; opencode `cli/src/acp/service.ts` L135–143. Cursor and Copilot: only `session/new`, `session/load` (Cursor) and `prompt` are documented; everything else is **?**.

### 3.3 Tool × operation × route

Rows are operations from §A Job 3. In each cell, the ACP route comes first and the native CLI or SDK route second.

| Operation | Claude Code | Codex CLI | Gemini CLI | OpenCode |
|---|---|---|---|---|
| **Start headless in dir with prompt** | ACP ✅ *source*. Native ✅ *verified (ran)*: `claude -p "…" --session-id <uuid> --output-format stream-json` in cwd. Ekumen can pre-assign the id. | ACP ✅ *source*. Native ✅ `codex exec --json -C <dir> "…"` *from docs* [ref](https://learn.chatgpt.com/docs/developer-commands?surface=cli) | ACP ✅ *source*. Native ✅ `gemini -p "…" -o stream-json [--session-id <uuid>]` *source* | ACP ✅ *source*. Native ✅ `opencode run --format json [--session <id>]` (creates the session if the id is missing) *source* |
| **Resume by id** | ACP ✅ load/resume *source*. Native ✅ `-p --resume <id>`, same id returned *verified (ran)* | ACP ✅ *source*. Native ✅ `codex exec resume <id>` / `codex resume <id>` *source + docs* | ACP ◐ `session/load` only *source*. Native ✅ `--resume <id\|latest\|index>` *source* | ACP ✅ *source*. Native ✅ `run --session <id>` / `--continue` *source* |
| **Send to a running session** | ACP ◐ only to a session the adapter owns, via `_session/steering` *source*. Native ◐ to a host-owned process via `--input-format stream-json`: a second message sent mid-turn ran as the next turn *verified (ran)*. **No CLI to send into a session started elsewhere**: `claude agents` has no send *verified (ran)*. A `claude agents send` feature request was closed 2026-07-08 *from docs (3rd party)* [#65264 mirror](https://claudeissues.com/issue/65264-feature-headless-enqueue-send-a-message-to-a-running-background-session-non-inte). | ACP ◐ owned only, via steering *source*. Native ✅ **`codex queue --thread <id> --message "…" [--resume]`** exists *source* (`cli/src/queue_cmd.rs`). Needs the local daemon or `--remote` *from docs*. App-server `turn/steer`, `thread/queue/add` *source*. Introduced in v0.149 per the brief's blog; version not verified. | ACP ❌ (v1 only, no steering) *source*. Native ❌ found none *source* | ACP ◐ steering *source*. Native ✅ `POST /api/session/:id/prompt` admits input into the session inbox with `delivery`, and works on any server-owned session *source* (`protocol/src/groups/session.ts`) |
| **Fork** | ACP ✅ (unstable) *source*. Native ✅ `--resume <id> --fork-session` returned a new id *verified (ran)* | ACP ✅ *source*. Native ✅ `codex exec fork <id> [prompt]`, `codex fork` *source* | ACP ❌. Native ❌ (no flag in the option list) *source* | ACP ✅ *source*. Native ✅ `run --session <id> --fork`, `POST /api/session/:id/fork` *source* |
| **Read status** | ACP ◐ `session/status` open/saved, v2 `state` running/idle/requires_action, own sessions only *source*. Native ◐ `claude agents --json` lists **all live sessions, interactive ones included**, with `status` `idle`/`busy` *verified (ran)*. A `waitingFor` field and other states are *from docs (3rd party)* only. | ACP ◐ v2 `state_update` only *source*. Native ✅ app-server `ThreadStatus` = `notLoaded \| idle \| systemError \| active{activeFlags: waitingOnApproval, waitingOnUserInput}` + `thread/status/changed`, `thread/loaded/list` *source*. The app-server is labelled *experimental*. | ACP ❌. Native ❌ (only process liveness and transcript mtime) *source* | ACP ❌. Native ✅ `idle \| busy \| retry` status events, `GET /api/session/active`, pending forms (= needs input) *source* (`schema/src/session-status-event.ts`) |
| **Structured output (JSON events)** | ACP ✅ `session/update`. Native ✅ `stream-json` (system/assistant/result events with `session_id`) *verified (ran)* | ACP ✅. Native ✅ `exec --json` NDJSON *docs* | ACP ✅. Native ✅ `-o json\|stream-json` *source* | ACP ✅. Native ✅ `--format json` + SSE from the server *source* |

Cursor and Copilot (cheap pass only): both have ACP start and load (*from docs*). Copilot has native `-p`, `--output-format json` (JSONL), `--resume <id>`, `--continue` (*from docs* [CLI reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-command-reference)). Their send, fork and status are **?**.

### 3.4 Session identifiers and transcripts (for `status` fallbacks)

| Tool | Session id | Where transcripts and state live | Evidence |
|---|---|---|---|
| Claude Code | UUID; caller can choose it with `--session-id`. `--fork-session` mints a new one. | `~/.claude/projects/<cwd with / → ->/<uuid>.jsonl`; the line `type` values seen include `user, assistant, system, queue-operation, permission-mode…`. Live registry: `~/.claude/sessions/<pid>.json` with `sessionId, cwd, kind, status, updatedAt, messagingSocketPath` (**undocumented; read `claude agents --json` instead**). | *verified (ran)* |
| Codex | UUID or thread name | `$CODEX_HOME/sessions/` (rollout JSONL), `archived_sessions/`, plus a state SQLite DB | *verified (source)* `rollout/src/lib.rs` |
| Gemini | UUID (`--session-id`) or a per-project index | `~/.gemini/tmp/<project-id>/chats/` | *verified (source)* `core/src/config/storage.ts` |
| OpenCode | Server session id | Server SQLite DB (`opencode debug paths db`); the background service owns sessions | *verified (source)*; *from docs* [cli](https://opencode.ai/docs/cli/) |

## 4. Recommendation

**ACP plus native fallbacks**, phased:

1. **v0.1 (now):** a native Claude Code adapter only.
   - status: `claude agents --json` (all live sessions) + transcript mtime
   - start: `claude -p --session-id` (idempotent, because Ekumen picks the id)
   - resume: `--resume`
   - fork: `--resume --fork-session`, which serves invariant 10 "asking does not derail"
   - send: to sessions Ekumen owns via stream-json stdin. To a lead-started session, send isn't possible, so the message is **queued as a side question** (invariant 10's fallback) and the session is flagged.
2. **v0.2–0.3 (dispatcher, more tools):** add one generic ACP driver for every session Ekumen starts. Codex, Gemini and OpenCode then cost little. Keep thin native **observers** per tool for discovery and status of sessions Ekumen didn't start:
   - Claude: `claude agents --json`
   - Codex: app-server `thread/loaded/list` and `ThreadStatus`
   - OpenCode: server `/api/session/active`
   - Gemini: transcript mtime only
3. Use unstable ACP features (fork, status, steering) only after checking the advertised capability. Target ACP v1 now and move to v2 once v2 is stable (its prompt-while-running and `state` model match Ekumen's needs).

**Tradeoffs**

| Option | For | Against |
|---|---|---|
| ACP-only | One driver for 6+ tools; uniform events and permissions; Zed and JetBrains keep adapters current | Can't see or message lead-started sessions (no attach); status and fork unstable; Claude and Codex adapters not vendor-owned; adds a Node runtime dependency for adapters |
| **ACP + native fallbacks** | Uniform control of owned sessions *and* visibility of all sessions; graceful degradation per capability | Two code paths per tool, so two places to keep in sync; needs capability negotiation |
| Native-only | Smallest dependency surface; fastest v0.1 for Claude; Codex and OpenCode native surfaces are richer than ACP (queue, server inbox, `waitingOnApproval`) | N bespoke event parsers; repeats what ACP already standardizes; Gemini, Cursor and Copilot all expose ACP as their main programmatic surface |

### 4.1 Go adapter interface sketch

```go
package agent // internal/agent

type Op string // "start","resume","send","send_running","fork","status","list","events"

type Caps struct {
    Ops             map[Op]Support // Supported | Partial | Unsupported
    AttachExternal  bool           // can act on sessions it did not start (OpenCode server, Codex daemon)
    NeedsInputKinds []string       // "permission","elicitation","user_question"
}

type SessionRef struct {
    Tool, ID, Cwd string
    Owner         Ownership // Managed (Ekumen started it) | Observed (lead started it)
}

type State string // running | idle | requires_action | done | not_loaded | unknown
type Status struct {
    State      State
    WaitingFor string    // e.g. "permission:Bash", when known
    Source     string    // "acp" | "native" | "transcript"
    ObservedAt time.Time
}

type Adapter interface {
    Tool() string
    Caps(ctx context.Context) (Caps, error)
    List(ctx context.Context, f ListFilter) ([]SessionInfo, error)                  // observer: discovery
    Status(ctx context.Context, s SessionRef) (Status, error)                       // observer
    Start(ctx context.Context, r StartReq) (SessionRef, error)                      // actuator; r.ID pre-assigned for idempotency
    Resume(ctx context.Context, s SessionRef) error                                 // actuator
    Send(ctx context.Context, s SessionRef, m Message, mode SendMode) (SendResult, error) // mode: NextTurn | Steer; ErrUnsupported → caller falls back
    Fork(ctx context.Context, s SessionRef, r ForkReq) (SessionRef, error)          // actuator
    Events(ctx context.Context, s SessionRef) (<-chan Event, error)                 // normalized from ACP session/update or native stream-json
}

// Composition: an ordered chain per tool, first driver that supports the op wins.
// claude := agent.Chain(acpDriver("claude-agent-acp"), claudeNative{}, transcriptObserver{})
```

This fits D-012 (if accepted): `List`/`Status` are **observers**, and `Start`/`Send`/`Fork`/`Resume` are **actuators**. Idempotency comes from pre-assigned session ids (Claude `--session-id`, Gemini `--session-id`, OpenCode `--session`) and the v2 `idempotencyKey` on `session/prompt` (added in schema 1.19.0, *verified (source)*). `ErrUnsupported` plus `Caps` let the loop choose the invariant-10 fallback (a queued side question) deterministically, in code.

## 5. Decision record drafts (for the lead; not decided by R1)

```yaml
id: D-R1-1   # orchestrator assigns the real D-number
title: Agent adapter base. ACP, native, or both?
tier: 2
raised_by: {task: R1, session: r1-acp, on: 2026-11-20}
question: Should Ekumen's agent adapters be built on ACP, on each tool's native CLI/SDK, or both?
options:
  - {id: acp-only, tradeoff: One driver for all tools; cannot see or message lead-started sessions; fork/status unstable}
  - {id: acp-plus-native, tradeoff: Uniform control of owned sessions plus native observers for all sessions; two paths per tool}
  - {id: native-only, tradeoff: Fewest deps, fastest v0.1; N bespoke drivers; ignores the standard surface of Gemini/Cursor/Copilot}
recommendation: acp-plus-native (v0.1 = native Claude adapter only; generic ACP driver arrives with the dispatcher)
context: {evidence: [.ekumen/research/R1-acp.md]}
blocks: [spine §6 adapter design, contract for the adapter interface]
contract_impact: [adapter interface contract (C-series, not yet drafted)]
```

```yaml
id: D-R1-2
title: Managed vs observed sessions
tier: 2
question: >
  Does Ekumen only act on sessions it starts (managed), or also on sessions the lead starts in a terminal (observed)?
  The tools can't send into an observed Claude Code or Gemini session.
options:
  - {id: both-with-degradation, tradeoff: Full visibility; observed sessions get status/resume/fork, and "send" becomes a queued side question (invariant 10)}
  - {id: managed-only, tradeoff: Every op works uniformly; the lead must start all sessions through Ekumen, which conflicts with the visibility goal}
recommendation: both-with-degradation
```

```yaml
id: D-R1-3
title: How Ekumen authenticates to Claude Code
tier: 2   # terms / outward
question: >
  Anthropic does not allow third-party products to offer claude.ai login or rate limits (including Agent SDK apps) unless approved
  (https://code.claude.com/docs/en/agent-sdk/overview). claude-agent-acp is an Agent SDK app.
options:
  - {id: drive-installed-cli, tradeoff: Ekumen spawns the user's own `claude` binary under the user's own login; no credentials handled by Ekumen}
  - {id: api-key-only-acp, tradeoff: Use claude-agent-acp with ANTHROPIC_API_KEY only; clean on terms; costs API spend}
  - {id: seek-approval, tradeoff: Ask Anthropic; slow, uncertain}
recommendation: drive-installed-cli for personal use. Never ship or broker Claude credentials. Revisit before any public release.
note: Personal use of one's own CLI is not forbidden; this matters once Ekumen is distributed.
```

```yaml
id: D-R1-4
title: ACP version target
tier: 1
question: Target ACP v1 (stable) with unstable features behind capability checks, or the v2 draft?
options:
  - {id: v1-plus-unstable, tradeoff: Works with every agent today; state/steering via extensions}
  - {id: v2-draft, tradeoff: Better model (prompt-while-running, state_update), but only the Claude and Codex adapters implement it; spec may change}
recommendation: v1-plus-unstable
default_if_no_answer: v1-plus-unstable
deadline: when the first ACP build task is briefed
```

## 6. Open items / not verified

- No live ACP session was run (no ACP agent installed; installing one means running untrusted code). Follow-up: a sandboxed spike with `claude-agent-acp` and `opencode acp`, with lead approval.
- Codex, Gemini and OpenCode native claims come from source at HEAD or from docs, not from running released binaries. `codex queue` exists in source; the "v0.149, Aug 2026" date comes only from a third-party blog ([danielvaughan](https://codex.danielvaughan.com/2026/08/29/codex-cli-v0149-multi-session-agents-dashboard-codex-queue-working-directory/)).
- Claude Code `claude agents --json` states other than `idle`/`busy` (e.g. a `waitingFor` field, `requires_action`) were not observed. Strings exist in the binary, but the semantics are unverified. Ekumen needs a reliable "needs input" for Claude: test by triggering a permission prompt in a background session (follow-up).
- Claude Code has cross-session messaging inside the product (agent-to-agent), but third-party issue mirrors describe it as buggy, and it is not a CLI surface. Don't depend on it.
- Cursor and Copilot: docs-only, shallow.
- Hands-on side effect: a few tiny Haiku sessions were created under `~/.claude/projects/-private-tmp-claude-501-ekumen-r1-scratch-cc-probe/` (removable with `claude purge <path>`).
