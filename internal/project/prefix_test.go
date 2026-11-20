package project

import (
	"slices"
	"testing"
)

func TestMatchPrefix_CLIJSON1(t *testing.T) {
	ids := []string{"q-01jd8zaaaa", "q-01jd8zbbbb", "q-01jd90cccc", "route-01ABC"}
	cases := []struct {
		in      string
		want    string
		matches []string
	}{
		{"q-01jd8zaaaa", "q-01jd8zaaaa", nil},
		{"Q-01JD8ZA", "q-01jd8zaaaa", nil},
		{"q-01jd9", "q-01jd90cccc", nil},
		{"route-01abc", "route-01ABC", nil},
		{"q-01jd8z", "", []string{"q-01jd8zaaaa", "q-01jd8zbbbb"}},
		{"q-zz", "", nil},
		{"", "", nil},
	}
	for _, c := range cases {
		got, m := MatchPrefix(ids, c.in)
		if got != c.want || !slices.Equal(m, c.matches) {
			t.Errorf("MatchPrefix(%q) = %q %v, want %q %v", c.in, got, m, c.want, c.matches)
		}
	}
}
