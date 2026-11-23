package observe

import "testing"

// GitHub #39: recorded branch, then stack suffix, then title; an open PR
// wins over a merged one whatever the rule.
func TestLinkTask_Issue39(t *testing.T) {
	prs := []PR{
		{Number: 1, Branch: "build/t-x", State: StateMerged, Title: "first"},
		{Number: 2, Branch: "build/t-x-2", State: StateMerged, Title: "second"},
		{Number: 3, Branch: "build/t-x-2a", State: StateOpen, Title: "not a stack"},
		{Number: 4, Branch: "other", State: StateMerged, Title: "T-y: titled"},
		{Number: 5, Branch: "orch/y", State: StateOpen, Title: "T-y brief: not the task's"},
		{Number: 6, Branch: "build/t-z-3", State: StateOpen, Title: "z three"},
		{Number: 7, Branch: "build/t-z", State: StateMerged, Title: "z one"},
	}
	cases := []struct {
		branch, id string
		num        int
		by         string
	}{
		{"build/t-x", "T-x", 1, LinkBranch}, // both merged: the recorded branch wins
		{"build/t-z", "T-z", 6, LinkStack},  // an open stacked PR wins over a merged base
		{"", "T-y", 4, LinkTitle},           // no branch: title only
		{"build/t-q", "T-q", 0, ""},         // nothing
		{"build/t-x-2", "T-none", 2, LinkBranch},
	}
	for _, c := range cases {
		l, ok := LinkTask(prs, c.branch, c.id)
		if c.num == 0 {
			if ok {
				t.Errorf("%s: linked %+v, want none", c.id, l)
			}
			continue
		}
		if !ok || l.Number != c.num || l.By != c.by {
			t.Errorf("%s: got #%d by %q (ok %v), want #%d by %s", c.id, l.Number, l.By, ok, c.num, c.by)
		}
	}
}
