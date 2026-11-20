package legacyimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// nodeValue turns a YAML node into plain values for JSON: mappings become
// map[string]any, sequences []any, and scalars keep their source text
// (dates stay "2026-11-20", not a time), except null, booleans and integers.
func nodeValue(n *yaml.Node) any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil
		}
		return nodeValue(n.Content[0])
	case yaml.AliasNode:
		return nodeValue(n.Alias)
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			m[n.Content[i].Value] = nodeValue(n.Content[i+1])
		}
		return m
	case yaml.SequenceNode:
		s := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			s = append(s, nodeValue(c))
		}
		return s
	case yaml.ScalarNode:
		switch n.ShortTag() {
		case "!!null":
			return nil
		case "!!bool":
			if b, err := strconv.ParseBool(n.Value); err == nil {
				return b
			}
		case "!!int":
			if i, err := strconv.Atoi(n.Value); err == nil {
				return i
			}
		}
		return n.Value
	}
	return nil
}

// mappingKeys returns the keys of a mapping node and their value nodes.
func mappingPairs(n *yaml.Node) [][2]*yaml.Node {
	if n != nil && n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out [][2]*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, [2]*yaml.Node{n.Content[i], n.Content[i+1]})
	}
	return out
}

// extraKeys returns the keys of mapping n that are not in known, with
// their values, or nil when there are none.
func extraKeys(n *yaml.Node, known []string) map[string]any {
	var out map[string]any
	for _, kv := range mappingPairs(n) {
		if slices.Contains(known, kv[0].Value) {
			continue
		}
		if out == nil {
			out = map[string]any{}
		}
		out[kv[0].Value] = nodeValue(kv[1])
	}
	return out
}

// isNull reports whether a node is absent or a YAML null.
func isNull(n *yaml.Node) bool {
	return n == nil || n.Kind == 0 || (n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null")
}

// frontMatter splits "---\n<yaml>\n---\n<body>" into its two parts. ok is
// false when the text does not start with front matter.
func frontMatter(text []byte) (fm, body []byte, ok bool) {
	if !bytes.HasPrefix(text, []byte("---\n")) {
		return nil, text, false
	}
	rest := text[4:]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		if bytes.HasSuffix(rest, []byte("\n---")) {
			return rest[:len(rest)-4], nil, true
		}
		return nil, text, false
	}
	return rest[:end+1], rest[end+5:], true
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// firstLine returns the first non-empty line of s, trimmed, cut to max
// runes.
func firstLine(s string, max int) string {
	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > max {
			return string(r[:max-1]) + "…"
		}
		return line
	}
	return ""
}

// entryLess orders entry file names like the file store: by stem, with runs
// of digits compared by value and a stem that is a prefix of another first.
func entryLess(a, b string) int {
	sa, sb := strings.TrimSuffix(a, ".md"), strings.TrimSuffix(b, ".md")
	i, j := 0, 0
	for i < len(sa) && j < len(sb) {
		if isDigit(sa[i]) && isDigit(sb[j]) {
			si, sj := i, j
			for i < len(sa) && isDigit(sa[i]) {
				i++
			}
			for j < len(sb) && isDigit(sb[j]) {
				j++
			}
			na, nb := strings.TrimLeft(sa[si:i], "0"), strings.TrimLeft(sb[sj:j], "0")
			if len(na) != len(nb) {
				return len(na) - len(nb)
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			continue
		}
		if sa[i] != sb[j] {
			return int(sa[i]) - int(sb[j])
		}
		i++
		j++
	}
	if c := (len(sa) - i) - (len(sb) - j); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func errorf(format string, args ...any) error { return fmt.Errorf("legacyimport: "+format, args...) }
