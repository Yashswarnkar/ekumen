package mapview

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/project"
)

func TestRenderSelfContained(t *testing.T) {
	page, err := Render(Data{Project: "demo", GeneratedAt: "2026-11-20T09:00:00Z",
		Nodes: []project.Node{{ID: "T-a", Kind: "task", Title: "</script><b>x", Status: "ready"}},
		Edges: []project.Edge{}, Tasks: []project.Task{}, Decisions: []project.Decision{}, Inbox: []project.InboxItem{}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	if strings.Contains(s, "/*DATA*/") || strings.Contains(s, "/*DAGRE*/") {
		t.Error("placeholders left in the page")
	}
	if !strings.Contains(s, "dagre") || !strings.Contains(s, "Chris Pettitt") {
		t.Error("dagre or its license is missing")
	}
	for _, bad := range []string{`src="http`, `href="https://fonts`, "cdnjs"} {
		if strings.Contains(s, bad) {
			t.Errorf("page loads %q; it must work offline", bad)
		}
	}
	if strings.Count(s, "</script>") != 2 {
		t.Errorf("data closed a script element early: %d </script>", strings.Count(s, "</script>"))
	}
}

// T-log-fixes job 8: the link base from a git remote. GitHub is the only
// built-in pattern; anything else needs --link-base.
func TestLinkBase_TLogFixes8(t *testing.T) {
	cases := []struct{ remote, branch, want string }{
		{"git@github.com:o/r.git", "main", "https://github.com/o/r/blob/main/"},
		{"git@github.com:o/r", "", ""},
		{"git@github.com:o/r", "abc1234", "https://github.com/o/r/blob/abc1234/"},
		{"https://github.com/o/r.git", "trunk", "https://github.com/o/r/blob/trunk/"},
		{"https://github.com/o/r", "main", "https://github.com/o/r/blob/main/"},
		{"https://token@github.com/o/r.git", "main", "https://github.com/o/r/blob/main/"},
		{"ssh://git@github.com/o/r.git", "main", "https://github.com/o/r/blob/main/"},
		{"https://gitlab.com/o/r.git", "main", ""},
		{"", "main", ""},
	}
	for _, c := range cases {
		if got := LinkBase(c.remote, c.branch); got != c.want {
			t.Errorf("LinkBase(%q, %q) = %q, want %q", c.remote, c.branch, got, c.want)
		}
	}
	if got := FileURL("https://h/o/r/blob/main", ".ekumen/spine.md"); got != "https://h/o/r/blob/main/.ekumen/spine.md" {
		t.Errorf("FileURL = %q", got)
	}
	if FileURL("", "x") != "" {
		t.Error("FileURL with no base")
	}
	for remote, want := range map[string]string{"git@github.com:o/ekumen.git": "ekumen", "https://h/o/r/": "r", "": ""} {
		if got := RepoName(remote); got != want {
			t.Errorf("RepoName(%q) = %q, want %q", remote, got, want)
		}
	}
}

// T-log-fixes job 7: the fragment keeps styles and content, scoped.
func TestRenderFragment_TLogFixes7(t *testing.T) {
	page, err := RenderFragment(Data{Project: "demo", Nodes: []project.Node{}, Edges: []project.Edge{},
		Tasks: []project.Task{}, Decisions: []project.Decision{}, Inbox: []project.InboxItem{}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	if strings.Contains(s, "<!doctype") || strings.Contains(s, "<body") || strings.Contains(s, "<head>") {
		t.Error("fragment has document wrappers")
	}
	if !strings.HasPrefix(strings.TrimSpace(s[strings.Index(s, `<div class="ekumen-map">`):]), `<div class="ekumen-map">`) {
		t.Error("no wrapper")
	}
	// Every style rule is scoped to the wrapper; no element ids.
	css := s[strings.Index(s, "<style>")+7 : strings.Index(s, "</style>")]
	css = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
	heads := regexp.MustCompile(`(?:^|[{};])\s*([^{};]+)\{`).FindAllStringSubmatch(css, -1)
	if len(heads) < 30 {
		t.Fatalf("found only %d rules", len(heads))
	}
	for _, m := range heads {
		head := strings.TrimSpace(m[1])
		if strings.HasPrefix(head, "@") {
			continue
		}
		for _, sel := range strings.Split(head, ",") {
			if !strings.HasPrefix(strings.TrimSpace(sel), ".ekumen-map") {
				t.Errorf("unscoped selector %q", sel)
			}
		}
	}
	if regexp.MustCompile(`\sid="[a-z]`).MatchString(s) || strings.Contains(s, "getElementById") {
		t.Error("fragment uses fixed element ids")
	}
}

var scriptRe = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

// Re-review finding 2: two fragments on one page declare nothing at the
// top level of a script, so they don't clash. With node installed, both
// fragments' scripts run in one global scope against a stub DOM.
func TestTwoFragmentsOnOnePage_TLogFixes7(t *testing.T) {
	d := Data{Project: "demo", Nodes: []project.Node{{ID: "T-a", Kind: "task", Title: "a", Status: "ready"}},
		Edges: []project.Edge{}, Tasks: []project.Task{}, Decisions: []project.Decision{}, Inbox: []project.InboxItem{}}
	f, err := RenderFragment(d)
	if err != nil {
		t.Fatal(err)
	}
	page := "<!doctype html><html><body>" + string(f) + string(f) + "</body></html>"
	scripts := scriptRe.FindAllStringSubmatch(page, -1)
	if len(scripts) != 4 {
		t.Fatalf("%d scripts, want 4", len(scripts))
	}
	for i, m := range scripts {
		var code []string
		for _, l := range strings.Split(strings.TrimSpace(m[1]), "\n") {
			if !strings.HasPrefix(l, "//") && !strings.HasPrefix(l, "/*!") {
				code = append(code, l)
			}
		}
		body := strings.TrimSpace(strings.Join(code, "\n"))
		iife := strings.HasPrefix(body, "(() => {") && strings.HasSuffix(body, "})();")
		umd := strings.HasPrefix(body, "(function(") // dagre's UMD wrapper
		if !iife && !umd {
			t.Errorf("script %d is not wrapped in a function: %.60q", i, body)
		}
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return
	}
	var js strings.Builder
	js.WriteString(`const vm = require("vm"); const els = []; let cur;
const mk = () => ({innerHTML: ""});
const root = () => { const m = {}; els.push(m); return {querySelector: q => (m[q] ??= mk())}; };
const ctx = {console}; ctx.window = ctx; ctx.self = ctx; vm.createContext(ctx);
const run = (src, r) => { ctx.document = {currentScript: {closest: () => r}}; vm.runInContext(src, ctx); };
const scripts = JSON.parse(require("fs").readFileSync(0, "utf8"));
for (let i = 0; i < scripts.length; i += 2) { const r = root(); run(scripts[i], r); run(scripts[i + 1], r); }
if (els.length !== 2 || !els[1]['[data-ek="graph"]'].innerHTML.includes("T-a")) { console.error("second map not drawn"); process.exit(1); }
`)
	var srcs []string
	for _, m := range scripts {
		srcs = append(srcs, m[1])
	}
	arg, _ := json.Marshal(srcs)
	cmd := exec.Command(node, "-e", js.String())
	cmd.Stdin = bytes.NewReader(arg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("two fragments on one page: %v\n%s", err, out)
	}
}
