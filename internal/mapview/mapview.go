// Package mapview renders `ekumen map`: one self-contained HTML file with
// the lead's inbox, the task graph and the decided list (C-cli-json §2).
// It is a Go port of tools/map-prototype. All data (statuses, priorities,
// edges) is computed by the CLI; the page only lays the graph out, with
// dagre 0.8.5 (MIT, assets/dagre.LICENSE) inlined so it works offline.
package mapview

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/Yashswarnkar/ekumen/internal/project"
)

//go:embed template.html
var tmpl string

//go:embed assets/dagre.min.js
var dagreJS string

//go:embed assets/dagre.LICENSE
var dagreLicense string

// Data is what the page renders: the same data as `ekumen graph` and
// `ekumen inbox --for lead`, plus tasks and decisions for details.
type Data struct {
	GeneratedAt string              `json:"generated_at"`
	Commit      string              `json:"commit"`
	Project     string              `json:"project"`
	Observed    bool                `json:"observed"`
	Counts      project.StateCounts `json:"counts"`
	Inbox       []project.InboxItem `json:"inbox"`
	Nodes       []project.Node      `json:"nodes"`
	Edges       []project.Edge      `json:"edges"`
	Tasks       []project.Task      `json:"tasks"`
	Decisions   []project.Decision  `json:"decisions"`
}

// Render returns the HTML page for d.
func Render(d Data) ([]byte, error) {
	b, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	// Keep the JSON from closing the script element or starting a comment.
	js := strings.NewReplacer("</", `<\/`, "<!--", `<\!--`).Replace(string(b))
	lib := "/*! dagre 0.8.5 | " + strings.Join(strings.Fields(dagreLicense), " ") + " */\n" + dagreJS
	page := strings.Replace(tmpl, "/*DAGRE*/", lib, 1)
	page = strings.Replace(page, "/*DATA*/null", js, 1)
	return []byte(page), nil
}
