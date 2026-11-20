package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/observe"
	"github.com/Yashswarnkar/ekumen/internal/store/files"
)

var update = flag.Bool("update", false, "rewrite golden files")

// harness runs commands in-process against a temp project with a fixed
// clock, deterministic event ids and a fake PR observer.
type harness struct {
	t    *testing.T
	root string
	now  time.Time
	n    int
	prs  observe.Observer
	git  GitInfo
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	root := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(filepath.Join(root, ".ekumen"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, root: root, now: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC), prs: observe.Static{}}
}

func (h *harness) env(stdin string, stdout, stderr *bytes.Buffer) Env {
	return Env{
		Stdout: stdout, Stderr: stderr, Stdin: strings.NewReader(stdin),
		Getenv: func(string) string { return "" },
		Dir:    h.root,
		Now:    func() time.Time { return h.now },
		NewID: func(t time.Time) (string, error) {
			h.n++
			var ent [10]byte
			ent[8], ent[9] = byte(h.n>>8), byte(h.n)
			return event.ULIDFromParts(uint64(t.UnixMilli()), ent)
		},
		Observer: func(string) observe.Observer { return h.prs },
		Commit:   func(string) string { return "abc1234" },
		Git:      func(string) GitInfo { return h.git },
		Remote:   func(string) string { return h.git.Remote },
	}
}

// run runs one command; the clock moves one minute afterwards.
func (h *harness) run(args ...string) (int, string, string) {
	return h.runIn("", args...)
}

func (h *harness) runIn(stdin string, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(args, h.env(stdin, &out, &errb))
	return code, out.String(), errb.String()
}

func (h *harness) tick() { h.now = h.now.Add(time.Minute) }

// must runs a command that has to succeed, then advances the clock.
func (h *harness) must(args ...string) string {
	h.t.Helper()
	code, out, errOut := h.run(args...)
	if code != 0 {
		h.t.Fatalf("ekumen %s: exit %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, out, errOut)
	}
	h.tick()
	return out
}

func (h *harness) write(rel, body string) string {
	h.t.Helper()
	p := filepath.Join(h.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return p
}

// raw appends an event no CLI command writes (loop and orchestrator events
// outside C-cli-json), straight through the store.
func (h *harness) raw(typ event.Type, role event.Role, actor string, kind event.SubjectKind, subject string, data any) string {
	h.t.Helper()
	h.n++
	var ent [10]byte
	ent[8], ent[9] = byte(h.n>>8), byte(h.n)
	id, err := event.ULIDFromParts(uint64(h.now.UnixMilli()), ent)
	if err != nil {
		h.t.Fatal(err)
	}
	e, err := event.New(id, typ, h.now, event.Actor{Role: role, ID: actor}, event.Subject{Kind: kind, ID: subject}, data)
	if err != nil {
		h.t.Fatal(err)
	}
	b, _ := event.Encode(e)
	if _, err := files.New(filepath.Join(h.root, ".ekumen")).Append(b); err != nil {
		h.t.Fatal(err)
	}
	h.tick()
	return id
}

func decodeJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("not one JSON object: %v\n%s", err, s)
	}
	return m
}

var entryStamp = regexp.MustCompile(`[0-9]{8}T[0-9]{6}Z`)

// normalize hides entry file timestamps, which the file store takes from
// the real clock.
func normalize(s string) string { return entryStamp.ReplaceAllString(s, "<STAMP>") }

func golden(t *testing.T, name, got string) {
	t.Helper()
	got = normalize(got)
	p := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/cli -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from golden (run go test ./internal/cli -update to accept)\n--- got\n%s", name, got)
	}
}

const tierOneDecision = `id: D-yaml-library
title: Which YAML library the CLI uses
tier: 1
raised_by: {task: null, session: main, on: 2026-11-20}
question: Which library is allowed?
options:
  - {id: go-yaml-in, tradeoff: "go.yaml.in/yaml/v3: maintained"}
  - {id: json-only, tradeoff: Standard library only}
recommendation: go-yaml-in
default_if_no_answer: go-yaml-in
deadline: 2026-11-21
blocks: [T-cli]
contract_impact: []
context:
  summary: First dependency outside the standard library.
  evidence: [.ekumen/briefs/T-cli/brief.md]
`

const claimLaneDecision = `id: D-claim-lane
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
contract_impact: [C1]
context:
  summary: The two contracts disagree.
`

const handoffText = `---
task: T-cli
author: t-cli
---

# T-cli handoff

## STATUS

Done, in review.

## FOLLOW-UPS

- core lags C-event-log v3

## DECISIONS RAISED

None.

## DEVIATIONS

None.
`

// buildFixture makes a small project through the CLI where a command
// exists, and through the store for loop events.
func buildFixture(h *harness) {
	t := h.t
	t.Helper()
	h.write(".ekumen/contracts/C-event-log.md", "---\nid: C-event-log\nversion: 3\n---\n# Event log\n")
	h.write(".ekumen/contracts/C-cli-json.md", "---\nid: C-cli-json\n---\n# CLI\n")
	h.write(".ekumen/briefs/T-core/brief.md", "# T-core\n")
	h.write(".ekumen/briefs/T-cli/brief.md", "# T-cli\n\nBuild the CLI.\n")
	h.write("t-core.yaml", "id: T-core\ntitle: Core domain\nkind: build\nbranch: build/t-core\nworktree: .worktrees/t-core\nblocked_by: [C1]\ntier_grants: [push_own_branch]\nsession: t-core\n")
	h.write("t-cli.yaml", "id: T-cli\ntitle: ekumen CLI\nkind: build\nbranch: build/t-cli\nworktree: .worktrees/t-cli\nblocked_by: [T-core, C-cli-json]\ntier_grants: [push_own_branch, open_draft_pr]\nsession: t-cli\n")
	h.write("t-skills.yaml", "id: T-skills\ntitle: Skills\nkind: build\nblocked_by: [T-cli]\n")
	h.write("d-yaml.yaml", tierOneDecision)
	h.write("d-claim.yaml", claimLaneDecision)
	h.write("d-old.yaml", strings.NewReplacer("D-yaml-library", "D-old-choice", "tier: 1", "tier: 2", "default_if_no_answer: go-yaml-in", "default_if_no_answer: null", "blocks: [T-cli]", "blocks: [T-skills]").Replace(tierOneDecision))
	h.write("d-new.yaml", strings.NewReplacer("D-yaml-library", "D-new-choice", "tier: 1", "tier: 2", "default_if_no_answer: go-yaml-in", "default_if_no_answer: null", "blocks: [T-cli]", "blocks: [T-skills]", "deadline: 2026-11-21", "deadline: 2026-11-20").Replace(tierOneDecision))
	h.write("amend.md", "Use go.yaml.in/yaml/v3 (D-yaml-library).\n")
	h.write("handoff.md", handoffText)

	h.raw(event.ContractDraftedType, event.RoleOrchestrator, "main", event.KindContract, "C-event-log",
		event.ContractDrafted{Title: "Event log", Path: "contracts/C-event-log.md", Aliases: []string{"C1"}})
	h.raw(event.ContractDraftedType, event.RoleOrchestrator, "main", event.KindContract, "C-cli-json",
		event.ContractDrafted{Title: "CLI commands and JSON output", Path: "contracts/C-cli-json.md"})
	h.must("--as", "orchestrator:main", "claim", "orchestrator")
	h.must("--as", "lead:yash", "contract", "freeze", "C1")
	h.must("--as", "orchestrator:main", "task", "draft", "--file", "t-core.yaml")
	h.must("--as", "orchestrator:main", "task", "draft", "--file", "t-cli.yaml")
	h.must("--as", "orchestrator:main", "task", "draft", "--file", "t-skills.yaml")
	h.must("--as", "lead:yash", "task", "approve", "T-core")
	h.must("--as", "lead:yash", "task", "approve", "T-cli", "--grants", "push_own_branch,open_draft_pr,restack")
	h.must("--as", "orchestrator:main", "decision", "raise", "--file", "d-yaml.yaml")
	h.must("--as", "lead:yash", "decision", "answer", "D-yaml-library", "go-yaml-in", "--note", "As recommended.")
	h.raw(event.DecisionAppliedType, event.RoleLoop, "loop", event.KindDecision, "D-yaml-library",
		event.DecisionApplied{AmendedTasks: []string{"T-cli"}, UnblockedTasks: []string{"T-cli"}, NotifiedSessions: []string{}})
	h.must("--as", "session:t-cli", "decision", "raise", "--file", "d-claim.yaml")
	h.must("--as", "lead:yash", "decision", "raise", "--file", "d-old.yaml")
	h.must("--as", "lead:yash", "decision", "raise", "--file", "d-new.yaml")
	h.must("--as", "lead:yash", "decision", "supersede", "D-old-choice", "--by", "D-new-choice", "--note", "replaced")
	h.must("--as", "lead:yash", "question", "ask", "T-cli", "--text", "Is the map offline?", "--to", "session:t-cli")
	h.must("--as", "session:t-cli", "question", "ask", "C-cli-json", "--text", "May inbox items carry a subject?")
	code, out, _ := h.run("question", "list", "--json", "--for", "orchestrator")
	if code != 0 {
		t.Fatal(out)
	}
	qs := decodeJSON(t, out)["questions"].([]any)
	h.must("--as", "orchestrator:main", "question", "answer", qs[0].(map[string]any)["id"].(string), "--text", "Yes, as a minor addition.")
	h.must("--as", "orchestrator:main", "task", "amend", "T-cli", "--text-file", "amend.md", "--note", "D-yaml-library")
	h.must("--as", "session:t-cli", "task", "handoff", "T-cli", "--text-file", "handoff.md")
	h.prs = observe.Static{
		{Number: 5, Branch: "build/t-core", State: observe.StateMerged, Checks: observe.ChecksSuccess, Title: "T-core"},
		{Number: 7, Branch: "build/t-cli", State: observe.StateDraft, Checks: observe.ChecksPending, Title: "T-cli"},
	}
}
