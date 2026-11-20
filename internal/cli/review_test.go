package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

func TestProjectionFailureIsNotSuccess(t *testing.T) {
	h := gapsFixture(t)
	dir := filepath.Join(h.root, ".ekumen", "decisions")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	h.write("d-ro.yaml", strings.ReplaceAll(gateDecision, "D-gate", "D-readonly"))
	code, out, _ := h.run("--json", "--as", "lead:me", "decision", "raise", "--file", "d-ro.yaml")
	if code != ExitInternal {
		t.Fatalf("exit %d, want %d\n%s", code, ExitInternal, out)
	}
	e := decodeJSON(t, out)["error"].(map[string]any)
	if e["code"] != CodeInternal || !strings.Contains(e["message"].(string), "events recorded (") || !strings.Contains(e["message"].(string), "ekumen project") {
		t.Errorf("error %v", e)
	}
	if _, out, _ := h.run("decision", "show", "D-readonly", "--json"); !strings.Contains(out, `"D-readonly"`) {
		t.Errorf("event not kept: %s", out)
	}
}

func TestApplyAmendTextFrontMatterMustMatchTask(t *testing.T) {
	h := gapsFixture(t)
	h.write("wrong-task.md", "---\ntask: T-core\n---\n\nGo.\n")
	out := mustExit(t, h, ExitRefused, "--as", "loop:main", "decision", "apply", "D-gate", "--amend-text-file", "wrong-task.md")
	if e := decodeJSON(t, out)["error"].(map[string]any); e["field"] != "task" {
		t.Errorf("error %v, want field task", e)
	}

	h.write("d-two.yaml", strings.NewReplacer("D-gate", "D-two", "blocks: [T-gated]", "blocks: [T-gated, T-skills]").Replace(gateDecision))
	h.must("--as", "lead:yash", "decision", "raise", "--file", "d-two.yaml")
	h.must("--as", "lead:yash", "decision", "answer", "D-two", "go")
	h.write("front.md", "---\nauthor: main\n---\n\nGo.\n")
	out = mustExit(t, h, ExitRefused, "--as", "loop:main", "decision", "apply", "D-two", "--amend-text-file", "front.md")
	if e := decodeJSON(t, out)["error"].(map[string]any); e["field"] != "front_matter" {
		t.Errorf("error %v, want field front_matter", e)
	}
	if fs, _ := filepath.Glob(filepath.Join(h.root, ".ekumen/briefs/*/amendments/*")); len(fs) != 1 {
		t.Errorf("amendment files written: %v", fs)
	}
}

func TestApplyRerunAfterPartialApply(t *testing.T) {
	h := gapsFixture(t)
	h.write("d-two.yaml", strings.NewReplacer("D-gate", "D-two", "blocks: [T-gated]", "blocks: [T-gated, T-skills]").Replace(gateDecision))
	h.must("--as", "lead:yash", "decision", "raise", "--file", "d-two.yaml")
	h.must("--as", "lead:yash", "decision", "answer", "D-two", "go")
	h.write(".ekumen/briefs/T-gated/amendments/20261120T090000Z-main.md", "---\ntask: T-gated\nauthor: main\ndate: 2026-11-20\n---\n\nApplied D-two.\n")
	h.raw(event.TaskAmendedType, event.RoleLoop, "main", event.KindTask, "T-gated",
		event.TaskAmended{File: "briefs/T-gated/amendments/20261120T090000Z-main.md", TextSHA256: "x", Note: "applies D-two", ByDecision: "D-two"})

	h.must("--as", "loop:main", "decision", "apply", "D-two")
	for task, want := range map[string]int{"T-gated": 1, "T-skills": 1} {
		var n int
		evs, _ := filepath.Glob(filepath.Join(h.root, ".ekumen/events/task", task, "*.TaskAmended.json"))
		for _, p := range evs {
			b, _ := os.ReadFile(p)
			if strings.Contains(string(b), `"by_decision": "D-two"`) || strings.Contains(string(b), `"by_decision":"D-two"`) {
				n++
			}
		}
		if n != want {
			t.Errorf("%s: %d amendments by D-two, want %d", task, n, want)
		}
	}
	_, out, _ := h.run("decision", "show", "D-two", "--json")
	if d := decodeJSON(t, out)["decision"].(map[string]any); d["applied_on"] == nil {
		t.Errorf("not applied: %v", d)
	}
}

func TestDecisionRaiseRecordsIDsNotAliases(t *testing.T) {
	h := gapsFixture(t)
	h.write("d-alias.yaml", strings.NewReplacer("D-gate", "D-alias", "contract_impact: []", "contract_impact: [C1]").Replace(gateDecision))
	h.must("--as", "lead:yash", "decision", "raise", "--file", "d-alias.yaml")
	evs, _ := filepath.Glob(filepath.Join(h.root, ".ekumen/events/decision/D-alias/*.DecisionRaised.json"))
	if len(evs) != 1 {
		t.Fatalf("%d DecisionRaised events", len(evs))
	}
	b, _ := os.ReadFile(evs[0])
	var e struct{ Data event.DecisionRaised }
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(e.Data.ContractImpact, []string{"C-event-log"}) {
		t.Errorf("contract_impact %v, want [C-event-log]", e.Data.ContractImpact)
	}

	h.write("d-ghost.yaml", strings.NewReplacer("D-gate", "D-ghost", "blocks: [T-gated]", "blocks: [T-gated]\nraised_by: {task: T-ghost, session: main, on: 2026-11-20}").Replace(gateDecision))
	out := mustExit(t, h, ExitRefused, "--as", "lead:yash", "decision", "raise", "--file", "d-ghost.yaml")
	if e := decodeJSON(t, out)["error"].(map[string]any); e["field"] != "raised_by.task" {
		t.Errorf("error %v, want field raised_by.task", e)
	}
}

func TestHelpWithJSONKeepsStdoutJSON(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"--json", "--help"}, {"--json", "-h"}, {"help", "--json"}, {"task", "list", "--json", "-h"}} {
		_, out, errOut := h.run(args...)
		if out != "" || !strings.Contains(errOut, "Usage: ekumen") {
			t.Errorf("%v: stdout %q, stderr %q; want usage on stderr only", args, out, errOut)
		}
	}
	code, out, errOut := h.run("--json")
	if code != ExitUsage || !strings.Contains(errOut, "Usage: ekumen") {
		t.Fatalf("no command: exit %d, stderr %q", code, errOut)
	}
	if e := decodeJSON(t, out)["error"].(map[string]any); e["code"] != CodeUsage {
		t.Errorf("no command: %v", e)
	}
}
