---
id: T-claude-adapter
title: Claude Code adapter
kind: build
repo: ekumen
base_branch: main
branch: build/t-claude-adapter
worktree: .worktrees/t-claude-adapter
session: t-claude-adapter
contracts_consumed: [C-adapter, C-event-log]
contracts_produced: []
blocked_by: [C-adapter, T-core]
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-claude-adapter · Claude Code adapter

*§A only. Amendments (§B) are files in `amendments/`, handoffs (§C) are files in `handoffs/` (C-event-log §8).*

**Goal:** Ship the first agent adapter: a Go package that implements C-adapter v1 for Claude Code. It lets Ekumen see sessions (managed and observed), report each one's status as `running · idle · needs_input · done · unknown`, start and resume a task's managed session in its worktree with the brief's start prompt, deliver a message to a managed session or report that it must be queued, and fork a session to ask a side question without derailing it (invariant 10). This is spine §8 step 4, "C-adapter → T-claude-adapter (session status joins the loop)". It is the adapter only. Connecting it to `ekumen reconcile` is a follow-up task.

**Context (verified against `main` at `6658d06`, 2026-11-20; re-verify against `origin/main` when you start):**
- **C-adapter is frozen at version 1** (lead, 2026-11-20, commit `a8af023`). §2 gives the normative Go shapes: `Adapter` (`Name, Available, Caps, List, Status, Events, Start, Resume, Send, Fork`), `SessionRef`, `Status`, `StartReq`, `ForkReq`, `Message`, `Delivery`, `Caps`, `Event`, and the errors `ErrUnsupported`, `ErrUnavailable`, `ErrNotFound`. Packages may add fields but never change these. §3 defines the fallbacks, §4 delivery and idempotency, §6 credentials and processes, and §7 conformance.
- **Nothing exists yet:** no `internal/agent` package and no `internal/agent/conformance` suite. The C-adapter §5 session events (`SessionStarted`, `SessionForked`, `MessageQueued`, `MessageDelivered`, `MessageUncertain`) are not in C-event-log v4, where §6 says "Session events: reserved", and they are not in `internal/event`. **The adapter writes no events.** It returns `SessionRef`, `Status` and `Delivery` values, and the loop records them later (follow-up).
- `TaskSessionAttached {session, adapter}` exists, in the orchestrator and loop lanes. `internal/core/testdata/log.jsonl` already uses `"adapter":"claude-code"`, so the adapter's `Name()` is **`claude-code`**.
- Decisions this task implements (all decided and applied; read them with `ekumen decision show <id>`):
  - **D-claude-status-source (D-013) `hooks-plus-listing`:** hooks give the events, and `claude agents --json` gives liveness. Undocumented files (`~/.claude/sessions`, `~/.claude/jobs`) are read only behind a fail-soft reader with version fixtures. Missing data fails toward "needs you".
  - **D-decision-delivery (D-014), answer `other`:** deliver to Ekumen-started sessions through their input stream, and to lead-started sessions as a queued side question. Side questions run in a fork.
  - **D-managed-observed-sessions (D-016) `both-with-degradation`.**
  - **D-claude-auth (D-017) `drive-installed-cli`:** run the lead's own installed `claude` under the lead's own login, and never handle credentials. Revisit before the first public release.
  - **D-adapter-base (D-015) `acp-plus-native`:** v0.1 ships the native Claude Code adapter only.
  - **D-agent-agnostic:** one adapter per tool, behind one interface.
  - **D-acp-version (D-018):** not used here, since v0.1 has no ACP driver.
- **Research:** R1 §3.3 and §4 cover the operations, verified hands-on on 2.1.328: `-p --session-id`, `--resume`, `--resume --fork-session`, and stream-json in and out. R2 §3 covers needs-input signals and §4 the adopt/avoid list. **C-adapter "Open before freeze"** says Claude Code's needs-input signal is unverified, so this task must confirm it hands-on before its status mapping is trusted, and report `unknown` until then.
- **Installed here:** `claude` 2.1.329 at `~/.local/bin/claude`. On 2026-11-20, `claude agents --json` printed sessions with the keys `id, cwd, kind, startedAt, sessionId, name, state, pid, status`. The values seen were `interactive/idle`, `interactive/busy` and `background/state=blocked` (no pid). No `waitingFor` was seen.
- **Claude Code CLI facts verified from the official docs on 2026-11-20.** Cite these, and don't use flags that aren't listed here or that you haven't verified hands-on:
  - [CLI reference](https://code.claude.com/docs/en/cli-reference):
    - `--bg/--background` starts a background agent and returns at once, printing the session ID and management commands. It can't be combined with `-p`.
    - `claude agents --json` prints the active sessions as a JSON array. `--all` adds completed background sessions, and `--cwd <path>` filters by directory.
    - `--session-id <uuid>`; `--resume/-r <id|name|transcript path>`; `--fork-session` (a new ID on resume); `--name/-n`.
    - `-p`; `--output-format text|json|stream-json`; `--input-format text|stream-json`; `--replay-user-messages`; `--include-hook-events`.
    - `--settings <file|json>`; `--permission-mode`; `--permission-prompts host|none` (v2.1.259 or later); `--permission-prompt-tool`; `--bare`.
    - `claude attach|logs|stop|respawn|rm <id>`.
  - [Agent view](https://code.claude.com/docs/en/agent-view):
    - **`claude agents --json` fields:**
      - `cwd`, `kind` (`interactive|background`) and `startedAt` are always present.
      - background sessions add `id` and `state` (`working|blocked|done|failed|stopped`).
      - while the process is alive: `pid` and `status` (`busy|waiting|idle`).
      - when `status` is `waiting`: `waitingFor` (`permission prompt|input needed|sandbox request|worker request|dialog open`).
      - when set: `sessionId` and `name`.
    - "A finished session waiting for your next prompt reads `done`, not `blocked`." The docs say not to parse `~/.claude/jobs/<id>/state.json`.
    - `claude --bg --name <n> "<prompt>"` is documented.
    - `claude --resume <full-uuid> --bg "<prompt>"` continues the conversation in the background under the same ID when possible (v2.1.257 or later). Otherwise it starts a copy with a new ID and prints a `note:` line.
    - The `--settings` flag carries over to background sessions. The supervisor stops sessions that are waiting or finished and have been unattached for about an hour, and resumes them on reply.
    - **There is no `claude send`.**
  - [Sessions](https://code.claude.com/docs/en/sessions):
    - For a running background session, `claude --resume <id> "prompt"` from a terminal sends the prompt as the next turn, then attaches.
    - With piped I/O, `--output-format`, `--settings` or similar flags, it **sends nothing and exits 1**. "Add `--fork-session` to resume a copy instead."
    - No prompt is sent while the session waits on an answer.
    - `--fork-session` gets a new ID and leaves the original unchanged on disk.
    - Two processes resuming one session both write one transcript, so fork instead.
    - Transcripts are at `~/.claude/projects/<project>/<session-id>.jsonl`. That location is documented, but the line format is internal.
  - [Hooks](https://code.claude.com/docs/en/hooks):
    - Hooks are set in settings files. The events include `SessionStart` (with `source`: `startup|resume|clear|compact|fork`), `UserPromptSubmit`, `PreToolUse`, `PermissionRequest` (`tool_name, tool_input`), `Notification`, `Stop`, `StopFailure` and `SessionEnd`.
    - The common input fields are `session_id, transcript_path, cwd, hook_event_name`.
    - The `Notification` matchers include `permission_prompt`, `idle_prompt`, `elicitation_dialog`, `agent_needs_input` and `agent_completed`.
    - `PreToolUse` with tool `AskUserQuestion` carries `tool_input.questions`.
    - Command hooks read their JSON on stdin. `async: true` exists.
  - [Headless](https://code.claude.com/docs/en/headless):
    - "Hooks still don't run in bare mode when you pass them with `--settings`." This implies that hooks passed in `--settings` **do** run without `--bare`.
    - Bare mode doesn't use the subscription login, so **never pass `--bare`** (D-017).
    - In a `-p` run with no permission host, prompts are denied (`permission_denied` system messages and `permission_denials` in the result).
    - `system/init` carries `session_id` and a `capabilities` array.
  - [Streaming input](https://code.claude.com/docs/en/agent-sdk/streaming-vs-single-mode): a user message is `{"type":"user","message":{"role":"user","content":…},"parent_tool_use_id":null}`. A message sent during tool calls reaches Claude after they finish, or starts a new turn.
- **Not verified, so check these hands-on before relying on them:**
  - whether `--bg` accepts `--session-id`, and whether `--fork-session` accepts `--session-id` (to choose the new ID)
  - whether `claude --resume <uuid> --bg "<msg>"` on a **running** or **blocked** background session is delivered as its next turn under the same ID
  - the exact stdout of `--bg`
  - whether `-p` sessions appear in `claude agents --json`
  - whether `waitingFor` appears for interactive sessions
- **This task's registry record is an imported placeholder.** Its `TaskDrafted` has empty `repo`, `base_branch`, `branch`, `worktree` and `brief`, and `tier_grants: []`. Its status is `draft`, and `blocked_by: [C-adapter, T-core]` are both closed. **T-task-update** (ready, in progress) adds `TaskUpdated` and `ekumen task update`. With them, the orchestrator proposes these fields and the lead approves them with `ekumen task approve`. Until that merges, the front matter of this brief is authoritative, and `ekumen task show` shows nulls. The grants come from `task approve --grants`. Before you start, the orchestrator runs `ekumen task attach T-claude-adapter --session t-claude-adapter`.
- `/blocked` and `/decide` exist (`skills/`, PRs #17 and #18). Use `/blocked` to escalate.
- T-reconcile is in progress. Its loop "never starts sessions" and reports `ready` tasks. Don't touch `internal/core`, `internal/cli` or `internal/observe`.

**Design (follow it unless hands-on facts contradict it; then see Escalate):**

1. **Managed sessions are Claude Code background sessions.**
   - `Start` runs `claude --bg` in `StartReq.Cwd`, the task's worktree, with `--name ekumen-<task>-<short key hash>`, `--settings <per-session settings file>` (hooks only, see 3) and `StartReq.Prompt` as the prompt.
   - Claude Code's own supervisor holds the process. Ekumen needs no daemon, so a one-pass CLI can start sessions, and the lead can `claude attach` to answer permission prompts.
   - If hands-on shows `--bg` accepts `--session-id`, use a UUIDv5 of `StartReq.Key` as the ID. That makes `Start` idempotent by construction. Otherwise, find an existing session by name in `claude agents --json --all` before starting.
   - The alternative is a host process holding a `claude -p --input-format stream-json` stdin, which is D-014's literal "input stream". It is **not** built here. It is open question 1 to the lead, and the brief stands as written unless the lead picks it.
2. **Observed sessions:** every session in `claude agents --json` that this adapter didn't start. They are read-only: `List` and `Status` work, `Fork` works (see 5), and `Send`, `Resume` and `Events` return `ErrUnsupported`, so the caller uses C-adapter §3. No PTY or keystroke injection, ever (R2 §4 avoid 1).
3. **Hooks only for managed sessions, per invocation.**
   - The per-session settings file passed with `--settings` registers command hooks:
     - `SessionStart`, `UserPromptSubmit`, `PermissionRequest`, `Notification`, `Stop`, `StopFailure`, `SessionEnd`
     - `PreToolUse` with matcher `AskUserQuestion`
   - Each hook appends its stdin JSON as one record to a per-session log in the adapter's state directory. Use `async: true` where allowed, so a hook never slows the session.
   - **No writes to `~/.claude/settings.json` or any project `.claude/` folder.** Installing hooks for observed sessions is out of scope (spine §7 lists hooks as Out; see open question 3).
   - The hook command must not need a new `ekumen` subcommand. If it can't be done without one, escalate.
4. **Status mapping.** Write it as a pure function with table tests. Inputs are the listing entry, the latest hook records and `now`. Each result carries `Source` (`listing`, `hooks` or `none`) and `ObservedAt`.

   | Signal | State | WaitingFor | Notes |
   |---|---|---|---|
   | listing `status: busy` | `running` | | |
   | listing `status: waiting`, `waitingFor: permission prompt` | `needs_input` | `permission` | |
   | listing `status: waiting`, `waitingFor: input needed` | `needs_input` | `question` | |
   | listing `status: waiting`, other `waitingFor` | `needs_input` | `other` | Put the raw value in `Detail`. |
   | listing `status: idle` | `idle` | | |
   | listing `state: working` | `running` | | |
   | listing `state: blocked` | `needs_input` | `other` | Unless hooks or `waitingFor` give a better reason. |
   | listing `state: done` | `idle` | | A finished turn that can take another prompt. |
   | listing `state: failed` or `stopped` | `done` | | `Detail` says which. |
   | hook `PermissionRequest`, or `Notification` `permission_prompt` | `needs_input` | `permission` | Dedupe the two (R2 §3 lesson 2) and keep the richer one, with `tool_name` in `Detail`. |
   | hook `PreToolUse` `AskUserQuestion`, or `Notification` `agent_needs_input` | `needs_input` | `question` | |
   | hook `Notification` `elicitation_dialog` | `needs_input` | `other` | |
   | hook `UserPromptSubmit` | `running` | | |
   | hook `Stop` | `idle` | | |
   | hook `StopFailure` or `SessionEnd` | `done` | | |
   | not listed and no hook data, or any parse error | `unknown` | | Never a guess (C-adapter §1). |

   **Precedence:**
   - The newest signal wins.
   - Liveness from the listing overrides a stale hook state. A hook `needs_input` newer than the last running signal is kept even if the listing says `busy`, which fails toward attention.
   - A listed session whose `kind`, `status` or `state` values are unknown to the adapter gives `unknown`, and the raw values go in `Detail`.
   - Until the hands-on check in the DoD passes, mark `needs_input` results from the listing alone with `Detail: "unverified signal"`.
5. **Fork** (invariant 10).
   - Run `claude -p --resume <id> --fork-session --output-format json` with `ForkReq.Prompt`. When `ReadOnly` is set, add `--permission-mode plan --permission-prompts none`.
   - It works for managed and observed sessions, and the original transcript is unchanged.
   - It returns the fork's `SessionRef` (from the result's `session_id`). The answer text is available through `Events` on the fork, or kept as the last result in the state directory. Choose one and document it.
   - **Idempotency by key:** if `--session-id` works with `--fork-session`, use a UUIDv5 of the key. Otherwise record key → fork id in the state directory before returning.
6. **Send** (`Message.Mode`):
   - `steer` returns `ErrUnsupported`.
   - `next_turn` to an observed session returns `ErrUnsupported`.
   - `next_turn` to a managed session:
     - When `idle` or `done`, run `claude --resume <uuid> --bg "<text>"`.
       - Exit 0 with the same ID means `delivered`.
       - A `note:` copy with a new ID, a non-zero exit after the process started, or a timeout means `uncertain`, with the reason in the error.
       - A failure before exec means `ErrUnavailable` (nothing was sent).
     - When `running` or `needs_input`, return `queued` with no side effect. If hands-on shows a running background session takes `--resume --bg` as its next turn, make running sessions `delivered` too, but `needs_input` always queues: an answer never goes into a prompt that is waiting (R2 §3 lesson 5).
     - When `unknown`, return `queued`.
   - **Idempotent by `Message.Key`:** the outcome is recorded in the state directory before the call returns, and a repeated key returns the recorded `Delivery` without sending.
   - **`uncertain` is never retried** (C-adapter §4).
7. **Resume:**
   - Resuming a managed session with no message is `claude respawn <id>` when it is stopped, and a no-op when it is live.
   - With a message, it is `Send` with mode `next_turn`.
   - Observed sessions return `ErrUnsupported`.
8. **Caps** (static per ownership, documented in the package README):

   | Op | New session | Managed | Observed |
   |---|---|---|---|
   | `list` | supported | supported | supported |
   | `status` | supported | supported | supported (`partial` until the hands-on check passes) |
   | `start` | supported | | |
   | `resume` | | supported | unsupported |
   | `send` | | partial | unsupported |
   | `fork` | | supported | supported |
   | `events` | | partial (hooks) | unsupported |

   `AttachObserved: false`.
9. **Process and credentials** (C-adapter §6, D-017):
   - Resolve the binary once to an absolute path. Use `EKUMEN_CLAUDE_BIN` if set, else `exec.LookPath("claude")`; aliases are never consulted.
   - `Available` runs `<bin> --version`, parses it, and returns `ErrUnavailable` below the minimum version the code needs. That is at least 2.1.259; derive the real minimum from the flags used, and state it.
   - Pass the environment through unchanged. Never set, read or log `ANTHROPIC_API_KEY`, `apiKeyHelper`, tokens or the keychain.
   - Never pass `--bare` or `--dangerously-skip-permissions`.
   - Run every process in the session's `Cwd`, with a context timeout.
10. **State directory:** `EKUMEN_STATE_DIR`, defaulting to `os.UserCacheDir()/ekumen/claude-code`. It holds the per-session settings files, hook logs, the key → session maps, and the delivery records. It is never under `.ekumen/`, since observations are not events (C-event-log §7 rule 2). Losing it degrades to `unknown` and re-lookup by name; it never duplicates a session.

**Job:**

1. **`internal/agent`**, generic and with no tool or vendor names (invariant 12):
   - The C-adapter §2 types and errors, verbatim.
   - String constants for states, ownerships, ops, support levels and delivery states.
   - A doc comment pointing to C-adapter.
2. **`internal/agent/conformance`**: the shared suite of C-adapter §7. It is a `Run(t, factory)` that takes an adapter plus a scripted fake of its tool. It covers:
   - each operation, and `ErrUnsupported` where `Caps` says unsupported
   - each §3 fallback trigger: `Send` to an observed session, `Fork` unsupported, status failing → `unknown`, `Start` unsupported
   - idempotent `Start`, `Send` and `Fork` by key
   - never retrying an `uncertain` delivery (the fake counts invocations)
   - `unknown` on missing or garbled data

   Prove the suite with a tiny in-package fake adapter, then run it against the Claude Code adapter.
3. **`internal/agent/claudecode`**: the adapter as designed above.
   - A pure `mapStatus` and a pure parser for `agents --json`, `--bg` output and hook records.
   - A thin runner interface around `exec.CommandContext`, so tests swap in a fake.
4. **The fake `claude`, so no test needs a real binary or the network.**
   - Use the test binary re-exec pattern: `TestMain` checks an env var and acts as `claude`.
   - It replays recorded fixtures from `testdata/<version>/` and appends its argv to a file, so tests assert the exact flags.
   - Fixtures:
     - `agents --json` with every documented `state`, `status` and `waitingFor` value, plus unknown values
     - `--bg` stdout, including the `note:` copy case
     - `-p --output-format json` results
     - one hook record per event type
   - Fixtures recorded from live runs are **sanitized**: no usernames, home paths, prompts or session content.
5. **Hands-on probe, opt-in.**
   - Add a test behind the build tag `claudelive` (or the env var `EKUMEN_CLAUDE_LIVE=1`) that runs the real binary in a temporary directory and records fresh fixtures. It is excluded from CI.
   - **Use it to confirm the needs-input signal** (C-adapter open item):
     - start a background session that hits a permission prompt and one that calls `AskUserQuestion`
     - record `claude agents --json` and the hook log for each
     - check the unverified items listed under Context
   - Clean up the sessions afterwards (`claude stop`/`rm`, `claude purge <tmpdir>`). Paste the commands and sanitized output into the handoff.
6. **Docs:** `internal/agent/claudecode/README.md` covers:
   - the verified versions and the minimum version
   - the capability table
   - the status mapping
   - what is documented and what was observed, with the URLs above
   - the state directory, and how to remove it

   Also add a contract-map row in `internal/core/README.md` only if it already lists adapter-level contracts; otherwise put the map in the package README. Don't edit `.ekumen/` contracts or the spine.

**Shape:** a short stack, each PR coherent and about 600 changed lines or fewer. Stack them with `restack`:
1. `internal/agent` types + `conformance` suite + in-package fake that passes it.
2. `claudecode`: binary resolution, `Available`, `Caps`, `List`, `Status` (listing + hook-log reader + `mapStatus`), fake binary, fixtures.
3. `claudecode`: `Start` (with hooks settings), `Resume`, `Send`, `Fork`, `Events`, the live probe, the README.

**Touches:** `internal/agent/` (new), `internal/agent/conformance/` (new), `internal/agent/claudecode/` (new, including `testdata/` and `README.md`). Nothing in `internal/core`, `internal/cli`, `internal/observe`, `internal/event`, `.ekumen/`, `skills/` or `docs/cli.md`.

**Definition of done:**
- [ ] `internal/agent` holds the C-adapter §2 types unchanged, and a test proves its non-test files contain no tool or vendor names: claude, codex, gemini, opencode, cursor, copilot, anthropic, openai (case-insensitive).
- [ ] The conformance suite passes against both the in-package fake and `claudecode`, and a deliberately broken fake (e.g. a `Send` that resends on a repeated key) fails it. Show that in the PR description.
- [ ] Table tests for `mapStatus` cover every row above, every documented `state`, `status` and `waitingFor` value, unknown values, stale versus fresh precedence, permission dedupe, and `unknown` on no data. Name them after the rule they enforce (e.g. `…_CAdapter1UnknownNeverGuess`).
- [ ] Fake-binary tests assert the exact argv for `Start`, `Send`, `Fork` and `Resume`. Every invocation uses the absolute binary path, never contains `--bare` or `--dangerously-skip-permissions`, and runs in the session's `Cwd`. `Start` and `Fork` with a repeated key make no second session, and a repeated `Send` key makes no second invocation. `uncertain` comes back on a `note:` copy, on a non-zero exit after start and on a timeout, and none of them is retried.
- [ ] Observed sessions: `Send`, `Resume` and `Events` return `ErrUnsupported`, `Fork` works read-only, and no file under `~/.claude` or a project `.claude/` is written (tests run with `HOME` set to a temporary directory and assert it stays empty apart from what the fake writes).
- [ ] **Hands-on check done on the installed `claude` (2.1.329 or newer):**
  - the permission-prompt and question cases are recorded as fixtures
  - each unverified item under Context is answered in the handoff
  - the mapping's "unverified signal" marking is removed only for the signals actually observed
- [ ] `go test -race ./...`, vet and gofmt are clean. CI is green on each draft PR, and `scripts/ci-check.sh` passes. CI never runs a real `claude`.
- [ ] Handoff recorded with `ekumen task handoff T-claude-adapter`, including:
  - the verified versions
  - the hands-on transcript (sanitized)
  - the proposed follow-up tasks (below)
  - the exact C-event-log text needed to add the C-adapter §5 session events (a minor version)
  - FRICTION

**Out of scope (follow-ups; propose them in the handoff):**
- **Wiring into the control loop:**
  - `reconcile` starting `ready` tasks with the brief's start prompt
  - writing `SessionStarted`, `SessionForked`, `MessageQueued`, `MessageDelivered` and `MessageUncertain`
  - delivering queued messages when a session goes `idle`
  - joining session status to the inbox, including the 10-minute `unknown` item

  These need the C-event-log §6 session events (a minor version) and touch `internal/core`/`internal/cli` while T-reconcile is still changing them. That is not trivially small, so it is a follow-up: "T-session-loop".
- Any `ekumen` CLI command (`session list/status`, a hidden hook command). Each one would change C-cli-json, so escalate.
- Installing hooks into user or project settings for observed sessions.
- Choosing the adapter per task (`agent:` in front matter, C-adapter "Open before freeze").
- The ACP driver and other tools' adapters; the dispatcher; `ekumen serve`.
- A host process that holds a `-p` stream-json stdin (open question 1).

**Escalate if** (raise with `/blocked`):
- Hands-on facts break the design: for example, `--bg` sessions can't take `--settings` hooks, `claude agents --json` doesn't list managed sessions reliably, or no idempotent `Start` is possible without a new `ekumen` command. Tier 2, `contract_impact: [C-adapter]` if the interface or fallbacks would change.
- `Send` to managed sessions can't reach `delivered` at all without the stream-json host. That bears on D-decision-delivery; raise it with the evidence, don't build the host.
- A required behaviour depends on an undocumented file (`~/.claude/sessions`, `~/.claude/jobs`) with no documented alternative. Keep it behind a fail-soft reader with fixtures, and raise it if the status mapping would depend on it.
- Anything would require reading, storing or passing credentials, or changing the lead's Claude Code settings.
- Keeping each PR to about 600 lines would mean dropping a DoD item.

**Start prompt:**

    export EKUMEN_ACTOR=session:t-claude-adapter
    Read .ekumen/spine.md (§5 rules 9–12, §6 agent layer, §7, §8), then `ekumen task show T-claude-adapter --json`
    (the brief, as amended). Read contracts C-adapter (all) and C-event-log (§3, §6, §7), research
    .ekumen/research/R1-acp.md (§3.3, §4, §6) and R2-landscape.md (§3, §4), and decisions D-claude-status-source,
    D-decision-delivery, D-managed-observed-sessions, D-claude-auth, D-adapter-base with `ekumen decision show`.
    Read the Claude Code docs pages cited in the brief's Context before using any flag.
    Work in .worktrees/t-claude-adapter on branch build/t-claude-adapter from main (the task record may show these
    empty until `ekumen task update` lands; the brief's front matter is authoritative).
    Use `ekumen` for every .ekumen/ write; never edit those files by hand. Never write to ~/.claude settings or a
    project .claude/ folder; clean up any live probe sessions you create.
    Report with `ekumen task handoff T-claude-adapter`, including a FRICTION section. Raise anything above your tier
    with /blocked (`ekumen decision raise`).

**FRICTION reporting:** the handoff's `FRICTION` section lists:
- what was painful in `ekumen` on this task: attach, handoff, and the placeholder record with empty fields and `tier_grants: []`;
- where C-adapter v1 was ambiguous for a real tool, for example:
  - `done` versus `idle` for a finished turn
  - `queued` versus `ErrUnsupported`
  - where a fork's answer text lives
  - ownership when no `SessionStarted` events exist
- every Claude Code behaviour you had to discover by trial rather than from the docs, with the version.

Each item names the field, flag or command involved, so the orchestrator can turn it into a backlog task.
