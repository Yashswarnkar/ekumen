package legacyimport

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/Yashswarnkar/ekumen/internal/store/files"
)

const entryTimestamp = "20060102T150405Z"

// entry is one §B amendment or §C handoff file, as it is or as the split
// of an old brief will write it.
type entry struct {
	Rel    string // path relative to .ekumen/
	Name   string
	At     time.Time
	Author string
	Text   []byte
}

// body returns the entry text after its front matter.
func (e entry) body() string {
	_, b, _ := frontMatter(e.Text)
	return string(b)
}

// newFile is a file the real run writes through the file store: a brief.md
// (Kind "") or an entry written at its historical time.
type newFile struct {
	Rel    string
	Text   []byte
	Task   string
	Kind   files.EntryKind
	Author string
	At     time.Time
}

// taskBrief is a task's brief in the new layout (C-event-log §8).
type taskBrief struct {
	Rel        string // briefs/<id>/brief.md
	Text       []byte // §A
	Amendments []entry
	Handoffs   []entry
	// Write lists files the split of an old single-file brief creates, and
	// Remove the old file, when this brief came from one.
	Write  []newFile
	Remove string
}

// readBrief returns a task's brief: the new layout when briefs/<id>/brief.md
// exists, otherwise the split of the old single file the registry names.
// It returns nil when the task has no brief yet.
func readBrief(fsys fs.FS, t *legacyTask, session string) (*taskBrief, error) {
	dir := "briefs/" + t.ID
	text, err := readFileIfExists(fsys, dir+"/brief.md")
	if err != nil {
		return nil, err
	}
	var old string
	if t.Brief != nil && *t.Brief != "" && *t.Brief != dir+"/brief.md" {
		old = *t.Brief
	}
	var oldText []byte
	if old != "" {
		if oldText, err = readFileIfExists(fsys, old); err != nil {
			return nil, err
		}
	}

	if text == nil {
		if oldText == nil {
			if t.Brief != nil && *t.Brief != "" {
				return nil, errorf("task %s: brief %s not found", t.ID, *t.Brief)
			}
			return nil, nil
		}
		return splitOldBrief(old, oldText, t, session)
	}

	b := &taskBrief{Rel: dir + "/brief.md", Text: text}
	for _, kind := range []string{"amendments", "handoffs"} {
		names, err := listEntries(fsys, dir+"/"+kind)
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			e, err := readEntry(fsys, dir+"/"+kind+"/"+n)
			if err != nil {
				return nil, err
			}
			if kind == "amendments" {
				b.Amendments = append(b.Amendments, e)
			} else {
				b.Handoffs = append(b.Handoffs, e)
			}
		}
	}
	if oldText != nil {
		// A partial earlier run: the split is already there. It must match
		// before the old file may go.
		split, err := splitOldBrief(old, oldText, t, session)
		if err != nil {
			return nil, err
		}
		for _, f := range split.Write {
			have, err := readFileIfExists(fsys, f.Rel)
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(have, f.Text) {
				return nil, errorf("task %s: %s exists and differs from the split of %s", t.ID, f.Rel, old)
			}
		}
		b.Remove = old
	}
	return b, nil
}

var entryNameRe = regexp.MustCompile(`^([0-9]{8}T[0-9]{6}Z)-([a-z0-9]+(?:-[a-z0-9]+)*)\.md$`)

func readEntry(fsys fs.FS, rel string) (entry, error) {
	text, err := fs.ReadFile(fsys, rel)
	if err != nil {
		return entry{}, err
	}
	name := path.Base(rel)
	m := entryNameRe.FindStringSubmatch(name)
	if m == nil {
		return entry{}, errorf("%s: name is not <timestamp>-<slug>.md", rel)
	}
	at, err := time.Parse(entryTimestamp, m[1])
	if err != nil {
		return entry{}, errorf("%s: %w", rel, err)
	}
	e := entry{Rel: rel, Name: name, At: at.UTC(), Author: m[2], Text: text}
	if fm, _, ok := frontMatter(text); ok {
		var h struct {
			Author string `yaml:"author"`
		}
		if yaml.Unmarshal(fm, &h) == nil && h.Author != "" {
			e.Author = h.Author
		}
	}
	return e, nil
}

var (
	sectionBRe = regexp.MustCompile(`(?m)^## §B\b.*$`)
	sectionCRe = regexp.MustCompile(`(?m)^## §C\b.*$`)
	commentRe  = regexp.MustCompile(`(?s)<!--.*?-->`)
	amendRe    = regexp.MustCompile(`^- ([0-9]{4}-[0-9]{2}-[0-9]{2}): ?(.*)$`)
	handoffRe  = regexp.MustCompile(`(?m)^### ([0-9]{4}-[0-9]{2}-[0-9]{2})[ \t]*$`)
	idLineRe   = regexp.MustCompile(`(?m)^id:.*$`)
	kindLineRe = regexp.MustCompile(`(?m)^kind:`)
)

// splitOldBrief splits an old single-file brief (front matter, §A, §B list,
// §C dated sections) into the new layout. Entries carry only a date, so
// they are timed 12:00:00Z on it; entries sharing a second get the file
// store's "-2", "-3" suffixes. The split is deterministic.
func splitOldBrief(old string, text []byte, t *legacyTask, session string) (*taskBrief, error) {
	fm, body, ok := frontMatter(text)
	if !ok {
		return nil, errorf("%s: no front matter", old)
	}
	s := string(body)
	bLoc := sectionBRe.FindStringIndex(s)
	cLoc := sectionCRe.FindStringIndex(s)
	if bLoc == nil || cLoc == nil || cLoc[0] < bLoc[0] {
		return nil, errorf("%s: want sections §A, §B, §C in order", old)
	}
	secA := strings.TrimRight(s[:bLoc[0]], "\n") + "\n"
	secB := s[bLoc[1]:cLoc[0]]
	secC := commentRe.ReplaceAllString(s[cLoc[1]:], "")

	// §A: front matter with the new id and the old one as an alias.
	newFM := idLineRe.ReplaceAllString(string(fm), fmt.Sprintf("id: %s\naliases: [%s]", t.ID, t.OldID))
	if !kindLineRe.MatchString(newFM) && t.Kind != "" {
		newFM = strings.Replace(newFM, "\naliases:", "\nkind: "+t.Kind+"\naliases:", 1)
		// Keep kind after title, where new briefs have it.
		newFM = moveKindAfterTitle(newFM)
	}
	dir := "briefs/" + t.ID
	b := &taskBrief{Rel: dir + "/brief.md", Text: []byte("---\n" + newFM + "---\n" + secA), Remove: old}
	b.Write = append(b.Write, newFile{Rel: b.Rel, Text: b.Text, Task: t.ID})

	used := map[string]int{}
	name := func(at time.Time, author string) string {
		base := at.Format(entryTimestamp) + "-" + author
		used[base]++
		if n := used[base]; n > 1 {
			return fmt.Sprintf("%s-%d.md", base, n)
		}
		return base + ".md"
	}
	mk := func(kind, date, author, section, content string) (entry, error) {
		day, err := time.Parse(time.DateOnly, date)
		if err != nil {
			return entry{}, errorf("%s: bad date %q", old, date)
		}
		at := day.Add(12 * time.Hour).UTC()
		n := name(at, author)
		text := fmt.Sprintf("---\ntask: %s\nauthor: %s\ndate: %s\nsource: %s %s\n---\n\n%s\n",
			t.ID, author, date, old, section, strings.TrimSpace(content))
		e := entry{Rel: dir + "/" + kind + "/" + n, Name: n, At: at, Author: author, Text: []byte(text)}
		b.Write = append(b.Write, newFile{Rel: e.Rel, Text: e.Text, Task: t.ID, Kind: files.EntryKind(kind), Author: author, At: at})
		return e, nil
	}

	// §B: one "- YYYY-MM-DD: text" item per amendment; indented lines
	// continue the item.
	var cur []string
	var curDate string
	flush := func() error {
		if curDate == "" {
			return nil
		}
		e, err := mk("amendments", curDate, "orchestrator", "§B", strings.Join(cur, "\n"))
		if err != nil {
			return err
		}
		b.Amendments = append(b.Amendments, e)
		curDate, cur = "", nil
		return nil
	}
	for line := range strings.SplitSeq(secB, "\n") {
		if m := amendRe.FindStringSubmatch(line); m != nil {
			if err := flush(); err != nil {
				return nil, err
			}
			curDate, cur = m[1], []string{m[2]}
			continue
		}
		if curDate != "" && strings.TrimSpace(line) != "" {
			cur = append(cur, strings.TrimSpace(line))
		} else if curDate == "" && strings.TrimSpace(line) != "" {
			return nil, errorf("%s: §B line is not a dated item: %q", old, line)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}

	// §C: "### YYYY-MM-DD" sections.
	locs := handoffRe.FindAllStringSubmatchIndex(secC, -1)
	if len(locs) == 0 && strings.TrimSpace(secC) != "" {
		return nil, errorf("%s: §C has text but no dated section", old)
	}
	if strings.TrimSpace(secC[:firstOr(locs, len(secC))]) != "" {
		return nil, errorf("%s: §C has text before its first dated section", old)
	}
	for i, l := range locs {
		end := len(secC)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		if session == "" {
			return nil, errorf("%s: §C entry but the registry names no session for %s", old, t.OldID)
		}
		e, err := mk("handoffs", secC[l[2]:l[3]], session, "§C", secC[l[1]:end])
		if err != nil {
			return nil, err
		}
		b.Handoffs = append(b.Handoffs, e)
	}
	return b, nil
}

func firstOr(locs [][]int, def int) int {
	if len(locs) == 0 {
		return def
	}
	return locs[0][0]
}

func moveKindAfterTitle(fm string) string {
	lines := strings.Split(fm, "\n")
	ki, ti := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(l, "kind:") {
			ki = i
		}
		if strings.HasPrefix(l, "title:") {
			ti = i
		}
	}
	if ki < 0 || ti < 0 {
		return fm
	}
	k := lines[ki]
	lines = append(lines[:ki], lines[ki+1:]...)
	if ti > ki {
		ti--
	}
	lines = append(lines[:ti+1], append([]string{k}, lines[ti+1:]...)...)
	return strings.Join(lines, "\n")
}

// Handoff sections (C-event-log §6 TaskHandoff). Old briefs write
// "KEY: value" lines; new handoff files write "## KEY" headings.
var (
	handoffKeys  = []string{"STATUS", "OUTPUT", "DECISIONS RAISED", "FOLLOW-UPS", "DEVIATIONS", "FRICTION"}
	inlineKeyRe  = regexp.MustCompile(`^(STATUS|OUTPUT|DECISIONS RAISED|FOLLOW-UPS|DEVIATIONS|FRICTION)\b[^:\n]*:[ \t]?(.*)$`)
	headingKeyRe = regexp.MustCompile(`^##[ \t]+(STATUS|OUTPUT|DECISIONS RAISED|FOLLOW-UPS|DEVIATIONS|FRICTION)\b.*$`)
)

// handoffSections splits a handoff body into its sections. A key may appear
// as "KEY: text" (old format) or "## KEY" (new format).
func handoffSections(body string) map[string]string {
	out := map[string]string{}
	cur := ""
	var buf []string
	flush := func() {
		if cur != "" {
			out[cur] = strings.TrimSpace(strings.Join(buf, "\n"))
		}
	}
	for line := range strings.SplitSeq(body, "\n") {
		if m := headingKeyRe.FindStringSubmatch(line); m != nil {
			flush()
			cur, buf = m[1], nil
			continue
		}
		if m := inlineKeyRe.FindStringSubmatch(line); m != nil {
			flush()
			cur, buf = m[1], []string{m[2]}
			continue
		}
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "# ") {
			flush()
			cur, buf = "", nil
			continue
		}
		if cur != "" {
			buf = append(buf, line)
		}
	}
	flush()
	return out
}

// nonEmptySection reports whether a section says something other than
// "none": it is non-empty and does not start with the word "none".
func nonEmptySection(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	l := strings.ToLower(strings.TrimLeft(s, "-*_ `"))
	if l == "none" || strings.HasPrefix(l, "none.") || strings.HasPrefix(l, "none ") ||
		strings.HasPrefix(l, "none,") || strings.HasPrefix(l, "none;") {
		return false
	}
	return true
}
