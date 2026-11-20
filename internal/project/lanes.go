package project

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
)

// CheckLane checks an event against the lane table of C-event-log v3 §3.
// It defers to core.CheckLaneIn except where core still has the v2 table
// (FOLLOW-UPs in the T-cli handoff):
//
//   - DecisionRaised: the lead, orchestrator and loop may raise, with
//     raised_by.task null allowed (D-lane-gaps). Sessions and integrators
//     raise only for their own task, as core checks.
//   - RouteResolved: the route's owner, whoever it is (D-lane-gaps). Routes
//     the loop has not written yet count (see Routes).
//   - RoleClaimed / RoleReleased: written by the claiming session itself
//     through `ekumen claim` (C-cli-json §3), so data.session must be the
//     actor. C-event-log §3 lists them in the loop's lane; the two
//     contracts disagree, raised as decision draft D-claim-lane.
func (s *Snapshot) CheckLane(e event.Event) error {
	lane := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s by %s:%s: %s", core.ErrLane, e.Type, e.Actor.Role, e.Actor.ID, fmt.Sprintf(format, args...))
	}
	switch e.Type {
	case event.DecisionRaisedType:
		switch e.Actor.Role {
		case event.RoleLead, event.RoleOrchestrator, event.RoleLoop:
			if e.Subject.Kind != event.KindDecision {
				return lane("subject must be a decision")
			}
			return nil
		}
	case event.RouteResolvedType:
		var d event.RouteResolved
		if err := json.Unmarshal(e.Data, &d); err != nil {
			return lane("%v", err)
		}
		r, ok := s.Routes()[d.RouteID]
		if !ok {
			return lane("no route %q", d.RouteID)
		}
		if !Matches(e.Actor, r.To) {
			return lane("route %q belongs to %q", d.RouteID, r.To)
		}
		return nil
	case event.RoleClaimedType, event.RoleReleasedType:
		var d event.RoleClaimed
		if err := json.Unmarshal(e.Data, &d); err != nil {
			return lane("%v", err)
		}
		if e.Subject.Kind != event.KindProject {
			return lane("subject must be the project")
		}
		if e.Actor.Role != event.RoleLoop && d.Session != e.Actor.ID {
			return lane("a session claims or releases roles only for itself (session %q)", d.Session)
		}
		return nil
	}
	return core.CheckLaneIn(&s.State, e)
}

// Matches reports whether an actor is the owner named by to: a role name
// or a session name (C-event-log §4).
func Matches(a event.Actor, to string) bool {
	return to != "" && (string(a.Role) == to || a.ID == to)
}

// Route is an item routed to an owner, recorded by the loop or still
// pending.
type Route struct {
	core.RouteRecord
	// Pending: the loop has not written its RoutedToOwner yet. Until the
	// loop exists (T-reconcile), the CLI computes these with core.Route so
	// that inboxes work.
	Pending bool
}

// Routes returns every route: those recorded by RoutedToOwner, plus those
// core.Route says the loop would write for the current log. A pending
// route that has a RouteResolved in the log is closed (core's reducer
// ignores a RouteResolved that comes before its RoutedToOwner).
func (s *Snapshot) Routes() map[string]Route {
	out := map[string]Route{}
	for id, r := range s.State.Routes {
		out[id] = Route{RouteRecord: *r}
	}
	resolved := map[string]string{}
	for _, e := range s.Events {
		if e.Type != event.RouteResolvedType {
			continue
		}
		var d event.RouteResolved
		if json.Unmarshal(e.Data, &d) == nil {
			if _, ok := resolved[d.RouteID]; !ok {
				resolved[d.RouteID] = e.ID
			}
		}
	}
	for _, p := range core.Route(&s.State, s.Events) {
		r := core.RouteRecord{ID: p.RouteID, Subject: p.Subject, To: p.To, FromEvent: p.FromEvent, Reason: p.Reason, Open: true}
		if by, ok := resolved[p.RouteID]; ok {
			r.Open, r.ClosedBy = false, by
		}
		out[p.RouteID] = Route{RouteRecord: r, Pending: true}
	}
	return out
}

// HasClaim reports whether session holds role.
func (s *Snapshot) HasClaim(role event.Role, session string) bool {
	return slices.ContainsFunc(s.State.Claims, func(c core.Claim) bool { return c.Role == role && c.Session == session })
}

// ClaimHolders lists the sessions holding role.
func (s *Snapshot) ClaimHolders(role event.Role) []string {
	var out []string
	for _, c := range s.State.Claims {
		if c.Role == role && !slices.Contains(out, c.Session) {
			out = append(out, c.Session)
		}
	}
	return out
}
