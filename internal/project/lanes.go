package project

import (
	"slices"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
)

// CheckLane checks an event against the lane table of C-event-log v4 §3
// (core.CheckLaneIn), against the current state.
func (s *Snapshot) CheckLane(e event.Event) error {
	return core.CheckLaneIn(&s.State, e)
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
// route that its owner already resolved is closed (core keeps such early
// resolutions in State.EarlyResolutions).
func (s *Snapshot) Routes() map[string]Route {
	out := map[string]Route{}
	for id, r := range s.State.Routes {
		out[id] = Route{RouteRecord: *r}
	}
	for _, p := range core.Route(&s.State, s.Events) {
		r := core.RouteRecord{ID: p.RouteID, Subject: p.Subject, To: p.To, FromEvent: p.FromEvent, Reason: p.Reason, Open: true}
		if res, ok := s.State.EarlyResolutions[p.RouteID]; ok {
			r.Open, r.ClosedBy, r.Note = false, res.Event, res.Note
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
