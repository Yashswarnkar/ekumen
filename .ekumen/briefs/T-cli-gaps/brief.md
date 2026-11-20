---
id: T-cli-gaps
title: CLI commands the orchestrator needs after the switch
kind: build
contracts_consumed: [C-cli-json, C-event-log, C-decision-record]
contracts_produced: []
blocked_by: [T-cli, T-core-align, D-apply-before-loop, D-cli-missing-writes]
tier_grants: [push_own_branch, open_draft_pr, restack]
---

# T-cli-gaps · CLI commands the orchestrator needs after the switch

*§A only. Amendments (§B) are files in `amendments/`, handoffs (§C) are files in `handoffs/` (C-event-log §8).*

**Goal:** After the switch point, nobody edits `.ekumen/` by hand. T-cli found writes the orchestrator does today that have no command yet. Add them, so the switch doesn't strand us.

**Context (to re-verify against origin/main when you start):** T-cli and T-core-align are merged. T-cli's handoff (`.ekumen/briefs/T-cli/handoffs/20261120T054519Z-t-cli.md`) lists the gaps and FRICTION this task addresses.

**Job:**
1. `ekumen decision apply <id> [--amend-text-file f]`, per D-apply-before-loop. Lead or orchestrator. Writes `DecisionApplied`, adds a §B amendment to each task in `blocks` (the text file, or a generated one-liner), and clears the gate. Idempotent: applying twice writes nothing new.
2. Per D-cli-missing-writes:
   - `ekumen contract draft --file`
   - `ekumen task defer <id> --reason`
   - `ekumen task attach <id> --session s [--adapter a]`
   - `ekumen external add|update|resolve`
   - plus `contract block` if D-contract-blockers adds contract blockers
3. **Prefix matching:** question and route ids accept a unique prefix (`q-01jd8z`); an ambiguous prefix is a usage error listing the matches.
4. **Handoff front matter:** define and parse `task`, `author` and `date` front matter on entries (as the hand-written ones use), and document it in `docs/cli.md`.
5. **Tidy-ups:**
   - Update the stale "stdlib only: no go.sum" comment in CI and turn the Go cache on.
   - Pick one path convention for output paths (relative to the repo root) and note it as a minor C-cli-json clarification in your handoff.

**Touches:** `internal/cli/`, `internal/project/`, `docs/cli.md`, `.github/workflows/ci.yml`, tests, and your handoff files.

**Definition of done:**
- [ ] Every command above, with golden `--json` tests and a lane test
- [ ] `decision apply` is idempotent (tested)
- [ ] `go test -race ./...`, vet and gofmt clean; CI green on the draft PR
- [ ] Handoff recorded **with `ekumen task handoff T-cli-gaps`**. After the switch this must work; if it doesn't, that is the first FRICTION item.

**Out of scope:** the control loop (T-reconcile); skills (T-skills); `serve`.

**Escalate if:** any command needs a lane or event the contracts don't have.

**Start prompt:**

    Read .ekumen/spine.md, then run `ekumen task show T-cli-gaps --json` (the brief, as amended)
    and read contracts C-cli-json, C-event-log and C-decision-record, then T-cli's handoff.
    Work only inside the brief as amended. Use `ekumen` for every .ekumen/ write; never edit those files by hand.
    Report with `ekumen task handoff T-cli-gaps`. Raise anything above your tier with `ekumen decision raise`.
