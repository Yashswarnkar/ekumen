package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func TestTaskUpdateGolden(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	orch := []string{"--as", "orchestrator:main", "--json", "task", "update"}
	cases := []struct {
		name string
		args []string
		code int
	}{
		{"applied", []string{"T-skills", "--repo", "ekumen", "--branch", "build/t-skills", "--reason", "fill placeholders"}, ExitOK},
		{"pending", []string{"T-cli", "--repo", "ekumen", "--base-branch", "main", "--reason", "fill placeholders"}, ExitOK},
		{"noop", []string{"T-cli", "--branch", "build/t-cli", "--base-branch", "main", "--reason", "again"}, ExitOK},
		{"empty", []string{"T-cli", "--worktree", "", "--reason", "clear"}, ExitRefused},
		{"bad-branch", []string{"T-cli", "--branch", "build/../x", "--reason", "x"}, ExitRefused},
		{"done", []string{"T-core", "--title", "Core", "--reason", "rename"}, ExitRefused},
		{"no-field", []string{"T-cli", "--reason", "nothing"}, ExitUsage},
	}
	for _, c := range cases {
		code, out, errOut := h.run(append(orch, c.args...)...)
		if code != c.code {
			t.Errorf("%s: exit %d, want %d\n%s%s", c.name, code, c.code, out, errOut)
		}
		golden(t, "task-update-"+c.name+".json", out)
		h.tick()
	}
	code, out, _ := h.run("--as", "orchestrator:other", "--json", "task", "update", "T-cli", "--title", "x", "--reason", "r")
	if code != ExitRefused {
		t.Errorf("without the claim: exit %d", code)
	}
	golden(t, "task-update-no-claim.json", out)
}

func TestTaskUpdatePendingUntilApproved(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	h.must("--as", "orchestrator:main", "task", "update", "T-cli", "--repo", "ekumen", "--reason", "fill placeholders")
	h.must("--as", "orchestrator:main", "task", "update", "T-cli", "--base-branch", "main", "--title", "The CLI", "--reason", "and the base")

	show := h.must("--json", "task", "show", "T-cli")
	task := decodeJSON(t, show)["task"].(map[string]any)
	if task["title"] != "ekumen CLI" || task["pending_update"] == nil {
		t.Fatalf("task = %v", task)
	}
	golden(t, "task-update-show-pending.json", mustJSON(t, task["pending_update"]))

	inbox := h.must("--json", "inbox", "--for", "lead")
	golden(t, "task-update-inbox-pending.json", inbox)
	if !strings.Contains(inbox, "Approve update to T-cli: repo ekumen, base_branch main, title The CLI (and the base)") {
		t.Errorf("no update action in\n%s", inbox)
	}

	out := h.must("--as", "lead:yash", "task", "approve", "T-cli")
	if !strings.Contains(out, "approved the update to T-cli") {
		t.Errorf("approve said %q", out)
	}
	task = decodeJSON(t, h.must("--json", "task", "show", "T-cli"))["task"].(map[string]any)
	if task["title"] != "The CLI" || task["pending_update"] != nil {
		t.Errorf("after approve: %v", task)
	}
	if inbox := h.must("--json", "inbox", "--for", "lead"); strings.Contains(inbox, "Approve update") {
		t.Errorf("action left after approve:\n%s", inbox)
	}
	code, _, _ := h.run("--as", "lead:yash", "task", "approve", "T-cli")
	if code != ExitRefused {
		t.Errorf("approve without a pending update: exit %d", code)
	}
}

func TestTaskUpdateApproveRefusesChangedBrief(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	h.must("--as", "orchestrator:main", "task", "update", "T-cli", "--repo", "ekumen", "--reason", "fill")
	h.write(".ekumen/briefs/T-cli/brief.md", "# T-cli\n\nEdited after approval.\n")
	code, out, _ := h.run("--as", "lead:yash", "--json", "task", "approve", "T-cli")
	if code != ExitRefused || !strings.Contains(out, "frozen-text-changed") {
		t.Errorf("exit %d: %s", code, out)
	}
}

func TestTaskUpdateDeferredRefusedAndChecked(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	h.must("--as", "orchestrator:main", "task", "defer", "T-skills", "--reason", "later")
	code, out, _ := h.run("--as", "orchestrator:main", "--json", "task", "update", "T-skills", "--title", "x", "--reason", "r")
	if code != ExitRefused || !strings.Contains(out, "deferred") {
		t.Errorf("exit %d: %s", code, out)
	}
	h.raw("TaskUpdated", "orchestrator", "main", "task", "T-skills", map[string]any{"title": "x", "reason": "merged in"})
	code, m := checkJSON(t, h)
	if code != ExitConflict || !strings.Contains(strings.Join(rulesOf(m), ","), "update-after-close") {
		t.Errorf("check: exit %d, rules %v", code, rulesOf(m))
	}
}
