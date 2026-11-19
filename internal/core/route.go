package core

import (
	"github.com/Yashswarnkar/ekumen/internal/event"
)

// RoutedToOwner is a RoutedToOwner payload the loop should write, with the
// subject to put on its envelope (the subject of the event being routed).
type RoutedToOwner struct {
	Subject event.Subject `json:"subject"`
	event.RoutedToOwner
}

// Routing reasons.
const (
	ReasonHandoff         = "handoff has follow-ups, decisions raised or deviations"
	ReasonQuestion        = "question addressed to owner"
	ReasonQuestionDefault = "question with no addressee: default owner"
	ReasonQuestionNoOwner = "question with no addressee and no session known: orchestrator"
)

// RouteID is the route id for an event. It is derived from the event id,
// so routing the same event twice gives the same id and the loop stays
// idempotent.
func RouteID(fromEvent string) string { return "route-" + fromEvent }

// Route decides, in code, which of newEvents need an owner to act
// (C-event-log §4), and returns the RoutedToOwner entries to write:
//
//   - TaskHandoff with has_followups, has_decisions or has_deviations →
//     the orchestrator.
//   - DecisionRaised → nothing: the decision itself is the lead's item
//     (see Inbox).
//   - QuestionAsked → its `to`; if empty, DefaultAddressee (on a decision,
//     the session that raised it, for an adapter to fork; on a task, its
//     session; on a contract, the orchestrator). When no session is known
//     yet, the orchestrator.
//
// s must be the state with newEvents already reduced in. Events already
// routed (their route id exists in s) and questions already answered are
// skipped, so Route is idempotent.
func Route(s *State, newEvents []event.Event) []RoutedToOwner {
	out := []RoutedToOwner{}
	for _, e := range SortEvents(newEvents) {
		rid := RouteID(e.ID)
		if _, done := s.Routes[rid]; done {
			continue
		}
		p, err := e.Payload()
		if err != nil {
			continue
		}
		var to, reason string
		switch d := p.(type) {
		case *event.TaskHandoff:
			if !d.HasFollowups && !d.HasDecisions && !d.HasDeviations {
				continue
			}
			to, reason = string(event.RoleOrchestrator), ReasonHandoff
		case *event.QuestionAsked:
			q, ok := s.FindQuestion(e.Subject, d.QuestionID)
			if !ok || q.Event != e.ID || q.Answered() {
				continue
			}
			switch {
			case d.To != "":
				to, reason = d.To, ReasonQuestion
			case s.DefaultAddressee(e.Subject) != "":
				to, reason = s.DefaultAddressee(e.Subject), ReasonQuestionDefault
			default:
				to, reason = string(event.RoleOrchestrator), ReasonQuestionNoOwner
			}
		default:
			continue
		}
		out = append(out, RoutedToOwner{
			Subject:       e.Subject,
			RoutedToOwner: event.RoutedToOwner{RouteID: rid, To: to, FromEvent: e.ID, Reason: reason},
		})
	}
	return out
}
