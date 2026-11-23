package core

import (
	"strings"
	"testing"
)

const goodBriefBody = `
# T-demo · Demo

**Goal:** Ship the demo. GitHub **#40**.

**Context (verify against origin/main when you start):**
- The CLI lives in internal/cli.

**Job:**
1. Build it.

**Touches:** internal/cli, your handoff files.

**Definition of done:**
- [ ] it works
- [ ] Handoff via ` + "`ekumen task handoff T-demo`" + `

**Out of scope:** the UI.

**Escalate if:** a contract looks wrong.

**Start prompt:**

    export EKUMEN_ACTOR=session:t-demo
    Read the spine. Report with ` + "`bin/ekumen task handoff T-demo`" + `.
`

func goodInput() BriefInput {
	return BriefInput{TaskID: "T-demo", Front: &BriefFront{ID: "T-demo", Title: "Demo", Kind: "build"},
		Body: goodBriefBody, Exists: func(id string) bool { return id == "T-core" }}
}

func sections(ps []BriefProblem) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Section+": "+p.Message)
	}
	return out
}

func TestCheckBriefPasses(t *testing.T) {
	if ps := CheckBrief(goodInput()); len(ps) != 0 {
		t.Fatalf("problems on a good brief: %v", sections(ps))
	}
}

func TestSplitBriefSections(t *testing.T) {
	secs, dups := SplitBriefSections("intro\n**Goal:** one\nmore\n## Job\n1. x\n**Not a section:** y\n**goal:** again\n")
	if secs["Goal"] != "again" || secs["Job"] != "1. x\n**Not a section:** y" || len(dups) != 1 || dups[0] != "Goal" {
		t.Fatalf("sections %q dups %v", secs, dups)
	}
}

func TestCheckBriefProblems(t *testing.T) {
	cases := []struct {
		name string
		edit func(*BriefInput)
		want string
	}{
		{"missing section", func(in *BriefInput) {
			in.Body = strings.Replace(in.Body, "**Out of scope:** the UI.", "", 1)
		}, "Out of scope: missing"},
		{"hint left", func(in *BriefInput) {
			in.Body = strings.Replace(in.Body, "Build it.", BriefHint+": steps", 1)
		}, "Job: still has a TODO(brief) hint"},
		{"empty", func(in *BriefInput) {
			in.Body = strings.Replace(in.Body, "the UI.", "<!-- later -->", 1)
		}, "Out of scope: empty"},
		{"unchanged from template", func(in *BriefInput) {
			in.Template = "**Escalate if:** a contract   looks wrong.\n"
		}, "Escalate if: unchanged from the template"},
		{"no checkbox", func(in *BriefInput) {
			in.Body = strings.NewReplacer("- [ ] it works", "it works", "- [ ] Handoff", "Handoff").Replace(in.Body)
		}, "Definition of done: needs at least one checkbox"},
		{"no actor", func(in *BriefInput) {
			in.Body = strings.Replace(in.Body, "export EKUMEN_ACTOR=session:t-demo", "start", 1)
		}, "Start prompt: must set the actor"},
		{"no handoff", func(in *BriefInput) {
			in.Body = strings.Replace(in.Body, "Report with `bin/ekumen task handoff T-demo`.", "Go.", 1)
		}, "Start prompt: must name the handoff command"},
		{"unknown blocker", func(in *BriefInput) { in.Front.BlockedBy = []string{"T-core", "T-nope"} },
			"front matter: blocked_by T-nope names nothing"},
		{"wrong id", func(in *BriefInput) { in.Front.ID = "T-other" }, `front matter: id is "T-other"`},
		{"no front matter", func(in *BriefInput) { in.Front = nil }, "front matter: missing"},
		{"bad kind", func(in *BriefInput) { in.Front.Kind = "chore" }, "front matter: kind must be"},
	}
	for _, c := range cases {
		in := goodInput()
		c.edit(&in)
		got := sections(CheckBrief(in))
		if len(got) != 1 || !strings.HasPrefix(got[0], c.want) {
			t.Errorf("%s: got %q, want one problem starting %q", c.name, got, c.want)
		}
	}
}
