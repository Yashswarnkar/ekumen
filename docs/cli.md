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
| Ids | An id or an alias (`C1`, `D-012`) is accepted anywhere; output always uses the id. |
| Exit codes | `0` ok · `1` usage · `2` refused (validation, lane, or role claim) · `3` not found · `4` conflict · `5` internal. |
| Errors | With `--json`: `{"error": {"code": "lane\|validation\|not_found\|conflict\|usage\|internal", "message": "…", "field": "…"}}`. |
| Writes | Print `{"ok": true, "events": [ids], "paths": [files written]}`. A write records all its events or none. After every write the projections are regenerated, and the changed ones are listed in `paths`. The CLI never commits; `git add` the paths yourself. |
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

Ranked: for the lead, pending decisions by priority, then conflicts; then, for every owner, open routed items (questions and handoffs needing action), oldest first. Until the control loop exists, the CLI computes the routes the loop would write (C-event-log §4), so questions and handoffs show up straight away. `--for session:t-cli` and `--for t-cli` are the same.

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

### `ekumen map --out <file.html>`

```
$ ekumen map --out /tmp/ekumen-map.html
wrote /tmp/ekumen-map.html: 7 nodes, 6 edges, 2 inbox items
```

One self-contained HTML file (inbox, pull requests, task graph, decided list) built from the same data as `graph` and `inbox --for lead`. It makes no network requests: dagre 0.8.5 (MIT) is inlined for the layout.

### `ekumen check`

```
$ ekumen check --json
{"ok": false, "conflicts": [{"rule": "decided-twice", "events": ["…", "…"], "message": "decision D-x decided with different answers"}], "invalid": []}
$ echo $?
4
```

Validates event and brief files (layout, envelope, payload), checks that files recorded by events still exist, and runs the C-event-log §5 rules, including `frozen-text-changed` against the current text of approved briefs and frozen contracts. Exit `4` with conflicts, `2` with invalid files only.

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

Write `briefs/<id>/brief.md` (§A) yourself before approval; the CLI hashes it at approval.

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

The handoff needs a `## STATUS` section; its first line is the event's `status_line`. `has_followups`, `has_decisions` and `has_deviations` are true when `FOLLOW-UPS`, `DECISIONS RAISED` and `DEVIATIONS` have any content other than "None". Any of them routes the handoff to the orchestrator's inbox. Front matter is ignored.

### `ekumen task block <id> --add x --remove y --reason t` (orchestrator, with the claim)

```
$ ekumen --as orchestrator:main task block T-skills --add D-new-choice --reason "needs the answer first"
```

`--add` and `--remove` repeat or take comma-separated lists.

### `ekumen contract freeze <id>` (lead)

```
$ ekumen --as lead:yash contract freeze C-event-log
froze C-event-log version 3
```

The version is the contract file's front-matter `version`, or the next one. Freezing a version again with the same text is refused (2); with different text it is a conflict (4).

### `ekumen route resolve <route id> --note t` (the route's owner)

```
$ ekumen --as orchestrator:main route resolve route-01M7W42VG0000000000000000N --note "follow-ups filed"
```

### `ekumen claim <role>` / `ekumen release <role>` (the claiming session)

```
$ ekumen --as orchestrator:main claim orchestrator
$ ekumen --as orchestrator:main release orchestrator
```

One holder per role. Every orchestrator write needs the claim.

### `ekumen project`

```
$ ekumen project
projections: 3 file(s) changed
```

Regenerates `decisions/<id>.yaml`, `registry.yaml` and `state.json` from the events (plus observed pull requests for task status). It does nothing on an empty log, so hand-written records are not replaced before they are imported.

## Projections

| File | Shape |
|---|---|
| `decisions/<id>.yaml` | C-decision-record §4: every §1 field, then the lifecycle fields that are set; `questions` is always present. |
| `registry.yaml` | As the hand-written registry: `project`, `lead`, `tasks` by id (with derived `status`), `external`, `contracts`. |
| `state.json` | Everything the CLI derives: tasks, decisions (with priority), contracts, externals, questions, the lead's and orchestrator's inboxes, conflicts and counts, as of a UTC date. |

They carry a "generated, do not edit" header. Decision files for ids not in the log are left alone.
