package project

import (
	"slices"
	"strings"
)

// MatchPrefix finds the id in ids that in names: an exact match (ignoring
// case), or else the only id that starts with in (C-cli-json v2 §1, for
// question and route ids). It returns the id, or "" and every id that
// starts with in (none: not found; several: ambiguous).
func MatchPrefix(ids []string, in string) (string, []string) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", nil
	}
	var matches []string
	for _, id := range ids {
		if strings.EqualFold(id, in) {
			return id, nil
		}
		if len(id) >= len(in) && strings.EqualFold(id[:len(in)], in) {
			matches = append(matches, id)
		}
	}
	slices.Sort(matches)
	matches = slices.Compact(matches)
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", matches
}

// QuestionIDs lists the id of every question on any subject.
func (s *Snapshot) QuestionIDs() []string {
	var out []string
	for _, q := range s.Questions() {
		out = append(out, q.ID)
	}
	return out
}

// RouteIDs lists the id of every route, recorded or pending.
func (s *Snapshot) RouteIDs() []string {
	return sortedKeys(s.Routes())
}
