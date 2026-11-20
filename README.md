# Ekumen

**Coordination for one engineer leading many coding agents.**

Agents do the work. Ekumen turns what they need from you into **decisions** with their background attached, keeps every task's state in a **git-native event log**, and runs a **control loop** that moves work forward once you decide.

> **Status: early, and used to build itself.** The `ekumen` CLI and a one-pass control loop work today. This repository's own design, tasks and decisions live in [`.ekumen/`](.ekumen) and are managed with `ekumen`. There is no `ekumen init` yet, so starting a new project means copying the layout by hand. The web UI and automatic session start are still to come.

## The problem

Run 5–20 coding-agent sessions on one project, with tools like Claude Code, Codex or Gemini CLI in parallel worktrees, and four things break:

1. **Visibility.** Any session may be waiting on you, and finding which one means scanning windows.
2. **Lost background.** A decision needs background, and asking for it in the session that raised it derails that session.
3. **Idle waiting.** Once you decide, the work that depends on it waits for you to notice.
4. **Drift.** Status lives in prose that goes stale, and agents reason confidently over it.

Most of that waiting is for **permission, not judgment**.

## How Ekumen works

| Idea | What it means |
|---|---|
| **Decisions are gates** | A choice above a session's tier becomes a record: question, options, recommendation and a 150-word context. It is ranked by how much work it holds up. You answer it once; the loop applies it to every affected brief. |
| **Tiers** | Tier 0: routine actions pre-approved per task (push own branch, open a draft PR). Tier 1: a session's choice with a default that applies at a deadline. Tier 2: only you decide. |
| **One owner per document** | Every record has one writer. Everyone else appends to their own *lane* (a handoff, a raised decision, a question), and the loop routes it to the owner's inbox. Write conflicts become routing, not merge conflicts. |
| **Event log as truth** | Every change is an append-only JSON event in `.ekumen/events/`, one file each, so git never conflicts. Status, priority and the task graph are computed from it, never typed. |
| **Control loop** | `ekumen reconcile` compares what was decided with what git and pull requests show. It takes only pre-authorised actions (apply decisions, route handoffs, apply tier-1 defaults, raise decisions on conflicts) and reports the rest. |
| **Code over tokens** | Anything deterministic is code: status, priority, routing, validation, graph layout. Agents are used only for judgment. |
| **Agent-agnostic** | Everything above talks about sessions and capabilities. Tool-specific behaviour lives only in adapters; the first, for Claude Code, is in progress. |

## Try it

You need Go 1.26+ (Node only if you want the UI build).

```bash
git clone https://github.com/Yashswarnkar/ekumen && cd ekumen
go build -o bin/ekumen ./cmd/ekumen

bin/ekumen status                  # tasks by status, decisions open, your inbox
bin/ekumen inbox --for lead        # what is waiting on you, ranked, with the next command to run
bin/ekumen decision show D-control-loop
bin/ekumen task show T-reconcile   # a brief with its amendments and handoffs
bin/ekumen reconcile --dry-run     # what one pass of the control loop would do
bin/ekumen map --out /tmp/map.html && open /tmp/map.html   # the build map, offline
bin/ekumen check                   # validate every record and run the conflict rules
```

Writes need an identity, e.g. `--as lead:<you>` or `EKUMEN_ACTOR=session:<name>`, and are refused outside that role's lane. Every command takes `--json`.

For agent sessions, install the `/blocked` and `/decide` skills with `bin/ekumen skills install --dir .claude/skills`. See [docs/skills.md](docs/skills.md).

## Documentation

- [docs/cli.md](docs/cli.md): every command, with examples, including which files their owners may edit by hand
- [docs/skills.md](docs/skills.md): agent skills for raising and answering decisions
- [docs/development.md](docs/development.md): building, testing, CI and the embedded UI
- [`.ekumen/spine.md`](.ekumen/spine.md): the design and its reasons
- [`.ekumen/contracts/`](.ekumen/contracts): the frozen interfaces (event log, decision record, CLI JSON, agent adapter)

## Built with itself

Ekumen has been developed through Ekumen since the first commit, by hand before the CLI existed and with `ekumen` since then:

- **[Decisions](.ekumen/decisions):** every design choice, including who raised it, the options, and your answer.
- **[Briefs](.ekumen/briefs):** each task's scope, plus what its agent reported back.
- **[Research](.ekumen/research):** what was learned before building.
- **CI:** `ekumen check` runs on every pull request, so a record that breaks the rules can't merge.

## Roadmap

1. ~~Event log, CLI, import of hand-written records~~ (done)
2. ~~Control loop, one pass on demand~~ (done); hardening in progress (atomic writes, role takeover, lane history)
3. **Roles and rules:** declare reusable agent roles (reviewer, auditor, tester) and when they run, e.g. "every build PR gets a reviewer"
4. **Agent adapters:** Claude Code first, then ACP-based tools, so the loop can start sessions and deliver answers
5. **Web UI:** task graph, decision inbox and decision screen, served by `ekumen serve`
6. `ekumen init`, packaging, more agent tools

## The name

From Ursula K. Le Guin's Hainish books: the Ekumen is a federation that coordinates autonomous worlds by consent, without ruling them. Your sessions keep their own agency; Ekumen keeps them moving.

## License

[Apache-2.0](LICENSE)
