package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Lane says who may write an event type (C-event-log v4 §3).
type Lane struct {
	// Roles may write the type. Ignored when Anyone is set.
	Roles []event.Role
	// Anyone: any role may write it, subject to the checks below.
	Anyone bool
	// OwnTask: a session or integrator may write it only for its own task.
	// Other roles in Roles are not limited this way (DecisionRaised by the
	// lead, orchestrator or loop may have raised_by.task null; D-lane-gaps).
	OwnTask bool
	// Addressee: only the question's addressee may write it (QuestionAnswered).
	Addressee bool
	// RouteOwner: only the owner the route is (or would be) routed to may
	// write it (RouteResolved, D-lane-gaps).
	RouteOwner bool
	// ClaimingSession: data.session must be the actor's id (RoleClaimed,
	// RoleReleased, D-claim-lane).
	ClaimingSession bool
	// ByDecision are roles that may write the type only when its
	// data.by_decision is set (TaskAmended by the loop when it applies a
	// decision; C-event-log v5 §3, D-apply-amend-lane). They must also be
	// listed in Roles.
	ByDecision []event.Role
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
	orchOrLoop   = []event.Role{event.RoleOrchestrator, event.RoleLoop}
	taskSessions = []event.Role{event.RoleSession, event.RoleIntegrator}
	raisers      = []event.Role{event.RoleLead, event.RoleOrchestrator, event.RoleSession, event.RoleIntegrator, event.RoleLoop}
)

// Lanes is the lane table of C-event-log v5 §3, as data. The subject kinds
// follow from §6 (task events are about tasks, and so on; questions may be
// about a task, decision or contract). Role events are project-wide.
var Lanes = map[event.Type]Lane{
	event.TaskApprovedType:       {Roles: lead, Subjects: taskOnly},
	event.DecisionDecidedType:    {Roles: lead, Subjects: decisionOnly},
	event.DecisionSupersededType: {Roles: lead, Subjects: decisionOnly},
	event.ContractFrozenType:     {Roles: lead, Subjects: contractOnly},
	event.SpineApprovedType:      {Roles: lead, Subjects: projectOnly},

	event.TaskDraftedType:             {Roles: orchestrator, Subjects: taskOnly},
	event.TaskAmendedType:             {Roles: orchOrLoop, ByDecision: loop, Subjects: taskOnly},
	event.TaskBlockersChangedType:     {Roles: orchestrator, Subjects: taskOnly},
	event.TaskDeferredType:            {Roles: orchestrator, Subjects: taskOnly},
	event.ContractDraftedType:         {Roles: orchestrator, Subjects: contractOnly},
	event.ContractBlockersChangedType: {Roles: orchestrator, Subjects: contractOnly},
	event.ContractVersionedType:       {Roles: orchestrator, Subjects: contractOnly},
	event.ExternalAddedType:           {Roles: orchestrator, Subjects: externalOnly},
	event.ExternalUpdatedType:         {Roles: orchestrator, Subjects: externalOnly},
	event.ExternalResolvedType:        {Roles: orchestrator, Subjects: externalOnly},

	event.TaskHandoffType:    {Roles: taskSessions, OwnTask: true, Subjects: taskOnly},
	event.DecisionRaisedType: {Roles: raisers, OwnTask: true, Subjects: decisionOnly},

	event.QuestionAskedType:    {Anyone: true, Subjects: questionable},
	event.QuestionAnsweredType: {Anyone: true, Addressee: true, Subjects: questionable},
	event.RouteResolvedType:    {Anyone: true, RouteOwner: true},
	event.RoleClaimedType:      {Anyone: true, ClaimingSession: true, Subjects: projectOnly},
	event.RoleReleasedType:     {Anyone: true, ClaimingSession: true, Subjects: projectOnly},

	event.DecisionDefaultedType:   {Roles: loop, Subjects: decisionOnly},
	event.DecisionAppliedType:     {Roles: loop, Subjects: decisionOnly},
	event.TaskSessionAttachedType: {Roles: orchOrLoop, Subjects: taskOnly},
	event.RoutedToOwnerType:       {Roles: loop},
	event.ConflictDetectedType:    {Roles: loop},
}

// ErrLane is wrapped by every lane error.
var ErrLane = errors.New("lane")

func laneErr(e event.Event, format string, args ...any) error {
	return fmt.Errorf("%w: %s %s by %s %q: %s", ErrLane, e.Type, e.ID, e.Actor.Role, e.Actor.ID, fmt.Sprintf(format, args...))
}

// CheckLane checks the parts of C-event-log §3 that need only the event:
// the type has a lane, the actor's role may write it, the subject kind fits
// the type, and a role claim or release names the actor's own session. The
// parts that need state (own task, addressee, route owner) are in
// CheckLaneIn.
func CheckLane(e event.Event) error {
	lane, ok := Lanes[e.Type]
	if !ok {
		return laneErr(e, "type has no lane")
	}
	if !lane.Anyone && !slices.Contains(lane.Roles, e.Actor.Role) {
		return laneErr(e, "role %q may not write it (lane: %v)", e.Actor.Role, lane.Roles)
	}
	if slices.Contains(lane.ByDecision, e.Actor.Role) {
		var d struct {
			ByDecision string `json:"by_decision"`
		}
		if err := json.Unmarshal(orEmptyJSON(e.Data), &d); err != nil {
			return laneErr(e, "%v", err)
		}
		if d.ByDecision == "" {
			return laneErr(e, "role %q may write it only when applying a decision (data.by_decision)", e.Actor.Role)
		}
	}
	if len(lane.Subjects) > 0 && !slices.Contains(lane.Subjects, e.Subject.Kind) {
		return laneErr(e, "subject kind %q not allowed (want %v)", e.Subject.Kind, lane.Subjects)
	}
	if lane.ClaimingSession {
		var d event.RoleClaimed // RoleReleased has the same fields
		if err := json.Unmarshal(orEmptyJSON(e.Data), &d); err != nil {
			return laneErr(e, "%v", err)
		}
		if d.Session != e.Actor.ID {
			return laneErr(e, "a session claims or releases a role only for itself (data.session %q)", d.Session)
		}
	}
	return nil
}

func orEmptyJSON(b []byte) []byte {
	if len(b) == 0 || string(b) == "null" {
		return []byte("{}")
	}
	return b
}

// SessionTasks returns the ids of the session's own tasks (C-event-log v3
// §3): tasks whose TaskDrafted.session names it, and tasks whose latest
// TaskSessionAttached names it.
func (s *State) SessionTasks(session string) []string {
	var out []string
	if session == "" {
		return out
	}
	for _, id := range sortedKeys(s.Tasks) {
		t := s.Tasks[id]
		if t.Session == session || t.DraftSession == session {
			out = append(out, id)
		}
	}
	return out
}

// CheckLaneIn is CheckLane plus the rules that need state (C-event-log v4 §3):
//   - TaskHandoff and DecisionRaised by a session or integrator: the subject
//     task (for a handoff) or raised_by.task (for a decision) must be the
//     actor's own task (SessionTasks), and raised_by.session must be the
//     actor. Such a decision cannot have raised_by.task null. The lead,
//     orchestrator and loop may raise with any raised_by (D-lane-gaps).
//   - QuestionAnswered: only by the question's addressee (see Addressee).
//   - RouteResolved: only by the route's owner (see RouteOwner), matched by
//     role name or session name. A route the loop has not written yet
//     counts, so an owner can resolve it early.
func CheckLaneIn(s *State, e event.Event) error {
	if err := CheckLane(e); err != nil {
		return err
	}
	lane := Lanes[e.Type]
	p, err := e.Payload()
	if err != nil {
		return laneErr(e, "%v", err)
	}
	if lane.OwnTask && slices.Contains(taskSessions, e.Actor.Role) {
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
			return laneErr(e, "task %q is not the session's own task (own: %v)", task, own)
		}
	}
	if slices.Contains(lane.ByDecision, e.Actor.Role) {
		if err := checkApplyAmendment(s, e, p.(*event.TaskAmended)); err != nil {
			return err
		}
	}
	if lane.Addressee {
		d := p.(*event.QuestionAnswered)
		q, ok := s.FindQuestion(e.Subject, d.QuestionID)
		if !ok {
			return laneErr(e, "no question %q on %s %s", d.QuestionID, e.Subject.Kind, e.Subject.ID)
		}
		to := s.Addressee(e.Subject, q)
		if !ActorIs(e.Actor, to) {
			return laneErr(e, "only the addressee %q may answer question %q", to, d.QuestionID)
		}
	}
	if lane.RouteOwner {
		d := p.(*event.RouteResolved)
		to, ok := s.RouteOwner(d.RouteID)
		if !ok {
			return laneErr(e, "no route %q", d.RouteID)
		}
		if !ActorIs(e.Actor, to) {
			return laneErr(e, "route %q is routed to %q", d.RouteID, to)
		}
	}
	return nil
}

// checkApplyAmendment is the state part of the loop's TaskAmended lane
// (C-event-log v5 §3, D-apply-amend-lane): by_decision must name a decided
// or defaulted decision that gates the amended task, i.e. lists it in
// `blocks`, or is listed in the task's blocked_by. A loop actor cannot
// amend an unrelated task.
func checkApplyAmendment(s *State, e event.Event, d *event.TaskAmended) error {
	did := s.Resolve(d.ByDecision)
	dec, ok := s.Decisions[did]
	if !ok || len(dec.Raises) == 0 {
		return laneErr(e, "by_decision %q names no decision", d.ByDecision)
	}
	if dec.DecidedEvent == "" {
		return laneErr(e, "by_decision %s is %s; only a decided or defaulted decision is applied", did, dec.Status)
	}
	task := s.Resolve(e.Subject.ID)
	gates := slices.ContainsFunc(dec.Blocks, func(b string) bool { return s.Resolve(b) == task })
	if t, ok := s.Tasks[task]; ok && !gates {
		gates = slices.ContainsFunc(t.BlockedBy, func(b string) bool { return s.Resolve(b) == did })
	}
	if !gates {
		return laneErr(e, "by_decision %s does not block task %s", did, task)
	}
	return nil
}

// ActorIs reports whether an actor is the owner named by to: a role name or
// a session name (C-event-log §4). An empty to matches nobody.
func ActorIs(a event.Actor, to string) bool {
	return to != "" && (string(a.Role) == to || a.ID == to)
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
