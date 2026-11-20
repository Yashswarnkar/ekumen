---
id: C-adapter
aliases: [C4]
title: Agent adapter interface
status: draft          # draft | frozen
frozen_on: null
version: 0
consumers: [T-reconcile, T-server, T-skills]
producers: [T-claude-adapter]
---

# C-adapter · Agent adapter interface

How Ekumen sees and drives coding-agent sessions without knowing which tool runs them. Per spine §6 and rule 12, **this contract and the adapters are the only places a tool or vendor name may appear**. Everything above it, including the core, the control loop, the CLI and the UI, works in terms of sessions, statuses and capabilities.

Decisions behind it: D-013 (status source), D-014 (raising and delivery), D-015 (ACP plus native), D-016 (managed and observed sessions), D-017 (credentials), D-018 (ACP version). Research: `.ekumen/research/R1-acp.md` §4 (interface sketch), `R2-landscape.md` §4 (patterns to adopt and avoid).

## 1. Concepts

| Term | Meaning |
|---|---|
| **Adapter** | Code that speaks to one kind of agent tool. Named by a short slug, e.g. `claude-code`, `acp`, `codex`. |
| **Session** | One agent conversation, identified by `{adapter, id}`. `id` is the tool's own session id. |
| **Ownership** | `managed`: Ekumen started it. `observed`: someone else did (usually the lead, in a terminal). |
| **Status** | `running` · `idle` · `needs_input` · `done` · `unknown`, plus optional `waiting_for` (`permission` · `question` · `other`) and `detail` text. |
| **Capability** | What an adapter can do for a given session: each operation is `supported`, `partial` or `unsupported`. |

**Unknown counts as "needs you".** When an adapter can't tell a session's status, it reports `unknown`, never a guess. The inbox shows a session that stays `unknown` for longer than 10 minutes as an item to check. Missing data fails toward getting a human's attention (R2 §4).

## 2. Operations

```go
package agent // internal/agent

type Adapter interface {
    Name() string
    Available(ctx context.Context) error                        // tool installed and runnable; ErrUnavailable otherwise
    Caps(ctx context.Context, s *SessionRef) (Caps, error)      // s == nil: capabilities for new sessions

    // Observers: read-only, safe to call on every loop pass.
    List(ctx context.Context, f ListFilter) ([]SessionInfo, error)
    Status(ctx context.Context, s SessionRef) (Status, error)
    Events(ctx context.Context, s SessionRef, since string) (<-chan Event, error) // optional; ErrUnsupported

    // Actuators: idempotent; every request carries a key Ekumen chose.
    Start(ctx context.Context, r StartReq) (SessionRef, error)  // r.Key → same session if repeated
    Resume(ctx context.Context, s SessionRef, m *Message) error
    Send(ctx context.Context, s SessionRef, m Message) (Delivery, error)
    Fork(ctx context.Context, s SessionRef, r ForkReq) (SessionRef, error)
}

type SessionRef  struct{ Adapter, ID, Cwd string; Ownership string }   // "managed" | "observed"
type Status      struct{ State, WaitingFor, Detail, Source string; ObservedAt time.Time }
type StartReq    struct{ Key, Cwd, Prompt, Task string }                 // Key: idempotency
type ForkReq     struct{ Key, Prompt string; ReadOnly bool }
type Message     struct{ Key, Text string; Mode string }                 // Mode: "next_turn" | "steer"
type Delivery    struct{ Key, State string }                             // "delivered" | "queued" | "uncertain"
type Caps        struct{ Ops map[string]string; AttachObserved bool }    // op → supported|partial|unsupported
type Event       struct{ Kind, Text string; At time.Time }               // message|tool_use|needs_input|turn_end|error
```

The Go shapes are normative for field meaning. Packages may add fields, never change these.

**Errors:** `ErrUnsupported` (the operation isn't possible for this session), `ErrUnavailable` (tool missing or broken), `ErrNotFound` (unknown session). Anything else is a transient error the loop may retry on its next pass. **Actuators are never retried after an ambiguous failure.** See §4.

## 3. Fallbacks (chosen in code, never by an agent)

| Need | Capability missing | Fallback |
|---|---|---|
| Deliver an answer or message | `send` unsupported, or the session is observed | Queue it: record `MessageQueued`, and deliver it with the next `Resume`, or show it to the lead as "tell this session: …" in the inbox. |
| Ask a side question without derailing (spine rule 10) | `fork` unsupported | Queue it to the original session as a side question, marked as such. |
| Know a session's status | `status` unsupported or failing | `unknown` (§1). |
| Start a task | `start` unsupported | Can't dispatch automatically. The inbox shows "start this task" with the brief's start prompt for the lead to paste. |

An adapter may combine several drivers (e.g. ACP for control plus a native status reader). The first driver that supports an operation wins (D-015).

## 4. Delivery and idempotency

Recording an answer and delivering it are separate steps (R2 §4):

1. The answer is an event (`DecisionDecided`, `QuestionAnswered`). It is final once written.
2. Delivery is attempted by the loop, with a key derived from that event's id. The outcome is recorded as `MessageDelivered`, `MessageQueued` or `MessageUncertain`.
3. **Uncertain is never retried automatically.** If the tool may or may not have received it, the lead sees it in the inbox and decides. This avoids double prompts.

`Start` and `Fork` use keys the same way: repeating a call with the same key returns the same session or fails with `ErrNotFound`. It never creates a second one.

## 5. Session events (additions to C-event-log §6, a minor change)

| Type | Lane | `data` |
|---|---|---|
| `SessionStarted` | loop, orchestrator | `task, adapter, session, ownership, key` |
| `SessionForked` | loop | `from_session, session, adapter, key, for_question` |
| `MessageQueued` | loop | `key, to_session, from_event, reason` |
| `MessageDelivered` | loop | `key, to_session` |
| `MessageUncertain` | loop | `key, to_session, error` |

Session **status** is never an event. It is observed each pass (C-event-log §7 rule 2).

## 6. Credentials and processes

- An adapter **never reads, stores, copies or brokers credentials.** It runs the tool already installed on the machine, under the user's own login (D-017, generalized).
- The tool binary is found by absolute path (configured, or resolved once at startup), never through shell aliases. R1 found an alias that broke subcommands.
- Started processes run in the task's worktree, with the environment Ekumen was started with, plus nothing secret.

## 7. Conformance

Every adapter passes a shared test suite (`internal/agent/conformance`) against a scripted fake of its tool. It covers each operation, each fallback trigger, idempotent `Start` and `Send`, `unknown` on missing data, and never retrying uncertain delivery. Each adapter states the tool versions it was verified against, and keeps recorded fixtures for any undocumented output it parses (R2 §4: undocumented formats need per-version fixtures).

## Open before freeze

- **Choosing the adapter for a task.** Proposal: optional `agent: <adapter>` in the brief's front matter (and `TaskDrafted`), falling back to a project default in `.ekumen/config.yaml`. This needs a small C-event-log addition.
- **The `unknown` threshold.** 10 minutes is a guess. Proposal: make it a project setting with that default.
- **Claude Code's "needs input" signal** is unverified (R1 §6). T-claude-adapter must confirm it hands-on before its status mapping is trusted. Until then that adapter reports `unknown` rather than guessing.
