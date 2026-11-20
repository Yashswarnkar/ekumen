package core

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// C-event-log v5 §3: the lane table, written out as in the contract so a
// change to Lanes has to match it. The loop's TaskAmended needs
// data.by_decision, so with empty data it is refused here; see
// TestApplyAmendLane_DApplyAmendLane.
var contractLanes = map[event.Role][]event.Type{
	event.RoleLead: {event.TaskApprovedType, event.DecisionDecidedType, event.DecisionSupersededType, event.ContractFrozenType,
		event.DecisionRaisedType, event.SpineApprovedType},
	event.RoleOrchestrator: {event.TaskDraftedType, event.TaskAmendedType, event.TaskBlockersChangedType, event.TaskDeferredType,
		event.TaskSessionAttachedType, event.ContractDraftedType, event.ContractBlockersChangedType, event.ContractVersionedType,
		event.ExternalAddedType, event.ExternalUpdatedType, event.ExternalResolvedType, event.DecisionRaisedType},
	event.RoleSession:    {event.TaskHandoffType, event.DecisionRaisedType},
	event.RoleIntegrator: {event.TaskHandoffType, event.DecisionRaisedType},
	event.RoleLoop: {event.DecisionDefaultedType, event.DecisionAppliedType, event.TaskSessionAttachedType,
		event.RoutedToOwnerType, event.ConflictDetectedType, event.DecisionRaisedType},
}

// Types any role may write, subject to a state or data check: questions,
// route resolutions (the route's owner) and role claims (the claiming
// session).
var anyoneTypes = []event.Type{event.QuestionAskedType, event.QuestionAnsweredType, event.RouteResolvedType,
	event.RoleClaimedType, event.RoleReleasedType}

func subjectFor(typ event.Type) event.Subject {
	l := Lanes[typ]
	if len(l.Subjects) == 0 {
		return event.Subject{Kind: event.KindProject, ID: event.ProjectSubjectID}
	}
	k := l.Subjects[0]
	if k == event.KindProject {
		return event.Subject{Kind: k, ID: event.ProjectSubjectID}
	}
	return event.Subject{Kind: k, ID: map[event.SubjectKind]string{
		event.KindTask: "T-x", event.KindDecision: "D-x", event.KindContract: "C-x", event.KindExternal: "X-x"}[k]}
}

func TestLaneTable_EventLog3(t *testing.T) {
	for _, typ := range event.Types {
		if _, ok := Lanes[typ]; !ok {
			t.Errorf("%s has no lane", typ)
		}
	}
	for _, typ := range event.Types {
		for _, role := range event.Roles {
			want := slices.Contains(contractLanes[role], typ) || slices.Contains(anyoneTypes, typ)
			data := json.RawMessage(`{}`)
			if typ == event.RoleClaimedType || typ == event.RoleReleasedType {
				data = json.RawMessage(`{"role":"orchestrator","session":"x"}`)
			}
			e := event.Event{ID: "01JD8Z3K6Q4M2V7X9T1B5N0RCE", Type: typ, Actor: event.Actor{Role: role, ID: "x"},
				Subject: subjectFor(typ), Data: data}
			err := CheckLane(e)
			if (err == nil) != want {
				t.Errorf("CheckLane(%s by %s) = %v, want allowed=%v", typ, role, err, want)
			}
			if err != nil && !errors.Is(err, ErrLane) {
				t.Errorf("error does not wrap ErrLane: %v", err)
			}
		}
	}
}

// C-event-log v4 §3: RoleClaimed and RoleReleased "by the claiming session"
// (D-claim-lane).
func TestLaneClaimingSession_EventLog3(t *testing.T) {
	for _, tt := range []struct {
		actor, session string
		ok             bool
	}{
		{"orch-1", "orch-1", true},
		{"orch-1", "orch-2", false},
		{"loop", "orch-1", false},
	} {
		for _, typ := range []event.Type{event.RoleClaimedType, event.RoleReleasedType} {
			data, _ := json.Marshal(event.RoleClaimed{Role: event.RoleOrchestrator, Session: tt.session})
			e := event.Event{Type: typ, Actor: event.Actor{Role: event.RoleSession, ID: tt.actor},
				Subject: event.Subject{Kind: event.KindProject, ID: event.ProjectSubjectID}, Data: data}
			if err := CheckLane(e); (err == nil) != tt.ok {
				t.Errorf("%s by %s for %s: %v, want ok=%v", typ, tt.actor, tt.session, err, tt.ok)
			}
		}
	}
}

func TestLaneSubjectKinds_EventLog3(t *testing.T) {
	e := event.Event{Type: event.TaskApprovedType, Actor: event.Actor{Role: event.RoleLead, ID: "lead"},
		Subject: event.Subject{Kind: event.KindDecision, ID: "D-x"}}
	if CheckLane(e) == nil {
		t.Error("TaskApproved about a decision passed")
	}
	e = event.Event{Type: "FutureThing", Actor: event.Actor{Role: event.RoleLead, ID: "lead"}}
	if CheckLane(e) == nil {
		t.Error("unknown type passed")
	}
	for _, k := range []event.SubjectKind{event.KindTask, event.KindDecision, event.KindContract} {
		e := event.Event{Type: event.QuestionAskedType, Actor: event.Actor{Role: event.RoleSession, ID: "s"}, Subject: event.Subject{Kind: k}}
		if err := CheckLane(e); err != nil {
			t.Errorf("question on %s: %v", k, err)
		}
	}
	e = event.Event{Type: event.QuestionAskedType, Actor: event.Actor{Role: event.RoleSession, ID: "s"}, Subject: event.Subject{Kind: event.KindExternal}}
	if CheckLane(e) == nil {
		t.Error("question on an external passed")
	}
}

// C-event-log §3: "session (its own task only)".
func TestLaneOwnTask_EventLog3(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-core")
	b.draftTask("T-other")
	b.attach("T-core", "t-core")
	own := b.add(event.TaskHandoffType, event.RoleSession, "t-core", event.KindTask, "T-core", event.TaskHandoff{File: "h.md"})
	other := b.add(event.TaskHandoffType, event.RoleSession, "t-core", event.KindTask, "T-other", event.TaskHandoff{File: "h.md"})
	integ := b.add(event.TaskHandoffType, event.RoleIntegrator, "t-core", event.KindTask, "T-core", event.TaskHandoff{File: "h.md"})
	raiseOwn := b.raise(record("D-own", "T-core", "t-core", 2), "t-core")
	raiseOther := b.raise(record("D-other", "T-other", "t-core", 2), "t-core")
	projectRec := record("D-proj", "T-core", "t-core", 2)
	projectRec.RaisedBy.Task = nil
	raiseProject := b.raise(projectRec, "t-core")
	wrongSession := record("D-wrong", "T-core", "someone-else", 2)
	raiseWrong := b.raise(wrongSession, "t-core")
	unattached := b.add(event.TaskHandoffType, event.RoleSession, "ghost", event.KindTask, "T-other", event.TaskHandoff{File: "h.md"})
	s := b.state()

	for _, tt := range []struct {
		name string
		e    event.Event
		ok   bool
	}{
		{"handoff own task", own, true},
		{"handoff other task", other, false},
		{"integrator own task", integ, true},
		{"raise for own task", raiseOwn, true},
		{"raise for other task", raiseOther, false},
		{"session raise with null task", raiseProject, false},
		{"raised_by.session not the actor", raiseWrong, false},
		{"session with no attached task", unattached, false},
	} {
		if err := CheckLaneIn(&s, tt.e); (err == nil) != tt.ok {
			t.Errorf("%s: CheckLaneIn = %v, want ok=%v", tt.name, err, tt.ok)
		}
	}
}

// C-event-log §3: "QuestionAnswered only by the question's addressee".
func TestLaneAddressee_EventLog3(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-core")
	b.attach("T-core", "t-core")
	b.raise(record("D-x", "T-core", "t-core", 2), "t-core")
	b.draftContract("C-x")
	b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindDecision, "D-x", event.QuestionAsked{QuestionID: "q1", Text: "?"})
	b.add(event.QuestionAskedType, event.RoleSession, "t-core", event.KindDecision, "D-x", event.QuestionAsked{QuestionID: "q2", Text: "?", To: "lead"})
	b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindContract, "C-x", event.QuestionAsked{QuestionID: "q3", Text: "?"})
	b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindTask, "T-core", event.QuestionAsked{QuestionID: "q4", Text: "?"})
	answer := func(role event.Role, actor string, kind event.SubjectKind, subj, q string) event.Event {
		return b.add(event.QuestionAnsweredType, role, actor, kind, subj, event.QuestionAnswered{QuestionID: q, Text: "!"})
	}
	cases := []struct {
		name string
		e    event.Event
		ok   bool
	}{
		{"default on decision: raiser session", answer(event.RoleSession, "t-core", event.KindDecision, "D-x", "q1"), true},
		{"default on decision: not the raiser", answer(event.RoleSession, "other", event.KindDecision, "D-x", "q1"), false},
		{"explicit to a role", answer(event.RoleLead, "yash", event.KindDecision, "D-x", "q2"), true},
		{"explicit to a role, wrong role", answer(event.RoleOrchestrator, "orch", event.KindDecision, "D-x", "q2"), false},
		{"default on contract: orchestrator", answer(event.RoleOrchestrator, "orch", event.KindContract, "C-x", "q3"), true},
		{"default on task: its session", answer(event.RoleSession, "t-core", event.KindTask, "T-core", "q4"), true},
		{"no such question", answer(event.RoleLead, "lead", event.KindTask, "T-core", "q9"), false},
	}
	s := b.state()
	for _, tt := range cases {
		if err := CheckLaneIn(&s, tt.e); (err == nil) != tt.ok {
			t.Errorf("%s: CheckLaneIn = %v, want ok=%v", tt.name, err, tt.ok)
		}
	}
}

// C-event-log v3 §3: the lead, orchestrator and loop raise decisions with
// any raised_by, including task null (D-lane-gaps).
func TestLaneDecisionRaisers_EventLog3(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-core")
	rec := record("D-proj", "T-core", "orch", 2)
	rec.RaisedBy.Task = nil
	var evs []event.Event
	for _, role := range []event.Role{event.RoleLead, event.RoleOrchestrator, event.RoleLoop} {
		rec.ID = "D-by-" + string(role)
		evs = append(evs, b.add(event.DecisionRaisedType, role, "someone", event.KindDecision, rec.ID, rec))
	}
	s := b.state()
	for _, e := range evs {
		if err := CheckLaneIn(&s, e); err != nil {
			t.Errorf("%s: %v", e.Actor.Role, err)
		}
	}
}

// C-event-log v3 §3: "A session's own task is the task whose
// TaskDrafted.session names it, or the latest TaskSessionAttached for that
// session (written by the orchestrator or the loop)".
func TestLaneOwnTaskSources_EventLog3(t *testing.T) {
	b := newLog(t)
	b.add(event.TaskDraftedType, event.RoleOrchestrator, "orch", event.KindTask, "T-drafted", event.TaskDrafted{
		Title: "x", Kind: event.TaskKindBuild, BlockedBy: []string{}, TierGrants: []string{}, Session: "s-draft"})
	b.draftTask("T-attached")
	b.add(event.TaskSessionAttachedType, event.RoleOrchestrator, "orch", event.KindTask, "T-attached",
		event.TaskSessionAttached{Session: "s-att"})
	byDraft := b.add(event.TaskHandoffType, event.RoleSession, "s-draft", event.KindTask, "T-drafted", event.TaskHandoff{File: "h.md"})
	byAttach := b.add(event.TaskHandoffType, event.RoleSession, "s-att", event.KindTask, "T-attached", event.TaskHandoff{File: "h.md"})
	notOwn := b.add(event.TaskHandoffType, event.RoleSession, "s-att", event.KindTask, "T-drafted", event.TaskHandoff{File: "h.md"})
	s := b.state()
	if got := s.Tasks["T-drafted"].Session; got != "s-draft" {
		t.Errorf("T-drafted session = %q, want the drafted session", got)
	}
	for _, tt := range []struct {
		name string
		e    event.Event
		ok   bool
	}{{"TaskDrafted.session", byDraft, true}, {"TaskSessionAttached by orchestrator", byAttach, true}, {"other task", notOwn, false}} {
		if err := CheckLaneIn(&s, tt.e); (err == nil) != tt.ok {
			t.Errorf("%s: %v, want ok=%v", tt.name, err, tt.ok)
		}
	}
	// A later attachment wins for Session; the drafted session stays an owner.
	b.add(event.TaskSessionAttachedType, event.RoleLoop, "loop", event.KindTask, "T-drafted", event.TaskSessionAttached{Session: "s-new"})
	s = b.state()
	if got := s.Tasks["T-drafted"].Session; got != "s-new" {
		t.Errorf("session after attach = %q", got)
	}
	if own := s.SessionTasks("s-draft"); !slices.Equal(own, []string{"T-drafted"}) {
		t.Errorf("SessionTasks(s-draft) = %v", own)
	}
}

// C-event-log v3 §3: RouteResolved by "the route's owner", for routes
// written by the loop and for routes it has not written yet (D-lane-gaps).
func TestLaneRouteResolved_EventLog3(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-core")
	b.attach("T-core", "t-core")
	b.add(event.RoutedToOwnerType, event.RoleLoop, "loop", event.KindProject, "project", event.RoutedToOwner{RouteID: "r1", To: "orchestrator"})
	b.add(event.RoutedToOwnerType, event.RoleLoop, "loop", event.KindProject, "project", event.RoutedToOwner{RouteID: "r2", To: "t-core"})
	h := b.add(event.TaskHandoffType, event.RoleSession, "t-core", event.KindTask, "T-core", event.TaskHandoff{File: "h.md", HasFollowups: true})
	q := b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindTask, "T-core", event.QuestionAsked{QuestionID: "q1", Text: "?"})
	res := func(role event.Role, actor, route string) event.Event {
		return b.add(event.RouteResolvedType, role, actor, event.KindProject, "project", event.RouteResolved{RouteID: route})
	}
	cases := []struct {
		name string
		e    event.Event
		ok   bool
	}{
		{"orchestrator, own route", res(event.RoleOrchestrator, "orch", "r1"), true},
		{"orchestrator, a session's route", res(event.RoleOrchestrator, "orch", "r2"), false},
		{"session, own route", res(event.RoleSession, "t-core", "r2"), true},
		{"missing route", res(event.RoleOrchestrator, "orch", "r3"), false},
		{"unwritten handoff route, orchestrator", res(event.RoleOrchestrator, "orch", RouteID(h.ID)), true},
		{"unwritten handoff route, lead", res(event.RoleLead, "lead", RouteID(h.ID)), false},
		{"unwritten question route, task session", res(event.RoleSession, "t-core", RouteID(q.ID)), true},
		{"unwritten question route, orchestrator", res(event.RoleOrchestrator, "orch", RouteID(q.ID)), false},
	}
	s := b.state()
	for _, tt := range cases {
		if err := CheckLaneIn(&s, tt.e); (err == nil) != tt.ok {
			t.Errorf("%s: CheckLaneIn = %v, want ok=%v", tt.name, err, tt.ok)
		}
	}
}
