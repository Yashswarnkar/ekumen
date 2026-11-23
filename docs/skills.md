# Skills

Ekumen ships three agent skills: `/blocked`, `/decide` and `/brief`. They are plain `SKILL.md` files in the [Agent Skills](https://agentskills.io) format, thin wrappers around `ekumen … --json` ([C-cli-json](../.ekumen/contracts/C-cli-json.md)). The agent supplies judgment (the question, the options, a recommendation, a short context summary); every read and write is a CLI call. The sources are in [`skills/`](../skills), and each `ekumen` binary embeds the skills that match its commands.

| Skill | Who uses it | Actor | What it does |
|---|---|---|---|
| [`/blocked`](../skills/blocked/SKILL.md) | A task session, about its own task | `session:<name>` (`EKUMEN_ACTOR` or `--as`); the session must be attached to the task | Checks whether the choice needs a decision at all (tier grants, an existing pending decision), drafts a C-decision-record, raises it, and keeps working. Later answers the lead's questions about its decisions and picks up applied ones from `brief.amendments`. |
| [`/brief`](../skills/brief/SKILL.md) | An orchestrator session, when the lead asks for a brief or its inbox shows a `brief` action | `orchestrator:<session>`, holding the orchestrator claim | Reads the spine scope and the issue and verifies context against main, scaffolds with `ekumen brief new` (or `--existing`), guides filling each section, runs `ekumen brief check` until it passes, and tells the lead the task is ready for `task approve`. Never approves. |
| [`/decide`](../skills/decide/SKILL.md) | The lead, or an orchestrator session on the lead's explicit word | `lead:<handle>` to answer; `loop:<this session>` to apply | Picks a decision from the lead's inbox, shows it in a fixed order, answers follow-up questions from the record or queues them to the raising session, records the lead's answer, applies it, and prints the line to send to each blocked session. |

No skill commits. Writes print the files they touched (`paths`); commit them yourself.

## Install

```
ekumen skills list
ekumen skills install --dir <skills folder>
```

`install` writes each skill to `<dir>/<name>/`. Files that are already identical are left alone; a file that differs is refused (exit 2, naming it) unless you pass `--force`. It needs no actor and writes nothing under `.ekumen/`. It never guesses a folder: pass the one your agent tool reads.

Skills folders, as each tool documented them when this was written (check your tool's docs if a path doesn't load):

| Tool | User level | Project level |
|---|---|---|
| Claude Code | `~/.claude/skills` | `.claude/skills` |
| Codex CLI | `~/.codex/skills` | `.codex/skills` |
| Gemini CLI | `~/.gemini/skills` | `.gemini/skills` |
| OpenCode | `~/.config/opencode/skill` | `.opencode/skill` |
| Cursor | `~/.cursor/skills` | `.cursor/skills` |
| GitHub Copilot | `~/.copilot/skills` | `.github/skills` |

For example, for every session in this repository with Claude Code: `ekumen skills install --dir .claude/skills`.

## Without skills support

The skills are checklists over the CLI, so any agent (or person) can follow them by hand. Point the agent at the `SKILL.md` text, or run the steps yourself:

**As a task session (`/blocked`):**

1. `ekumen task show <task> --json`: is the action in `task.tier_grants`? Then just do it.
2. `ekumen decision list --status pending --json`: is it already asked?
3. Fill in the template in [`skills/blocked/SKILL.md`](../skills/blocked/SKILL.md) and pipe it to `ekumen decision raise --file - --json`.
4. Keep working. Later: `ekumen question list --for <session> --open --json`, answer each with `ekumen question answer <qid> --text … --json`, and check `ekumen decision show <id> --json` for `applied_on`.

**As the lead (`/decide`):**

1. `ekumen inbox --for lead --json`, then `ekumen decision show <id> --json`.
2. Ask the raising session: `ekumen question ask <id> --text … --json` (no `--to`), and tell that session to run step 4 above.
3. `ekumen decision answer <id> <option|other> --note … --as lead:<handle> --json`.
4. `ekumen decision apply <id> --as loop:<session> --json`, then tell each session in `blocks` to read its new amendment.

**As the orchestrator (`/brief`):**

1. Read the spine scope and the issue; check the facts you will cite against main.
2. `ekumen brief new <id> --title … --from-issue N --json` (or `ekumen brief new <id> --existing --json` for a task drafted without a brief).
3. Replace every `TODO(brief)` line in `.ekumen/briefs/<id>/brief.md`, following the section guidance in [`skills/brief/SKILL.md`](../skills/brief/SKILL.md).
4. `ekumen brief check <id> --json` until `ok` is true, then tell the lead it is ready for `ekumen task approve <id>`.

## Tests

`internal/cli/skills_test.go` keeps the skills honest: front matter in the Agent Skills set, every `ekumen` line in a fenced block runs with `-h` and has `--json`, no generated `.ekumen/` paths and no agent tool or vendor names in a `SKILL.md`, and the whole raise → question → answer → decide → apply flow on a fixture, using the template from `/blocked`.
