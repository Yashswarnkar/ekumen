package core

import (
	"cmp"
	"slices"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Urgency weights a decision's deadline against today (UTC dates, so the
// result does not depend on the time of day):
//
//	no deadline         1
//	due in 8+ days      1
//	due in 4–7 days     2
//	due in 2–3 days     3
//	due tomorrow        4
//	due today           5
//	overdue             6
//
// Steps, not a smooth curve, so a priority reads as a small whole number and
// only changes once a day. A deadline that is not a date counts as none.
// This is the formula raised for confirmation as decision draft
// D-urgency-formula.
func Urgency(deadline *string, now time.Time) int {
	if deadline == nil {
		return 1
	}
	due, err := time.Parse(time.DateOnly, *deadline)
	if err != nil {
		return 1
	}
	today, _ := time.Parse(time.DateOnly, now.UTC().Format(time.DateOnly))
	days := int(due.Sub(today).Hours() / 24)
	switch {
	case days < 0:
		return 6
	case days == 0:
		return 5
	case days == 1:
		return 4
	case days <= 3:
		return 3
	case days <= 7:
		return 2
	}
	return 1
}

// BlockedByDecision lists, sorted, the tasks a decision blocks directly or
// through contracts, transitively (spine §3, C-decision-record §1):
//
//  1. the tasks in its `blocks`, and tasks whose blocked_by names it;
//  2. tasks whose blocked_by names a contract in its `contract_impact`;
//  3. then, repeatedly, tasks whose blocked_by names a task already counted.
//
// Deferred tasks are left out. Done tasks cannot be told apart without
// observations and are counted. The reach through contracts is this
// package's reading, raised in decision draft D-blocker-semantics.
func BlockedByDecision(s *State, decisionID string) []string {
	id := s.Resolve(decisionID)
	d, ok := s.Decisions[id]
	if !ok {
		return nil
	}
	set := map[string]bool{}
	for _, b := range d.Blocks {
		set[s.Resolve(b)] = true
	}
	contracts := map[string]bool{}
	for _, c := range d.ContractImpact {
		contracts[s.Resolve(c)] = true
	}
	for tid, t := range s.Tasks {
		for _, b := range t.BlockedBy {
			if r := s.Resolve(b); r == id || contracts[r] {
				set[tid] = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for tid, t := range s.Tasks {
			if set[tid] {
				continue
			}
			for _, b := range t.BlockedBy {
				if set[s.Resolve(b)] {
					set[tid], changed = true, true
					break
				}
			}
		}
	}
	var out []string
	for tid := range set {
		if t, ok := s.Tasks[tid]; ok && t.Drafted() && !t.Deferred {
			out = append(out, tid)
		}
	}
	slices.Sort(out)
	return out
}

// Priority is a decision's priority: tasks blocked (BlockedByDecision) ×
// Urgency of its deadline (spine §3, C-decision-record §1 "Not stored").
// A decision that blocks nothing has priority 0.
func Priority(s *State, decisionID string, now time.Time) int {
	d, ok := s.Decisions[s.Resolve(decisionID)]
	if !ok {
		return 0
	}
	return len(BlockedByDecision(s, decisionID)) * Urgency(d.Deadline, now)
}

// RankedDecision is a pending decision with its computed priority.
type RankedDecision struct {
	ID       string `json:"id"`
	Priority int    `json:"priority"`
	Blocked  int    `json:"blocked"`
	Urgency  int    `json:"urgency"`
}

// PendingDecisions returns the raised, pending decisions ranked by priority
// (highest first), then earliest deadline (none last), then id.
func PendingDecisions(s *State, now time.Time) []RankedDecision {
	out := []RankedDecision{}
	for _, id := range sortedKeys(s.Decisions) {
		d := s.Decisions[id]
		if len(d.Raises) == 0 || d.Status != StatusPending {
			continue
		}
		n, u := len(BlockedByDecision(s, id)), Urgency(d.Deadline, now)
		out = append(out, RankedDecision{ID: id, Priority: n * u, Blocked: n, Urgency: u})
	}
	slices.SortStableFunc(out, func(a, b RankedDecision) int {
		if c := cmp.Compare(b.Priority, a.Priority); c != 0 {
			return c
		}
		da, db := s.Decisions[a.ID].Deadline, s.Decisions[b.ID].Deadline
		switch {
		case da != nil && db == nil:
			return -1
		case da == nil && db != nil:
			return 1
		case da != nil && db != nil && *da != *db:
			return cmp.Compare(*da, *db)
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return out
}

// InboxItem is one thing waiting on an owner.
type InboxItem struct {
	// Kind is "route" (an open routed item) or "decision" (a pending decision,
	// in the lead's inbox only).
	Kind     string        `json:"kind"`
	ID       string        `json:"id"` // route id or decision id
	Subject  event.Subject `json:"subject"`
	Reason   string        `json:"reason,omitempty"`
	Priority int           `json:"priority,omitempty"`
}

// Inbox kinds.
const (
	InboxRoute    = "route"
	InboxDecision = "decision"
)

// Inbox lists what waits on owner, a role name or a session name
// (C-event-log §4): for the lead, pending decisions ranked by priority
// (PendingDecisions) come first; then every open route addressed to owner,
// oldest first. now is needed only for decision urgency.
func Inbox(s *State, owner string, now time.Time) []InboxItem {
	out := []InboxItem{}
	if owner == string(event.RoleLead) {
		for _, r := range PendingDecisions(s, now) {
			out = append(out, InboxItem{Kind: InboxDecision, ID: r.ID,
				Subject: event.Subject{Kind: event.KindDecision, ID: r.ID}, Priority: r.Priority})
		}
	}
	var routes []*RouteRecord
	for _, r := range s.Routes {
		if r.Open && r.To == owner {
			routes = append(routes, r)
		}
	}
	slices.SortFunc(routes, func(a, b *RouteRecord) int { return cmp.Or(cmp.Compare(a.Event, b.Event), cmp.Compare(a.ID, b.ID)) })
	for _, r := range routes {
		out = append(out, InboxItem{Kind: InboxRoute, ID: r.ID, Subject: r.Subject, Reason: r.Reason})
	}
	return out
}
