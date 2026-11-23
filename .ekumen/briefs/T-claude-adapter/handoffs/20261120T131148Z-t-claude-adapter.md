---
task: T-claude-adapter
author: t-claude-adapter
date: 2026-11-20
---

## STATUS

code complete in a 5-PR draft stack (#25-#29, all draft). The hands-on check of background sessions is only partly done: background sessions could not be started without changing the lead's Claude Code trust settings, so the permission-prompt and question cases are not recorded yet. The live probe is ready for the lead to run in a trusted folder (command below).

## PRs (stack order, each based on the previous)

- #25 https://github.com/Yashswarnkar/ekumen/pull/25 internal/agent types + conformance suite (+ the two raised decisions)
- #26 https://github.com/Yashswarnkar/ekumen/pull/26 claudecode listing/hook parsers + mapStatus
- #27 https://github.com/Yashswarnkar/ekumen/pull/27 claudecode Available, Caps, List, Status, fake claude
- #28 https://github.com/Yashswarnkar/ekumen/pull/28 claudecode Start (hook settings), Send, Resume
- #29 https://github.com/Yashswarnkar/ekumen/pull/29 claudecode Fork, Events, conformance run, live probe, README, this handoff
The brief's 3-PR shape became 5, to keep each one near 600 lines without dropping a DoD item: PR 2 of the brief is split into the pure status mapping and the adapter read side, and PR 3 into Start/Send/Resume and Fork/Events/probe/README.

## VERSIONS

verified on claude 2.1.329 (macOS). Minimum 2.1.259: `--permission-prompts` (Fork) needs 2.1.259 and `--resume <id> --bg` keeping the ID needs 2.1.257 (docs).

## VERIFIED HANDS-ON (2.1.329, own throwaway sessions only)

- `--fork-session` accepts `--session-id`: `claude -p --resume <U0> --fork-session --session-id <U2> --output-format json --permission-mode plan --permission-prompts none "<q>"` returned `session_id` = U2, `result` "OK", `permission_denials` []. Fork IDs are now a UUIDv5 of the key.
- `-p` sessions appear in `claude agents --json` while running, as `kind: interactive` with `pid`, `status` busy then idle, `sessionId` and an auto `name`; they drop out when they exit.
- `claude agents --json` shapes: background `cwd,id,kind,name,sessionId,startedAt,state` (no pid when the supervisor has stopped it); interactive `cwd,kind,name,pid,sessionId,startedAt,status`. `startedAt` is epoch ms. `id` = first 8 chars of `sessionId`. Values seen: background blocked/failed, interactive idle/busy. No `waitingFor` seen anywhere.
- `claude --bg` and `claude --resume <id> --bg` in an untrusted folder: stderr "Workspace not trusted. Run `claude` in <dir> once and accept the trust prompt, then retry.", exit 1, nothing started. Tried a $TMPDIR scratch folder and a gitignored folder under the repo; both untrusted. `-p` in the same folder works.
- Hooks passed with `--settings` did not run: this machine's managed policy sets `allowManagedHooksOnly` (`--debug-file` line "Skipping plugin hooks - allowManagedHooksOnly is enabled and no managed plugins"); the organisation's own managed hook ran.
- `claude respawn 0badf00d` → "No session matching '0badf00d'", exit 1; `claude stop 0badf00d` → same plus a hint, exit 1. `claude --version` → "2.1.329 (Claude Code)".

## UNVERIFIED ITEMS FROM THE BRIEF

- `--bg` accepts `--session-id`: not checked (trust). Start does not use it; it records the key before running and finds the session by name in `agents --json --all`.
- `--resume <uuid> --bg` on a running or blocked session: not checked (trust). `claude --help` says it "starts a copy and says so when the session is already running", so Send keeps queuing for running and needs_input sessions.
- exact `--bg` stdout: not checked; fixtures are `*.synthetic.txt` and the parser takes the first UUID, else an 8-hex id, plus any `note:` line.
- `-p` sessions in `agents --json`: verified, yes (above).
- `waitingFor` on interactive sessions: not seen in the lead's live sessions at probe time; no session was waiting.
- needs-input signal: not observed, so every listing-only needs_input keeps `Detail: "unverified signal"` (`verifiedListing` in status.go is empty).

## HANDS-ON TRANSCRIPT (sanitized; <P> is a mktemp folder under $TMPDIR, all removed)

  $ cd <P>/work && claude --bg --name ekumen-probe-ok --session-id 11111111-1111-4111-8111-000000000001 --settings <P>/settings.json "Reply with exactly OK and nothing else."
  Workspace not trusted. Run `claude` in <P>/work once and accept the trust prompt, then retry.   (exit 1)
  $ claude -p --session-id 11111111-1111-4111-8111-000000000000 --settings <P>/settings.json --output-format json "Reply with exactly OK and nothing else."
  {"type":"result","subtype":"success","is_error":false,"result":"OK","session_id":"11111111-1111-4111-8111-000000000000","permission_denials":[],...}   (exit 0; <P>/hooks stayed empty)
  $ claude -p --resume 11111111-...-000000000000 --fork-session --session-id 11111111-...-000000000002 --output-format json --permission-mode plan --permission-prompts none "What did you reply last time? Answer in one word."
  {"session_id":"11111111-1111-4111-8111-000000000002","result":"OK","permission_denials":[],"subtype":"success",...}
  while it ran, claude agents --json listed {"kind":"interactive","pid":…,"sessionId":"11111111-…-000000000002","name":"work-bf","status":"busy"} then "idle", then nothing
  $ claude --resume 11111111-...-000000000000 --bg "Reply OK"   → the same trust refusal, exit 1
  $ claude purge -y <P>/work   → "Purged 1 item" (the probe transcripts); rm -r <P>
  No background session was created, so there was nothing to stop or rm; `claude agents --json --all | grep -c ekumen-probe` → 0.
To finish the check, in a folder already trusted in Claude Code:
  EKUMEN_CLAUDE_LIVE=1 EKUMEN_CLAUDE_PROBE_DIR=<trusted folder> go test -tags claudelive -run TestLive -v ./internal/agent/claudecode
It starts ekumen-probe-* sessions (OK, permission prompt, AskUserQuestion), Sends and Forks, writes sanitized fixtures to testdata/live-<version>/, then stops and removes them.

## DECISIONS RAISED

- D-claude-bg-workspace-trust (tier 2): untrusted task worktrees make `--bg` refuse to start. Rec unsupported-then-paste (Start/Send return ErrUnsupported, so C-adapter §3 "start this task" / queue applies). Implemented that way; nothing waits on it.
- D-claude-hooks-policy-blocked (tier 2): managed policy can disable `--settings` hooks. Rec listing-primary-hooks-optional. Implemented that way; nothing waits on it.

## FOLLOW-UPS

- T-session-loop: wire the adapter into reconcile. Start ready tasks with the brief's start prompt (key = task id + approval event id), write the session events below, deliver queued messages when a session turns idle (re-using the message key), show `unknown` for over 10 minutes and ErrUnsupported starts as inbox items, and attach the session (`TaskSessionAttached {session, adapter: claude-code}`). Needs the C-event-log minor version below; touches internal/core and internal/cli.
- T-claude-live-check: the lead (or a session in a trusted folder) runs the live probe, commits the sanitized fixtures, moves observed values into `verifiedListing`, and flips observed `status` to supported if the needs-input signal shows up.
- Worktree trust bootstrap: depends on D-claude-bg-workspace-trust; if Claude Code honours trust on a parent folder, a one-time README step for the worktrees folder.
- Per-task adapter choice (`agent:` front matter) and the project `unknown` threshold, both still "open before freeze" in C-adapter.
- Optional: an `ekumen session list/status` CLI (changes C-cli-json, so tier 2).

## C-EVENT-LOG TEXT for the C-adapter §5 session events (minor version 5)

In §3 Lanes, add to the **loop** row: `SessionStarted`, `SessionForked`, `MessageQueued`, `MessageDelivered`, `MessageUncertain`; and to the **orchestrator** row: `SessionStarted`.
Replace the §6 line "**Session events:** reserved, defined with C-adapter (the agent adapter contract)." with:
  **Session** (C-adapter §5; status is never an event, §7 rule 2)

  | Type | `data` |
  |---|---|
  | `SessionStarted` | `task, adapter, session, ownership (managed\|observed), key` |
  | `SessionForked` | `from_session, session, adapter, key, for_question` |
  | `MessageQueued` | `key, to_session, from_event, reason` |
  | `MessageDelivered` | `key, to_session` |
  | `MessageUncertain` | `key, to_session, error` |

  The subject of every session event is the task the session works on. `key` is the idempotency key the adapter was called with; a second `SessionStarted` or `SessionForked` with the same `adapter` and `key` is a conflict (§5). A `MessageQueued` is superseded by a later `MessageDelivered` or `MessageUncertain` with the same `key`; `MessageUncertain` is final and is never followed by another delivery attempt with that key.
Add a "Version 5" section: "- Session events from C-adapter §5: `SessionStarted`, `SessionForked`, `MessageQueued`, `MessageDelivered`, `MessageUncertain` (loop lane; `SessionStarted` also orchestrator)."

## DEVIATIONS

- 5 PRs instead of 3 (sizes, above).
- Hook records are one file per record (`hooks/<key hash>/r.*`, written to a temp file and renamed), not appended lines in one log: concurrent async hooks could interleave partial lines, and the file time orders records.
- `ListFilter` and `SessionInfo` are named in C-adapter §2 but not defined; `internal/agent` defines them (`{Cwd, Ownership}` and `{Ref, Name, Status, StartedAt}`).
- Ownership comes from the adapter's state directory, not from `SessionRef.Ownership`, so a caller can't promote an observed session to managed.
- `queued` deliveries are not recorded, so a queued key can be delivered later; only `delivered` and `uncertain` are. `Send` pre-records `uncertain` before running, so a crash mid-send is never retried.
- `Resume(s, m)` with a message returns an error naming the outcome when it is not `delivered`, since its signature has no `Delivery`.
- A trust refusal on `Start` or `Send` is `ErrUnsupported`, not `uncertain`: it was observed to start and send nothing.
- The probe ran in a $TMPDIR folder with `-p` only; no background session was created.

## FRICTION

- ekumen: the task record still showed `branch`, `brief` null with a `pending_update` after rebasing onto main; the brief's front matter stayed authoritative. `ekumen decision raise` writes events into the current worktree, so on a stacked task the raised decisions had to be committed on the bottom PR to be visible early; a task session has no obvious "where do my .ekumen writes go" rule.
- ekumen: no way to record side effects of hands-on checks (sessions made, scratch paths, purge) except free text in the handoff.
- C-adapter: `ListFilter` and `SessionInfo` are used by §2 but not defined. `Resume(ctx, s, *Message) error` cannot report queued vs delivered. `queued` vs `ErrUnsupported` for an observed Send: both lead to the §3 queue fallback; the adapter returns ErrUnsupported (Caps says send unsupported). `done` vs `idle`: a background `state: done` session takes another prompt, so it maps to `idle`; `done` is for failed/stopped/ended. Where a fork's answer lives is unspecified: here, the state directory, exposed as a `message` event by `Events` on the fork. Ownership without `SessionStarted` events: the adapter's state directory is the only record, so losing it makes managed sessions look observed until restarted. `Start` cannot report "needs the lead first" (trust) except as ErrUnsupported.
- Claude Code 2.1.329, found by trial: background sessions refuse untrusted folders (exit 1, stderr text above); `allowManagedHooksOnly` silently disables `--settings` hooks; `startedAt` is epoch ms; `id` is the sessionId prefix; `-p` sessions are listed as `interactive`; `--fork-session` honours `--session-id`; `claude --help` documents that `--resume --bg` on a running session starts a copy.
- ekumen: `task handoff` needs Markdown headings (`## STATUS`); a `STATUS:` line is rejected with "sections found: (none)", which the brief and the handoff instructions do not say.
- Local: each exec of a Go test binary took about 1s of wall time on this Mac with no CPU use (likely endpoint security scanning), so the claudecode tests take about 3 minutes locally; CI on Linux is unaffected.
