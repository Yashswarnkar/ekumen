---
id: C-cli-json
aliases: [C3]
title: CLI commands and JSON output
status: draft          # draft | frozen
frozen_on: null
version: 0
consumers: [T-skills, T-reconcile, T-server, agents]
producers: [T-cli]
---

# C-cli-json · CLI commands and JSON output

`ekumen` is how people, agents and scripts read and change Ekumen state (spine invariants 9 and 11). This contract fixes the command set for the self-hosting slice, how a caller says who it is, and the JSON every command prints with `--json`. The JSON is the stable interface; human-readable output may change freely.

## 1. Conventions

- **Project root:** the nearest parent directory containing `.ekumen/`, or `--root <dir>`.
- **Who is acting:** `--as <role>:<id>` or the `EKUMEN_ACTOR` environment variable, e.g. `session:t-core`, `orchestrator:main`, `lead:Yashswarnkar`. Roles are the C-event-log §3 roles. Writes outside the actor's lane are refused (exit 2). Orchestrator writes also need a live role claim (`ekumen claim orchestrator`).
- **`--json`:** prints exactly one JSON object to stdout and nothing else. Logs and warnings go to stderr.
- **Exit codes:** `0` ok · `1` usage error · `2` refused (validation, lane, or role claim) · `3` not found · `4` conflict detected · `5` internal error.
- **Errors with `--json`:** `{"error": {"code": "lane|validation|not_found|conflict|usage|internal", "message": "…", "field": "…"}}`. `field` is optional.
- **Writes** return `{"ok": true, "events": ["<event id>", …], "paths": ["<file written>", …]}`. A write either records all its events or none.
- **Ids** may be given as an id or an alias, and output always uses the id.
- **Times** are RFC 3339 UTC; dates are `YYYY-MM-DD`.

## 2. Read commands

| Command | Prints |
|---|---|
| `ekumen status` | `{project, generated_at, counts: {tasks_by_status, decisions_open, routes_open, conflicts}, inbox_top: [InboxItem…≤5]}` |
| `ekumen inbox [--for lead\|orchestrator\|<session>]` | `{for, items: [InboxItem]}`, ranked. The default is the caller's own role. |
| `ekumen task list [--status s]` / `task show <id>` | `{tasks: [Task]}` / `{task: Task, brief: {text, amendments: [Entry], handoffs: [Entry]}}` |
| `ekumen decision list [--status s]` / `decision show <id>` | `{decisions: [Decision]}` / `{decision: Decision}` |
| `ekumen question list [--for x] [--open]` | `{questions: [Question]}` |
| `ekumen graph` | `{nodes: [Node], edges: [Edge]}` |
| `ekumen map --out <file.html>` | Writes a self-contained static build map (inbox, graph, decided list) from the same data as `graph` and `inbox`. With `--json`: `{"ok": true, "paths": [file]}`. |
| `ekumen check` | `{ok, conflicts: [Conflict], invalid: [{path, error}]}`. Validates every file and runs the C-event-log §5 rules. Exit 4 if any conflicts are found. |

## 3. Write commands

| Command | Lane | Records |
|---|---|---|
| `ekumen decision raise --file <d.yaml>` | session, integrator, orchestrator, lead | `DecisionRaised` after C-decision-record §2 validation |
| `ekumen decision answer <id> <option\|other> [--note t]` | lead | `DecisionDecided` |
| `ekumen decision supersede <id> --by <id> [--note t]` | lead | `DecisionSuperseded` |
| `ekumen question ask <subject id> --text t [--to x]` | anyone | `QuestionAsked` |
| `ekumen question answer <question id> --text t` | the addressee | `QuestionAnswered` |
| `ekumen task draft --file <t.yaml>` | orchestrator | `TaskDrafted` |
| `ekumen task approve <id> [--grants a,b]` | lead | `TaskApproved` (hashes `brief.md`) |
| `ekumen task amend <id> --text-file f` | orchestrator | amendment file + `TaskAmended` |
| `ekumen task handoff <id> --text-file f` | the task's session | handoff file + `TaskHandoff` (the `has_*` flags are parsed from the section headings) |
| `ekumen task block <id> --add x --remove y --reason t` | orchestrator | `TaskBlockersChanged` |
| `ekumen contract freeze <id>` | lead | `ContractFrozen` (hashes the file) |
| `ekumen route resolve <route id> --note t` | the route's owner | `RouteResolved` |
| `ekumen claim <role>` / `ekumen release <role>` | the claiming session | `RoleClaimed` / `RoleReleased` |

Lead commands are run by the lead, or by an orchestrator session acting on the lead's explicit word with `--as lead:<handle>`. The event records the actor as given.

## 4. Object shapes

```jsonc
// Task
{"id": "T-core", "aliases": [], "title": "…", "kind": "build", "status": "running",
 "blocked_by": ["C-event-log"], "blocked_by_open": [], "approved_on": "2026-11-20",
 "branch": "build/t-core", "session": "t-core", "tier_grants": ["push_own_branch"],
 "observed": {"pr": {"number": 4, "state": "open", "checks": "success"} }}   // observed is null when unknown

// Decision: every C-decision-record §1 field, plus:
{"priority": 4.0, "holds_up": ["C-cli-json", "T-cli"]}

// Question
{"id": "…", "subject": {"kind": "decision", "id": "D-…"}, "asked_by": "lead:Yashswarnkar", "to": "session:t-core",
 "text": "…", "asked_at": "…", "answer": null, "answered_by": null, "answered_at": null}

// InboxItem
{"kind": "decision|question|route|conflict", "id": "…", "title": "…", "priority": 4.0,
 "reason": "blocks 3 tasks", "created_at": "…"}

// Node / Edge (Edge: from is needed by to)
{"id": "D-…", "kind": "task|decision|contract|external", "title": "…", "status": "…"}
{"from": "C-event-log", "to": "T-core"}

// Entry (an amendment or handoff file)
{"path": "…", "author": "…", "at": "…", "sha256": "…", "text": "…"}

// Conflict
{"rule": "decided-twice", "events": ["…"], "message": "…"}
```

## 5. Versioning

Adding commands, flags or fields is a minor change; callers must ignore fields they don't know. Removing or renaming anything, or changing a meaning, needs a tier-2 decision.

## Open before freeze

- Should the CLI commit to git by itself after a write (`--commit`), or leave committing to the caller? Proposal: leave it to the caller in v0.1; agents already commit their work.
- `ekumen map` duplicates part of the future UI. Proposal: keep it. It's the build map until `ekumen serve` has views, and afterwards a static export of them.
