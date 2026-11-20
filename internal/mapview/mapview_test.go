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
