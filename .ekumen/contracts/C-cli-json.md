---
id: C-cli-json
aliases: [C3]
title: CLI commands and JSON output
status: frozen         # draft | frozen
frozen_on: 2026-11-20
version: 3
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
- **Writes** return `{"ok": true, "events": ["<event id>", …], "paths": ["<file written>", …]}`. A write either records all its events or none. `ekumen reconcile` is the one write that prints its own shape (Reconcile, §4), a superset of this; it exits `0` when the pass completed, including when it recorded conflicts, and `5` when it stopped partway, naming the events it had recorded.
- **Kinds:** `decision show`, `task show` and `contract show` given a valid id (or alias) of another kind exit 3 (`not_found`); the message names the command that shows it.
- **Ids** may be given as an id or an alias, and output always uses the id. Question and route ids also accept a unique prefix; an ambiguous one is a usage error listing the matches.
- **Paths** in output are relative to the repository root (e.g. `.ekumen/briefs/T-core/brief.md`).
- **Times** are RFC 3339 UTC; dates are `YYYY-MM-DD`.

## 2. Read commands

| Command | Prints |
|---|---|
| `ekumen status` | `{project, generated_at, counts: {tasks_by_status, decisions_open, routes_open, conflicts}, inbox_top: [InboxItem…≤5]}` |
| `ekumen inbox [--for lead\|orchestrator\|<session>]` | `{for, items: [InboxItem]}`, ranked. The default is the caller's own role. For the lead: pending decisions by priority, then conflicts, then `action` items (the lead's next moves), then routed items. |
| `ekumen task list [--status s]` / `task show <id>` | `{tasks: [Task]}` / `{task: Task, brief: {text, amendments: [Entry], handoffs: [Entry]}}` |
| `ekumen decision list [--status s]` / `decision show <id>` | `{decisions: [Decision]}` / `{decision: Decision}` |
| `ekumen question list [--for x] [--open]` | `{questions: [Question]}` |
| `ekumen graph` | `{nodes: [Node], edges: [Edge]}`. Decision nodes carry `waiting_on` (§4). |
| `ekumen contract show <id>` | `{contract: {id, title, aliases, status, version, frozen_on, path, consumers, producers, blocked_by, freezes: [{version, text_sha256, at}], versions: [{version, by_decision, text_sha256, at}], pending}, text}`. Status, version, `frozen_on`, `freezes`, `versions` and `pending` (the highest `ContractVersioned` above `version`, else null) come from events; `consumers` and `producers` from front matter; `text` is the body. A path that can't be read, or front matter that doesn't parse, exits 3 (`field: path`). |
| `ekumen skills list` | `{skills: [{name, description, files: [path…]}]}`: the skills embedded in the binary. Needs no actor and no project. |
| `ekumen map --out <file.html> [--fragment] [--link-base <url>]` | Writes a self-contained static build map (inbox, graph, decided list) from the same data as `graph` and `inbox`. With `--json`: `{"ok": true, "paths": [file]}`. `--fragment` omits the `<!doctype>`, `<html>`, `<head>` and `<body>` wrappers, for embedding. Items link to their source files on the git host, in a new tab. The link base is `…/blob/<commit the map was generated from>/` from the `origin` remote (GitHub `git@`, `ssh://` and `https://` remotes), else origin's default branch, or `--link-base`; with neither, no links. |
| `ekumen check` | `{ok, conflicts: [Conflict], invalid: [{path, error}]}`. Validates every file, reports each amendment or handoff file that no event records (`error` starting `orphan-entry:`), and runs the C-event-log §5 rules. Exit 4 if any conflicts are found. |
| `ekumen project [--check]` | Regenerates the projections and prints a write result. With `--check`, writes nothing and prints `{ok, invalid: [{path, error}]}`; exit 2 if a committed `decisions/<id>.yaml` is stale or missing, or names no decision in the log (D-projections-in-git). |

## 3. Write commands

| Command | Lane | Records |
|---|---|---|
| `ekumen decision raise --file <d.yaml>` | session, integrator, orchestrator, lead | `DecisionRaised` after C-decision-record §2 validation |
| `ekumen decision answer <id> <option\|other> [--note t]` | lead | `DecisionDecided` |
| `ekumen decision supersede <id> --by <id> [--note t]` | lead | `DecisionSuperseded` |
| `ekumen decision apply <id> [--amend-text-file f]` | loop (`ekumen reconcile` applies every decided decision; `decision apply` applies one) | `DecisionApplied`, plus a §B amendment (`TaskAmended` with `by_decision`) per blocked task, all written as the loop (no orchestrator claim; D-apply-amend-lane). Idempotent. Refused if the apply would not take effect. |
| `ekumen reconcile [--dry-run]` | loop (`--as loop:<session>`; `--dry-run` needs no actor and writes nothing) | One pass of the control loop (D-control-loop), each action all-or-none, in order: `DecisionDefaulted` for a pending tier-1 decision whose deadline is before today; for each decided or defaulted decision not applied or superseded, what `decision apply` records; `RoutedToOwner` for each routable entry not yet routed; for each unrecorded conflict, a tier-2 `DecisionRaised` (`D-conflict-<rule>-<suffix>`, options `fix`/`accept`) then `ConflictDetected`. Idempotent: a second pass records nothing. Run one pass at a time. |
| `ekumen question ask <subject id> --text t [--to x]` | anyone | `QuestionAsked` |
| `ekumen question answer <question id> --text t` | the addressee | `QuestionAnswered` |
| `ekumen task draft --file <t.yaml>` | orchestrator | `TaskDrafted` |
| `ekumen task approve <id> [--grants a,b]` | lead | `TaskApproved` (hashes `brief.md`). On an approved task only when it has a pending `TaskUpdated` and isn't done; it accepts the update. When the update doesn't move the brief, the event records the first approval's hash and §A need not match; `check` keeps reporting `frozen-text-changed`. |
| `ekumen task update <id> [--repo r] [--base-branch b] [--branch b] [--worktree w] [--brief p] [--title t] --reason t` | orchestrator | `TaskUpdated` with the given fields that differ from the current (or already pending) values; none differ → no-op write result (`events: []`). Empty values, invalid branch names and paths outside the repository or `.ekumen/` are refused, naming the field; a done or deferred task is refused (D-placeholder-task-fields). |
| `ekumen task amend <id> --text-file f` | orchestrator | amendment file + `TaskAmended` |
| `ekumen task handoff <id> --text-file f` | the task's session | handoff file + `TaskHandoff` (the `has_*` flags are parsed from the section headings) |
| `ekumen task block <id> --add x --remove y --reason t` | orchestrator | `TaskBlockersChanged` |
| `ekumen task defer <id> --reason t` | orchestrator | `TaskDeferred` |
| `ekumen task attach <id> --session s [--adapter a]` | orchestrator | `TaskSessionAttached` |
| `ekumen contract draft --file <c.yaml>` · `contract block <id> --add x --remove y --reason t` | orchestrator | `ContractDrafted` · `ContractBlockersChanged` |
| `ekumen external add --file f` · `external update <id> --file f` · `external resolve <id> --note t` | orchestrator | `ExternalAdded` · `ExternalUpdated` · `ExternalResolved` |
| `ekumen contract freeze <id>` | lead | `ContractFrozen` (hashes the file). A new version of a frozen contract is refused until `contract version` has recorded it for this text. |
| `ekumen contract version <id> --by <decision>` | orchestrator | `ContractVersioned`. The version comes from the front matter. Refused unless the contract is frozen, the decision is decided or defaulted and names the contract in `contract_impact`, and the version is higher than the frozen one (D-contract-version-recording). |
| `ekumen spine approve` | lead | `SpineApproved` (hashes `.ekumen/spine.md`; D-post-switch-hand-edits) |
| `ekumen route resolve <route id> --note t` | the route's owner | `RouteResolved` |
| `ekumen claim <role>` / `ekumen release <role>` | the claiming session (C-event-log v4) | `RoleClaimed` / `RoleReleased` |

**Local commands** write nothing under `.ekumen/` and need no actor or project:

| Command | Effect |
|---|---|
| `ekumen skills install --dir <path> [--force]` | Writes each embedded skill to `<path>/<name>/`. Prints `{"ok": true, "events": [], "paths": [file…]}` (identical files are skipped, so a re-run prints `"paths": []`). A file that exists and differs is refused (exit 2, `field: force`) unless `--force`. |

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
{"kind": "decision|question|route|conflict|action", "id": "…", "title": "…", "priority": 4.0,
 "reason": "blocks 3 tasks", "created_at": "…",
 "subject": {"kind": "task", "id": "T-…"},     // optional: what the item is about
 "action": "approve|freeze|apply|brief",        // kind action only
 "command": "ekumen task approve T-…"}          // kind action only

// Node / Edge (Edge: from is needed by to)
{"id": "D-…", "kind": "task|decision|contract|external", "title": "…", "status": "…",
 "waiting_on": "lead|loop"}   // decision nodes only: lead = pending, or its apply action is in the lead's inbox; loop = decided, not applied, not yet in the lead's inbox
{"from": "C-event-log", "to": "T-core"}

// Entry (an amendment or handoff file)
{"path": "…", "author": "…", "at": "…", "sha256": "…", "text": "…"}

// Conflict
{"rule": "decided-twice", "events": ["…"], "message": "…"}

// Task gains, when an update is pending:
"pending_update": {"fields": {…}, "current": {…}, "reason": "…", "events": ["…"]}

// Reconcile
{"ok": true, "dry_run": false, "observed": {"prs": "ok|unavailable|truncated"},
 "actions": [ReconcileAction], "ready": [ReadyTask] /* null when prs is not ok */,
 "not_startable": [{"id": "T-…", "missing": ["branch", "worktree"], "decision": "D-…"}],
 "unblocked": ["T-…"], "notify": ["<session>"], "orchestrator_actions": [InboxItem],
 "events": ["<event id>"], "paths": ["<file written>"],
 "planned": [Event] /* --dry-run only */}

// ReconcileAction
{"kind": "default|apply|route|conflict", "subject": {"kind": "decision", "id": "D-…"},
 "detail": "…", "events": ["<event id>"], "paths": ["<file written>"]}
```

`action` items are the lead's next moves:

- `approve`: a task drafted with its `brief.md`, with no open blockers, not approved or deferred; or a pending task update (title `Approve update to T-x: … (<reason>)`).
- `freeze`: a draft contract with no open blockers, or a frozen contract with a newer version recorded by `contract version`.
- `apply`: a decision decided or defaulted a day or more ago and not applied.
- `brief` (orchestrator's inbox): a task drafted without a brief.

## 5. Versioning

Adding commands, flags or fields is a minor change; callers must ignore fields they don't know. Removing or renaming anything, or changing a meaning, needs a tier-2 decision.

## Settled at freeze (2026-11-20)

- The CLI never commits to git; the caller does.
- `ekumen map` stays: it is the build map until `ekumen serve` has views, and a static export afterwards.

## Version 2 (2026-11-20)

- New writes: `decision apply`, `task defer`, `task attach`, `contract draft`/`block`, `external add`/`update`/`resolve` (D-apply-before-loop, D-cli-missing-writes, D-contract-blockers).
- Id prefixes for questions and routes; paths relative to the repository root.
- Task status in projections and output includes observed PR state (D-projection-observed).

## Version 3 (2026-11-23)

Consolidated from handoff proposals by the orchestrator (GitHub #57; D-cli-json-v3 and others).

- New writes: `reconcile` (D-control-loop, D-cli-json-v3), `task update` (D-placeholder-task-fields), `contract version` (D-contract-version-recording), `spine approve` (D-post-switch-hand-edits).
- New reads: `contract show`, `project --check`, `skills list`; local command `skills install` (D-cli-json-v3).
- `decision apply` writes as the loop only (D-apply-amend-lane).
- InboxItem kind `action` with `action` and `command`; graph decision nodes carry `waiting_on`; Task carries `pending_update`; Reconcile output shape.
- `check` reports `orphan-entry`; `map --fragment` and links to source files.
