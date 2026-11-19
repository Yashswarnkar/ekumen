---
id: R1
title: Agent Client Protocol (ACP) as the agent adapter base
contracts_consumed: []
contracts_produced: []
blocked_by: []
tier_grants: [read_web, clone_public_repos_to_scratch, write_own_output, push_own_branch, open_draft_pr]
---

# R1 · Agent Client Protocol (ACP) as the agent adapter base

## §A Brief (frozen on approval)

**Goal:** Decide, with evidence, whether Ekumen's agent adapter layer should be built on Agent Client Protocol, used for some tools only, or skipped. Find out exactly which session operations each major agent tool supports today.

**Context (as of 2026-11-19, from web search, not verified):**
- Several projects drive Claude Code, Codex, Cursor and Gemini over ACP.
- Codex reportedly added `codex queue` in v0.149 (Aug 2026) to send a message into a running session. Only one blog source.
- A Claude Code request to send a message to a running background session was closed in July 2026; whether it shipped is unknown.
- Spine §6 defines the adapter operations we need: `start, resume, send, fork, status`, plus fallbacks.

**Job:**
1. Read the ACP specification and its official docs. Record the version, who maintains it, and the license.
2. Map ACP's methods to our five operations. For each, note: supported, partly supported (how), or not supported.
3. For each of **Claude Code, Codex CLI, Gemini CLI, OpenCode** (and Cursor and Copilot if cheap): does it speak ACP natively, through an official adapter, or through a community adapter? Then, both through ACP and through the tool's own CLI or SDK, can you:
   - start a headless session in a given directory with a prompt
   - resume a session by id
   - send a message to a **running** session
   - fork a session
   - read status: running / idle / needs-input / done
   - get structured output (JSON events)
4. Note each tool's session identifiers and where it stores transcripts. They matter for `status` fallbacks.
5. Where cheap, verify claims hands-on in a scratch directory (e.g. `--help` output, a minimal ACP client run). Mark every claim *verified* or *from docs*.
6. Recommend: ACP-only, ACP plus native fallbacks, or native adapters only. Sketch the Go adapter interface this implies.

**Touches:** `docs/research/R1-acp.md` only.

**Definition of done:**
- [ ] Capability matrix (tool × operation × ACP vs native), each cell marked *verified* or *from docs*, with source links
- [ ] Recommendation with tradeoffs, and a sketch of the adapter interface
- [ ] Every choice needing the lead written as a decision record draft in the report (tier, options, recommendation)
- [ ] Handoff written in §C, including FRICTION

**Out of scope:** writing adapter code; designing the event schema; the UI.

**Escalate if:** ACP turns out to be abandoned or locked to one vendor; a tool's license or terms forbid driving it programmatically; anything needs paid accounts beyond what the lead has.

**Start prompt:**

    Read .ekumen/spine.md, then this brief (.ekumen/briefs/R1-acp.md).
    Work only inside §A as amended by §B. Never edit the spine, contracts, §A or §B.
    Write your findings to docs/research/R1-acp.md. Report only by appending to §C.
    Anything above your tier: draft it as a decision record in your report, then stop.

## §B Amendments (orchestrator; append-only, dated)
- 2026-11-20: approved by lead.
- 2026-11-20: output path changed from `docs/research/R1-acp.md` to `.ekumen/research/R1-acp.md` (D-011). Read every `docs/research/` in §A as `.ekumen/research/`.

## §C Handoff (task session; append-only, dated)

<!-- Format:
### YYYY-MM-DD
STATUS: …
OUTPUT: …
DECISIONS RAISED: …
FOLLOW-UPS: …
DEVIATIONS: …
FRICTION: what was painful by hand; what was ambiguous in the Ekumen format
-->
