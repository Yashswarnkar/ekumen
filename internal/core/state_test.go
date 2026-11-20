package core

import (
	"encoding/json"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// C-decision-record §3: lifecycle transitions.
func TestDecisionLifecycle_DecisionRecord3(t *testing.T) {
	type step func(b *logBuilder)
	decide := func(a string) step { return func(b *logBuilder) { b.decide("D-x", a) } }
	dflt := func(b *logBuilder) {
		b.add(event.DecisionDefaultedType, event.RoleLoop, "loop", event.KindDecision, "D-x",
			event.DecisionDefaulted{Answer: "a", Deadline: "2026-11-27"})
	}
	supersede := func(b *logBuilder) {
		b.add(event.DecisionSupersededType, event.RoleLead, "lead", event.KindDecision, "D-x", event.DecisionSuperseded{By: "D-y"})
	}
	apply := func(b *logBuilder) { b.apply("D-x") }

	tests := []struct {
		name       string
		steps      []step
		status     string
		answer     string
		applied    bool
		superseded string
	}{
		{"raised is pending", nil, StatusPending, "", false, ""},
		{"decided", []step{decide("a")}, StatusDecided, "a", false, ""},
		{"defaulted", []step{dflt}, StatusDefaulted, "a", false, ""},
		{"decided then applied stays decided", []step{decide("b"), apply}, StatusDecided, "b", true, ""},
		{"defaulted then applied", []step{dflt, apply}, StatusDefaulted, "a", true, ""},
		{"pending is never applied", []step{apply}, StatusPending, "", false, ""},
		{"never reopened: first answer wins", []step{decide("a"), decide("b")}, StatusDecided, "a", false, ""},
		{"no default after a decision", []step{decide("b"), dflt}, StatusDecided, "b", false, ""},
		{"superseded", []step{supersede}, StatusSuperseded, "", false, "D-y"},
		{"superseded is never applied", []step{supersede, apply}, StatusSuperseded, "", false, "D-y"},
		{"decided can be superseded", []step{decide("a"), supersede}, StatusSuperseded, "a", false, "D-y"},
		{"apply is idempotent", []step{decide("a"), apply, apply}, StatusDecided, "a", true, ""},
	}
	for _, tt := range tests {
		b := newLog(t)
		b.draftTask("T-core")
		b.raise(record("D-x", "T-core", "t-core", 1), "t-core")
		for _, st := range tt.steps {
			st(b)
		}
		s := b.state()
		d := s.Decisions["D-x"]
		if d.Status != tt.status || d.Answer != tt.answer || (d.AppliedOn != "") != tt.applied || d.SupersededBy != tt.superseded {
			t.Errorf("%s: status=%s answer=%q applied=%q superseded=%q", tt.name, d.Status, d.Answer, d.AppliedOn, d.SupersededBy)
		}
		if tt.answer != "" && (d.DecidedOn != "2026-11-20" || d.DecidedBy == "") {
			t.Errorf("%s: decided_by=%q decided_on=%q", tt.name, d.DecidedBy, d.DecidedOn)
		}
	}
}

func TestReduceTaskEvents(t *testing.T) {
	b := newLog(t)
	b.add(event.TaskDraftedType, event.RoleOrchestrator, "orch", event.KindTask, "T-core", event.TaskDrafted{
		Title: "core", Aliases: []string{"T1"}, BlockedBy: []string{"C-a"}, TierGrants: []string{"x"}})
	b.lax = true
	b.approve("T1", "sha-a") // by alias: invalid to write, but readers resolve it
	b.lax = false
	b.approve("T-core", "sha-b")
	b.add(event.TaskBlockersChangedType, event.RoleOrchestrator, "orch", event.KindTask, "T-core",
		event.TaskBlockersChanged{Added: []string{"C-b", "C-a"}, Removed: []string{"C-a"}})
	b.add(event.TaskAmendedType, event.RoleOrchestrator, "orch", event.KindTask, "T-core", event.TaskAmended{File: "a.md", TextSHA256: "s"})
	b.attach("T-core", "t-core")
	b.attach("T-core", "t-core-2")
	b.add(event.TaskHandoffType, event.RoleSession, "t-core-2", event.KindTask, "T-core", event.TaskHandoff{File: "h.md", HasFollowups: true})
	b.add(event.TaskDeferredType, event.RoleOrchestrator, "orch", event.KindTask, "T-core", event.TaskDeferred{Reason: "later"})
	s := b.state()
	tk := s.Tasks["T-core"]
	if s.Resolve("T1") != "T-core" || !s.Has("T1") {
		t.Error("alias not resolved")
	}
	if !tk.Approved || tk.ApprovedSHA256 != "sha-a" {
		t.Errorf("first approval should win: %+v", tk)
	}
	if len(tk.BlockedBy) != 1 || tk.BlockedBy[0] != "C-b" {
		t.Errorf("blocked_by = %v", tk.BlockedBy)
	}
	if tk.Session != "t-core-2" || len(tk.Amendments) != 1 || len(tk.Handoffs) != 1 || !tk.Deferred {
		t.Errorf("task = %+v", tk)
	}
}

func TestReduceContractsExternalsRolesRoutes(t *testing.T) {
	b := newLog(t)
	b.draftContract("C-a")
	b.freeze("C-a", 1, "h1")
	b.add(event.ContractVersionedType, event.RoleOrchestrator, "orch", event.KindContract, "C-a",
		event.ContractVersioned{Version: 2, ByDecision: "D-x", TextSHA256: "h2"})
	b.draftContract("C-b")
	b.add(event.ExternalAddedType, event.RoleOrchestrator, "orch", event.KindExternal, "X-creds",
		event.ExternalAdded{What: "creds", OwnerTeam: "infra", Blocks: []string{"T-a"}})
	b.add(event.ExternalUpdatedType, event.RoleOrchestrator, "orch", event.KindExternal, "X-creds",
		event.ExternalUpdated{Fields: map[string]json.RawMessage{"needed_by": json.RawMessage(`"2026-12-01"`), "blocks": json.RawMessage(`["T-b"]`)}})
	b.add(event.RoleClaimedType, event.RoleLoop, "loop", event.KindProject, "project", event.RoleClaimed{Role: "orchestrator", Session: "o1"})
	b.add(event.RoleClaimedType, event.RoleLoop, "loop", event.KindProject, "project", event.RoleClaimed{Role: "integrator", Session: "i1"})
	b.add(event.RoleReleasedType, event.RoleLoop, "loop", event.KindProject, "project", event.RoleReleased{Role: "orchestrator", Session: "o1"})
	b.add(event.RoutedToOwnerType, event.RoleLoop, "loop", event.KindProject, "project", event.RoutedToOwner{RouteID: "r1", To: "orchestrator"})
	b.add(event.RoutedToOwnerType, event.RoleLoop, "loop", event.KindProject, "project", event.RoutedToOwner{RouteID: "r1", To: "lead"})
	b.add(event.ConflictDetectedType, event.RoleLoop, "loop", event.KindProject, "project", event.ConflictDetected{Rule: "role", Events: []string{"b", "a"}})
	s := b.state()
	// A ContractVersioned does not freeze by itself: it waits for a
	// ContractFrozen of that version (C-event-log v5, D-contract-version-recording).
	if c := s.Contracts["C-a"]; c.Status != ContractFrozen || c.Version != 1 || c.TextSHA256 != "h1" ||
		c.Pending == nil || c.Pending.Version != 2 || c.Pending.ByDecision != "D-x" {
		t.Errorf("C-a = %+v", c)
	}
	if s.Contracts["C-b"].Status != ContractDraft {
		t.Error("C-b should be draft")
	}
	if x := s.Externals["X-creds"]; x.NeededBy != "2026-12-01" || len(x.Blocks) != 1 || x.Blocks[0] != "T-b" {
		t.Errorf("external = %+v", x)
	}
	if len(s.Claims) != 1 || s.Claims[0].Role != "integrator" {
		t.Errorf("claims = %+v", s.Claims)
	}
	if r := s.Routes["r1"]; r.To != "orchestrator" || !r.Open {
		t.Errorf("route = %+v (a repeated route id must not change it)", r)
	}
	if c := s.Conflicts[0]; c.Events[0] != "a" {
		t.Errorf("conflict events not sorted: %v", c.Events)
	}
}

func TestReduceSkipsUnknownAndBadEvents_EventLog9(t *testing.T) {
	b := newLog(t)
	e := b.draftTask("T-a")
	unknown := e
	unknown.ID, unknown.Type = "01JD8Z3K6Q4M2V7X9T1B5N0R99", "FutureThing"
	bad := e
	bad.ID, bad.Data = "01JD8Z3K6Q4M2V7X9T1B5N0R98", []byte(`{"title":5}`)
	onExternal := b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindProject, "project", event.QuestionAsked{QuestionID: "q"})
	s := Reduce(append(b.events, unknown, bad))
	if len(s.Skipped) != 3 {
		t.Fatalf("skipped = %+v", s.Skipped)
	}
	_ = onExternal
	if s.Tasks["T-a"].Title != "T-a" {
		t.Error("good event lost")
	}
}

func TestQuestionsAnsweredAndRoutesClosed(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	q := b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindTask, "T-a", event.QuestionAsked{QuestionID: "q1", Text: "?", To: "s1"})
	b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindTask, "T-a", event.QuestionAsked{QuestionID: "q1", Text: "dup"})
	b.add(event.RoutedToOwnerType, event.RoleLoop, "loop", event.KindTask, "T-a", event.RoutedToOwner{RouteID: RouteID(q.ID), To: "s1", FromEvent: q.ID})
	b.add(event.QuestionAnsweredType, event.RoleSession, "s1", event.KindTask, "T-a", event.QuestionAnswered{QuestionID: "q1", Text: "yes"})
	b.add(event.QuestionAnsweredType, event.RoleSession, "s1", event.KindTask, "T-a", event.QuestionAnswered{QuestionID: "q1", Text: "no"})
	s := b.state()
	qs := s.Tasks["T-a"].Questions
	if len(qs) != 1 || qs[0].Answer != "yes" || qs[0].Text != "?" {
		t.Errorf("questions = %+v", qs)
	}
	if r := s.Routes[RouteID(q.ID)]; r.Open || r.ClosedBy == "" {
		t.Errorf("route not closed by the answer: %+v", r)
	}
}
