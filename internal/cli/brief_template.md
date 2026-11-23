---
id: {{id}}
title: {{title_yaml}}
kind: {{kind}}
contracts_consumed: []
contracts_produced: []
blocked_by: [{{blocked_by}}]
tier_grants: [{{tier_grants}}]
---

# {{id}} · {{title}}

**Goal:** {{goal}}

**Context (verify against origin/main when you start):**
- TODO(brief): the facts the session needs, each checked against main: files, commands, records, prior decisions and examples to follow.

**Job:**
1. TODO(brief): numbered steps, each a concrete outcome; name commands, flags and files exactly.

**Touches:** TODO(brief): the files and folders this task may change, plus its handoff files.

**Definition of done:**
- [ ] TODO(brief): one checkable result per step, each one a reviewer can verify
- [ ] Handoff via `ekumen task handoff {{id}}`, including FRICTION

**Out of scope:** TODO(brief): the nearby work this task must not do, with the issue or task that owns it.

**Escalate if:** TODO(brief): the situations where the session stops and asks (raise a decision, or ask the lead in its terminal).

**Start prompt:**

    export EKUMEN_ACTOR=session:{{session}}
    TODO(brief): what to read first, in order (spine sections, `ekumen task show {{id}}`, the issue, contracts).
    Use `ekumen` for every .ekumen/ write. Report with `ekumen task handoff {{id}}`, push your branch, and open a draft PR
    titled "{{id}}: …". Ask the lead in your terminal before anything outward-facing beyond that.
