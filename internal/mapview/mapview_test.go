package mapview

import (
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
		{"git@github.com:o/r", "", "https://github.com/o/r/blob/main/"},
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
	if strings.Contains(s, "\nbody {") || !strings.Contains(s, ".ekumen-map {") {
		t.Error("fragment styles the host page's body")
	}
}
