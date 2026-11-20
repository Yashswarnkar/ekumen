# Ekumen

**Coordination for one engineer leading many coding agents.**

Agents do the work. Ekumen turns what they need from you into decisions with their background attached, and moves the work forward once you decide.

> **Status: designing.** No code yet. Read [VISION.md](VISION.md) for where this is going and [SPEC.md](SPEC.md) for the starter design.

## The problem

Run 5–20 coding-agent sessions on one project (Claude Code, Codex, Gemini CLI, in parallel worktrees) and four things break:

1. **Visibility.** Any session may be waiting on you, and finding which means scanning windows.
2. **Background gets lost.** A decision needs background, and asking for it in the session that raised it derails that session.
3. **Idle waiting.** Once you decide, the work that depends on it waits for you to notice and start it.
4. **Drift.** Status lives in prose across documents that disagree, and agents reason confidently over stale state.

Most of that waiting is for **permission, not judgment**.

## The idea

- **Tier grants** pre-approve routine actions per task (push its own branch, open a draft PR, rebase), removing most permission waits.
- **Decisions are gates in the task graph.** Each comes with its question, options, recommendation and a short context packet, and is ranked by how much work it holds up.
- **Ask without derailing.** Questions about a decision go to a fork of the session that raised it.
- **Deciding moves work forward.** Answering amends the affected task briefs, clears blocks, and notifies or starts sessions.
- **Code over tokens.** Status, priority, graph layout, routing and validation are deterministic software. Agents are used only for judgment.
- **Agent-agnostic and git-native.** Plain files in `.ekumen/`, an append-only event log, and adapters per agent tool. A database and sync come later.

## The name

From Ursula K. Le Guin's Hainish books: the Ekumen is a federation that coordinates autonomous worlds by consent, without ruling them. Your sessions keep their own agency; Ekumen keeps them moving.

## Built with itself

This repo's own design decisions live in [`.ekumen/decisions/`](.ekumen/decisions), in the format the tool will manage.

The `/blocked` and `/decide` agent skills are the day-one way in: see [docs/skills.md](docs/skills.md).

The `/blocked` and `/decide` agent skills are the day-one way in: see [docs/skills.md](docs/skills.md).

## License

[Apache-2.0](LICENSE)
