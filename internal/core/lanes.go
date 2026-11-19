package core

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Lane says who may write an event type (C-event-log §3).
type Lane struct {
	// Roles may write the type. Empty with Anyone set means every role.
	Roles []event.Role
	// Anyone: any role may write it (QuestionAsked, QuestionAnswered).
	Anyone bool
	// OwnTask: a session or integrator may write it only for its own task.
	OwnTask bool
	// Addressee: only the question's addressee may write it (QuestionAnswered).
	Addressee bool
	// RoutedToOrchestrator: only for routes whose owner is the orchestrator
	// (RouteResolved, "items routed to it").
	RoutedToOrchestrator bool
	// Subjects are the subject kinds the type may be about. Empty means any.
	Subjects []event.SubjectKind
}

var (
	taskOnly     = []event.SubjectKind{event.KindTask}
	decisionOnly = []event.SubjectKind{event.KindDecision}
	contractOnly = []event.SubjectKind{event.KindContract}
	externalOnly = []event.SubjectKind{event.KindExternal}
	projectOnly  = []event.SubjectKind{event.KindProject}
	questionable = []event.SubjectKind{event.KindTask, event.KindDecision, event.KindContract}
)

var (
	lead         = []event.Role{event.RoleLead}
	orchestrator = []event.Role{event.RoleOrchestrator}
	loop         = []event.Role{event.RoleLoop}
	taskSessions = []event.Role{event.RoleSession, event.RoleIntegrator}
)

// Lanes is the lane table of C-event-log §3, as data. The subject kinds
// follow from §6 (task events are about tasks, and so on; questions may be
// about a task, decision or contract). Role events are project-wide.
var Lanes = map[event.Type]Lane{
	event.TaskApprovedType:       {Roles: lead, Subjects: taskOnly},
	event.DecisionDecidedType:    {Roles: lead, Subjects: decisionOnly},
	event.DecisionSupersededType: {Roles: lead, Subjects: decisionOnly},
	event.ContractFrozenType:     {Roles: lead, Subjects: contractOnly},

	event.TaskDraftedType:         {Roles: orchestrator, Subjects: taskOnly},
	event.TaskAmendedType:         {Roles: orchestrator, Subjects: taskOnly},
	event.TaskBlockersChangedType: {Roles: orchestrator, Subjects: taskOnly},
	event.TaskDeferredType:        {Roles: orchestrator, Subjects: taskOnly},
	event.ContractDraftedType:     {Roles: orchestrator, Subjects: contractOnly},
	event.ContractVersionedType:   {Roles: orchestrator, Subjects: contractOnly},
	event.ExternalAddedType:       {Roles: orchestrator, Subjects: externalOnly},
	event.ExternalUpdatedType:     {Roles: orchestrator, Subjects: externalOnly},
	event.ExternalResolvedType:    {Roles: orchestrator, Subjects: externalOnly},
	event.RouteResolvedType:       {Roles: orchestrator, RoutedToOrchestrator: true},

	event.TaskHandoffType:    {Roles: taskSessions, OwnTask: true, Subjects: taskOnly},
	event.DecisionRaisedType: {Roles: taskSessions, OwnTask: true, Subjects: decisionOnly},

	event.QuestionAskedType:    {Anyone: true, Subjects: questionable},
	event.QuestionAnsweredType: {Anyone: true, Addressee: true, Subjects: questionable},

	event.DecisionDefaultedType:   {Roles: loop, Subjects: decisionOnly},
	event.DecisionAppliedType:     {Roles: loop, Subjects: decisionOnly},
	event.TaskSessionAttachedType: {Roles: loop, Subjects: taskOnly},
	event.RoutedToOwnerType:       {Roles: loop},
	event.ConflictDetectedType:    {Roles: loop},
	event.RoleClaimedType:         {Roles: loop, Subjects: projectOnly},
	event.RoleReleasedType:        {Roles: loop, Subjects: projectOnly},
}

// ErrLane is wrapped by every lane error.
var ErrLane = errors.New("lane")

func laneErr(e event.Event, format string, args ...any) error {
	return fmt.Errorf("%w: %s %s by %s %q: %s", ErrLane, e.Type, e.ID, e.Actor.Role, e.Actor.ID, fmt.Sprintf(format, args...))
}

// CheckLane checks the parts of C-event-log §3 that need only the event:
// the type has a lane, the actor's role may write it, and the subject kind
// fits the type. The parts that need state (own task, addressee, routed to
// the orchestrator) are in CheckLaneIn.
func CheckLane(e event.Event) error {
	lane, ok := Lanes[e.Type]
	if !ok {
		return laneErr(e, "type has no lane")
	}
	if !lane.Anyone && !slices.Contains(lane.Roles, e.Actor.Role) {
		return laneErr(e, "role %q may not write it (lane: %v)", e.Actor.Role, lane.Roles)
	}
	if len(lane.Subjects) > 0 && !slices.Contains(lane.Subjects, e.Subject.Kind) {
		return laneErr(e, "subject kind %q not allowed (want %v)", e.Subject.Kind, lane.Subjects)
	}
	return nil
}

// SessionTasks returns the ids of tasks whose attached session is this one
// (the latest TaskSessionAttached names it). A session's "own task"
// (C-event-log §3) is one of these.
func (s *State) SessionTasks(session string) []string {
	var out []string
	for _, id := range sortedKeys(s.Tasks) {
		if s.Tasks[id].Session == session {
			out = append(out, id)
		}
	}
	return out
}

// CheckLaneIn is CheckLane plus the rules that need state (C-event-log §3):
//   - TaskHandoff and DecisionRaised by a session or integrator: the subject
//     task (for a handoff) or raised_by.task (for a decision) must be a task
//     the actor's session is attached to, and raised_by.session must be the
//     actor. A decision with raised_by.task null cannot be raised by a session.
//   - QuestionAnswered: only by the question's addressee (see Addressee).
//   - RouteResolved: only for a route whose owner is the orchestrator.
func CheckLaneIn(s *State, e event.Event) error {
	if err := CheckLane(e); err != nil {
		return err
	}
	lane := Lanes[e.Type]
	p, err := e.Payload()
	if err != nil {
		return laneErr(e, "%v", err)
	}
	if lane.OwnTask {
		own := s.SessionTasks(e.Actor.ID)
		var task string
		switch d := p.(type) {
		case *event.TaskHandoff:
			task = s.Resolve(e.Subject.ID)
		case *event.DecisionRaised:
			if d.RaisedBy.Task == nil {
				return laneErr(e, "raised_by.task is null; a %s may raise decisions only for its own task", e.Actor.Role)
			}
			if d.RaisedBy.Session != e.Actor.ID {
				return laneErr(e, "raised_by.session %q is not the actor", d.RaisedBy.Session)
			}
			task = s.Resolve(*d.RaisedBy.Task)
		}
		if !slices.Contains(own, task) {
			return laneErr(e, "task %q is not the session's own task (attached: %v)", task, own)
		}
	}
	if lane.Addressee {
		d := p.(*event.QuestionAnswered)
		q, ok := s.FindQuestion(e.Subject, d.QuestionID)
		if !ok {
			return laneErr(e, "no question %q on %s %s", d.QuestionID, e.Subject.Kind, e.Subject.ID)
		}
		to := s.Addressee(e.Subject, q)
		if to == "" || (string(e.Actor.Role) != to && e.Actor.ID != to) {
			return laneErr(e, "only the addressee %q may answer question %q", to, d.QuestionID)
		}
	}
	if lane.RoutedToOrchestrator {
		d := p.(*event.RouteResolved)
		r, ok := s.Routes[d.RouteID]
		if !ok {
			return laneErr(e, "no route %q", d.RouteID)
		}
		if r.To != string(event.RoleOrchestrator) {
			return laneErr(e, "route %q is routed to %q, not the orchestrator", d.RouteID, r.To)
		}
	}
	return nil
}

// Addressee returns who a question is addressed to: its `to` if set,
// otherwise the owner it was routed to, otherwise the default of
// C-event-log §4 (see DefaultAddressee). The result is a role name or a
// session name.
func (s *State) Addressee(sub event.Subject, q Question) string {
	if q.To != "" {
		return q.To
	}
	if r, ok := s.Routes[RouteID(q.Event)]; ok {
		return r.To
	}
	return s.DefaultAddressee(sub)
}

// DefaultAddressee is where a question with an empty `to` goes
// (C-event-log §4): on a decision, the session that raised it (an adapter
// forks it); on a task, that task's session; on a contract, the
// orchestrator. It returns "" when the session is not known yet.
func (s *State) DefaultAddressee(sub event.Subject) string {
	id := s.Resolve(sub.ID)
	switch sub.Kind {
	case event.KindDecision:
		if d, ok := s.Decisions[id]; ok {
			return d.RaisedBy.Session
		}
	case event.KindTask:
		if t, ok := s.Tasks[id]; ok {
			return t.Session
		}
	case event.KindContract:
		return string(event.RoleOrchestrator)
	}
	return ""
}
