package core

import (
	"slices"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// sameInstant builds events that all share one timestamp, with ULID
// entropy chosen so that id order is the reverse of the lifecycle.
type sameInstant struct {
	t      *testing.T
	n      byte
	events []event.Event
}

func (b *sameInstant) add(typ event.Type, role event.Role, actor string, kind event.SubjectKind, subject string, data any) event.Event {
	b.t.Helper()
	b.n++
	var ent [10]byte
	ent[0] = 0xff - b.n // later in the lifecycle → smaller id
	id, err := event.ULIDFromParts(uint64(t0.UnixMilli()), ent)
	if err != nil {
		b.t.Fatal(err)
	}
	e, err := event.New(id, typ, t0, event.Actor{Role: role, ID: actor}, event.Subject{Kind: kind, ID: subject}, data)
	if err != nil {
		b.t.Fatal(err)
	}
	b.events = append(b.events, e)
	return e
}

// C-event-log v4 §2: events that share a timestamp are applied in
// lifecycle order (drafted/raised, approved/frozen/asked,
// amended/answered/decided/..., applied), whatever their ids.
func TestSameTimestampLifecycleOrder_EventLog2(t *testing.T) {
	b := &sameInstant{t: t}
	b.add(event.TaskDraftedType, event.RoleOrchestrator, "orch", event.KindTask, "T-a", event.TaskDrafted{
		Title: "a", Kind: event.TaskKindBuild, Branch: "b", Worktree: "w", BlockedBy: []string{"D-x"}, TierGrants: []string{}})
	b.add(event.TaskApprovedType, event.RoleLead, "lead", event.KindTask, "T-a", event.TaskApproved{TierGrants: []string{"g"}})
	b.add(event.DecisionRaisedType, event.RoleOrchestrator, "orch", event.KindDecision, "D-x", record("D-x", "T-a", "orch", 2))
	b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindDecision, "D-x", event.QuestionAsked{QuestionID: "q1", Text: "?", To: "orch"})
	b.add(event.QuestionAnsweredType, event.RoleOrchestrator, "orch", event.KindDecision, "D-x", event.QuestionAnswered{QuestionID: "q1", Text: "!"})
	b.add(event.DecisionDecidedType, event.RoleLead, "lead", event.KindDecision, "D-x", event.DecisionDecided{Answer: "a"})
	b.add(event.DecisionAppliedType, event.RoleLoop, "loop", event.KindDecision, "D-x", event.DecisionApplied{})
	b.add(event.ContractDraftedType, event.RoleOrchestrator, "orch", event.KindContract, "C-x", event.ContractDrafted{Title: "x"})
	b.add(event.ContractFrozenType, event.RoleLead, "lead", event.KindContract, "C-x", event.ContractFrozen{Version: 1})

	// The ids alone would apply everything backwards.
	byID := slices.Clone(b.events)
	slices.SortFunc(byID, func(x, y event.Event) int { return compareStrings(x.ID, y.ID) })
	if byID[0].Type != event.ContractFrozenType {
		t.Fatalf("fixture: ids should sort against the lifecycle, first is %s", byID[0].Type)
	}

	s := Reduce(b.events)
	task := s.Tasks["T-a"]
	if !task.Approved || task.Title != "a" || !slices.Equal(task.TierGrants, []string{"g"}) {
		t.Errorf("task: approved=%v title=%q grants=%v", task.Approved, task.Title, task.TierGrants)
	}
	d := s.Decisions["D-x"]
	if d.Status != StatusDecided || d.AppliedOn == "" || d.Title == "" {
		t.Errorf("decision: status=%s applied_on=%q title=%q", d.Status, d.AppliedOn, d.Title)
	}
	if len(d.Questions) != 1 || !d.Questions[0].Answered() {
		t.Errorf("question not answered: %+v", d.Questions)
	}
	if c := s.Contracts["C-x"]; c.Status != ContractFrozen || c.Title != "x" {
		t.Errorf("contract: %+v", c)
	}
	if got := DeriveTaskStatus(&s, Observed{}, "T-a"); got != TaskReady {
		t.Errorf("status = %s, want ready", got)
	}

	// Different milliseconds still order by time first.
	later := event.Event{ID: mustULID(t, t0.Add(time.Millisecond), 0), Type: event.TaskDraftedType}
	earlier := event.Event{ID: mustULID(t, t0, 0xff), Type: event.DecisionAppliedType}
	if got := SortEvents([]event.Event{later, earlier}); got[0].ID != earlier.ID {
		t.Error("rank overrode time order")
	}
	for _, typ := range event.Types {
		if LifecycleRank(typ) > 3 {
			t.Errorf("%s has no lifecycle rank", typ)
		}
	}
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func mustULID(t *testing.T, at time.Time, first byte) string {
	t.Helper()
	var ent [10]byte
	ent[0] = first
	id, err := event.ULIDFromParts(uint64(at.UnixMilli()), ent)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// C-event-log v3 §4: a RouteResolved written before the loop's
// RoutedToOwner is kept, and closes the route when it appears.
func TestEarlyRouteResolution_EventLog4(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-core")
	h := b.add(event.TaskHandoffType, event.RoleSession, "t-core", event.KindTask, "T-core", event.TaskHandoff{File: "h.md", HasFollowups: true})
	rid := RouteID(h.ID)
	res := b.add(event.RouteResolvedType, event.RoleOrchestrator, "orch", event.KindTask, "T-core", event.RouteResolved{RouteID: rid, Note: "done"})
	b.add(event.RouteResolvedType, event.RoleOrchestrator, "orch", event.KindTask, "T-core", event.RouteResolved{RouteID: rid, Note: "again"})
	s := b.state()
	if got := s.EarlyResolutions[rid]; got.Event != res.ID || got.Note != "done" {
		t.Fatalf("early resolution = %+v", got)
	}
	if len(Inbox(&s, "orchestrator", t0)) != 0 {
		t.Error("nothing is routed yet, inbox should be empty")
	}
	b.add(event.RoutedToOwnerType, event.RoleLoop, "loop", event.KindTask, "T-core",
		event.RoutedToOwner{RouteID: rid, To: "orchestrator", FromEvent: h.ID, Reason: ReasonHandoff})
	s = b.state()
	r := s.Routes[rid]
	if r == nil || r.Open || r.ClosedBy != res.ID || r.Note != "done" {
		t.Errorf("route = %+v, want closed by the early resolution", r)
	}
	if len(s.EarlyResolutions) != 0 {
		t.Errorf("early resolutions left: %v", s.EarlyResolutions)
	}
	if n := len(Inbox(&s, "orchestrator", t0)); n != 0 {
		t.Errorf("orchestrator inbox has %d items", n)
	}
}

// C-event-log v4 §6, D-contract-blockers: blocked_by[] on ContractDrafted
// and ContractBlockersChanged; a task blocked by a contract stays blocked
// while the contract has open blockers, and those blockers count in
// decision priority.
func TestContractBlockers_EventLog6(t *testing.T) {
	b := newLog(t)
	rec := record("D-gate", "T-x", "orch", 2)
	rec.RaisedBy.Task = nil
	b.add(event.DecisionRaisedType, event.RoleOrchestrator, "orch", event.KindDecision, "D-gate", rec)
	b.add(event.ContractDraftedType, event.RoleOrchestrator, "orch", event.KindContract, "C-api",
		event.ContractDrafted{Title: "api", Path: "contracts/C-api.md", BlockedBy: []string{"D-gate"}})
	b.freeze("C-api", 1, "sha")
	b.draftTask("T-impl", "C-api")
	b.approve("T-impl", "sha")
	b.draftTask("T-after", "T-impl")
	s := b.state()
	if got := s.Contracts["C-api"].BlockedBy; !slices.Equal(got, []string{"D-gate"}) {
		t.Errorf("contract blocked_by = %v", got)
	}
	if got := ContractOpenBlockers(&s, Observed{}, "C-api"); !slices.Equal(got, []string{"D-gate"}) {
		t.Errorf("ContractOpenBlockers = %v", got)
	}
	if got := DeriveTaskStatus(&s, Observed{}, "T-impl"); got != TaskBlocked {
		t.Errorf("frozen contract with an open blocker: task is %s, want blocked", got)
	}
	if got := BlockedByDecision(&s, "D-gate"); !slices.Equal(got, []string{"T-after", "T-impl"}) {
		t.Errorf("BlockedByDecision through the contract = %v", got)
	}
	if got := Priority(&s, "D-gate", t0); got != 2 {
		t.Errorf("priority = %d, want 2", got)
	}

	b.add(event.ContractBlockersChangedType, event.RoleOrchestrator, "orch", event.KindContract, "C-api",
		event.ContractBlockersChanged{Removed: []string{"D-gate"}, Added: []string{"X-vendor"}, Reason: "r"})
	s = b.state()
	if got := s.Contracts["C-api"].BlockedBy; !slices.Equal(got, []string{"X-vendor"}) {
		t.Errorf("after change blocked_by = %v", got)
	}
	if got := DeriveTaskStatus(&s, Observed{}, "T-impl"); got != TaskBlocked {
		t.Errorf("blocker naming nothing never clears: %s", got)
	}
	b.add(event.ContractBlockersChangedType, event.RoleOrchestrator, "orch", event.KindContract, "C-api",
		event.ContractBlockersChanged{Removed: []string{"X-vendor"}, Added: []string{"C-api"}, Reason: "cycle"})
	s = b.state()
	if got := DeriveTaskStatus(&s, Observed{}, "T-impl"); got != TaskBlocked {
		t.Errorf("a contract blocked by itself should stay blocked: %s", got)
	}
	b.add(event.ContractBlockersChangedType, event.RoleOrchestrator, "orch", event.KindContract, "C-api",
		event.ContractBlockersChanged{Removed: []string{"C-api"}, Reason: "clear"})
	s = b.state()
	if got := DeriveTaskStatus(&s, Observed{}, "T-impl"); got != TaskReady {
		t.Errorf("contract clear: task is %s, want ready", got)
	}
}
