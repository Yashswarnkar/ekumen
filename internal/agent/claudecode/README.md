# claudecode: the Claude Code adapter

Implements C-adapter v1 (`.ekumen/contracts/C-adapter.md`) for Claude Code. Adapter name: `claude-code`.

## Versions

- **Minimum:** 2.1.259, derived from the flags used: `--permission-prompts` (Fork) needs 2.1.259, and `claude --resume <id> --bg` keeping the same ID needs 2.1.257. `Available` refuses older versions with `ErrUnavailable`.
- **Verified against:** 2.1.329 (macOS, 2026-11-20) and 2.1.330 (macOS, 2026-11-23: `--` handling and the read-only fork). What was verified hands-on and what is taken from the docs is listed below.

## Capabilities

| Op | New session | Managed | Observed |
|---|---|---|---|
| `list` | supported | supported | supported |
| `status` | supported | supported | partial |
| `start` | supported | unsupported | unsupported |
| `resume` | unsupported | supported | unsupported |
| `send` | unsupported | partial | unsupported |
| `fork` | unsupported | supported | supported |
| `events` | unsupported | partial (hooks, fork answer) | unsupported |

`AttachObserved: false`. A session is **managed** only if this adapter's state directory records that it started or forked it; the ownership a caller puts in a `SessionRef` is not trusted. Everything else in `claude agents --json` is **observed** and read-only: no keystroke or PTY injection, ever.

Contract map:

| C-adapter | Here |
|---|---|
| §2 `Start` | `claude --bg --name ekumen-<task>-<key hash> --settings <state>/settings/<key hash>.json -- <prompt>` in `StartReq.Cwd`. The key is claimed exclusively before the run. A repeated key returns the recorded session or looks it up by name. If `--bg` exits non-zero and nothing is listed by that name, the key is dropped and a repeat starts again. Only after a timeout does the key stay pending; a repeat then fails with `ErrNotFound` until the session is listed, and it is never started twice. |
| §2 `Send` | `next_turn` to an idle or done managed session: `claude --resume <uuid> --bg -- <text>` in the session's folder. Running, needs-input and unknown sessions get `queued` with no side effect. `steer` and observed sessions get `ErrUnsupported`. |
| §2 `Fork` | `claude -p --resume <id> --fork-session --session-id <uuidv5(key)> --output-format json --permission-mode plan --permission-prompts none -- <prompt>` in the session's folder. **Every fork is read-only**, whatever `ForkReq.ReadOnly` says: a side question never needs to change the worktree, and the zero value must not be the unsafe one. The live probe `TestLiveForkReadOnly` asks a fork to write a file and checks it could not. The answer (up to 64 KiB) is kept in the state directory and returned as a `message` event by `Events` on the fork. A fork that exits non-zero drops its key; a timed-out one stays pending. |
| §2 `Resume` | No message: `claude respawn <short id>` when the listing says `stopped`, otherwise a no-op. With a message: `Send`, and any outcome other than `delivered` is an error. |
| §3 start fallback | A folder Claude Code does not trust makes `--bg` exit 1 with "Workspace not trusted"; `Start` and `Send` return `ErrUnsupported` (nothing was started or sent), so the loop shows "start this task" or queues (D-claude-bg-workspace-trust). |
| §4 delivery | The delivery record is written as `uncertain` before the run and replaced by the outcome, so a crash mid-send is never retried. `uncertain` comes from a `note:` line or a different session ID in the output (a copy was started), a non-zero exit, or a timeout. Only `delivered` and `uncertain` are recorded; `queued` is not, so the same key can be delivered later. |
| §6 | The binary is `EKUMEN_CLAUDE_BIN` or `claude` on `PATH`, resolved once to an absolute path. Every process gets the caller's environment unchanged and a timeout. Free text (prompt, message, question) always follows `--`, so text such as `--permission-mode bypassPermissions` is never read as a flag. On 2.1.330, `claude -p -- "--version"` sent `--version` as the prompt, and `-- agents` was a prompt, not the subcommand. Session IDs must be a UUID or an 8-hex short id before they reach `--resume` or `respawn`. `--bare` and `--dangerously-skip-permissions` are never passed, and the permission mode is Claude Code's default (D-session-delivery-bg). |

## Status mapping

`mapStatus` (`status.go`) takes the listing entry, the session's hook records and `now`. `Source` is `listing`, `hooks` or `none`.

| Signal | State | WaitingFor | Detail |
|---|---|---|---|
| listing `status: busy`, or no `status` and `state: working` | running | | |
| listing `status: waiting`, `waitingFor: permission prompt` | needs_input | permission | `unverified signal` |
| listing `status: waiting`, `waitingFor: input needed` | needs_input | question | `unverified signal` |
| listing `status: waiting`, any other `waitingFor` | needs_input | other | `unverified signal; waiting: <raw>` |
| listing `status: idle`, or no `status` and `state: done` | idle | | |
| listing no `status` and `state: blocked` | needs_input | other | `unverified signal; blocked` |
| listing no `status` and `state: failed` or `stopped` | done | | the state |
| hook `PermissionRequest`, or `Notification` `permission_prompt` | needs_input | permission | tool name (the twins are merged) |
| hook `PreToolUse` `AskUserQuestion`, or `Notification` `agent_needs_input` | needs_input | question | |
| hook `Notification` `elicitation_dialog` | needs_input | other | `elicitation_dialog` |
| hook `UserPromptSubmit` / `Stop` / `StopFailure`, `SessionEnd` | running / idle / done | | |
| unknown `kind`, `status` or `state` | unknown | | the raw values |
| not listed and no hooks, or the listing or a hook record fails or is garbled | unknown | | why |

Precedence: when the process is alive, the listing's `status` is used over `state`. The listing is read now, so it is primary (D-claude-hooks-policy-blocked). A newest hook `needs_input` is used only (a) when the listing also says needs-input, since the hook names what is awaited, or (b) for one minute (`hookGrace`) while the listing says running or idle, in case the listing has not caught up yet. After that, listing liveness wins: answering a permission prompt fires no hook, so an older `needs_input` says nothing about now (#32 item 4). A session that is not listed is `done` only if its last hook says so, otherwise `unknown`. A listing element that does not decode makes only that session `unknown`. Hook records that are too large (>256 KiB), unreadable or for another session are skipped; beyond the newest 256, records are pruned.

**`unverified signal`:** no permission prompt or question has been observed in `claude agents --json` yet (see below), so every listing-only `needs_input` carries it. Once the live probe records a value, add it to `verifiedListing` in `status.go`.

## Documented, observed, assumed

Docs: [CLI reference](https://code.claude.com/docs/en/cli-reference), [Agent view](https://code.claude.com/docs/en/agent-view), [Sessions](https://code.claude.com/docs/en/sessions), [Hooks](https://code.claude.com/docs/en/hooks), [Headless](https://code.claude.com/docs/en/headless).

Observed on 2.1.329:

- `claude agents --json` keys: background `cwd, id, kind, name, sessionId, startedAt, state`; interactive `cwd, kind, name, pid, sessionId, startedAt, status`. `startedAt` is epoch milliseconds. `id` is the first 8 characters of `sessionId`. Values seen: background `blocked` and `failed` (no `pid`), interactive `idle` and `busy`. No `waitingFor` seen.
- `claude -p` sessions are listed while they run, as `kind: interactive` with `pid`, `status` and an auto-generated `name`, and drop out when they exit.
- `--fork-session` accepts `--session-id`, so a fork's ID is chosen by the caller (the result's `session_id` matched).
- `claude --bg` and `claude --resume <id> --bg` in an untrusted folder print `Workspace not trusted. Run \`claude\` in <dir> once and accept the trust prompt, then retry.` on stderr, exit 1, and start nothing. `-p` runs in the same folder work.
- With a managed policy that sets `allowManagedHooksOnly`, hooks passed with `--settings` do not run at all (`--debug-file` logs "Skipping plugin hooks - allowManagedHooksOnly is enabled"). The adapter then works from the listing alone (D-claude-hooks-policy-blocked).
- `claude respawn` and `claude stop` with an unknown id print `No session matching '<id>'` and exit 1.

Assumed, not yet observed (the probe could not start background sessions in a trusted scratch folder without changing the user's trust settings): the `--bg` stdout format (fixtures `*.synthetic.txt`; the parser takes the first UUID, else an 8-hex id, and any `note:` line), whether `--bg` accepts `--session-id` (not used: `Start` finds sessions by name), what `--resume <id> --bg` does to a running or blocked session (`Send` queues instead), `waitingFor` on interactive sessions, and the hook record shapes (fixtures follow the hooks docs).

The read-only fork probe needs no trusted folder:

```
EKUMEN_CLAUDE_LIVE=1 go test -tags claudelive -run TestLiveForkReadOnly -v ./internal/agent/claudecode
```

To check the rest, run the background-session probe in a folder you have already trusted in Claude Code:

```
EKUMEN_CLAUDE_LIVE=1 EKUMEN_CLAUDE_PROBE_DIR=<trusted folder> \
  go test -tags claudelive -run 'TestLive$' -v ./internal/agent/claudecode
```

It starts sessions named `ekumen-probe-*`, then stops and removes every `ekumen-probe-*` session in the folder, found by name, including one whose `Start` reported an error. Afterwards `claude purge <folder>` deletes their transcripts. It writes fixtures to `testdata/live-<version>/` through an allowlist (`sanitize_test.go`): listing entries keep only the fields the adapter reads, and hook records keep only enum-valued fields and the session id. Every other field is dropped, including `transcript_path`, `cwd`, `message`, `error` and `prompt`. A fixture that still contains `/Users/`, `/home/`, the home folder, the probe folder or the user name is deleted and the probe fails.

## State directory

`EKUMEN_STATE_DIR`, else `$XDG_STATE_HOME/ekumen/claude-code`, else `~/.local/state/ekumen/claude-code`. It is made absolute when the adapter is created. It is a state directory, not a cache: key and delivery records must survive cache purges. Records are written to a temporary file, fsynced and renamed; new keys are claimed exclusively. It holds:

- `sessions/<session id>.json`: managed sessions and fork answers
- `keys/<start|fork>-<key hash>.json`: idempotency keys, written before the tool runs
- `deliveries/<key hash>.json`: Send outcomes
- `settings/<key hash>.json`: each managed session's `--settings` file (hooks only)
- `hooks/<key hash>/r.*`: one hook input per file

Nothing is written to `~/.claude`, a project's `.claude/` folder or `.ekumen/`. Deleting the directory (`rm -r` it) loses the adapter's memory: managed sessions become observed (so `Send` returns `ErrUnsupported` for them), and status falls back to the listing. `Start` finds existing sessions again by name, so it never starts a second session. Delivery and fork records are lost, though, so a `Send` or `Fork` key that is repeated after the deletion is not recognised. Only delete it when no messages are in flight. Retention: hook records are pruned to the newest 256 per session; session, key, delivery and fork records are kept until the directory is removed (a follow-up).

## Tests

The test binary doubles as a fake `claude` (`fake_test.go`, `fakeact_test.go`): it logs argv and working folder and replays `testdata/2.1.329/`. `agents-observed.json`, `fork-result.json` and `trust-refused.txt` are sanitized recordings; `agents-documented.json` covers every documented value plus unknown ones; `hooks/` and `*.synthetic.txt` follow the docs. Tests set `HOME` to an empty folder and check it stays empty. CI never runs a real `claude`.
