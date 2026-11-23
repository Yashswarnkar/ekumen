package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/Yashswarnkar/ekumen/internal/core"
)

const orch = "orchestrator:main"

func fakeGH(calls *[]string) func(string, ...string) ([]byte, error) {
	return func(_ string, args ...string) ([]byte, error) {
		*calls = append(*calls, strings.Join(args, " "))
		if strings.Join(args[:3], " ") != "issue view 40" {
			return nil, errors.New("no such issue")
		}
		return []byte(`{"number":40,"title":"Environment setup","body":"Add init.\r\nAnd task start.","url":"https://github.com/o/r/issues/40"}`), nil
	}
}

// fillBrief replaces every TODO(brief) line of a scaffold with real text.
func fillBrief(text string) string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if i := strings.Index(l, core.BriefHint); i >= 0 {
			l = l[:i] + "Filled in by the orchestrator."
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func TestBriefNewFromIssue(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	var calls []string
	h.gh = fakeGH(&calls)
	out := h.must("--as", orch, "brief", "new", "T-demo", "--from-issue", "40", "--blocked-by", "T-core",
		"--grants", "push_own_branch,open_draft_pr", "--json")
	res := decodeJSON(t, out)
	paths := res["paths"].([]any)
	if paths[0] != ".ekumen/briefs/T-demo/brief.md" || len(res["events"].([]any)) != 1 {
		t.Fatalf("write result %s", out)
	}
	if len(calls) != 1 || calls[0] != "issue view 40 --json number,title,body,url" {
		t.Fatalf("gh calls %q", calls)
	}
	b, err := os.ReadFile(filepath.Join(h.root, ".ekumen/briefs/T-demo/brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, want := range []string{
		"id: T-demo\ntitle: Environment setup\nkind: build\n",
		"blocked_by: [T-core]\ntier_grants: [push_own_branch, open_draft_pr]\n",
		"# T-demo · Environment setup\n",
		"**Goal:** GitHub **#40**: [Environment setup](https://github.com/o/r/issues/40).\n\n> Add init.\n> And task start.\n",
		"export EKUMEN_ACTOR=session:t-demo\n",
		"`ekumen task handoff T-demo`",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("brief lacks %q\n%s", want, text)
		}
	}
	task := dig(t, decodeJSON(t, h.must("task", "show", "T-demo", "--json")), "task").(map[string]any)
	if task["status"] != "draft" || task["branch"] != "build/t-demo" || task["session"] != "t-demo" || task["title"] != "Environment setup" {
		t.Errorf("task %v", task)
	}

	// Unfilled: brief check fails by section; ekumen check reports it but passes.
	code, out, _ := h.run("brief", "check", "T-demo", "--json")
	rep := decodeJSON(t, out)
	if code != ExitRefused || rep["ok"] != false {
		t.Fatalf("brief check on a scaffold: exit %d %s", code, out)
	}
	var secs []string
	for _, p := range rep["problems"].([]any) {
		secs = append(secs, p.(map[string]any)["section"].(string))
	}
	want := []string{"Context", "Job", "Touches", "Definition of done", "Out of scope", "Escalate if", "Start prompt"}
	if !slices.Equal(secs, want) {
		t.Errorf("problem sections %v, want %v (Goal is filled from the issue)", secs, want)
	}
	code, out, _ = h.run("check", "--json")
	chk := decodeJSON(t, out)
	if code != ExitOK || chk["ok"] != true || len(chk["briefs"].([]any)) != 1 {
		t.Fatalf("check with an unfilled draft brief: exit %d %s", code, out)
	}
	_, human, _ := h.run("brief", "check", "T-demo")
	if !strings.Contains(human, "Context: still has a TODO(brief) hint") {
		t.Errorf("human output:\n%s", human)
	}

	// Filled: both pass.
	h.write(".ekumen/briefs/T-demo/brief.md", fillBrief(text))
	if code, out, _ := h.run("brief", "check", "T-demo", "--json"); code != ExitOK {
		t.Fatalf("brief check on a filled brief: exit %d %s", code, out)
	}
	if chk := decodeJSON(t, h.must("check", "--json")); len(chk["briefs"].([]any)) != 0 {
		t.Errorf("check still reports briefs: %v", chk)
	}

	// The id is now in use.
	code, out, _ = h.run("--as", orch, "brief", "new", "T-demo", "--title", "Again", "--json")
	if code != ExitRefused || !strings.Contains(out, "already in use") {
		t.Errorf("reused id: exit %d %s", code, out)
	}
}

func TestBriefNewWithoutGH(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	code, out, errOut := h.run("--as", orch, "brief", "new", "T-demo", "--title", "Demo", "--kind", "research", "--from-issue", "40", "--json")
	if code != ExitOK || !strings.Contains(errOut, "gh CLI is not installed") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	b, _ := os.ReadFile(filepath.Join(h.root, ".ekumen/briefs/T-demo/brief.md"))
	if !strings.Contains(string(b), "**Goal:** "+core.BriefHint) || !strings.Contains(string(b), "kind: research") {
		t.Errorf("brief:\n%s", b)
	}
	// No --title and no issue: refused, nothing written.
	code, out, _ = h.run("--as", orch, "brief", "new", "T-other", "--from-issue", "40", "--json")
	if code != ExitRefused || !strings.Contains(out, `"field": "title"`) {
		t.Errorf("no title: exit %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(h.root, ".ekumen/briefs/T-other")); !os.IsNotExist(err) {
		t.Errorf("refused brief new left files: %v", err)
	}
}

func TestBriefNewRefusals(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--as", "session:t-cli", "brief", "new", "T-demo", "--title", "x"}, ExitRefused, `"code": "lane"`},
		{[]string{"--as", orch, "brief", "new", "T-cli", "--title", "x"}, ExitRefused, "already in use"},
		{[]string{"--as", orch, "brief", "new", "demo", "--title", "x"}, ExitRefused, `"field": "id"`},
		{[]string{"--as", orch, "brief", "new", "T-demo", "--title", "x", "--kind", "chore"}, ExitRefused, `"field": "kind"`},
		{[]string{"--as", orch, "brief", "new", "T-demo", "--title", "x", "--from-issue", "abc"}, ExitUsage, "invalid value"},
		{[]string{"--as", orch, "brief", "new", "T-core", "--existing"}, ExitRefused, "approved"},
		{[]string{"brief", "check", "T-skills"}, ExitNotFound, "brief new T-skills --existing"},
		{[]string{"brief", "check", "T-nope"}, ExitNotFound, "no task"},
		{[]string{"brief", "nope"}, ExitUsage, "unknown command"},
	} {
		code, out, _ := h.run(append(c.args, "--json")...)
		if code != c.code || !strings.Contains(out, c.want) {
			t.Errorf("%v: exit %d, want %d with %q\n%s", c.args, code, c.code, c.want, out)
		}
	}
	// A leftover brief.md for an unused id is not overwritten.
	h.write(".ekumen/briefs/T-left/brief.md", "mine\n")
	code, out, _ := h.run("--as", orch, "brief", "new", "T-left", "--title", "x", "--json")
	if code != ExitRefused || !strings.Contains(out, "already exists") {
		t.Errorf("leftover brief: exit %d %s", code, out)
	}
	if code, out, _ := h.run("task", "show", "T-left", "--json"); code != ExitNotFound {
		t.Errorf("refused brief new drafted the task: %d %s", code, out)
	}
}

func TestBriefNewExistingAndProjectTemplate(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	h.write(".ekumen/templates/brief.md", "---\nid: {{id}}\ntitle: {{title_yaml}}\nkind: {{kind}}\nblocked_by: [{{blocked_by}}]\n---\n\n"+
		"**Goal:** {{goal}}\n\n**Context:** Write the context.\n\n**Job:** j\n\n**Touches:** t\n\n**Definition of done:**\n- [ ] done\n\n"+
		"**Out of scope:** o\n\n**Escalate if:** e\n\n**Start prompt:**\n\n    export EKUMEN_ACTOR=session:{{session}}\n    ekumen task handoff {{id}}\n")
	// The inbox points the orchestrator at the scaffold.
	inbox := decodeJSON(t, h.must("inbox", "--for", "orchestrator", "--json"))["items"].([]any)
	if c := inbox[0].(map[string]any)["command"]; c != "ekumen brief new T-skills --existing" {
		t.Fatalf("inbox brief command %v", c)
	}
	out := h.must("--as", orch, "brief", "new", "T-skills", "--existing", "--json")
	if res := decodeJSON(t, out); len(res["events"].([]any)) != 0 || res["paths"].([]any)[0] != ".ekumen/briefs/T-skills/brief.md" {
		t.Fatalf("--existing result %s", out)
	}
	b, _ := os.ReadFile(filepath.Join(h.root, ".ekumen/briefs/T-skills/brief.md"))
	if !strings.HasPrefix(string(b), "---\nid: T-skills\ntitle: Skills\nkind: build\nblocked_by: [T-cli]\n") {
		t.Errorf("scaffold from the project template:\n%s", b)
	}
	// Goal (hint) and Context (template text) are unfilled; the rest of the template's short answers count as unchanged.
	code, out, _ := h.run("brief", "check", "T-skills", "--json")
	if code != ExitRefused || !strings.Contains(out, "unchanged from the template") || !strings.Contains(out, "TODO(brief)") {
		t.Errorf("check on a project-template scaffold: exit %d %s", code, out)
	}
	if code, out, _ := h.run("--as", orch, "brief", "new", "T-skills", "--existing", "--json"); code != ExitRefused || !strings.Contains(out, "already has a brief") {
		t.Errorf("second --existing: exit %d %s", code, out)
	}
	// check does not treat templates/ as invalid.
	if chk := decodeJSON(t, h.must("check", "--json")); len(chk["invalid"].([]any)) != 0 {
		t.Errorf("check invalid: %v", chk["invalid"])
	}
}

// The three briefs named in T-brief's definition of done pass the check
// that new briefs must pass (they are approved and frozen).
func TestRealBriefsPass(t *testing.T) {
	for _, id := range []string{"T-inbox-v1", "T-start", "T-brief"} {
		b, err := os.ReadFile(filepath.Join("..", "..", ".ekumen", "briefs", id, "brief.md"))
		if err != nil {
			t.Fatal(err)
		}
		front, body, ok, err := splitFrontMatter(string(b))
		if !ok || err != nil {
			t.Fatalf("%s: front matter %v", id, err)
		}
		var f core.BriefFront
		if err := yaml.Unmarshal([]byte(front), &f); err != nil {
			t.Fatal(err)
		}
		in := core.BriefInput{TaskID: id, Front: &f, Body: body, Template: builtinBriefTemplate, Exists: func(string) bool { return true }}
		if ps := core.CheckBrief(in); len(ps) != 0 {
			t.Errorf("%s: %+v", id, ps)
		}
	}
}
