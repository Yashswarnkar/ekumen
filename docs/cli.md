# `ekumen` CLI

The CLI is how people, agents and scripts read and change Ekumen state. The command set and the `--json` shapes are fixed by the contract [C-cli-json](../.ekumen/contracts/C-cli-json.md); this page shows how to use them. The JSON is the stable interface. The plain-text output is for people and may change.

## Conventions

| | |
|---|---|
| Project root | The nearest parent directory containing `.ekumen/`, or `--root <dir>`. |
| Who is acting | `--as <role>:<id>` or `EKUMEN_ACTOR`, e.g. `session:t-core`, `orchestrator:main`, `lead:Yashswarnkar`. Roles: `lead`, `orchestrator`, `session`, `integrator`, `loop`. Every write needs it. |
| `--json` | Prints exactly one JSON object on stdout. Warnings go to stderr. |
| Flags | Global flags (`--root`, `--as`, `--json`) go anywhere. Flags and positional arguments may be mixed. |
| Files | `--file` and `--text-file` take a path, or `-` for stdin. |
| Ids | An id or an alias (`C1`, `D-012`) is accepted anywhere; output always uses the id. Question and route ids also take a unique prefix, in any case (`q-01m7w3`); an ambiguous prefix is a usage error (exit 1) that lists the matches. |
| Paths | Every path the CLI prints (`paths` of a write, `Entry.path`, `Task.brief`, `check`'s `invalid[].path`, `map`'s output) is relative to the project root, e.g. `.ekumen/briefs/T-core/brief.md`. Paths recorded inside events (`file`, `path`, `brief`) and in `registry.yaml` stay relative to `.ekumen/`, as the event log records them. |
| Exit codes | `0` ok · `1` usage · `2` refused (validation, lane, or role claim) · `3` not found · `4` conflict · `5` internal. |
| Errors | With `--json`: `{"error": {"code": "lane\|validation\|not_found\|conflict\|usage\|internal", "message": "…", "field": "…"}}`. |
| Writes | Print `{"ok": true, "events": [ids], "paths": [files written]}`. A write records all its events or none. After every write the projections are regenerated, and the changed ones are listed in `paths`. The CLI never commits; `git add` the paths yourself (`registry.yaml` and `state.json` are git-ignored, see [Projections](#projections)). |
| Project name | `project:` in `.ekumen/config.yaml` if that committed, owner-edited file exists; else, when the project root is the top of a git repository, the `origin` remote's repository name; else the name of the folder holding `.ekumen/` (so a project in a monorepo subfolder is named after its folder). `registry.yaml` is not read. |
| Observed state | `status`, `task`, `graph`, `map` and the projections read pull requests with `gh pr list` from the project root. Without `gh` (or with `EKUMEN_OBSERVE=off`) a task's `observed` is `null`, a warning goes to stderr, and statuses are derived from events alone. |

## Read commands

### `ekumen status`

```
$ ekumen status --as lead:yash --json
{
  "project": "ekumen",
  "generated_at": "2026-11-20T09:21:00Z",
  "counts": {"tasks_by_status": {"done": 1, "draft": 1, "running": 1}, "decisions_open": 2, "routes_open": 2, "conflicts": 0},
  "inbox_top": [{"kind": "decision", "id": "D-new-choice", "title": "…", "priority": 5, "reason": "blocks 1 task, due 2026-11-20", "created_at": "…"}]
}
```

`inbox_top` is the first five items of the caller's inbox (the lead's when no actor is given).

### `ekumen inbox [--for lead|orchestrator|<session>]`

```
$ ekumen inbox --for orchestrator
inbox for orchestrator:
  route     route-01M7W42VG0000000000000000N p=0    T-cli handoff: Done, in review. (handoff has follow-ups, …)
```

Ranked: for the lead, pending decisions by priority, then conflicts, then the lead's next moves (`kind: action`, below); then, for every owner, open routed items (questions and handoffs needing action), oldest first. Until the control loop exists, the CLI computes the routes the loop would write (C-event-log §4), so questions and handoffs show up straight away. `--for session:t-cli` and `--for t-cli` are the same.

The lead's `action` items carry `action` and `command` (C-cli-json v3), with `subject` naming the item:

| `action` | When | `command` |
|---|---|---|
| `approve` | A task is drafted, has its `brief.md`, has no open blockers, and is not approved or deferred. | `ekumen task approve <id>` |
| `freeze` | A draft contract has no open blockers, or a frozen contract has a newer version recorded by `contract version`. | `ekumen contract freeze <id>` |
| `apply` | A decision has been decided or defaulted for a day or more and is not applied. | `ekumen decision apply <id> --as loop:<orchestrator session>` |

```
$ ekumen inbox --for lead --json
{"for": "lead", "items": [
  {"kind": "action", "id": "C-http-api", "title": "Freeze C-http-api: HTTP API for the UI", "priority": 0,
   "reason": "draft contract with no open blockers", "created_at": "…",
   "subject": {"kind": "contract", "id": "C-http-api"}, "action": "freeze", "command": "ekumen contract freeze C-http-api"}]}
```

### `ekumen task list [--status s]` / `ekumen task show <id>`

```
$ ekumen task list --status blocked
T-skills               blocked    Skills  [blocked by D-new-choice, T-cli]

$ ekumen task show T-cli --json     # {task, brief: {text, amendments: [Entry], handoffs: [Entry]}}
```

### `ekumen decision list [--status s]` / `ekumen decision show <id>`

```
$ ekumen decision list --status pending
D-claim-lane                       t2 pending    p=3    Who writes RoleClaimed
$ ekumen decision show D-yaml-library      # prints the decision as YAML; --json gives {decision}
```

`priority` = tasks held up × deadline urgency (C-decision-record §1), for pending decisions; `holds_up` lists the contracts in `contract_impact` and every task reached downstream.

### `ekumen question list [--for x] [--open]`

```
$ ekumen question list --open --for t-cli
q-01m7w3vh40000000000000000h on task T-cli, lead:yash → session:t-cli: Is the map offline? [open]
```

### `ekumen graph`

```
$ ekumen graph --json      # {nodes: [{id, kind, title, status}], edges: [{from, to}]}
```

Nodes are tasks, contracts, external dependencies, and decisions that still gate (not applied, not superseded). An edge `{from: A, to: B}` means B needs A.

### `ekumen map --out <file.html> [--fragment] [--link-base url]`

```
$ ekumen map --out /tmp/ekumen-map.html
wrote /tmp/ekumen-map.html: 7 nodes, 6 edges, 2 inbox items
```

One self-contained HTML file (inbox, pull requests, task graph, decided list) built from the same data as `graph` and `inbox --for lead`. It makes no network requests: dagre 0.8.5 (MIT) is inlined for the layout.

- `--fragment` writes the page without `<!doctype>`, `<html>`, `<head>` and `<body>`, for embedding in another page. Every style rule (custom properties and dark mode included) is scoped to the wrapping `<div class="ekumen-map">`, and the page uses no fixed element ids: its script finds elements inside its own wrapper, and SVG marker ids are unique per page, so several maps can share one host page.
- **Links.** Every item links to its full text on the git host, in a new tab: decisions to `.ekumen/decisions/<id>.yaml`, tasks to their `brief.md` and latest handoff, contracts to their body, PRs to the PR. The link base comes from the `origin` remote: `git@github.com:o/r.git`, `ssh://git@github.com/o/r.git` and `https://github.com/o/r(.git)` give `https://github.com/o/r/blob/<commit>/`, at the commit the map was generated from, so links show the text the map shows. Without a commit it uses origin's default branch (`origin/HEAD`); with neither, no links: no branch is guessed. GitHub is the only built-in host; for any other, pass `--link-base` (e.g. `--link-base https://git.example.com/o/r/src/branch/main/`). With no remote and no `--link-base`, ids are plain text.

### `ekumen check`

```
$ ekumen check --json
{"ok": false, "conflicts": [{"rule": "decided-twice", "events": ["…", "…"], "message": "decision D-x decided with different answers"}], "invalid": []}
$ echo $?
4
```

Validates event and brief files (layout, envelope, payload), checks that files recorded by events still exist and that every amendment and handoff file is recorded by an event (`invalid[].error` starting `orphan-entry:` otherwise), and runs the C-event-log §5 rules:

- `frozen-text-changed` against the current text of approved briefs and frozen contracts (a contract's text may also match a newer version recorded by `contract version` and not yet frozen);
- `orphan-step`: a lifecycle step whose prerequisite is nowhere in the log, e.g. a `DecisionApplied` with no `DecisionDecided`. Steps whose prerequisite exists are never dropped for being stamped earlier than it: within one item the log keeps time order, except that an event stamped before its own prerequisite (an apply before its decide, an answer before its question, anything before the item's creating event) is moved to right after it (D-imported-event-time-order);
- `spine-unapproved`: `.ekumen/spine.md` does not match the latest `ekumen spine approve`. Nothing is reported before the first approval.

Exit `4` with conflicts, `2` with invalid files only.

## Write commands

### `ekumen decision raise --file <d.yaml>` (session, integrator, orchestrator, lead)

```yaml
# d.yaml
id: D-claim-lane
title: Who writes RoleClaimed
tier: 2
question: C-cli-json has sessions claim roles; C-event-log puts RoleClaimed in the loop's lane. Which?
options:
  - {id: claiming-session, tradeoff: matches the CLI}
  - {id: loop-only, tradeoff: matches the table}
recommendation: claiming-session
default_if_no_answer: null
deadline: null
blocks: []
contract_impact: [C-event-log]
context:
  summary: The two contracts disagree.
```

```
$ ekumen --as session:t-cli decision raise --file d.yaml
raised D-claim-lane
```

Validated against C-decision-record §2; errors name the field. Lifecycle fields (`status`, `answer`, …) and unknown fields are refused. If `raised_by` is left out it is filled in: `task` is the session's own task (or `null`), `session` the actor's id, `on` today (UTC). A session may raise only for its own task.

### `ekumen decision answer <id> <option|other> [--note t]` (lead)

```
$ ekumen --as lead:yash decision answer D-claim-lane claiming-session --note "As recommended."
$ ekumen --as lead:yash decision answer D-x other --note "Use TOML instead"
```

### `ekumen decision supersede <id> --by <id> [--note t]` (lead)

```
$ ekumen --as lead:yash decision supersede D-old-choice --by D-new-choice --note "replaced"
```

### `ekumen question ask <subject id> --text t [--to x]` (anyone)

```
$ ekumen --as lead:yash question ask D-claim-lane --text "What breaks if the loop writes it?"
asked q-01m7… on decision D-claim-lane
```

The subject is a task, decision or contract. Without `--to`, a question goes to the session that raised the decision, the task's session, or the orchestrator for a contract.

### `ekumen question answer <question id> --text t` (the addressee)

```
$ ekumen --as session:t-cli question answer q-01m7… --text "Nothing; it only records the claim."
```

### `ekumen task draft --file <t.yaml>` (orchestrator, with the claim)

```yaml
id: T-skills
title: /blocked and /decide skills
kind: build              # research | contract | build
brief: briefs/T-skills/brief.md   # default
repo: ekumen
base_branch: main
branch: build/t-skills
worktree: .worktrees/t-skills
blocked_by: [T-cli]
tier_grants: [push_own_branch, open_draft_pr, restack]
session: t-skills        # optional: makes this the session's own task
aliases: []
```

```
$ ekumen --as orchestrator:main task draft --file t.yaml
```

Write `briefs/<id>/brief.md` (§A) yourself before approval; the CLI hashes it at approval. It is one of the [owner-edited prose files](#owner-edited-prose-files).

### `ekumen task approve <id> [--grants a,b]` (lead)

```
$ ekumen --as lead:yash task approve T-skills --grants push_own_branch,open_draft_pr
```

Records the sha256 of `brief.md`. Editing §A afterwards is reported by `check` as `frozen-text-changed`.

### `ekumen task amend <id> --text-file f [--note t]` (orchestrator, with the claim)

```
$ ekumen --as orchestrator:main task amend T-cli --text-file amend.md --note D-yaml-library
amendment recorded for T-cli
  .ekumen/briefs/T-cli/amendments/20261120T091900Z-main.md
  .ekumen/events/task/T-cli/01M7….TaskAmended.json
```

### `ekumen task handoff <id> --text-file f` (the task's session)

```
$ ekumen --as session:t-cli task handoff T-cli --text-file handoff.md
```

The handoff needs a `## STATUS` section; its first line is the event's `status_line`. `has_followups`, `has_decisions` and `has_deviations` are true when `FOLLOW-UPS`, `DECISIONS RAISED` and `DEVIATIONS` have any content other than "None". Any of them routes the handoff to the orchestrator's inbox. See [Entry front matter](#entry-front-matter).

### Entry front matter

Amendments (§B) and handoffs (§C) start with YAML front matter:

```markdown
---
task: T-cli          # the task id (or an alias)
author: t-cli        # the writer: its id, its file-name slug, or <role>:<id>
date: 2026-11-20     # UTC date, YYYY-MM-DD
---

# T-cli handoff
```

Every key is optional, and other keys are kept and ignored. When the file has no front matter, `task amend`, `task handoff` and `decision apply` add all three (the task, the actor's id, today in UTC) before writing it, so the stored file and its `sha256` include them. When it has front matter, each key that is present is checked: `task` must name the task being written, `author` the actor, and `date` must be a date; otherwise the write is refused (exit 2, `field` names the key). The front matter is never rewritten.

### `ekumen task defer <id> --reason t` (orchestrator, with the claim)

```
$ ekumen --as orchestrator:main task defer T-skills --reason "after v0.1"
deferred T-skills
```

Records `TaskDeferred`; the task's status becomes `deferred`, and it no longer counts toward any decision's priority. Deferring a deferred task is refused.

### `ekumen task attach <id> --session s [--adapter a]` (orchestrator with the claim, or loop)

```
$ ekumen --as orchestrator:main task attach T-skills --session t-skills --adapter acp
attached T-skills to session t-skills
```

Records `TaskSessionAttached`. The session (`t-skills` or `session:t-skills`) then writes in the task's lane, alongside the session named at draft time. Attaching the same session and adapter again records nothing.

### `ekumen task block <id> --add x --remove y --reason t` (orchestrator, with the claim)

```
$ ekumen --as orchestrator:main task block T-skills --add D-new-choice --reason "needs the answer first"
```

`--add` and `--remove` repeat or take comma-separated lists.

### `ekumen decision apply <id> [--amend-text-file f]` (loop; the orchestrator runs it as `loop:<its session>`)

```
$ ekumen --as loop:orchestrator decision apply D-gate
ekumen: warning: no session messaging exists yet; tell the sessions of T-gated yourself
applied D-gate: amended 1 task(s), unblocked 1
  .ekumen/briefs/T-gated/amendments/20261120T093000Z-orchestrator.md
  .ekumen/events/task/T-gated/01M7….TaskAmended.json
  .ekumen/events/decision/D-gate/01M7….DecisionApplied.json
  …
```

For a decided or defaulted decision (C-decision-record §3, D-apply-before-loop): adds a §B amendment to each task in `blocks` (the text of `--amend-text-file`, or a generated line naming the answer and note), then records `DecisionApplied {amended_tasks, unblocked_tasks, notified_sessions: []}`. `unblocked_tasks` are the tasks that were `blocked` before and are not after. All of it is recorded, or none of it.

- **Idempotent:** applying an applied decision prints `{"ok": true, "events": [], "paths": []}` and writes nothing.
- A pending or superseded decision is refused (exit 2).
- Everything is written as the loop: `DecisionApplied`, and each `TaskAmended` with `by_decision: <id>`, which is in the loop's lane (C-event-log v5 §3, D-apply-amend-lane). No orchestrator claim is needed. The loop may amend a task only for a decided (or defaulted) decision that blocks it (in the decision's `blocks` or the task's `blocked_by`); anything else is a `lane` conflict. Other amendments stay with the orchestrator.
- The command checks that the apply takes effect in the log before recording it.

### `ekumen contract draft --file <c.yaml> [--text-file body.md]` (orchestrator, with the claim)

```yaml
# c.yaml
id: C-http-api
title: HTTP API
path: contracts/C-http-api.md   # default; relative to .ekumen/
blocked_by: [D-adapter-base]     # optional (D-contract-blockers)
aliases: []
```

```
$ ekumen --as orchestrator:main contract draft --file c.yaml --text-file c-http-api.md
drafted C-http-api
```

`--text-file` copies the contract's body to `path`, and never replaces an existing file. Without it, the file must already be there.

### `ekumen contract block <id> --add x --remove y --reason t` (orchestrator, with the claim)

```
$ ekumen --as orchestrator:main contract block C-http-api --add D-adapter-base --reason "waits on the adapter base"
```

Records `ContractBlockersChanged`. A task blocked by the contract stays blocked while the contract has open blockers, even once it is frozen.

### `ekumen external add --file f` · `external update <id> --file f` · `external resolve <id> --note t` (orchestrator, with the claim)

```yaml
# x.yaml
id: X-sandbox-creds
what: Sandbox credentials for the staging cluster
owner_team: platform
asked_on: 2026-11-18     # UTC dates
needed_by: 2026-11-25
blocks: [T-claude-adapter]
aliases: []
```

```
$ ekumen --as orchestrator:main external add --file x.yaml
$ printf 'needed_by: 2026-12-01\n' | ekumen --as orchestrator:main external update X-sandbox-creds --file -
$ ekumen --as orchestrator:main external resolve X-sandbox-creds --note "granted"
```

An external dependency is a gate: every task in `blocks` is blocked until it is resolved. `what`, `owner_team`, `asked_on` and `needed_by` are required. `update` takes any of `what`, `owner_team`, `asked_on`, `needed_by` and `blocks`, and records only the fields that change. A resolved dependency can't be updated or resolved again.

### `ekumen contract freeze <id>` (lead)

```
$ ekumen --as lead:yash contract freeze C-event-log
froze C-event-log version 3
```

The version is the contract file's front-matter `version`, or the next one. Freezing a version again with the same text is refused (2); with different text it is a conflict (4). A new version of a frozen contract is frozen only after `contract version` recorded it for this text (refused, 2, otherwise).

### `ekumen contract version <id> --by <decision>` (orchestrator, with the claim)

The contract-change protocol (C-decision-record §3, D-contract-version-recording):

1. The lead decides a tier-2 decision whose `contract_impact` names the contract.
2. The orchestrator edits the contract body and bumps `version` in its front matter.
3. The orchestrator records the change against the decision:
   ```
   $ ekumen --as orchestrator:main contract version C-event-log --by D-imported-event-time-order
   recorded C-event-log version 5 by D-imported-event-time-order; the lead freezes it with `ekumen contract freeze C-event-log`
   ```
4. The lead freezes it: `ekumen --as lead:yash contract freeze C-event-log`. Until then, `check` accepts the recorded text, and the lead's inbox shows a `freeze` action.

Records `ContractVersioned {version, by_decision, text_sha256}`. Refused (2) unless the contract is frozen, the decision is decided (or defaulted) and names the contract in `contract_impact`, the front-matter version is higher than the frozen one, and the text changed. Recording the same version, text and decision again records nothing.

### `ekumen spine approve` (lead)

```
$ ekumen --as lead:yash spine approve
approved .ekumen/spine.md
```

Records `SpineApproved {text_sha256}` for the current `.ekumen/spine.md` (D-post-switch-hand-edits). From then on `check` reports `spine-unapproved` whenever the spine does not match the latest approval. Approving the same text again records nothing.

### `ekumen route resolve <route id> --note t` (the route's owner)

```
$ ekumen --as orchestrator:main route resolve route-01M7W42VG0000000000000000N --note "follow-ups filed"
$ ekumen --as orchestrator:main route resolve route-01m7w42vg --note "follow-ups filed"   # a unique prefix
```

### `ekumen claim <role>` / `ekumen release <role>` (the claiming session)

```
$ ekumen --as orchestrator:main claim orchestrator
$ ekumen --as orchestrator:main release orchestrator
```

One holder per role. Every orchestrator write needs the claim.

### `ekumen project [--check]`

```
$ ekumen project
projections: 3 file(s) changed
$ ekumen project --check --json
{"ok": false, "invalid": [{"path": ".ekumen/decisions/D-x.yaml", "error": "does not match the events: run `ekumen project` and commit it"}]}
```

Regenerates `decisions/<id>.yaml`, `registry.yaml` and `state.json` from the events (plus observed pull requests for task status). It does nothing on an empty log, so hand-written records are not replaced before they are imported.

`--check` writes nothing: it fails (exit 2) if a committed `decisions/<id>.yaml` is stale or missing, or names no decision in the log. CI runs it (`scripts/ci-check.sh`).

## Projections

| File | Shape |
|---|---|
| `decisions/<id>.yaml` | C-decision-record §4: every §1 field, then the lifecycle fields that are set; `questions` is always present. |
| `registry.yaml` | As the hand-written registry: `project`, `lead`, `tasks` by id (with derived `status`), `external`, `contracts`. |
| `state.json` | Everything the CLI derives: tasks, decisions (with priority), contracts, externals, questions, the lead's and orchestrator's inboxes, conflicts and counts, as of a UTC date. |

They carry a "generated, do not edit" header. Decision files for ids not in the log are left alone.

**What is committed** (D-projections-in-git): `decisions/<id>.yaml` is committed, so each decision reads well on the git host, and CI checks it matches the events (`project --check`). `registry.yaml` and `state.json` aggregate the whole project, so two branches that each record one event would always conflict on them: they are git-ignored, and every write (and `ekumen project`) regenerates them. A fresh clone needs neither; read commands derive everything from the events.

## Owner-edited prose files

After the switch point nobody edits `.ekumen/` by hand, with these exceptions (D-post-switch-hand-edits). They are prose with no CLI writer, edited by their owner only:

| File | Owner | Guard |
|---|---|---|
| `.ekumen/spine.md` | orchestrator, with the lead's approval | `ekumen spine approve` (lead) hashes it; `check` reports `spine-unapproved` after an unapproved edit. |
| `.ekumen/contracts/<id>.md` | orchestrator | Draft: free to edit. Frozen: changed only through a decided tier-2 decision, `contract version` and `contract freeze`; `check` reports `frozen-text-changed` otherwise. |
| `.ekumen/briefs/<id>/brief.md` (§A) | orchestrator | Only before `task approve`, which hashes it; afterwards `check` reports `frozen-text-changed`. Changes go in §B (`task amend`). |
| `.ekumen/research/*.md` | the research task's session | Cited as evidence; no hash yet. |
| `.ekumen/config.yaml` (optional) | orchestrator | `project:` names the project; `lead:` is the lead's handle (else the latest lead in the log). |

Everything else (events, amendments, handoffs, generated files) is written by `ekumen`. An amendment or handoff file with no event is reported by `check` as `orphan-entry`.
