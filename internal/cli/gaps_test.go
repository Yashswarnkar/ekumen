package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/observe"
)

const gateDecision = `id: D-gate
title: Gate for T-gated
tier: 2
question: Go or stop?
options:
  - {id: go, tradeoff: moves}
  - {id: stop, tradeoff: waits}
recommendation: go
default_if_no_answer: null
deadline: null
blocks: [T-gated]
contract_impact: []
context:
  summary: A gate to apply.
`

// gapsFixture is buildFixture plus a ready task gated by two decided,
// unapplied decisions, and the input files for the new writes.
func gapsFixture(t *testing.T) *harness {
	h := newHarness(t)
	buildFixture(h)
	h.prs = observe.Static{}
	h.write(".ekumen/briefs/T-gated/brief.md", "# T-gated\n")
	h.write("t-gated.yaml", "id: T-gated\ntitle: Gated\nkind: build\nbranch: build/t-gated\nworktree: .worktrees/t-gated\nsession: t-gated\n")
	h.write("d-gate.yaml", gateDecision)
	h.write("d-gate2.yaml", strings.ReplaceAll(gateDecision, "D-gate", "D-gate-two"))
	h.write("c-http.yaml", "id: C-http-api\ntitle: HTTP API\nblocked_by: [D-claim-lane]\n")
	h.write("c-http.md", "---\nid: C-http-api\nversion: 1\n---\n# HTTP API\n")
	h.write("x-creds.yaml", "id: X-sandbox-creds\nwhat: Sandbox credentials\nowner_team: platform\nasked_on: 2026-11-18\nneeded_by: 2026-11-25\nblocks: [T-gated]\n")
	h.write("x-update.yaml", "needed_by: 2026-12-01\nowner_team: platform\n")
	h.write("apply.md", "D-gate-two: go, with the details from the thread.\n")
	h.must("--as", "orchestrator:main", "task", "draft", "--file", "t-gated.yaml")
	h.must("--as", "lead:yash", "task", "approve", "T-gated")
	h.must("--as", "orchestrator:main", "decision", "raise", "--file", "d-gate.yaml")
	h.must("--as", "orchestrator:main", "decision", "raise", "--file", "d-gate2.yaml")
	h.must("--as", "lead:yash", "decision", "answer", "D-gate", "go", "--note", "As recommended.")
	h.must("--as", "lead:yash", "decision", "answer", "D-gate-two", "go")
	return h
}

// Golden --json output of every write added by T-cli-gaps
// (C-cli-json v2 §1, §3), and their effect on state.
func TestNewWritesGolden_CLIJSON3(t *testing.T) {
	h := gapsFixture(t)
	if st := taskStatus(t, h, "T-gated"); st != "blocked" {
		t.Fatalf("T-gated %s, want blocked", st)
	}
	steps := []struct {
		name string
		args []string
	}{
		{"write-decision-apply.json", []string{"--as", "loop:main", "decision", "apply", "D-gate"}},
		{"write-decision-apply-again.json", []string{"--as", "loop:main", "decision", "apply", "D-gate"}},
		{"write-decision-apply-text.json", []string{"--as", "loop:main", "decision", "apply", "D-gate-two", "--amend-text-file", "apply.md"}},
		{"write-contract-draft.json", []string{"--as", "orchestrator:main", "contract", "draft", "--file", "c-http.yaml", "--text-file", "c-http.md"}},
		{"write-contract-block.json", []string{"--as", "orchestrator:main", "contract", "block", "C-http-api", "--add", "T-core", "--remove", "D-claim-lane", "--reason", "needs core"}},
		{"write-external-add.json", []string{"--as", "orchestrator:main", "external", "add", "--file", "x-creds.yaml"}},
		{"write-external-update.json", []string{"--as", "orchestrator:main", "external", "update", "X-sandbox-creds", "--file", "x-update.yaml"}},
		{"write-external-resolve.json", []string{"--as", "orchestrator:main", "external", "resolve", "X-sandbox-creds", "--note", "granted"}},
		{"write-task-attach.json", []string{"--as", "orchestrator:main", "task", "attach", "T-gated", "--session", "session:t-gated-2", "--adapter", "acp"}},
		{"write-task-attach-again.json", []string{"--as", "orchestrator:main", "task", "attach", "T-gated", "--session", "t-gated-2", "--adapter", "acp"}},
		{"write-task-defer.json", []string{"--as", "orchestrator:main", "task", "defer", "T-skills", "--reason", "after v0.1"}},
	}
	for _, st := range steps {
		golden(t, st.name, h.must(append(st.args, "--json")...))
		switch st.name {
		case "write-decision-apply.json":
			// D-gate-two still gates T-gated.
			if s := taskStatus(t, h, "T-gated"); s != "blocked" {
				t.Errorf("after one apply T-gated is %s", s)
			}
		case "write-decision-apply-text.json":
			if s := taskStatus(t, h, "T-gated"); s != "ready" {
				t.Errorf("after both applies T-gated is %s, want ready", s)
			}
			_, out, _ := h.run("decision", "show", "D-gate-two", "--json") // no tick: keep the goldens' ids
			d := decodeJSON(t, out)["decision"].(map[string]any)
			if u := d["applied"].(map[string]any)["unblocked_tasks"].([]any); len(u) != 1 || u[0] != "T-gated" {
				t.Errorf("D-gate-two unblocked %v, want [T-gated]", u)
			}
		case "write-external-add.json":
			if s := taskStatus(t, h, "T-gated"); s != "blocked" {
				t.Errorf("with an open external dependency T-gated is %s", s)
			}
		case "write-external-resolve.json":
			if s := taskStatus(t, h, "T-gated"); s != "ready" {
				t.Errorf("after resolve T-gated is %s", s)
			}
		case "write-task-defer.json":
			if s := taskStatus(t, h, "T-skills"); s != "deferred" {
				t.Errorf("T-skills %s", s)
			}
		}
	}
	golden(t, "task-show-gated.json", h.must("task", "show", "T-gated", "--json"))
	golden(t, "decision-show-gate.json", h.must("decision", "show", "D-gate", "--json"))
	golden(t, "projection-registry-gaps.yaml", readFile(t, filepath.Join(h.root, ".ekumen/registry.yaml")))
	if b := readFile(t, filepath.Join(h.root, ".ekumen/contracts/C-http-api.md")); !strings.Contains(b, "# HTTP API") {
		t.Errorf("contract body not written: %q", b)
	}
	// The session now attached owns the task's lane.
	h.write("h2.md", "## STATUS\n\nStarted.\n")
	h.must("--as", "session:t-gated-2", "task", "handoff", "T-gated", "--text-file", "h2.md")
	if code, out, _ := h.run("check", "--json"); code != 0 {
		t.Errorf("check after the new writes: %s", out)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// decision apply is idempotent: a second apply records nothing and writes
// no files (C-decision-record §3).
func TestDecisionApplyIdempotent_DecisionRecord3(t *testing.T) {
	h := gapsFixture(t)
	h.must("--as", "loop:main", "decision", "apply", "D-gate")
	events := countEvents(t, h)
	amends, _ := os.ReadDir(filepath.Join(h.root, ".ekumen/briefs/T-gated/amendments"))
	for i := 0; i < 2; i++ {
		out := decodeJSON(t, h.must("--as", "loop:main", "decision", "apply", "D-gate", "--json"))
		if len(out["events"].([]any)) != 0 || len(out["paths"].([]any)) != 0 || out["ok"] != true {
			t.Errorf("repeat apply wrote %v", out)
		}
	}
	if n := countEvents(t, h); n != events {
		t.Errorf("repeat apply recorded %d events", n-events)
	}
	if again, _ := os.ReadDir(filepath.Join(h.root, ".ekumen/briefs/T-gated/amendments")); len(again) != len(amends) || len(amends) != 1 {
		t.Errorf("amendments %d then %d, want 1 and 1", len(amends), len(again))
	}
}

// Lanes for the new writes (C-event-log v4 §3): refused, nothing recorded,
// no file left behind.
func TestNewWritesLanes_EventLog3(t *testing.T) {
	h := gapsFixture(t)
	h.must("--as", "orchestrator:main", "external", "add", "--file", "x-creds.yaml")
	cases := []struct {
		name string
		args []string
	}{
		{"session applies", []string{"--as", "session:t-gated", "decision", "apply", "D-gate"}},
		{"orchestrator applies as itself", []string{"--as", "orchestrator:main", "decision", "apply", "D-gate"}},
		{"lead applies", []string{"--as", "lead:yash", "decision", "apply", "D-gate"}},
		{"session defers", []string{"--as", "session:t-gated", "task", "defer", "T-gated", "--reason", "x"}},
		{"orchestrator without claim defers", []string{"--as", "orchestrator:other", "task", "defer", "T-gated", "--reason", "x"}},
		{"session attaches", []string{"--as", "session:t-gated", "task", "attach", "T-gated", "--session", "t-x"}},
		{"session drafts a contract", []string{"--as", "session:t-gated", "contract", "draft", "--file", "c-http.yaml", "--text-file", "c-http.md"}},
		{"lead drafts a contract", []string{"--as", "lead:yash", "contract", "draft", "--file", "c-http.yaml", "--text-file", "c-http.md"}},
		{"session blocks a contract", []string{"--as", "session:t-gated", "contract", "block", "C-cli-json", "--add", "T-core", "--reason", "x"}},
		{"lead adds an external", []string{"--as", "lead:yash", "external", "add", "--file", "x-creds.yaml"}},
		{"session updates an external", []string{"--as", "session:t-gated", "external", "update", "X-sandbox-creds", "--file", "x-update.yaml"}},
		{"session resolves an external", []string{"--as", "session:t-gated", "external", "resolve", "X-sandbox-creds", "--note", "x"}},
	}
	before := countEvents(t, h)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, _ := h.run(append(c.args, "--json")...)
			if code != ExitRefused {
				t.Fatalf("exit %d, want %d: %s", code, ExitRefused, out)
			}
			if e := decodeJSON(t, out)["error"].(map[string]any); e["code"] != CodeLane {
				t.Errorf("code %v, want lane: %v", e["code"], e["message"])
			}
		})
	}
	if after := countEvents(t, h); after != before {
		t.Errorf("refused writes recorded %d events", after-before)
	}
	if _, err := os.Stat(filepath.Join(h.root, ".ekumen/contracts/C-http-api.md")); err == nil {
		t.Error("a refused contract draft left its body behind")
	}
	if ents, _ := os.ReadDir(filepath.Join(h.root, ".ekumen/briefs/T-gated/amendments")); len(ents) != 0 {
		t.Errorf("a refused apply left %d amendments", len(ents))
	}
}

// Refusals and usage errors for the new writes, prefixes and front matter.
func TestNewWritesErrors_CLIJSON1(t *testing.T) {
	h := gapsFixture(t)
	h.write("t-wrong.md", "---\ntask: T-core\nauthor: t-gated\ndate: 2026-11-20\n---\n\n## STATUS\n\nx\n")
	h.write("bad-date.md", "---\ntask: T-gated\ndate: 20 Nov\n---\n\n## STATUS\n\nx\n")
	h.write("bad-author.md", "---\nauthor: someone-else\n---\n\n## STATUS\n\nx\n")
	h.write("x-bad.yaml", "id: X-bad\nwhat: x\nowner_team: t\nasked_on: soon\nneeded_by: 2026-11-25\nblocks: []\n")
	h.write("x-field.yaml", "id: X-bad\nwhat: x\nowner: t\n")
	h.write("x-upd-bad.yaml", "aliases: [X9]\n")
	h.write("c-nobody.yaml", "id: C-nobody\ntitle: No body\n")
	h.write("c-escape.yaml", "id: C-escape\ntitle: Escape\npath: ../../etc/x.md\n")
	h.write("d-pending.yaml", strings.ReplaceAll(gateDecision, "D-gate", "D-pending"))
	h.must("--as", "orchestrator:main", "decision", "raise", "--file", "d-pending.yaml")
	h.must("--as", "lead:yash", "question", "ask", "T-gated", "--text", "One?", "--to", "orchestrator")
	h.must("--as", "lead:yash", "question", "ask", "T-gated", "--text", "Two?", "--to", "orchestrator")
	cases := []struct {
		name  string
		args  []string
		code  int
		ecode string
		field string
	}{
		{"apply pending", []string{"--as", "loop:main", "decision", "apply", "D-pending"}, ExitRefused, CodeValidation, "id"},
		{"apply superseded", []string{"--as", "loop:main", "decision", "apply", "D-old-choice"}, ExitRefused, CodeValidation, "id"},
		{"apply unknown", []string{"--as", "loop:main", "decision", "apply", "D-nope"}, ExitNotFound, CodeNotFound, "id"},
		{"defer without reason", []string{"--as", "orchestrator:main", "task", "defer", "T-gated"}, ExitUsage, CodeUsage, "reason"},
		{"attach without session", []string{"--as", "orchestrator:main", "task", "attach", "T-gated"}, ExitUsage, CodeUsage, "session"},
		{"attach a role", []string{"--as", "orchestrator:main", "task", "attach", "T-gated", "--session", "lead"}, ExitRefused, CodeValidation, "session"},
		{"contract id in use", []string{"--as", "orchestrator:main", "contract", "draft", "--file", "c-http.yaml"}, ExitRefused, CodeValidation, ""},
		{"contract without body", []string{"--as", "orchestrator:main", "contract", "draft", "--file", "c-nobody.yaml"}, ExitRefused, CodeValidation, "path"},
		{"contract path escapes", []string{"--as", "orchestrator:main", "contract", "draft", "--file", "c-escape.yaml", "--text-file", "c-http.md"}, ExitRefused, CodeValidation, "path"},
		{"contract block nothing", []string{"--as", "orchestrator:main", "contract", "block", "C-cli-json", "--reason", "x"}, ExitUsage, CodeUsage, ""},
		{"contract block unknown", []string{"--as", "orchestrator:main", "contract", "block", "C-nope", "--add", "T-core", "--reason", "x"}, ExitNotFound, CodeNotFound, "id"},
		{"external bad date", []string{"--as", "orchestrator:main", "external", "add", "--file", "x-bad.yaml"}, ExitRefused, CodeValidation, "asked_on"},
		{"external unknown field", []string{"--as", "orchestrator:main", "external", "add", "--file", "x-field.yaml"}, ExitRefused, CodeValidation, "owner"},
		{"external id in use", []string{"--as", "orchestrator:main", "external", "add", "--file", "x-creds.yaml"}, ExitRefused, CodeValidation, "id"},
		{"external update field", []string{"--as", "orchestrator:main", "external", "update", "X-sandbox-creds", "--file", "x-upd-bad.yaml"}, ExitRefused, CodeValidation, "aliases"},
		{"external update unknown", []string{"--as", "orchestrator:main", "external", "update", "X-nope", "--file", "x-update.yaml"}, ExitNotFound, CodeNotFound, "id"},
		{"ambiguous question prefix", []string{"--as", "orchestrator:main", "question", "answer", "q-", "--text", "x"}, ExitUsage, CodeUsage, "id"},
		{"unknown route prefix", []string{"--as", "orchestrator:main", "route", "resolve", "route-zzz", "--note", "x"}, ExitNotFound, CodeNotFound, "id"},
		{"handoff for another task in front matter", []string{"--as", "session:t-gated", "task", "handoff", "T-gated", "--text-file", "t-wrong.md"}, ExitRefused, CodeValidation, "task"},
		{"handoff bad date", []string{"--as", "session:t-gated", "task", "handoff", "T-gated", "--text-file", "bad-date.md"}, ExitRefused, CodeValidation, "date"},
		{"handoff bad author", []string{"--as", "session:t-gated", "task", "handoff", "T-gated", "--text-file", "bad-author.md"}, ExitRefused, CodeValidation, "author"},
	}
	h.must("--as", "orchestrator:main", "external", "add", "--file", "x-creds.yaml")
	h.must("--as", "orchestrator:main", "contract", "draft", "--file", "c-http.yaml", "--text-file", "c-http.md")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, _ := h.run(append(c.args, "--json")...)
			if code != c.code {
				t.Fatalf("exit %d, want %d: %s", code, c.code, out)
			}
			e := decodeJSON(t, out)["error"].(map[string]any)
			if e["code"] != c.ecode {
				t.Errorf("code %v, want %s (%v)", e["code"], c.ecode, e["message"])
			}
			if c.field != "" && e["field"] != c.field {
				t.Errorf("field %v, want %s (%v)", e["field"], c.field, e["message"])
			}
			if c.name == "ambiguous question prefix" && strings.Count(e["message"].(string), "q-01") < 2 {
				t.Errorf("ambiguity should list the matches: %v", e["message"])
			}
		})
	}
	if _, err := os.Stat(filepath.Join(h.root, "etc")); err == nil {
		t.Error("contract path escaped the project")
	}
}

// Question and route ids accept a unique prefix (C-cli-json v2 §1).
func TestIDPrefixes_CLIJSON1(t *testing.T) {
	h := gapsFixture(t)
	_, out, _ := h.run("question", "list", "--open", "--for", "t-cli", "--json")
	qid := decodeJSON(t, out)["questions"].([]any)[0].(map[string]any)["id"].(string)
	h.must("--as", "session:t-cli", "question", "answer", strings.ToUpper(qid[:len(qid)-4]), "--text", "Yes.")
	_, out, _ = h.run("question", "list", "--open", "--for", "t-cli", "--json")
	if qs := decodeJSON(t, out)["questions"].([]any); len(qs) != 0 {
		t.Errorf("question still open: %v", qs)
	}
	_, out, _ = h.run("inbox", "--for", "orchestrator", "--json")
	var route string
	for _, it := range decodeJSON(t, out)["items"].([]any) {
		if id := it.(map[string]any)["id"].(string); strings.HasPrefix(id, "route-") {
			route = id
		}
	}
	if route == "" {
		t.Fatalf("no route in %s", out)
	}
	h.must("--as", "orchestrator:main", "route", "resolve", route[:len(route)-3], "--note", "done")
}

// Entry front matter: added when missing, kept when given (docs/cli.md).
func TestEntryFrontMatter(t *testing.T) {
	f, err := ParseEntryFront("---\ntask: T-cli\nauthor: t-cli\ndate: 2026-11-20\nextra: [1]\n---\n\n# x\n")
	if err != nil || !f.Present || f.Task != "T-cli" || f.Author != "t-cli" || f.Date != "2026-11-20" {
		t.Errorf("%+v %v", f, err)
	}
	if f, err := ParseEntryFront("# no front matter\n"); err != nil || f.Present {
		t.Errorf("%+v %v", f, err)
	}
	if _, err := ParseEntryFront("---\ntask: T-cli\n"); err == nil {
		t.Error("unclosed front matter accepted")
	}
	if _, err := ParseEntryFront("---\ntask: [T-cli]\n---\n"); err == nil {
		t.Error("non-string task accepted")
	}

	h := gapsFixture(t)
	h.write("plain.md", "## STATUS\n\nGoing.\n")
	h.must("--as", "session:t-gated", "task", "handoff", "T-gated", "--text-file", "plain.md")
	h.write("given.md", "---\ntask: T-gated\nauthor: session:t-gated\ndate: 2026-11-19\n---\n\n## STATUS\n\nGiven.\n")
	h.must("--as", "session:t-gated", "task", "handoff", "T-gated", "--text-file", "given.md")
	hs := decodeJSON(t, h.must("task", "show", "T-gated", "--json"))["brief"].(map[string]any)["handoffs"].([]any)
	if len(hs) != 2 {
		t.Fatalf("handoffs %v", hs)
	}
	var texts []string
	for _, e := range hs {
		texts = append(texts, e.(map[string]any)["text"].(string))
	}
	all := strings.Join(texts, "\n")
	if !strings.Contains(all, "---\ntask: T-gated\nauthor: t-gated\ndate: 2026-11-20\n---\n\n## STATUS\n\nGoing.") {
		t.Errorf("front matter not added:\n%s", all)
	}
	if !strings.Contains(all, "date: 2026-11-19") {
		t.Errorf("given front matter not kept:\n%s", all)
	}
}
