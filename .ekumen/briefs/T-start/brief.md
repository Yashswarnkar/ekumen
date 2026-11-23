---
id: T-start
title: "Environment setup: ekumen init and ekumen task start"
kind: build
contracts_consumed: [C-event-log, C-cli-json, C-adapter]
contracts_produced: []
blocked_by: []
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-start · Environment setup: `ekumen init` and `ekumen task start`

**Goal:** v1 capability 4 (spine §7), GitHub **#40**. The lead's workflow is one terminal per brief (spine §8). Today the orchestrator sets each one up by hand: worktree, branch, session attach, CLI build, a prompt file, and the exact command to paste. `ekumen task start <id>` must do all of that. A new project has no way to begin; `ekumen init` creates one.

**Context (verify against origin/main when you start):**
- The manual recipe this replaces is in `.worktrees/*.prompt.txt` and spine §8. For each task the orchestrator ran:
  1. `git worktree add .worktrees/<task> -b <branch> origin/<base>`
  2. `ekumen task attach <task> --session <session>` (orchestrator lane)
  3. wrote a prompt file containing setup lines plus the brief's **Start prompt**
  4. gave the lead `cd .worktrees/<task> && <agent command> "$(cat ../<task>.prompt.txt)"`
- Task records hold `repo`, `base_branch`, `branch`, `worktree`, `session` and `brief` (C-event-log; `ekumen task update` exists).
- Spine rule 12: **no agent tool name may be hard-coded outside the adapter layer.** The command that starts a session comes from project config, e.g. `.ekumen/config.yaml`:

  ```yaml
  project: ekumen
  lead: Yashswarnkar
  agents:
    default: claude-code
    claude-code: {command: "~/.local/bin/claude"}
  setup: ["go build -o bin/ekumen ./cmd/ekumen"]   # run in each new worktree
  ```

  `ProjectName` already reads `project:` from this file. Extend the file; don't invent a second one.

**Job:**
1. **`ekumen init [--project name] [--lead handle]`** in a git repo with no `.ekumen/`:
   - creates the layout (`events/`, `briefs/`, `contracts/`, `decisions/`, `research/`)
   - creates `spine.md` from a short template with the section headings and placeholder text
   - creates `config.yaml`
   - adds `.ekumen/registry.yaml`, `.ekumen/state.json` and `.worktrees/` to `.gitignore`

   It refuses if `.ekumen/` already exists. It writes nothing outside the repo and doesn't commit; it prints the next steps.
2. **`ekumen task start <id> [--open] [--agent name]`**, orchestrator lane:
   - **Check:** the task is approved and has no open blockers. Otherwise, explain what's missing (approval, blockers, empty location fields) and exit 2.
   - **Worktree:** create the branch from `origin/<base_branch>` and the worktree at the recorded path, or reuse them if they already exist and are clean. Never touch a dirty worktree.
   - **Attach:** run `ekumen task attach` with the recorded session, but only if it isn't attached already.
   - **Setup:** run the `setup` commands from config inside the worktree.
   - **Prompt file:** write `.worktrees/<id>.prompt.txt`, containing `EKUMEN_ACTOR=session:<session>`, the setup reminder, and the brief's **Start prompt** block, extracted verbatim.
   - **Print** the one command the lead pastes into a new terminal.
   - **`--open`** opens a new terminal window running that command: macOS Terminal via `osascript` only. Elsewhere, print the command and say `--open` isn't supported on this platform.
   - **Idempotent:** running it twice changes nothing and prints the same command.
3. **`ekumen task start --list`:** every approved, unblocked task that has no worktree yet. These are the tasks the lead could open now.
4. Document all of this in `docs/cli.md` and the README's "Try it" section.

**Touches:** `internal/cli/` (new `init.go` and `start.go`), `internal/project/` (config reading), the embedded spine template, `docs/cli.md`, `README.md` (Try it only), tests (with temp git repos), your handoff files.

**Definition of done:**
- [ ] `ekumen init` in an empty temp repo produces a project on which `ekumen check` is clean
- [ ] `ekumen task start` on a temp project creates the worktree and branch, attaches the session, writes the prompt and prints the command. A second run is a no-op. An unapproved or blocked task is refused with a clear reason.
- [ ] On this repo, `ekumen task start --list` shows what's actually startable
- [ ] No agent tool name in `internal/` outside config defaults that a test documents as defaults
- [ ] `go test -race ./...`, vet and gofmt clean; CI green (main is protected)
- [ ] Handoff via `ekumen task handoff T-start`, including FRICTION

**Out of scope:** the loop starting sessions by itself (#44, T-dispatch); multi-project registry (#45); any agent tool's own settings, including `~/.claude`.

**Escalate if:** the config file shape needs a contract (it may belong in C-cli-json), or `--open` can't be done safely. Ask the lead in your terminal.

**Start prompt:**

    export EKUMEN_ACTOR=session:t-start
    Read .ekumen/spine.md (§5 rule 12, §7, §8), then `bin/ekumen task show T-start`, then GitHub issue #40,
    then contracts C-cli-json and C-adapter §1 and §6. Look at .worktrees/*.prompt.txt: that is the manual recipe you're replacing.
    Use `ekumen` for every .ekumen/ write. Report with `bin/ekumen task handoff T-start`, push your branch, and open a draft PR
    titled "T-start: …". Ask the lead in this terminal before anything outward-facing beyond that.
