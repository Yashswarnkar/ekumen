package core

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// BriefSections are the §A sections every brief has, in order (the format
// of `ekumen brief new` and `ekumen brief check`, docs/cli.md).
var BriefSections = []string{"Goal", "Context", "Job", "Touches", "Definition of done", "Out of scope", "Escalate if", "Start prompt"}

// BriefHint marks text a template left for the author to replace. A
// section still holding it is unfilled.
const BriefHint = "TODO(brief)"

// SectionFrontMatter names front-matter problems in a BriefProblem.
const SectionFrontMatter = "front matter"

// BriefProblem is one thing `brief check` found wrong, by section.
type BriefProblem struct {
	Section string `json:"section"`
	Message string `json:"message"`
}

// BriefFront is the part of a brief's front matter that is checked.
type BriefFront struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Kind      string   `json:"kind"`
	BlockedBy []string `json:"blocked_by"`
}

// BriefInput is what CheckBrief needs: the brief split into its front
// matter (parsed by the caller; FrontErr when it could not be) and body,
// the template body the brief was made from (so a section left as the
// template wrote it counts as unfilled), and a lookup for blocked_by ids.
type BriefInput struct {
	TaskID   string
	Front    *BriefFront
	FrontErr string
	Body     string
	Template string
	Exists   func(id string) bool
}

var (
	boldLabel    = regexp.MustCompile(`^\*\*([A-Za-z][A-Za-z ]*?)(?:\s*\([^)]*\))?:\*\*\s*(.*)$`)
	headingLabel = regexp.MustCompile(`^#{2,3}\s+([A-Za-z][A-Za-z ]*?)(?:\s*\([^)]*\))?:?\s*$`)
	checkbox     = regexp.MustCompile(`(?m)^\s*[-*] \[[ xX]\] \S`)
	htmlComment  = regexp.MustCompile(`(?s)<!--.*?-->`)
)

// SplitBriefSections finds the known sections in a brief body. A section
// starts at a line `**Name:**` (an optional parenthesis before the colon,
// e.g. `**Context (verify against main):**`) or a heading `## Name`, and
// runs to the next known section. It returns each section's text and the
// names seen more than once.
func SplitBriefSections(body string) (map[string]string, []string) {
	out := map[string]string{}
	var dups []string
	cur := ""
	var buf []string
	flush := func() {
		if cur != "" {
			out[cur] = strings.TrimSpace(strings.Join(buf, "\n"))
		}
	}
	for _, line := range strings.Split(body, "\n") {
		name, rest := "", ""
		if m := boldLabel.FindStringSubmatch(line); m != nil {
			name, rest = m[1], m[2]
		} else if m := headingLabel.FindStringSubmatch(line); m != nil {
			name = m[1]
		}
		if canon := canonicalSection(name); canon != "" {
			flush()
			if _, seen := out[canon]; seen {
				dups = append(dups, canon)
			}
			cur, buf = canon, []string{rest}
			continue
		}
		if cur != "" {
			buf = append(buf, line)
		}
	}
	flush()
	return out, dups
}

func canonicalSection(name string) string {
	name = strings.TrimSpace(name)
	for _, s := range BriefSections {
		if strings.EqualFold(s, name) {
			return s
		}
	}
	return ""
}

func normalizeSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// CheckBrief validates a brief's §A against the format: front matter that
// names the task, every section present, filled in (not empty, no
// TODO(brief) hint, not the template's own text), a Definition of done
// with at least one checkbox, a Start prompt that sets the actor and names
// the handoff command, and blocked_by ids that exist. It returns the
// problems in section order; none means the brief passes.
func CheckBrief(in BriefInput) []BriefProblem {
	var out []BriefProblem
	add := func(section, format string, args ...any) {
		out = append(out, BriefProblem{Section: section, Message: fmt.Sprintf(format, args...)})
	}
	switch {
	case in.FrontErr != "":
		add(SectionFrontMatter, "%s", in.FrontErr)
	case in.Front == nil:
		add(SectionFrontMatter, "missing: start the brief with --- front matter (id, title, kind, blocked_by, tier_grants)")
	default:
		f := in.Front
		if f.ID != in.TaskID {
			add(SectionFrontMatter, "id is %q, want %q", f.ID, in.TaskID)
		}
		if strings.TrimSpace(f.Title) == "" {
			add(SectionFrontMatter, "title is empty")
		}
		if !slices.Contains([]string{event.TaskKindResearch, event.TaskKindContract, event.TaskKindBuild}, f.Kind) {
			add(SectionFrontMatter, "kind must be research, contract or build, got %q", f.Kind)
		}
		for _, b := range f.BlockedBy {
			if in.Exists != nil && !in.Exists(b) {
				add(SectionFrontMatter, "blocked_by %s names nothing recorded", b)
			}
		}
	}

	secs, dups := SplitBriefSections(in.Body)
	tmpl, _ := SplitBriefSections(in.Template)
	for _, name := range BriefSections {
		text, ok := secs[name]
		if !ok {
			add(name, "missing: add a section starting **%s:**", name)
			continue
		}
		if slices.Contains(dups, name) {
			add(name, "appears more than once")
		}
		bare := strings.TrimSpace(htmlComment.ReplaceAllString(text, ""))
		switch {
		case bare == "":
			add(name, "empty")
			continue
		case strings.Contains(text, BriefHint):
			add(name, "still has a %s hint from the template: replace it with the real content", BriefHint)
			continue
		case in.Template != "" && tmpl[name] != "" && normalizeSpace(text) == normalizeSpace(tmpl[name]):
			add(name, "unchanged from the template")
			continue
		}
		switch name {
		case "Definition of done":
			if !checkbox.MatchString(text) {
				add(name, "needs at least one checkbox line (- [ ] …)")
			}
		case "Start prompt":
			if !strings.Contains(text, "EKUMEN_ACTOR=") && !strings.Contains(text, "--as session:") {
				add(name, "must set the actor (export EKUMEN_ACTOR=session:<session>)")
			}
			if !strings.Contains(text, "task handoff") {
				add(name, "must name the handoff command (ekumen task handoff <id>)")
			}
		}
	}
	return out
}
