package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/observe"
)

// Golden tests: every read command's --json output against the fixture
// (C-cli-json §2, §4).
func TestReadCommandsGolden_CLIJSON2(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	cases := []struct {
		name string
		args []string
	}{
		{"status.json", []string{"status", "--as", "lead:yash"}},
		{"inbox-lead.json", []string{"inbox", "--for", "lead"}},
		{"inbox-orchestrator.json", []string{"inbox", "--as", "orchestrator:main"}},
		{"inbox-session.json", []string{"inbox", "--for", "session:t-cli"}},
		{"task-list.json", []string{"task", "list"}},
		{"task-list-blocked.json", []string{"task", "list", "--status", "blocked"}},
		{"task-show.json", []string{"task", "show", "T-cli"}},
		{"decision-list.json", []string{"decision", "list"}},
		{"decision-list-pending.json", []string{"decision", "list", "--status", "pending"}},
		{"decision-show.json", []string{"decision", "show", "D-yaml-library"}},
		{"question-list.json", []string{"question", "list"}},
		{"question-list-open.json", []string{"question", "list", "--open", "--for", "t-cli"}},
		{"graph.json", []string{"graph"}},
		{"check.json", []string{"check"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, errOut := h.run(append(c.args, "--json")...)
			if code != 0 {
				t.Fatalf("exit %d: %s %s", code, out, errOut)
			}
			decodeJSON(t, out) // exactly one object
			golden(t, c.name, out)
		})
	}
}

func TestProjectionsGolden_EventLog8(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	h.must("project") // with observed PRs now
	for _, f := range []string{"decisions/D-yaml-library.yaml", "decisions/D-claim-lane.yaml", "decisions/D-old-choice.yaml", "registry.yaml", "state.json"} {
		b, err := os.ReadFile(filepath.Join(h.root, ".ekumen", f))
		if err != nil {
			t.Fatal(err)
		}
		golden(t, "projection-"+strings.ReplaceAll(f, "/", "-"), string(b))
	}
	// Running it again changes nothing.
	out := h.must("project", "--json")
	if got := decodeJSON(t, out)["paths"].([]any); len(got) != 0 {
		t.Errorf("second `ekumen project` rewrote %v", got)
	}
}

func TestProjectionsSkipEmptyLog(t *testing.T) {
	h := newHarness(t)
	h.write(".ekumen/registry.yaml", "# hand-written\nproject: demo\n")
	code, out, errOut := h.run("project", "--json")
	if code != 0 || !strings.Contains(errOut, "empty") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	b, _ := os.ReadFile(filepath.Join(h.root, ".ekumen/registry.yaml"))
	if string(b) != "# hand-written\nproject: demo\n" {
		t.Errorf("hand-written registry was overwritten: %s", b)
	}
	_ = out
}

// Exit codes and the error shape (C-cli-json §1).
func TestExitCodes_CLIJSON1(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	h.write("bad-decision.yaml", strings.Replace(tierOneDecision, "id: D-yaml-library", "id: D-another", 1)+"status: decided\n")
	h.write("tier1-no-default.yaml", strings.NewReplacer("id: D-yaml-library", "id: D-no-default", "default_if_no_answer: go-yaml-in\n", "").Replace(tierOneDecision))
	cases := []struct {
		name  string
		args  []string
		code  int
		ecode string
		field string
	}{
		{"unknown command", []string{"frobnicate"}, ExitUsage, CodeUsage, ""},
		{"missing argument", []string{"task", "show"}, ExitUsage, CodeUsage, ""},
		{"bad flag", []string{"task", "list", "--nope"}, ExitUsage, CodeUsage, ""},
		{"bad status", []string{"task", "list", "--status", "sleeping"}, ExitUsage, CodeUsage, "status"},
		{"no actor", []string{"decision", "answer", "D-claim-lane", "claiming-session"}, ExitUsage, CodeUsage, ""},
		{"bad actor", []string{"--as", "boss:me", "decision", "answer", "D-claim-lane", "x"}, ExitUsage, CodeUsage, ""},
		{"unknown task", []string{"task", "show", "T-nope"}, ExitNotFound, CodeNotFound, "id"},
		{"unknown decision", []string{"decision", "show", "D-nope"}, ExitNotFound, CodeNotFound, "id"},
		{"unknown question", []string{"--as", "lead:yash", "question", "answer", "q-x", "--text", "t"}, ExitNotFound, CodeNotFound, "id"},
		{"missing file", []string{"--as", "lead:yash", "decision", "raise", "--file", "nope.yaml"}, ExitNotFound, CodeNotFound, "file"},
		{"not an option", []string{"--as", "lead:yash", "decision", "answer", "D-claim-lane", "maybe"}, ExitRefused, CodeValidation, "answer"},
		{"other without note", []string{"--as", "lead:yash", "decision", "answer", "D-claim-lane", "other"}, ExitRefused, CodeValidation, "note"},
		{"decided twice", []string{"--as", "lead:yash", "decision", "answer", "D-yaml-library", "json-only"}, ExitRefused, CodeValidation, "id"},
		{"lifecycle field", []string{"--as", "lead:yash", "decision", "raise", "--file", "bad-decision.yaml"}, ExitRefused, CodeValidation, "status"},
		{"tier 1 needs default", []string{"--as", "lead:yash", "decision", "raise", "--file", "tier1-no-default.yaml"}, ExitRefused, CodeValidation, "default_if_no_answer"},
		{"id in use", []string{"--as", "lead:yash", "decision", "raise", "--file", "d-yaml.yaml"}, ExitRefused, CodeValidation, "id"},
		{"approve twice", []string{"--as", "lead:yash", "task", "approve", "T-cli"}, ExitRefused, CodeValidation, "id"},
		{"freeze same text", []string{"--as", "lead:yash", "contract", "freeze", "C-event-log"}, ExitRefused, CodeValidation, "version"},
		{"no root", []string{"--root", "/nonexistent", "status"}, ExitNotFound, CodeNotFound, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, _ := h.run(append(c.args, "--json")...)
			if code != c.code {
				t.Fatalf("exit %d, want %d: %s", code, c.code, out)
			}
			e := decodeJSON(t, out)["error"].(map[string]any)
			if e["code"] != c.ecode {
				t.Errorf("code %v, want %s", e["code"], c.ecode)
			}
			if c.field != "" && e["field"] != c.field {
				t.Errorf("field %v, want %s (%v)", e["field"], c.field, e["message"])
			}
		})
	}
}

func TestCheckFindsConflicts_EventLog5(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	// A second answer that bypassed the CLI (e.g. from a git merge).
	h.raw(event.DecisionDecidedType, event.RoleLead, "yash", event.KindDecision, "D-yaml-library", event.DecisionDecided{Answer: "json-only"})
	// A handoff written outside its lane.
	h.raw(event.TaskHandoffType, event.RoleSession, "intruder", event.KindTask, "T-core", event.TaskHandoff{File: "x", StatusLine: "x"})
	// §A edited after approval.
	h.write(".ekumen/briefs/T-core/brief.md", "# T-core, edited\n")
	code, out, _ := h.run("check", "--json")
	if code != ExitConflict {
		t.Fatalf("exit %d, want %d: %s", code, ExitConflict, out)
	}
	var res struct {
		OK        bool `json:"ok"`
		Conflicts []struct {
			Rule string `json:"rule"`
		} `json:"conflicts"`
		Invalid []struct{ Path, Error string } `json:"invalid"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	var rules []string
	for _, c := range res.Conflicts {
		rules = append(rules, c.Rule)
	}
	if got := strings.Join(rules, ","); got != "lane,decided-twice,frozen-text-changed" {
		t.Errorf("rules %s", got)
	}
	if res.OK || len(res.Invalid) != 1 { // the handoff's file "x" is missing
		t.Errorf("ok %v, invalid %v", res.OK, res.Invalid)
	}
}

// Lane refusals (C-event-log v3 §3, C-cli-json §1).
func TestLaneRefusals_EventLog3(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	h.write("d-session.yaml", strings.NewReplacer("id: D-yaml-library", "id: D-from-core", "raised_by: {task: null, session: main, on: 2026-11-20}", "raised_by: {task: T-core, session: t-cli, on: 2026-11-20}").Replace(tierOneDecision))
	h.write("t-x.yaml", "id: T-x\ntitle: X\nkind: build\n")
	cases := []struct {
		name string
		args []string
	}{
		{"session answers a decision", []string{"--as", "session:t-cli", "decision", "answer", "D-claim-lane", "loop-only"}},
		{"orchestrator approves", []string{"--as", "orchestrator:main", "task", "approve", "T-skills"}},
		{"session drafts a task", []string{"--as", "session:t-cli", "task", "draft", "--file", "t-x.yaml"}},
		{"handoff for another task", []string{"--as", "session:t-cli", "task", "handoff", "T-core", "--text-file", "handoff.md"}},
		{"decision for another task", []string{"--as", "session:t-cli", "decision", "raise", "--file", "d-session.yaml"}},
		{"non-addressee answers", []string{"--as", "session:t-core", "question", "answer", "QID", "--text", "x"}},
		{"orchestrator without claim", []string{"--as", "orchestrator:other", "task", "amend", "T-cli", "--text-file", "amend.md"}},
		{"second orchestrator claim", []string{"--as", "orchestrator:other", "claim", "orchestrator"}},
		{"release a role not held", []string{"--as", "session:t-cli", "release", "orchestrator"}},
		{"resolve someone else's route", []string{"--as", "session:t-cli", "route", "resolve", "ROUTE", "--note", "x"}},
		{"lead freezes nothing but session tries", []string{"--as", "session:t-cli", "contract", "freeze", "C-cli-json"}},
	}
	_, out, _ := h.run("question", "list", "--open", "--for", "t-cli", "--json")
	qid := decodeJSON(t, out)["questions"].([]any)[0].(map[string]any)["id"].(string)
	_, out, _ = h.run("inbox", "--for", "orchestrator", "--json")
	route := decodeJSON(t, out)["items"].([]any)[0].(map[string]any)["id"].(string)
	before := countEvents(t, h)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{}, c.args...)
			for i, a := range args {
				args[i] = strings.NewReplacer("QID", qid, "ROUTE", route).Replace(a)
			}
			code, out, _ := h.run(append(args, "--json")...)
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
	// A refused handoff leaves no file behind.
	ents, _ := os.ReadDir(filepath.Join(h.root, ".ekumen/briefs/T-core/handoffs"))
	if len(ents) != 0 {
		t.Errorf("refused handoff left %d files", len(ents))
	}
}

func countEvents(t *testing.T, h *harness) int {
	n := 0
	filepath.Walk(filepath.Join(h.root, ".ekumen/events"), func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// End to end: raise → question → answer → amend → handoff → status, on a
// fresh temp project, as the different roles would run it.
func TestEndToEnd(t *testing.T) {
	h := newHarness(t)
	h.write(".ekumen/briefs/T-demo/brief.md", "# T-demo\n")
	h.write("t.yaml", "id: T-demo\ntitle: Demo\nkind: build\nbranch: build/t-demo\nworktree: .worktrees/t-demo\nblocked_by: []\nsession: t-demo\n")
	h.must("--as", "orchestrator:main", "claim", "orchestrator")
	h.must("--as", "orchestrator:main", "task", "draft", "--file", "t.yaml")
	h.must("--as", "lead:yash", "task", "approve", "T-demo")
	if st := taskStatus(t, h, "T-demo"); st != "ready" {
		t.Fatalf("status %s, want ready", st)
	}

	// raise (from stdin, raised_by filled in from the actor)
	d := strings.NewReplacer("id: D-yaml-library", "id: D-demo", "raised_by: {task: null, session: main, on: 2026-11-20}\n", "", "blocks: [T-cli]", "blocks: [T-demo]").Replace(tierOneDecision)
	code, out, errOut := h.runIn(d, "--as", "session:t-demo", "decision", "raise", "--file", "-", "--json")
	if code != 0 {
		t.Fatalf("raise: %s %s", out, errOut)
	}
	h.tick()
	var dec struct {
		Decision struct {
			RaisedBy struct {
				Task    *string `json:"task"`
				Session string  `json:"session"`
			} `json:"raised_by"`
			Priority float64 `json:"priority"`
		} `json:"decision"`
	}
	json.Unmarshal([]byte(h.must("decision", "show", "D-demo", "--json")), &dec)
	if dec.Decision.RaisedBy.Task == nil || *dec.Decision.RaisedBy.Task != "T-demo" || dec.Decision.RaisedBy.Session != "t-demo" {
		t.Errorf("raised_by not filled: %+v", dec.Decision.RaisedBy)
	}
	if dec.Decision.Priority != 4 { // 1 task × due tomorrow (4)
		t.Errorf("priority %v", dec.Decision.Priority)
	}
	if st := taskStatus(t, h, "T-demo"); st != "blocked" {
		t.Errorf("status %s, want blocked", st)
	}

	// question from the lead, routed by default to the raising session
	h.must("--as", "lead:yash", "question", "ask", "D-demo", "--text", "Why not JSON?")
	inbox := decodeJSON(t, h.must("--as", "session:t-demo", "inbox", "--json"))["items"].([]any)
	if len(inbox) != 1 || inbox[0].(map[string]any)["kind"] != "question" {
		t.Fatalf("session inbox %v", inbox)
	}
	qid := inbox[0].(map[string]any)["id"].(string)
	h.must("--as", "session:t-demo", "question", "answer", qid, "--text", "People edit these by hand.")

	// answer
	h.must("--as", "lead:yash", "decision", "answer", "D-demo", "go-yaml-in")
	b, _ := os.ReadFile(filepath.Join(h.root, ".ekumen/decisions/D-demo.yaml"))
	for _, want := range []string{"status: decided", "answer: go-yaml-in", "answered_by: session:t-demo", "decided_by: yash"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("projection lacks %q:\n%s", want, b)
		}
	}

	// amend
	h.write("am.md", "D-demo: go-yaml-in.\n")
	h.must("--as", "orchestrator:main", "task", "amend", "T-demo", "--text-file", "am.md", "--note", "D-demo")

	// handoff, read from stdin
	code, out, errOut = h.runIn(strings.NewReplacer("task: T-cli", "task: T-demo", "author: t-cli", "author: t-demo").Replace(handoffText), "--as", "session:t-demo", "task", "handoff", "T-demo", "--text-file", "-", "--json")
	if code != 0 {
		t.Fatalf("handoff: %s %s", out, errOut)
	}
	h.tick()
	w := decodeJSON(t, out)
	if len(w["events"].([]any)) != 1 || !strings.Contains(strings.Join(toStrings(w["paths"]), " "), "handoffs/") {
		t.Errorf("handoff result %v", w)
	}
	show := decodeJSON(t, h.must("task", "show", "T-demo", "--json"))
	brief := show["brief"].(map[string]any)
	if len(brief["amendments"].([]any)) != 1 || len(brief["handoffs"].([]any)) != 1 {
		t.Errorf("brief %v", brief)
	}
	if a := brief["handoffs"].([]any)[0].(map[string]any)["author"]; a != "session:t-demo" {
		t.Errorf("handoff author %v", a)
	}

	// status: the handoff has follow-ups, so it is routed to the orchestrator
	st := decodeJSON(t, h.must("--as", "orchestrator:main", "status", "--json"))
	counts := st["counts"].(map[string]any)
	if counts["decisions_open"].(float64) != 0 || counts["routes_open"].(float64) != 1 {
		t.Errorf("counts %v", counts)
	}
	top := st["inbox_top"].([]any)
	if len(top) != 1 || !strings.HasPrefix(top[0].(map[string]any)["id"].(string), "route-") {
		t.Fatalf("inbox_top %v", top)
	}
	h.must("--as", "orchestrator:main", "route", "resolve", top[0].(map[string]any)["id"].(string), "--note", "filed")
	st = decodeJSON(t, h.must("--as", "orchestrator:main", "status", "--json"))
	if n := st["counts"].(map[string]any)["routes_open"].(float64); n != 0 {
		t.Errorf("routes_open %v after resolve", n)
	}
	if code, out, _ := h.run("check", "--json"); code != 0 {
		t.Errorf("check: %s", out)
	}

	// map
	h.prs = observe.Static{{Number: 1, Branch: "build/t-demo", State: observe.StateOpen, Checks: observe.ChecksSuccess}}
	h.must("map", "--out", "map.html")
	page, err := os.ReadFile(filepath.Join(h.root, "map.html"))
	if err != nil || !strings.Contains(string(page), `"id":"T-demo"`) {
		t.Errorf("map: %v", err)
	}
	if st := taskStatus(t, h, "T-demo"); st != "in_review" {
		t.Errorf("status %s with an open PR, want in_review", st)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func taskStatus(t *testing.T, h *harness, id string) string {
	t.Helper()
	m := decodeJSON(t, h.must("task", "show", id, "--json"))
	return m["task"].(map[string]any)["status"].(string)
}

func TestObservedNullWhenUnavailable(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	h.prs = observe.None{}
	code, out, errOut := h.run("task", "list", "--json")
	if code != 0 || !strings.Contains(errOut, "observed state unknown") {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	for _, task := range decodeJSON(t, out)["tasks"].([]any) {
		if o := task.(map[string]any)["observed"]; o != nil {
			t.Errorf("observed %v, want null", o)
		}
	}
}

func TestParseHandoff(t *testing.T) {
	h := ParseHandoff(handoffText)
	if h.StatusLine != "Done, in review." || !h.HasFollowups || h.HasDecisions || h.HasDeviations {
		t.Errorf("%+v", h)
	}
	h = ParseHandoff("## Status\n- Blocked on D-x\n## Decisions raised\n```yaml\n- id: D-x\n```\n## Deviations\n1. none of note\n")
	if h.StatusLine != "Blocked on D-x" || h.HasFollowups || !h.HasDecisions || !h.HasDeviations {
		t.Errorf("%+v", h)
	}
}

func TestYAMLToJSONKeepsDatesAndOn(t *testing.T) {
	v, err := yamlToJSON([]byte("raised_by: {task: null, session: s, on: 2026-11-20}\ntier: 2\nok: yes\n"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	if got := string(b); got != `{"ok":"yes","raised_by":{"on":"2026-11-20","session":"s","task":null},"tier":2}` {
		t.Errorf("%s", got)
	}
}
