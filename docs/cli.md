# `ekumen` CLI

The CLI is how people, agents and scripts read and change Ekumen state. The command set and the `--json` shapes are fixed by the contract [C-cli-json](../.ekumen/contracts/C-cli-json.md); this page shows how to use them. The JSON is the stable interface. The plain-text output is for people and may change. The `/blocked` and `/decide` agent skills wrap these commands; see [Skills](skills.md).

## Conventions

| | |
|---|---|
| Project root | The nearest parent directory containing `.ekumen/`, or `--root <dir>`. |
| Who is acting | `--as <role>:<id>` or `EKUMEN_ACTOR`, e.g. `session:t-core`, `orchestrator:main`, `lead:Yashswarnkar`. Roles: `lead`, `orchestrator`, `session`, `integrator`, `loop`. Every write needs it. |
| `--json` | Prints exactly one JSON object on stdout. Warnings go to stderr, and so does usage text (`help`, `-h`), which then leaves stdout empty. |
| Flags | Global flags (`--root`, `--as`, `--json`) go anywhere. Flags and positional arguments may be mixed. |
| Files | `--file` and `--text-file` take a path, or `-` for stdin. |
| Ids | An id or an alias (`C1`, `D-012`) is accepted anywhere; output always uses the id. Question and route ids also take a unique prefix, in any case (`q-01m7w3`); an ambiguous prefix is a usage error (exit 1) that lists the matches. |
| Paths | Every path the CLI prints (`paths` of a write, `Entry.path`, `Task.brief`, `check`'s `invalid[].path`, `map`'s output) is relative to the project root, e.g. `.ekumen/briefs/T-core/brief.md`. Paths recorded inside events (`file`, `path`, `brief`) and in `registry.yaml` stay relative to `.ekumen/`, as the event log records them. |
| Exit codes | `0` ok · `1` usage · `2` refused (validation, lane, or role claim) · `3` not found · `4` conflict · `5` internal. |
| Errors | With `--json`: `{"error": {"code": "lane\|validation\|not_found\|conflict\|usage\|internal", "message": "…", "field": "…", "details": {…}}}`. `field` and `details` are optional; `details` is structured, e.g. `{events, paths}` already recorded when a write fails after recording them. |
| Writes | Print `{"ok": true, "events": [ids], "paths": [files written]}`. A write records all its events or none. After every write the projections are regenerated, and the changed ones are listed in `paths`; if that fails, the events stay recorded and the write exits `5` (`internal`, naming the recorded events, which `--json` also gives as `error.details: {events, paths}`): run `ekumen project`. The CLI never commits; `git add` the paths yourself (`registry.yaml` and `state.json` are git-ignored, see [Projections](#projections)). |
| Project name | `project:` in `.ekumen/config.yaml` if that committed, owner-edited file exists; else, when the project root is the top of a git repository, the `origin` remote's repository name; else the name of the folder holding `.ekumen/` (so a project in a monorepo subfolder is named after its folder). `registry.yaml` is not read. |
| Observed state | `status`, `task`, `graph`, `map` and the projections read pull requests with `gh pr list` from the project root. Without `gh` (or with `EKUMEN_OBSERVE=off`) a task's `observed` is `null`, a warning goes to stderr, and statuses are derived from events alone. `gh pr list` is capped at 200 pull requests; when it returns exactly that many, a warning goes to stderr because older ones may be missing (issue #15). |

## Read commands

### `ekumen status`

```
$ ekumen status --as lead:yash --json
{
  "project": "ekumen",
  "generated_at": "2026-11-20T09:21:00Z",
  "counts": {"tasks_by_status": {"done": 1, "draft": 1, "running": 1}, "decisions_open": 2, "routes_open": 2, "conflicts": 0},
  "inbox_top": [{"kind": "decision", "id": "D-new-choice", "title": "…", "priority": 5, "reason": "holds up 1 unfinished task, due 2026-11-20", "created_at": "…", "what": "…", "why": "…", …}]
}
```

`inbox_top` is the first five items of the caller's inbox (the lead's when no actor is given).

### `ekumen inbox [--for lead|orchestrator|<session>]`

Each item is a block: **what** to do, **why** it matters, what it **unblocks** (as paths from the item to each unfinished task), and the command to **run**. No scores are printed; the order is the ranking.

```
$ ekumen inbox --for lead
inbox for lead: 12 items, most pressing first

 1. Decide D-write-atomicity: How writes stay all-or-nothing and safe with concurrent writers
    why:      T-loop-hardening can't start until this is decided.
    unblocks: D-write-atomicity → T-loop-hardening
    touches:  C-event-log (context only; not counted in the ranking)
    options:  lock-and-commit-last (recommended), journal, lock-only
    run:      ekumen decision answer D-write-atomicity <option>
    details:  ekumen decision show D-write-atomicity · ekumen why D-write-atomicity

 2. Decide D-claude-bg-workspace-trust: How managed Start handles a task worktree that Claude Code does not trust yet
    raised:   on branch build/t-claude-adapter, in PR #25 (not merged, so not on main yet)
    why:      Nothing unfinished waits on this.
    unblocks: no unfinished task
    options:  unsupported-then-paste (recommended), lead-trusts-worktree-root, adapter-accepts-trust
    run:      none yet: answering waits on D-branch-decision-answer
    …
```

Ranked: for the lead, pending decisions by priority, then conflicts, then the lead's next moves (`kind: action`, below); for the orchestrator, its next moves (`kind: action`, `action: brief`, below); then, for every owner, open routed items (questions and handoffs needing action), oldest first. Until the control loop exists, the CLI computes the routes the loop would write (C-event-log §4), so questions and handoffs show up straight away. `--for session:t-cli` and `--for t-cli` are the same.

**How decisions are ranked (T-inbox-v1).** Priority = unfinished tasks held up × deadline urgency (1 with no deadline or 8+ days away, up to 6 when overdue), then earliest deadline, then id. A decision *gates* a task when its `blocks` lists the task **or** the task's `blocked_by` lists the decision. It holds up the tasks it gates and, transitively, every task waiting on those (through tasks and the contracts they block). Done and deferred tasks never count and are never walked through. Contracts in `contract_impact` are shown as `touches` (context) and do not count. A decision that is applied or superseded holds up nothing.

**Decisions raised on branches (GitHub #38).** The lead's inbox also lists decisions raised on the head branch of an open or draft PR that are not in the local log, read with `gh api graphql` (read-only: nothing is fetched, merged or checked out). They carry `on_branch`, `pr` (the lowest PR number whose branch has it), `answerable: false` and `blocked_by: "D-branch-decision-answer"`: answering one is refused (exit 2) until that decision says how an answer on main refers to a decision not on main yet. `decision show` and `why` show them, with a warning that they are not on main.

With `--json`, every item has the C-cli-json fields plus `what`, `why`, `unblocks` (task ids), `path` (one id chain per unblocked task), `direct` (tasks that wait on it directly) and `contract_context`; decisions add `options` and `recommendation`, branch decisions `on_branch`, `pr`, `answerable` and `blocked_by` (C-cli-json §5, a minor change).

The lead's `action` items carry `action` and `command` (C-cli-json v3), with `subject` naming the item:

| `action` | When | `command` |
|---|---|---|
| `approve` | A task is drafted, has its `brief.md`, has no open blockers, and is not approved or deferred. Also an approved task, not deferred or done, with a pending `task update`; the title reads `Approve update to T-x: <field value, …> (<reason>)`. | `ekumen task approve <id>` |
| `freeze` | A draft contract has no open blockers, or a frozen contract has a newer version recorded by `contract version`. | `ekumen contract freeze <id>` |
| `apply` | A decision has been decided or defaulted for a day or more and is not applied. | `ekumen reconcile --as loop:<session>` (one pass applies every such decision) |

The orchestrator's `action` items have the same shape. They are derived, not routed or stored:

| `action` | When | `command` |
|---|---|---|
| `brief` | A task is drafted, not approved or deferred, and has no `brief.md`, so it can never reach the lead's `approve`. | `write .ekumen/briefs/<id>/brief.md` |

```
$ ekumen inbox --for lead --json
{"for": "lead", "items": [
  {"kind": "action", "id": "C-http-api", "title": "Freeze C-http-api: HTTP API for the UI", "priority": 0,
   "reason": "draft contract with no open blockers", "created_at": "…",
   "subject": {"kind": "contract", "id": "C-http-api"}, "action": "freeze", "command": "ekumen contract freeze C-http-api",
   "what": "Freeze C-http-api: HTTP API for the UI",
   "why": "Draft contract with no open blockers. 3 unfinished tasks wait on this: T-server directly, and T-decision-views and T-graph-view through them.",
   "unblocks": ["T-decision-views", "T-graph-view", "T-server"],
   "path": [["C-http-api", "T-server"], ["C-http-api", "T-server", "T-decision-views"], ["C-http-api", "T-server", "T-graph-view"]],
   "direct": ["T-server"], "contract_context": []}]}
```

### `ekumen why <id>`

For a task, decision (on main or on an open branch), contract or external dependency: what would move it on, what it waits on (its open blockers, each with *their* open blockers), and what unfinished work waits on it, as paths.

```
$ ekumen why T-next
T-next · task · draft
Comes after T-wait

next: its open blockers clearing (below), then the lead approving it

waits on:
  - T-wait: not done yet (draft)
    - D-wide: waits on the lead's answer

waiting on it (unfinished work only):
  Nothing unfinished waits on this.
```

`--json` prints `{id, kind, title, status, next, waits_on: [{id, why, on: […], repeat?}], why, direct, unblocks, path, contract_context, statuses}`, plus `on_branch` and `pr` for a branch decision. Exit `3` for an unknown id.
```

### `ekumen task list [--status s]` / `ekumen task show <id>`

```
$ ekumen task list --status blocked
T-skills               blocked    Skills  [blocked by D-new-choice, T-cli]

$ ekumen task show T-cli --json     # {task, brief: {text, amendments: [Entry], handoffs: [Entry]}}
```

**Which pull request stands for a task (GitHub #39).** A PR is linked to a task when its head branch is the task's recorded branch (`branch`), or that branch plus `-<n>` (`stack`: `build/t-reconcile-2`), or its title starts with `<task id>:` (`title`; an alias works too). Among the PRs that match, an open or draft PR wins over a merged one, and a merged one over a closed one, so a task with a PR still open is not done; then the earlier rule, then the higher number. `observed.pr` carries `branch` and `linked_by`, and `task show` prints `pull request: #19 merged (…), branch build/t-reconcile, linked by branch (the recorded branch)`.

A task with an update waiting for the lead (see `task update` under [Write commands](#write-commands)) carries `pending_update`; every other field shows the current values:

```
"pending_update": {"fields": {"branch": "build/t-skills"}, "current": {"branch": ""},
                   "reason": "fill imported placeholders", "events": ["01M7…"]}
```

### `ekumen decision list [--status s]` / `ekumen decision show <id>`

```
$ ekumen decision list --status pending
D-claim-lane                       t2 pending    p=3    Who writes RoleClaimed
$ ekumen decision show D-yaml-library      # prints the decision as YAML; --json gives {decision}
```

`priority` = unfinished tasks held up × deadline urgency, for pending decisions (see [How decisions are ranked](#ekumen-inbox---for-leadorchestratorsession)); `holds_up` lists the contracts in `contract_impact` and the unfinished tasks held up. An applied or superseded decision holds up nothing.

`decision show`, `task show` and `contract show` given an id of another kind still exit `3`, and the message names the command that shows it:

```
$ ekumen decision show C-http-api
ekumen: no decision "C-http-api" (C-http-api is a contract id; use `ekumen contract show C-http-api`)
```

 Aliases count by the item they name (`C1` is a contract).

### `ekumen contract show <id>`

```
$ ekumen contract show C1
C-event-log · Event log
aliases: C1
status: frozen, version 3, frozen on 2026-11-20
path: .ekumen/contracts/C-event-log.md
consumers: T-core, T-cli
producers: T-core
frozen   v3 2026-11-20T09:03:00Z sha256 f7f9…
versioned v4 2026-11-20T09:22:00Z by D-claim-lane sha256 1263…
pending: version 4 (D-claim-lane) waits for `ekumen contract freeze C-event-log`

# Event log
…
```

With `--json`: `{contract: {id, title, aliases, status, version, frozen_on, path, consumers, producers, blocked_by, freezes: [{version, text_sha256, at}], versions: [{version, by_decision, text_sha256, at}], pending}, text}`. `status`, `version`, `frozen_on` (the date of the freeze that recorded the current version), the freezes, versions and `pending` (a version recorded by `contract version`, not frozen yet, else `null`) come from events; `consumers` and `producers` from the file's front matter; `text` is the file's body after the front matter. A contract with no path has an empty `text`; a path that cannot be read, or front matter that does not parse, exits `3` (`not_found`, `field: path`).

### `ekumen question list [--for x] [--open]`

```
$ ekumen question list --open --for t-cli
q-01m7w3vh40000000000000000h on task T-cli, lead:yash → session:t-cli: Is the map offline? [open]
```

### `ekumen graph`

```
$ ekumen graph --json      # {nodes: [{id, kind, title, status, waiting_on?}], edges: [{from, to}]}
```

Nodes are tasks, contracts, external dependencies, and decisions that still gate (not applied, not superseded). An edge `{from: A, to: B}` means B needs A. Decision nodes carry `waiting_on`: `lead` for a pending decision, or for a decided one whose apply is already an `apply` action in the lead's inbox (a day after deciding); `loop` for one decided or defaulted, not applied, and not yet the lead's. So the `lead` nodes are exactly the decisions and `apply` actions in `inbox --for lead`.

### `ekumen map --out <file.html> [--fragment] [--link-base url]`

```
$ ekumen map --out /tmp/ekumen-map.html
wrote /tmp/ekumen-map.html: 7 nodes, 6 edges, 2 inbox items
```

One self-contained HTML file (inbox, pull requests, task graph, decided list) built from the same data as `graph` and `inbox --for lead`. It makes no network requests: dagre 0.8.5 (MIT) is inlined for the layout. In the graph, decisions waiting on the lead are drawn as solid gates with gate-coloured arrows; decisions waiting on the loop (decided, not applied, under a day old) are muted, dashed and labelled "apply pending", with plain arrows. A decision whose apply has reached the lead's inbox is drawn as a gate again, labelled "apply is yours". The legend names both.

- `--fragment` writes the page without `<!doctype>`, `<html>`, `<head>` and `<body>`, for embedding in another page. Every style rule (custom properties and dark mode included) is scoped to the wrapping `<div class="ekumen-map">`, and the page uses no fixed element ids: its script finds elements inside its own wrapper, and SVG marker ids are unique per page, so several maps can share one host page.
- **Links.** Every item links to its full text on the git host, in a new tab: decisions to `.ekumen/decisions/<id>.yaml`, tasks to their `brief.md` and latest handoff, contracts to their body, PRs to the PR. The link base comes from the `origin` remote: `git@github.com:o/r.git`, `ssh://git@github.com/o/r.git` and `https://github.com/o/r(.git)` give `https://github.com/o/r/blob/<commit>/`, at the commit the map was generated from, so links show the text the map shows. Without a commit it uses origin's default branch (`origin/HEAD`); with neither, no links: no branch is guessed. GitHub is the only built-in host; for any other, pass `--link-base`, an `http` or `https` URL (anything else is a usage error; e.g. `--link-base https://git.example.com/o/r/src/branch/main/`). With no remote and no `--link-base`, ids are plain text.

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
- `update-after-close`: a `TaskUpdated` that comes after the task's `TaskDeferred` in the log. `task update` refuses it, so it arrives through a merge. A done task is not checked: done is observed and has no place in the log, so an update proposed before the PR merged is ordinary history; it stays pending, the inbox does not offer it, and `task approve` refuses it.

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

An approved task is refused, unless it has a pending `task update` and is not done: then approving again accepts every pending update (D-placeholder-task-fields), with the grants given (or the current ones). When the update moves the brief to another file (compared after dropping a leading `.ekumen/`, with an empty brief meaning `briefs/<id>/brief.md`), the new file is hashed and becomes the frozen §A. Otherwise the event records the hash from the first approval, which stays in force: if §A was edited since, the update is still accepted, a warning says so, and `check` keeps reporting `frozen-text-changed`.

### `ekumen task update <id> [--repo r] [--base-branch b] [--branch b] [--worktree w] [--brief p] [--title t] --reason t` (orchestrator, with the claim)

```
$ ekumen --as orchestrator:main task update T-skills --repo ekumen --base-branch main \
    --branch build/t-skills --worktree .worktrees/t-skills --brief briefs/T-skills/brief.md \
    --reason "fill imported placeholders"
proposed an update to T-skills; it takes effect when the lead runs `ekumen task approve T-skills`
```

Records `TaskUpdated {<the fields that change>, reason}`. Before approval the update takes effect at once; the lead's later approval covers it. After approval it is **pending**: reads show the current values plus `pending_update`, the lead's inbox gets an `approve` action, and nothing changes (status included) until the lead runs `ekumen task approve <id>`. Several pending updates merge field by field, newest wins; one approval accepts them all.

- At least one field besides `--reason`; an empty value is refused, naming the field. Branches must be valid git branch names (no `HEAD`, no `..`, no component starting with `.` or ending in `.lock`); `--worktree` is relative and stays inside the repository; `--brief` is relative to `.ekumen/` and stays inside it. The same checks apply to a `TaskUpdated` that arrives by merge: `check` lists it as invalid and the reducer ignores it.
- Giving only values the task already has (or already has pending) prints a no-op result and writes nothing.
- A done or deferred task is refused.
- `kind`, `blocked_by`, `tier_grants`, `session` and `aliases` are not updated here: use `task block`, `task approve --grants` and `task attach`.

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
- `ekumen reconcile` applies every decided or defaulted decision the same way, with the same code (`core.ApplyEffectOf`); `decision apply` stays for applying one decision by hand, or with `--amend-text-file`.

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

### `ekumen reconcile [--dry-run]` (loop)

One pass of the control loop (D-control-loop, spine §6, §7). It compares the event log with the observed pull requests and takes only the loop's own actions, in this order:

| `kind` | When | Records |
|---|---|---|
| `default` | A pending tier-1 decision whose `deadline` is before today (UTC). The deadline day itself is still answerable. | `DecisionDefaulted {answer: default_if_no_answer, deadline}` |
| `apply` | A decided or defaulted decision that is neither applied nor superseded, including one defaulted earlier in this pass. **Held** (not taken, listed in `held`) while pull requests are not fully observed, because `DecisionApplied` records `unblocked_tasks` for good, and while the decision is contested (decided with different answers: the conflict goes to the lead first). | What `decision apply` records: a §B amendment (generated line) per drafted task in `blocks` not yet amended for it, then `DecisionApplied`. The effect is worked out again against the log just before writing. |
| `route` | A handoff with follow-ups, decisions raised or deviations, or an unanswered question, not yet routed (C-event-log §4). | `RoutedToOwner` |
| `conflict` | A C-event-log §5 conflict not yet recorded. | A tier-2 decision `D-conflict-<rule>-<the first 6 hex digits of sha256("<rule>\|<sorted event ids, comma-joined>")>` (options `fix` and `accept`, no recommendation, no blocks, the events' files as evidence, raised by the loop for no task), then `ConflictDetected {rule, events}` on that decision. The suffix grows by 2 digits only on a hash collision; when 10 digits are taken too, the conflict is skipped with a reason. A conflict whose events grow (a third answer) is a new conflict with a new decision. |

It also reports, without acting: actions `held` and `skipped` (each `{kind, subject, reason}`, `skipped` with the error `code`), `ready` tasks, tasks `not_startable` because branch or worktree is empty (D-placeholder-task-fields), the tasks this pass `unblocked`, the sessions to `notify` of amended tasks, and the orchestrator's `brief` actions (`orchestrator_actions`, InboxItems as in `inbox --for orchestrator`). It never starts or messages sessions.

```
$ ekumen --as loop:main reconcile
reconcile: 3 action(s), 4 event(s); pull requests observed: ok
  default  default D-late to `a`: deadline 2026-11-19 passed
  apply    apply D-late (defaulted `a`): amend T-gated; unblock T-gated
  route    route route-01M7W42VG0000000000000000N (task T-cli) to orchestrator: handoff has follow-ups, decisions raised or deviations
ready: T-gated
not startable (no branch or worktree, D-placeholder-task-fields): none
unblocked: T-gated
notify: t-gated
  …
```

```jsonc
// ekumen --as loop:main reconcile --json
{"ok": true, "dry_run": false, "observed": {"prs": "ok"},     // ok | unavailable | truncated
 "actions": [{"kind": "apply", "subject": {"kind": "decision", "id": "D-late"},
              "detail": "apply D-late (defaulted `a`): amend T-gated; unblock T-gated",
              "events": ["01M7…", "01M7…"], "paths": [".ekumen/briefs/T-gated/amendments/…md", "…"]}],
 "ready": [{"id": "T-gated", "branch": "build/t-gated", "worktree": ".worktrees/t-gated",
            "session": "t-gated", "brief": ".ekumen/briefs/T-gated/brief.md"}],   // null when unknown
 "not_startable": [{"id": "T-skills", "missing": ["branch", "worktree"], "decision": "D-placeholder-task-fields"}],   // null when unknown
 "held": [{"kind": "apply", "subject": {"kind": "decision", "id": "D-x"}, "reason": "contested: …"}],
 "skipped": [],                                    // {kind, subject, reason, code}
 "unblocked": ["T-gated"], "notify": ["t-gated"],
 "orchestrator_actions": [{"kind": "action", "id": "T-skills", "action": "brief", "command": "write .ekumen/briefs/T-skills/brief.md", "…": "…"}],
 "events": ["01M7…"], "paths": ["…", ".ekumen/decisions/D-late.yaml", ".ekumen/registry.yaml", ".ekumen/state.json"]}
```

- **Actor:** `--as loop:<session>`; any other role is refused (exit 2), with or without `--dry-run`. `--dry-run` alone needs no actor.
- **One pass:** the log is loaded and pull requests observed once. Each action is written as its own all-or-none write, in plan order, each admitted against the log including the actions already written. Projections are regenerated once at the end, from the same observation. A pass that writes no events does not touch the projections; run `ekumen project` to refresh them.
- **Idempotent and closed:** every action's last event marks it done (`DecisionApplied` after the amendments, `ConflictDetected` after the conflict's decision), so a second pass right after records nothing (held actions aside).
- **Failures don't wedge the loop:** an action that cannot be planned or written is rolled back, listed in `skipped` with its reason and error `code`, and the pass goes on with the others. The next pass retries it, and reports it again if it fails again.
- **The report is what the pass did:** `ready` and `not_startable` are derived from the log as written at the end of the pass, and `unblocked` and `notify` from the `DecisionApplied` events it actually wrote; a held or failed apply never shows its tasks there. `held` is always present, so an empty `actions` with a non-empty `held` means work is waiting, not that there is nothing to do. A dry run reports what the plan expects.
- **`--dry-run`** writes nothing (no events, entries or projections). It prints the same report with `dry_run: true`, the planned event ids, and `planned`: the planned events in full. Amendment files and their hashes are only known once written, so planned `TaskAmended` events have an empty `file` and `text_sha256`.
- **Observation:** when pull requests are `unavailable` or `truncated`, the pass still defaults, routes and records conflicts, but holds every apply (`held`, reason "observation incomplete"), and `ready` and `not_startable` are `null` (unknown).
- **Exit codes:** `0` when every action was taken or held, including when the pass recorded conflicts: they are now decisions in the lead's inbox, and `ekumen check` stays the CI gate. With anything in `skipped`, the full report is still printed, with `ok: false`, and the exit is `2` when every skip was a refusal (lane or validation), else `5`. If the events were recorded but the projections could not be written, the exit is `5` with the error shape, and `error.details` is `{events, paths}`: what was recorded.
- **Run one pass at a time.** There is no write lock yet (issue #12). Before each default or apply, the pass reads the log again and drops the action (held, "already done by another writer") if another writer did it since the load. That narrows the window but does not close it. Two passes that both write an apply leave two §B amendments for good: the reducer ignores the second `DecisionApplied`, but not its amendment. Two passes run as different `loop:<session>` actors can also raise the same conflict decision, which `check` reports as `id-collision`.

## Skills commands

These need no actor and write nothing under `.ekumen/`; they work outside a project. See [Skills](skills.md).

### `ekumen skills list`

```
$ ekumen skills list --json
{"skills": [{"name": "blocked", "description": "Use in an Ekumen task session when …", "files": ["SKILL.md"]},
            {"name": "decide", "description": "Use when the lead …", "files": ["SKILL.md"]}]}
```

The skills embedded in this binary, by name; `files` are relative to the skill's folder.

### `ekumen skills install --dir <path> [--force]`

```
$ ekumen skills install --dir .agent/skills --json
{"ok": true, "events": [], "paths": [".agent/skills/blocked/SKILL.md", ".agent/skills/decide/SKILL.md"]}
```

Writes each skill to `<path>/<name>/`. `paths` lists the files written, as `--dir` was given; identical files are skipped, so running it again prints `"paths": []`. A file that exists and differs is refused (exit 2, `field: force`, naming the file) and nothing is written, unless `--force`.

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
