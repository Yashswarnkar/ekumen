---
task: T-claude-adapter
author: t-claude-adapter
date: 2026-11-23
---

## STATUS

#32 fix-up run done: every Safety, Correctness and Privacy item fixed on the branch that introduced it, stack #25–#29 restacked onto main abdf20e and pushed; all five branches pass build, vet, gofmt, `go test -race ./...` and `scripts/ci-check.sh` locally.

## #32 ITEMS AND FIX COMMITS

Safety
1. Flag injection: fixed in 1024f65 (#28: Start, Send, Resume) and 38ca74b (#29: Fork). Free text always follows `--`; session ids must be a UUID or 8-hex short id before `--resume`/`respawn`. Verified hands-on on 2.1.330 with the absolute binary: `claude -p -- "--version"` sent "--version" as the prompt (without `--` it printed the version), and `-- agents` went to the model, not the agents subcommand. Table tests with flag-shaped prompts, messages, questions and ids.
2. Fork read-only by default: fixed in 38ca74b. Every fork runs `--permission-mode plan --permission-prompts none` regardless of `ForkReq.ReadOnly`. New live probe `TestLiveForkReadOnly` (ReadOnly=false, asks the fork to write a file): on 2.1.330 the fork answered "DENIED" and no file was written.

Correctness
3. Start/Fork key stuck Pending: fixed in 1024f65 (Start) and 38ca74b (Fork). A definite non-zero exit re-lists by name (Start) and otherwise drops the key; only a timeout or failed listing keeps it pending. Trust refusal matched on stdout+stderr.
4. Stale hook needs_input over live busy: fixed in 9fee4cd (#26). Listing liveness wins after `hookGrace` (1 min); a waiting/blocked listing still takes the hook's reason. Matches D-claude-hooks-policy-blocked (listing primary).
5. Conformance false-pass: fixed in 6ad7bae (#25). First calls must invoke the tool exactly once; distinct-keys Start check; CapsHonest (supported ops must not answer ErrUnsupported); observed Send must be ErrUnsupported or queued with no tool call; seven mutation fakes (resend, restart, refork, constant id, no tool, observed send, overclaim) each fail the named checks.
6. One mistyped listing entry: fixed in 9fee4cd. Per-element decode; bad entry → that session unknown; PID dropped; startedAt ms/seconds bounded to 2001..5138.
7. State writes: fixed in bee2a17 (#27). Unique temp + fsync + rename + dir fsync; exclusive claims (write then hard-link) for keys and deliveries; default dir $XDG_STATE_HOME or ~/.local/state, not the OS cache.
8. Bad hook file poisons status: fixed in 9fee4cd. Bad/oversized (>256 KiB)/foreign records skipped; pruned to newest 256.
9. Relative state dir: fixed in bee2a17. Made absolute in New; unresolvable → Available returns ErrUnavailable and writes fail.

Privacy
10. Sanitizer: fixed in 38ca74b. Allowlist (`sanitize_test.go`, tested in CI): listing entries keep adapter-read fields only; hook records keep enum fields and session id only. A fixture containing /Users/, /home/, the home dir, probe dir or user name is deleted and fails the probe.

Lower
- failed and stopped both → done: not changed; C-adapter has one terminal state and `Detail` says which. Listed as a C-adapter question below.
- Caps advertises stubbed ops: fixed in bee2a17/1024f65/38ca74b (each PR declares only what it implements).
- Status swallows cancellation: fixed in bee2a17 (returns the context error).
- Probe cleanup leak / short-id panic: fixed in 38ca74b (cleanup by `ekumen-probe-*` name in the listing; no slicing).
- README overstates "never a second session" for Send: fixed in 38ca74b.
- Retention: hook records pruned (9fee4cd), fork answers capped at 64 KiB (38ca74b); session/key/delivery record retention is a FOLLOW-UP.

## VERSIONS

claude 2.1.330 (macOS) for the `--` checks and the fork probe; earlier work verified on 2.1.329. Minimum unchanged: 2.1.259.

## HANDS-ON TRANSCRIPT (sanitized; <S> is a scratch folder, purged and removed)

  $ cd <S> && ~/.local/bin/claude -p --output-format json "--version"      → 2.1.330 (Claude Code)   (flag parsed)
  $ ~/.local/bin/claude -p --output-format json -- "--version Reply with exactly PONG"   → result "PONG"
  $ ~/.local/bin/claude -p --output-format json -- agents                 → a model reply, not the agents subcommand
  $ EKUMEN_CLAUDE_LIVE=1 go test -tags claudelive -run TestLiveForkReadOnly -v ./internal/agent/claudecode
    fork answer: "DENIED"; the fork could not write ekumen-fork-probe.txt (ReadOnly=false); PASS; `claude purge -y <tmp>` exit 0
  $ claude purge -y <S>   → Purged 1 item
No background sessions were created this run.

## DECISIONS RAISED

None new. D-claude-bg-workspace-trust (#30) and D-claude-hooks-policy-blocked (#31) still wait on the lead; the implemented recommendations are kept, and #31 now also has listing liveness beating stale hooks.

## FOLLOW-UPS

- Retention for the state dir's session, key, delivery and fork records (hooks are pruned already).
- C-adapter minor: say `ForkReq.ReadOnly` false is not honoured by adapters that fork read-only always, or drop it; consider a distinct terminal detail (`failed` vs `stopped`) in Status.
- The background-session live probe (`TestLive`) still needs a trusted folder; unchanged from the 2026-11-20 handoff (T-claude-live-check).
- T-session-loop as proposed in the 2026-11-20 handoff.

## DEVIATIONS

- Default state dir is $XDG_STATE_HOME/~/.local/state, not the brief's `os.UserCacheDir()` (#32 item 7).
- Forks are always read-only; `ForkReq.ReadOnly=false` is ignored (#32 item 2).
- Hook needs_input outranks a running listing only for 1 minute (brief said "newer than the last running signal"), per #32 item 4 and the amendment.

## FRICTION

- The lead's shell aliases `claude` to `claude code`, so hands-on checks through the alias silently passed "code" as a prompt and gave misleading results ("Input must be provided…"). Use the absolute binary for probes (the adapter already does).
- `--` behaviour is not in the CLI reference; found by trial on 2.1.330.
- origin/main moved mid-run (#55–#67); a local `ci-check.sh` with EKUMEN_CI_BASE=origin/main then reports main's new events as "deleted" on the stack — the message could say the base is ahead of the branch.
- Test binary exec is ~1 s per call on this Mac, so the claudecode package takes ~2–5 min locally.
