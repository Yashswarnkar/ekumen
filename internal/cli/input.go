package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// readInput reads a --file or --text-file argument; "-" is stdin.
func (r *run) readInput(flagName, path string) ([]byte, error) {
	if path == "" {
		return nil, fieldErr(CodeUsage, flagName, "--%s is required", flagName)
	}
	if path == "-" {
		b, err := io.ReadAll(r.env.Stdin)
		if err != nil {
			return nil, errf(CodeInternal, "read stdin: %v", err)
		}
		return b, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.env.Dir, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fieldErr(CodeNotFound, flagName, "read %s: %v", path, err)
	}
	return b, nil
}

// yamlToJSON parses one YAML document into plain JSON values, by tag, so
// dates stay strings ("2026-11-20") and keys like `on` stay keys.
func yamlToJSON(b []byte) (any, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == 0 {
		return nil, fmt.Errorf("empty document")
	}
	return nodeValue(&doc)
}

func nodeValue(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return nodeValue(n.Content[0])
	case yaml.AliasNode:
		return nodeValue(n.Alias)
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("line %d: keys must be plain scalars", k.Line)
			}
			v, err := nodeValue(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			if _, dup := m[k.Value]; dup {
				return nil, fmt.Errorf("line %d: duplicate key %q", k.Line, k.Value)
			}
			m[k.Value] = v
		}
		return m, nil
	case yaml.SequenceNode:
		out := []any{}
		for _, c := range n.Content {
			v, err := nodeValue(c)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.ScalarNode:
		switch n.ShortTag() {
		case "!!null":
			return nil, nil
		case "!!bool":
			var b bool
			if err := n.Decode(&b); err != nil {
				return nil, err
			}
			return b, nil
		case "!!int":
			i, err := strconv.ParseInt(n.Value, 0, 64)
			if err != nil {
				return nil, fmt.Errorf("line %d: %v", n.Line, err)
			}
			return i, nil
		case "!!float":
			var f float64
			if err := n.Decode(&f); err != nil {
				return nil, err
			}
			return f, nil
		}
		return n.Value, nil
	}
	return nil, fmt.Errorf("line %d: unsupported YAML node", n.Line)
}

// decodeStrict converts a JSON-able value into dst, refusing unknown
// fields. It returns the field name of an unknown field, if any.
func decodeStrict(v any, dst any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

var unknownFieldRe = regexp.MustCompile(`unknown field "([^"]+)"`)

func unknownField(err error) string {
	if m := unknownFieldRe.FindStringSubmatch(err.Error()); m != nil {
		return m[1]
	}
	return ""
}

// Handoff is what `task handoff` parses from a handoff's headings
// (C-cli-json §3: "the has_* flags are parsed from the section headings").
type Handoff struct {
	StatusLine    string
	HasFollowups  bool
	HasDecisions  bool
	HasDeviations bool
	Sections      []string
}

var headingRe = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)

func sectionKey(h string) string {
	var b strings.Builder
	for _, c := range strings.ToUpper(h) {
		if c >= 'A' && c <= 'Z' {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// emptyLine reports lines that don't count as content: blank, or a
// placeholder like "None", "n/a" or "-".
func emptyLine(l string) bool {
	l = strings.ToLower(strings.TrimSpace(l))
	l = strings.TrimLeft(l, "-*• ")
	l = strings.TrimRight(l, ". ")
	switch l {
	case "", "none", "n/a", "na", "nothing", "no", "```", "```yaml", "[]":
		return true
	}
	return false
}

// ParseHandoff reads the sections of a handoff file. It skips YAML front
// matter. STATUS's first content line is the status line; FOLLOW-UPS,
// DECISIONS RAISED and DEVIATIONS count when they have any content line.
func ParseHandoff(text string) Handoff {
	var h Handoff
	content := map[string][]string{}
	cur := ""
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	first, inFront, inFence := true, false, false
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			if strings.TrimSpace(line) == "---" {
				inFront = true
				continue
			}
		}
		if inFront {
			if strings.TrimSpace(line) == "---" {
				inFront = false
			}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if m := headingRe.FindStringSubmatch(line); m != nil && !inFence {
			cur = sectionKey(m[1])
			h.Sections = append(h.Sections, m[1])
			continue
		}
		if cur != "" && !emptyLine(line) {
			content[cur] = append(content[cur], strings.TrimSpace(line))
		}
	}
	if ls := content["STATUS"]; len(ls) > 0 {
		h.StatusLine = strings.TrimSpace(strings.TrimLeft(ls[0], "-*• "))
	}
	h.HasFollowups = len(content["FOLLOWUPS"]) > 0
	h.HasDecisions = len(content["DECISIONSRAISED"]) > 0
	h.HasDeviations = len(content["DEVIATIONS"]) > 0
	return h
}

// EntryFront is the YAML front matter of an amendment or handoff file
// (docs/cli.md, "Entry front matter"). Every key is optional; other keys
// are kept in the file and ignored.
type EntryFront struct {
	// Present: the file starts with a front-matter block.
	Present bool
	// Task is the task id (or alias) the entry is about.
	Task string
	// Author is the writer: the actor's id, its file-name slug, or
	// <role>:<id>.
	Author string
	// Date is the UTC date the entry was written, YYYY-MM-DD.
	Date string
}

// ParseEntryFront reads the front matter of an entry: a first line of
// "---", YAML, and a closing "---" line.
func ParseEntryFront(text string) (EntryFront, error) {
	var f EntryFront
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return f, nil
	}
	var block strings.Builder
	closed := false
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			closed = true
			break
		}
		block.WriteString(l)
	}
	if !closed {
		return f, fmt.Errorf("front matter has no closing ---")
	}
	f.Present = true
	if strings.TrimSpace(block.String()) == "" {
		return f, nil
	}
	v, err := yamlToJSON([]byte(block.String()))
	if err != nil {
		return f, fmt.Errorf("front matter: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return f, fmt.Errorf("front matter is not a YAML mapping")
	}
	for key, dst := range map[string]*string{"task": &f.Task, "author": &f.Author, "date": &f.Date} {
		switch x := m[key].(type) {
		case nil:
		case string:
			*dst = strings.TrimSpace(x)
		default:
			return f, fmt.Errorf("front matter %s: want a string, got %v", key, x)
		}
	}
	return f, nil
}

// withEntryFront puts generated front matter on an entry that has none.
func withEntryFront(body []byte, task, author, date string) []byte {
	head := fmt.Sprintf("---\ntask: %s\nauthor: %s\ndate: %s\n---\n\n", task, author, date)
	return append([]byte(head), body...)
}
