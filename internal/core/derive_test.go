package core

import (
	"slices"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Spine §3: task status is derived from events plus observations.
func TestDeriveTaskStatus_Spine3(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-draft")
	b.draftTask("T-ready")
	b.approve("T-ready", "s")
	b.add(event.TaskDraftedType, event.RoleOrchestrator, "orch", event.KindTask, "T-nobranch", event.TaskDrafted{Title: "x", BlockedBy: []string{}})
	b.approve("T-nobranch", "s")
	b.draftContract("C-open")
	b.draftContract("C-done")
	b.freeze("C-done", 1, "h")
	b.draftTask("T-by-contract", "C-open")
	b.approve("T-by-contract", "s")
	b.draftTask("T-by-frozen", "C-done")
	b.approve("T-by-frozen", "s")
	b.draftTask("T-by-task", "T-ready")
	b.approve("T-by-task", "s")
	b.draftTask("T-by-merged", "T-merged")
	b.approve("T-by-merged", "s")
	b.draftTask("T-merged")
	b.approve("T-merged", "s")
	b.draftTask("T-by-missing", "C-nothing")
	b.approve("T-by-missing", "s")
	b.draftTask("T-deferred")
	b.add(event.TaskDeferredType, event.RoleOrchestrator, "orch", event.KindTask, "T-deferred", event.TaskDeferred{})
	b.draftTask("T-review")
	b.approve("T-review", "s")
	b.draftTask("T-running")
	b.approve("T-running", "s")
	b.draftTask("T-draftpr")
	b.approve("T-draftpr", "s")
	b.draftTask("T-cycle-a", "T-cycle-b")
	b.approve("T-cycle-a", "s")
	b.draftTask("T-cycle-b", "T-cycle-a")
	b.approve("T-cycle-b", "s")

	// Decision gates: via blocks, via blocked_by, applied, superseded.
	b.draftTask("T-gated")
	b.approve("T-gated", "s")
	b.draftTask("T-gated-applied")
	b.approve("T-gated-applied", "s")
	b.draftTask("T-gated-superseded")
	b.approve("T-gated-superseded", "s")
	b.draftTask("T-by-decision", "D-pending")
	b.approve("T-by-decision", "s")
	b.attach("T-ready", "s-ready")
	rec := record("D-pending", "T-ready", "s-ready", 2)
	rec.Blocks = []string{"T-gated"}
	b.raise(rec, "s-ready")
	rec = record("D-applied", "T-ready", "s-ready", 2)
	rec.Blocks = []string{"T-gated-applied"}
	b.raise(rec, "s-ready")
	b.decide("D-applied", "a")
	b.apply("D-applied")
	rec = record("D-super", "T-ready", "s-ready", 2)
	rec.Blocks = []string{"T-gated-superseded"}
	b.raise(rec, "s-ready")
	b.add(event.DecisionSupersededType, event.RoleLead, "lead", event.KindDecision, "D-super", event.DecisionSuperseded{By: "D-pending"})

	// External gate.
	b.draftTask("T-external")
	b.approve("T-external", "s")
	b.add(event.ExternalAddedType, event.RoleOrchestrator, "orch", event.KindExternal, "X-creds", event.ExternalAdded{Blocks: []string{"T-external"}})

	s := b.state()
	obs := Observed{Tasks: map[string]TaskObservation{
		"T-merged":   {PR: PRMerged},
		"T-review":   {PR: PROpen},
		"T-running":  {Session: SessionNeedsInput},
		"T-draftpr":  {PR: PRDraft},
		"T-deferred": {Session: SessionRunning},
	}}
	want := map[string]TaskStatus{
		"T-draft":            TaskDraft,
		"T-ready":            TaskReady,
		"T-nobranch":         TaskApproved,
		"T-by-contract":      TaskBlocked,
		"T-by-frozen":        TaskReady,
		"T-by-task":          TaskBlocked,
		"T-by-merged":        TaskReady,
		"T-merged":           TaskDone,
		"T-by-missing":       TaskBlocked,
		"T-deferred":         TaskDeferred,
		"T-review":           TaskInReview,
		"T-running":          TaskRunning,
		"T-draftpr":          TaskRunning,
		"T-cycle-a":          TaskBlocked,
		"T-cycle-b":          TaskBlocked,
		"T-gated":            TaskBlocked,
		"T-gated-applied":    TaskReady,
		"T-gated-superseded": TaskReady,
		"T-by-decision":      TaskBlocked,
		"T-external":         TaskBlocked,
	}
	got := TaskStatuses(&s, obs)
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: status %s, want %s (open blockers %v)", id, got[id], w, OpenBlockers(&s, obs, id))
		}
	}
	if DeriveTaskStatus(&s, obs, "T-nope") != "" {
		t.Error("unknown task should have no status")
	}
	if ob := OpenBlockers(&s, obs, "T-gated"); !slices.Equal(ob, []string{"D-pending"}) {
		t.Errorf("open blockers = %v", ob)
	}
}

// Spine §3: decision priority = tasks blocked transitively × urgency.
func TestUrgency_Spine3Priority(t *testing.T) {
	now := time.Date(2026, 11, 20, 23, 59, 0, 0, time.UTC)
	for deadline, want := range map[string]int{
		"": 1, "2026-12-01": 1, "2026-11-28": 1, "2026-11-27": 2, "2026-11-24": 2,
		"2026-11-23": 3, "2026-11-22": 3, "2026-11-21": 4, "2026-11-20": 5, "2026-11-19": 6, "not-a-date": 1,
	} {
		var d *string
		if deadline != "" {
			d = ptr(deadline)
		}
		if got := Urgency(d, now); got != want {
			t.Errorf("Urgency(%q) = %d, want %d", deadline, got, want)
		}
	}
}

func priorityLog(t *testing.T) *logBuilder {
	b := newLog(t)
	b.draftContract("C-api")
	b.draftTask("T-raiser")
	b.attach("T-raiser", "s1")
	b.draftTask("T-a")
	b.draftTask("T-b", "C-api")  // through the contract
	b.draftTask("T-c", "T-a")    // transitively through T-a
	b.draftTask("T-d", "T-c")    // two hops
	b.draftTask("T-e", "D-wide") // names the decision in blocked_by
	b.draftTask("T-f", "T-a")    // deferred: not counted
	b.draftTask("T-unrelated")
	b.add(event.TaskDeferredType, event.RoleOrchestrator, "orch", event.KindTask, "T-f", event.TaskDeferred{})
	wide := record("D-wide", "T-raiser", "s1", 2)
	wide.Blocks, wide.ContractImpact, wide.Deadline = []string{"T-a"}, []string{"C-api"}, ptr("2026-11-21")
	b.raise(wide, "s1")
	narrow := record("D-narrow", "T-raiser", "s1", 1)
	narrow.Blocks, narrow.Deadline = []string{"T-unrelated"}, ptr("2026-11-20")
	b.raise(narrow, "s1")
	none := record("D-none", "T-raiser", "s1", 2)
	b.raise(none, "s1")
	late := record("D-late", "T-raiser", "s1", 2)
	b.raise(late, "s1")
	b.raise(record("D-done", "T-raiser", "s1", 2), "s1")
	b.decide("D-done", "a")
	return b
}

func TestPriority_Spine3Priority(t *testing.T) {
	s := priorityLog(t).state()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	blocked := BlockedByDecision(&s, "D-wide")
	if want := []string{"T-a", "T-b", "T-c", "T-d", "T-e"}; !slices.Equal(blocked, want) {
		t.Errorf("BlockedByDecision = %v, want %v", blocked, want)
	}
	if p := Priority(&s, "D-wide", now); p != 5*4 {
		t.Errorf("Priority(D-wide) = %d, want 20", p)
	}
	if p := Priority(&s, "D-narrow", now); p != 1*5 {
		t.Errorf("Priority(D-narrow) = %d, want 5", p)
	}
	if p := Priority(&s, "D-none", now); p != 0 {
		t.Errorf("Priority(D-none) = %d, want 0", p)
	}
	if Priority(&s, "D-nope", now) != 0 || BlockedByDecision(&s, "D-nope") != nil {
		t.Error("unknown decision")
	}
}

// C-event-log §4: the lead's inbox ranks pending decisions by priority.
func TestInbox_EventLog4(t *testing.T) {
	b := priorityLog(t)
	b.add(event.RoutedToOwnerType, event.RoleLoop, "loop", event.KindTask, "T-a", event.RoutedToOwner{RouteID: "r-lead", To: "lead"})
	b.add(event.RoutedToOwnerType, event.RoleLoop, "loop", event.KindTask, "T-a", event.RoutedToOwner{RouteID: "r-orch-2", To: "orchestrator"})
	b.add(event.RoutedToOwnerType, event.RoleLoop, "loop", event.KindTask, "T-a", event.RoutedToOwner{RouteID: "r-orch-1", To: "orchestrator"})
	b.add(event.RoutedToOwnerType, event.RoleLoop, "loop", event.KindTask, "T-a", event.RoutedToOwner{RouteID: "r-closed", To: "orchestrator"})
	b.add(event.RouteResolvedType, event.RoleOrchestrator, "orch", event.KindTask, "T-a", event.RouteResolved{RouteID: "r-closed"})
	s := b.state()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	var got []string
	for _, it := range Inbox(&s, "lead", now) {
		got = append(got, it.Kind+":"+it.ID)
	}
	// D-none and D-late tie at 0 with no deadline: by id.
	want := []string{"decision:D-wide", "decision:D-narrow", "decision:D-late", "decision:D-none", "route:r-lead"}
	if !slices.Equal(got, want) {
		t.Errorf("lead inbox = %v, want %v", got, want)
	}
	got = nil
	for _, it := range Inbox(&s, "orchestrator", now) {
		got = append(got, it.ID)
	}
	if want := []string{"r-orch-2", "r-orch-1"}; !slices.Equal(got, want) {
		t.Errorf("orchestrator inbox = %v, want %v (oldest first, closed excluded)", got, want)
	}
}

func TestPendingDecisionsDeadlineTiebreak(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-r")
	b.attach("T-r", "s")
	for _, x := range []struct{ id, deadline string }{{"D-c", ""}, {"D-b", "2026-12-30"}, {"D-a", "2026-12-31"}} {
		r := record(x.id, "T-r", "s", 2)
		if x.deadline != "" {
			r.Deadline = ptr(x.deadline)
		}
		b.raise(r, "s")
	}
	s := b.state()
	var got []string
	for _, r := range PendingDecisions(&s, t0) {
		got = append(got, r.ID)
	}
	if want := []string{"D-b", "D-a", "D-c"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// C-event-log §4: routing rules.
func TestRoute_EventLog4(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	b.attach("T-a", "s-a")
	b.draftTask("T-nosession")
	b.draftContract("C-x")
	b.raise(record("D-x", "T-a", "s-a", 2), "s-a")
	start := len(b.events)
	hFollow := b.add(event.TaskHandoffType, event.RoleSession, "s-a", event.KindTask, "T-a", event.TaskHandoff{HasFollowups: true})
	hDec := b.add(event.TaskHandoffType, event.RoleSession, "s-a", event.KindTask, "T-a", event.TaskHandoff{HasDecisions: true})
	hDev := b.add(event.TaskHandoffType, event.RoleSession, "s-a", event.KindTask, "T-a", event.TaskHandoff{HasDeviations: true})
	b.add(event.TaskHandoffType, event.RoleSession, "s-a", event.KindTask, "T-a", event.TaskHandoff{StatusLine: "done, nothing to route"})
	raised := b.raise(record("D-y", "T-a", "s-a", 2), "s-a")
	qTo := b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindTask, "T-a", event.QuestionAsked{QuestionID: "q1", To: "integrator"})
	qDec := b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindDecision, "D-x", event.QuestionAsked{QuestionID: "q2"})
	qTask := b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindTask, "T-a", event.QuestionAsked{QuestionID: "q3"})
	qContract := b.add(event.QuestionAskedType, event.RoleSession, "s-a", event.KindContract, "C-x", event.QuestionAsked{QuestionID: "q4"})
	qNoSession := b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindTask, "T-nosession", event.QuestionAsked{QuestionID: "q5"})
	qAnswered := b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindTask, "T-a", event.QuestionAsked{QuestionID: "q6", To: "s-a"})
	b.add(event.QuestionAnsweredType, event.RoleSession, "s-a", event.KindTask, "T-a", event.QuestionAnswered{QuestionID: "q6"})
	newEvents := b.events[start:]
	s := b.state()

	got := map[string]string{}
	for _, r := range Route(&s, newEvents) {
		got[r.FromEvent] = r.To
		if r.RouteID != RouteID(r.FromEvent) || r.Reason == "" {
			t.Errorf("route %+v", r)
		}
	}
	want := map[string]string{
		hFollow.ID: "orchestrator", hDec.ID: "orchestrator", hDev.ID: "orchestrator",
		qTo.ID: "integrator", qDec.ID: "s-a", qTask.ID: "s-a", qContract.ID: "orchestrator",
		qNoSession.ID: "orchestrator",
	}
	if len(got) != len(want) {
		t.Errorf("routed %d items, want %d: %v", len(got), len(want), got)
	}
	for id, to := range want {
		if got[id] != to {
			t.Errorf("event %s routed to %q, want %q", id, got[id], to)
		}
	}
	if _, ok := got[raised.ID]; ok {
		t.Error("DecisionRaised must not be routed; it is the lead's item itself")
	}
	if _, ok := got[qAnswered.ID]; ok {
		t.Error("answered question was routed")
	}

	// Idempotent: once the routes are written, routing again gives nothing.
	for _, r := range Route(&s, newEvents) {
		b.add(event.RoutedToOwnerType, event.RoleLoop, "loop", r.Subject.Kind, r.Subject.ID, r.RoutedToOwner)
	}
	s = b.state()
	if again := Route(&s, newEvents); len(again) != 0 {
		t.Errorf("second pass routed %v", again)
	}
	if n := len(Inbox(&s, "orchestrator", t0)); n != 5 {
		t.Errorf("orchestrator inbox has %d items, want 5", n)
	}
}

// C-event-log §5: one test per conflict rule.
func TestConflicts_EventLog5(t *testing.T) {
	t.Run("lane", func(t *testing.T) {
		b := newLog(t)
		b.draftTask("T-a")
		bad := b.add(event.TaskApprovedType, event.RoleOrchestrator, "orch", event.KindTask, "T-a", event.TaskApproved{})
		s := b.state()
		cs := Conflicts(&s, b.events, ConflictInputs{})
		if len(cs) != 1 || cs[0].Rule != RuleLane || cs[0].Events[0] != bad.ID {
			t.Errorf("conflicts = %+v", cs)
		}
	})
	t.Run("role", func(t *testing.T) {
		b := newLog(t)
		c1 := b.add(event.RoleClaimedType, event.RoleSession, "o1", event.KindProject, "project", event.RoleClaimed{Role: "orchestrator", Session: "o1"})
		c2 := b.add(event.RoleClaimedType, event.RoleSession, "o2", event.KindProject, "project", event.RoleClaimed{Role: "orchestrator", Session: "o2"})
		b.add(event.RoleClaimedType, event.RoleSession, "i1", event.KindProject, "project", event.RoleClaimed{Role: "integrator", Session: "i1"})
		s := b.state()
		cs := Conflicts(&s, b.events, ConflictInputs{})
		if len(cs) != 1 || cs[0].Rule != RuleRole || !slices.Equal(cs[0].Events, []string{c1.ID, c2.ID}) {
			t.Errorf("conflicts = %+v", cs)
		}
		if cs := Conflicts(&s, b.events, ConflictInputs{AliveSessions: map[string]bool{"o2": true, "i1": true}}); len(cs) != 0 {
			t.Errorf("dead claim counted: %+v", cs)
		}
		b.add(event.RoleReleasedType, event.RoleSession, "o1", event.KindProject, "project", event.RoleReleased{Role: "orchestrator", Session: "o1"})
		s = b.state()
		if cs := Conflicts(&s, b.events, ConflictInputs{}); len(cs) != 0 {
			t.Errorf("released claim counted: %+v", cs)
		}
	})
	t.Run("decided-twice", func(t *testing.T) {
		b := newLog(t)
		b.draftTask("T-a")
		b.attach("T-a", "s")
		b.raise(record("D-x", "T-a", "s", 2), "s")
		d1 := b.decide("D-x", "a")
		b.decide("D-x", "a") // same answer: not a conflict
		d3 := b.decide("D-x", "b")
		s := b.state()
		cs := Conflicts(&s, b.events, ConflictInputs{})
		if len(cs) != 1 || cs[0].Rule != RuleDecidedTwice || !slices.Contains(cs[0].Events, d1.ID) || !slices.Contains(cs[0].Events, d3.ID) {
			t.Errorf("conflicts = %+v", cs)
		}
		b2 := newLog(t)
		b2.draftTask("T-a")
		b2.attach("T-a", "s")
		b2.raise(record("D-x", "T-a", "s", 2), "s")
		b2.add(event.DecisionDecidedType, event.RoleLead, "lead", event.KindDecision, "D-x", event.DecisionDecided{Answer: "other", Note: "x"})
		b2.add(event.DecisionDecidedType, event.RoleLead, "lead", event.KindDecision, "D-x", event.DecisionDecided{Answer: "other", Note: "y"})
		s = b2.state()
		if cs := Conflicts(&s, b2.events, ConflictInputs{}); len(cs) != 1 {
			t.Errorf("two different 'other' answers: %+v", cs)
		}
	})
	t.Run("frozen-twice", func(t *testing.T) {
		b := newLog(t)
		b.draftContract("C-a")
		f1 := b.freeze("C-a", 1, "h1")
		f2 := b.freeze("C-a", 1, "h1") // same text as f1, but both contradict f3
		f3 := b.freeze("C-a", 1, "h2")
		b.freeze("C-a", 2, "h3") // other version: fine
		s := b.state()
		cs := Conflicts(&s, b.events, ConflictInputs{})
		if len(cs) != 1 || cs[0].Rule != RuleFrozenTwice || !slices.Equal(cs[0].Events, []string{f1.ID, f2.ID, f3.ID}) {
			t.Errorf("conflicts = %+v", cs)
		}
	})
	t.Run("frozen-text-changed", func(t *testing.T) {
		b := newLog(t)
		b.draftTask("T-a")
		ap := b.approve("T-a", "brief-v1")
		b.draftContract("C-a")
		fr := b.freeze("C-a", 1, "c-v1")
		b.draftTask("T-unapproved")
		s := b.state()
		same := map[string]string{".ekumen/briefs/T-a/brief.md": "brief-v1", ".ekumen/contracts/C-a.md": "c-v1",
			".ekumen/briefs/T-unapproved/brief.md": "whatever"}
		if cs := Conflicts(&s, b.events, ConflictInputs{TextSHA256: same}); len(cs) != 0 {
			t.Errorf("unchanged text: %+v", cs)
		}
		if cs := Conflicts(&s, b.events, ConflictInputs{}); len(cs) != 0 {
			t.Errorf("no hashes given: %+v", cs)
		}
		changed := map[string]string{".ekumen/briefs/T-a/brief.md": "brief-v2", ".ekumen/contracts/C-a.md": "c-v2"}
		cs := Conflicts(&s, b.events, ConflictInputs{TextSHA256: changed})
		if len(cs) != 2 || cs[0].Events[0] != ap.ID || cs[1].Events[0] != fr.ID {
			t.Errorf("conflicts = %+v", cs)
		}
	})
	t.Run("id-collision", func(t *testing.T) {
		b := newLog(t)
		t1 := b.draftTask("T-a")
		t2 := b.add(event.TaskDraftedType, event.RoleOrchestrator, "orch-other-branch", event.KindTask, "T-a", event.TaskDrafted{Title: "other"})
		b.draftTask("T-b")
		b.draftTask("T-b") // same writer: not a collision
		b.attach("T-a", "s")
		b.attach("T-b", "s2")
		d1 := b.raise(record("D-x", "T-a", "s", 2), "s")
		d2 := b.raise(record("D-x", "T-b", "s2", 2), "s2")
		s := b.state()
		if s.Tasks["T-a"].Title != "T-a" {
			t.Error("first draft should win")
		}
		var got []Conflict
		for _, c := range Conflicts(&s, b.events, ConflictInputs{}) {
			if c.Rule == RuleIDCollision {
				got = append(got, c)
			}
		}
		if len(got) != 2 || !slices.Equal(got[0].Events, []string{d1.ID, d2.ID}) || !slices.Equal(got[1].Events, []string{t1.ID, t2.ID}) {
			t.Errorf("conflicts = %+v", got)
		}
	})
	t.Run("already recorded conflicts are not reported again", func(t *testing.T) {
		b := newLog(t)
		b.draftContract("C-a")
		f1 := b.freeze("C-a", 1, "h1")
		f2 := b.freeze("C-a", 1, "h2")
		s := b.state()
		cs := Conflicts(&s, b.events, ConflictInputs{})
		b.add(event.ConflictDetectedType, event.RoleLoop, "loop", event.KindProject, "project",
			event.ConflictDetected{Rule: cs[0].Rule, Events: []string{f2.ID, f1.ID}})
		s = b.state()
		if cs := Conflicts(&s, b.events, ConflictInputs{}); len(cs) != 0 {
			t.Errorf("re-reported: %+v", cs)
		}
	})
	t.Run("non-contradicting events are not conflicts", func(t *testing.T) {
		b := newLog(t)
		b.draftTask("T-a")
		b.attach("T-a", "s")
		b.raise(record("D-x", "T-a", "s", 2), "s")
		b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindDecision, "D-x", event.QuestionAsked{QuestionID: "q1"})
		b.add(event.QuestionAskedType, event.RoleOrchestrator, "orch", event.KindDecision, "D-x", event.QuestionAsked{QuestionID: "q2"})
		s := b.state()
		if cs := Conflicts(&s, b.events, ConflictInputs{}); len(cs) != 0 {
			t.Errorf("conflicts = %+v", cs)
		}
	})
}
