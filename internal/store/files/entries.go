package files

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// EntryKind is the folder of a brief entry (C-event-log §8).
type EntryKind string

// The two kinds of brief entry.
const (
	Amendments EntryKind = "amendments" // §B, owner: orchestrator
	Handoffs   EntryKind = "handoffs"   // §C, owner: the task's session
)

const (
	briefsDir = "briefs"
	briefFile = "brief.md"
	// TimestampFormat is the UTC timestamp at the start of entry names.
	TimestampFormat = "20060102T150405Z"
	// maxSuffix bounds the "-N" suffixes tried for one second and author.
	maxSuffix = 1000
)

// entryNameRe matches "<timestamp>-<slug>.md"; a "-N" clash suffix is part
// of the slug as far as the layout is concerned.
var entryNameRe = regexp.MustCompile(`^([0-9]{8}T[0-9]{6}Z)-([a-z0-9]+(?:-[a-z0-9]+)*)\.md$`)

// ignoredNames are placeholder files that are not entries and not worth a
// warning.
var ignoredNames = map[string]bool{".gitkeep": true}

// Entry is one amendment (§B) or handoff (§C) file.
type Entry struct {
	Kind   EntryKind
	Name   string // file name, e.g. "20261120T091403Z-t-core.md"
	Path   string // full path
	Rel    string // slash-separated path relative to the store root
	SHA256 string // lowercase hex SHA-256 of Body
	Body   []byte
}

// Brief is a task's brief as stored: §A, then its amendments and handoffs,
// each in name order (which is time order).
type Brief struct {
	Task       string
	Path       string // full path of brief.md
	Rel        string
	SHA256     string // of Text
	Text       []byte // §A
	Amendments []Entry
	Handoffs   []Entry
}

func validEntryKind(k EntryKind) error {
	if k != Amendments && k != Handoffs {
		return invalid("entry kind %q is not %q or %q", k, Amendments, Handoffs)
	}
	return nil
}

// WriteEntry writes body as a new amendment or handoff of task, named
// briefs/<task>/<kind>/<UTC YYYYMMDDTHHMMSSZ>-<author>.md, and returns its
// full path and the lowercase hex SHA-256 of body.
//
// It never overwrites. If the name is taken (same author, same second) it
// tries "<timestamp>-<author>-2.md", "-3", and so on. author must be a
// lowercase slug such as "orchestrator" or "t-core". The write is atomic.
func (s *Store) WriteEntry(task string, kind EntryKind, author string, body []byte) (string, string, error) {
	if err := validTask(task); err != nil {
		return "", "", err
	}
	if err := validEntryKind(kind); err != nil {
		return "", "", err
	}
	if err := validAuthor(author); err != nil {
		return "", "", err
	}
	dir, err := s.join(briefsDir, task, string(kind))
	if err != nil {
		return "", "", err
	}
	ts := s.clock().UTC().Format(TimestampFormat)
	for n := 1; n <= maxSuffix; n++ {
		name := ts + "-" + author + ".md"
		if n > 1 {
			name = ts + "-" + author + "-" + strconv.Itoa(n) + ".md"
		}
		p, err := s.writeNew(dir, name, body)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		return p, sha256Hex(body), nil
	}
	return "", "", fmt.Errorf("files: more than %d entries by %q in %s at %s", maxSuffix, author, dir, ts)
}

// ReadBrief returns the brief of task: §A from brief.md, then its
// amendments and handoffs, each sorted by name. Names sort as text except
// that numbers compare by value, so a "-10" clash suffix sorts after "-9"
// and the unsuffixed name comes first.
//
// Files in amendments/ and handoffs/ that don't match the layout are
// skipped and reported as warnings (.gitkeep is ignored silently). A
// missing brief.md is an error wrapping fs.ErrNotExist.
func (s *Store) ReadBrief(task string) (*Brief, []Warning, error) {
	if err := validTask(task); err != nil {
		return nil, nil, err
	}
	dir, err := s.join(briefsDir, task)
	if err != nil {
		return nil, nil, err
	}
	b := &Brief{Task: task, Path: filepath.Join(dir, briefFile)}
	if b.Text, err = os.ReadFile(b.Path); err != nil {
		return nil, nil, err
	}
	b.SHA256 = sha256Hex(b.Text)
	b.Rel, _ = s.Rel(b.Path)

	var warns []Warning
	for _, kind := range []EntryKind{Amendments, Handoffs} {
		ents, w, err := s.readEntries(filepath.Join(dir, string(kind)), kind)
		if err != nil {
			return nil, nil, err
		}
		warns = append(warns, w...)
		if kind == Amendments {
			b.Amendments = ents
		} else {
			b.Handoffs = ents
		}
	}
	return b, warns, nil
}

func (s *Store) readEntries(dir string, kind EntryKind) ([]Entry, []Warning, error) {
	des, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var out []Entry
	var warns []Warning
	for _, de := range des {
		name := de.Name()
		p := filepath.Join(dir, name)
		if ignoredNames[name] {
			continue
		}
		if !de.Type().IsRegular() {
			warns = append(warns, Warning{p, "not a regular file"})
			continue
		}
		m := entryNameRe.FindStringSubmatch(name)
		if m == nil {
			warns = append(warns, Warning{p, "name is not <timestamp>-<slug>.md"})
			continue
		}
		if _, err := time.Parse(TimestampFormat, m[1]); err != nil {
			warns = append(warns, Warning{p, "timestamp is not a valid UTC time"})
			continue
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return nil, nil, err
		}
		rel, _ := s.Rel(p)
		out = append(out, Entry{Kind: kind, Name: name, Path: p, Rel: rel, SHA256: sha256Hex(body), Body: body})
	}
	sort.Slice(out, func(i, j int) bool { return lessName(out[i].Name, out[j].Name) })
	return out, warns, nil
}

// lessName orders entry names: text order, except that runs of digits
// compare by numeric value, and a name that is a prefix of another (before
// ".md") comes first. Equal-by-value names fall back to plain text order.
func lessName(a, b string) bool {
	sa, sb := stem(a), stem(b)
	if c := naturalCompare(sa, sb); c != 0 {
		return c < 0
	}
	return a < b
}

func stem(name string) string {
	if len(name) > 3 && name[len(name)-3:] == ".md" {
		return name[:len(name)-3]
	}
	return name
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func naturalCompare(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			si, sj := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na, nb := trimZeros(a[si:i]), trimZeros(b[sj:j])
			if len(na) != len(nb) {
				return cmpInt(len(na), len(nb))
			}
			if na != nb {
				if na < nb {
					return -1
				}
				return 1
			}
			continue
		}
		if a[i] != b[j] {
			return cmpInt(int(a[i]), int(b[j]))
		}
		i++
		j++
	}
	return cmpInt(len(a)-i, len(b)-j)
}

func trimZeros(s string) string {
	for len(s) > 1 && s[0] == '0' {
		s = s[1:]
	}
	return s
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
