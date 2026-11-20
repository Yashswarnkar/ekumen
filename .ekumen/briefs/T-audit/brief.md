---
id: T-audit
title: Recurring read-only audit
kind: research
contracts_consumed: [C-event-log, C-decision-record, C-cli-json]
contracts_produced: []
blocked_by: [T-core-align]
tier_grants: [push_own_branch, open_draft_pr]
---

# T-audit · Recurring read-only audit

*§A only. Amendments (§B) are files in `amendments/`. **Each audit run is one handoff** in `handoffs/`.*

**Goal:** A fresh pair of eyes that finds what the build process misses: gaps and ambiguities in the design, drift between documents and code, friction nobody closed, and places where the lead is confused. It **detects and reports, and never fixes** (spine invariant 5). Everything it finds goes to the lead as findings or decision drafts. This is a light, early version of the auditor in the spine's roadmap.

**How it runs:** the orchestrator starts a run with a fresh session, after the switch point, then after every three merged task PRs or once a week, whichever comes first. Later the control loop will trigger it. **Start cold:** read only the repo, never a previous chat. If the repo alone isn't enough to understand the project, that is itself a finding.

**Mechanical checks are not your job.** `ekumen check` (in CI) covers invalid records, lane violations and conflicts. Run it once and report its result, but spend your effort on judgment (spine rule 9).

**Each run, look for:**

1. **Contract quality:** ambiguities, contradictions between contracts (e.g. one contract assigning an event to a lane another contract forbids), rules with no defined meaning, examples that contradict the text.
2. **Design vs reality:**
   - Does the code do what frozen contracts say? Use `internal/core/README.md`'s section map, and sample the tests named after rules.
   - Do the spine's §7 scope and §8 order match the task list and what has merged?
3. **Decision health:**
   - decided but never applied
   - applied but not reflected in the contract or brief it names
   - decisions that contradict each other
   - pending tier-2 decisions blocking a lot of work
   - tier-1 defaults about to fire that the lead may not have seen
4. **Friction backlog:** collect every FRICTION and FOLLOW-UPS item from all handoffs. For each, say whether it was addressed (cite the decision, amendment or commit) or is still open. This table is the main output.
5. **Lead experience:**
   - Patterns in `ekumen question list`, e.g. repeated questions about the same concept.
   - Whether `README.md`, `docs/development.md` and `docs/cli.md` work for a newcomer. Actually follow `docs/development.md`.
   - Whether the inbox would tell the lead what to do next.
6. **Principles:**
   - Tool or vendor names outside the adapter layer (spine rule 12).
   - Agents asked to do deterministic work that code should do (rule 9).
   - Anything bypassing lanes, such as hand-edited files after the switch.

**Output: one handoff per run,** recorded with `ekumen task handoff T-audit`, containing:
- `## SUMMARY`: the five findings that matter most, each in one sentence.
- `## FINDINGS`: a table of id (`F-<run date>-<n>`), severity (high / medium / low), what, evidence (paths, commits, decision ids), and suggested owner (lead, orchestrator, or a task).
- `## FRICTION BACKLOG`: the table from item 4.
- `## DECISIONS RAISED`: anything that needs the lead. Raise it with `ekumen decision raise` (tier 2 unless clearly in-contract), and list the ids here.
- `## STATUS`, `## FOLLOW-UPS`, `## DEVIATIONS`, `## FRICTION`, as in every handoff.

**Touches:** your handoff files only, plus decisions you raise through `ekumen`. **Never edit** code, docs, the spine, contracts, briefs or records. Not even a typo fix: report it.

**Definition of done (per run):**
- [ ] `ekumen check` result reported
- [ ] All six areas covered, or the handoff says why one was skipped
- [ ] Friction backlog table complete across every handoff in the repo
- [ ] Handoff recorded through `ekumen task handoff`, committed on your branch, draft PR open
- [ ] Every finding cites evidence; no finding is based on guesswork

**Out of scope:** fixing anything; re-running builds or long test suites beyond `go test ./...` once; judging style preferences that no contract or principle covers.

**Escalate if:** you find something that makes current work unsafe to continue, such as a frozen contract that is self-contradictory where code already depends on it. Raise it as a tier-2 decision and put it first in SUMMARY.

**Start prompt:**

    You are the auditor for this repo. Start cold: read only the repository.
    Read .ekumen/spine.md, then `ekumen task show T-audit --json` (this brief, as amended),
    then every contract in .ekumen/contracts/, then `ekumen status --json`, `ekumen inbox --for lead --json`,
    `ekumen decision list --json`, `ekumen question list --json`, and every handoff under .ekumen/briefs/*/handoffs/.
    Never edit anything except through `ekumen task handoff T-audit` and `ekumen decision raise`.
