package core

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Tests for T-log-fixes, one per decision (or audit finding) it implements.

// restamp gives an event a new id and time, as a backdated import or a
// skewed clock would.
func restamp(t *testing.T, e event.Event, at time.Time) event.Event {
	t.Helper()
	e.ID = mustULID(t, at, e.ID[len(e.ID)-1])
	e.At = at
	return e
}

func conflictRules(cs []Conflict) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Rule)
	}
	return out
}

// D-imported-event-time-order (causal-within-subject): an apply stamped
// earlier than its decide is applied; within a subject, lifecycle step
// beats time; across subjects, time order holds; project events keep time
// order.
func TestCausalOrderWithinSubject_DImportedEventTimeOrder(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	b.raise(record("D-x", "T-a", "s1", 2), "s1")
	decided := b.decide("D-x", "a")
	applied := b.apply("D-x")
	// The decide was imported "at noon"; the apply was written before noon.
	noon := t0.Add(3 * time.Hour)
	b.events[2] = restamp(t, decided, noon)
	b.events[3] = restamp(t, applied, t0.Add(time.Hour))

	s := b.state()
	if d := s.Decisions["D-x"]; d.Status != StatusDecided || d.AppliedOn == "" {
		t.Fatalf("apply stamped before its decide was dropped: status=%s applied_on=%q", d.Status, d.AppliedOn)
	}
	if got := OrphanStepConflicts(&s, b.events); len(got) != 0 {
		t.Errorf("orphan steps: %v", got)
	}

	// An approval stamped before its draft still approves.
	b2 := newLog(t)
	draft := b2.draftTask("T-b")
	b2.approve("T-b", "h")
	b2.events[0] = restamp(t, draft, t0.Add(time.Hour))
	if s := b2.state(); !s.Tasks["T-b"].Approved || s.Tasks["T-b"].Title != "T-b" {
		t.Errorf("approval before draft: %+v", s.Tasks["T-b"])
	}

	// Within one subject: step first, then time.
	sorted := SortEvents(b.events)
	var types []event.Type
	for _, e := range sorted {
		if e.Subject.ID == "D-x" {
			types = append(types, e.Type)
		}
	}
	if want := []event.Type{event.DecisionRaisedType, event.DecisionDecidedType, event.DecisionAppliedType}; !slices.Equal(types, want) {
		t.Errorf("D-x order %v, want %v", types, want)
	}

	// Project events (role claims) are not reordered by step: claim,
	// release, claim again leaves the role held.
	b3 := newLog(t)
	for _, typ := range []event.Type{event.RoleClaimedType, event.RoleReleasedType, event.RoleClaimedType} {
		b3.add(typ, event.RoleOrchestrator, "o1", event.KindProject, event.ProjectSubjectID,
			event.RoleClaimed{Role: event.RoleOrchestrator, Session: "o1"})
	}
	if s := b3.state(); len(s.Claims) != 1 {
		t.Errorf("claims after claim, release, claim: %+v", s.Claims)
	}

	// Any input order gives the same result.
	want := SortEvents(b.events)
	r := rand.New(rand.NewPCG(3, 4))
	for range 30 {
		sh := slices.Clone(b.events)
		r.Shuffle(len(sh), func(i, j int) { sh[i], sh[j] = sh[j], sh[i] })
		if got := SortEvents(sh); !reflect.DeepEqual(got, want) {
			t.Fatal("order depends on input order")
		}
	}
}

// D-imported-event-time-order (review finding 1): an event addressed to an
// alias is ordered after the event that creates the item and the alias, so
// an approval of T-old stamped before T-new (aliases [T-old]) was drafted
// approves T-new, and leaves no phantom task. An approval of an alias
// nobody declares is an orphan step.
func TestAliasCausalOrder_DImportedEventTimeOrder(t *testing.T) {
	b := newLog(t)
	draft := b.add(event.TaskDraftedType, event.RoleOrchestrator, "orch", event.KindTask, "T-new",
		event.TaskDrafted{Title: "new", Kind: event.TaskKindBuild, Aliases: []string{"T-old"}})
	approve := b.approve("T-old", "h")
	stray := b.approve("T-nobody", "h")
	b.events[0] = restamp(t, draft, t0.Add(10*time.Minute))
	b.events[1] = restamp(t, approve, t0.Add(5*time.Minute))
	s := b.state()
	if !s.Tasks["T-new"].Approved {
		t.Errorf("T-new not approved: %+v", s.Tasks["T-new"])
	}
	if _, phantom := s.Tasks["T-old"]; phantom {
		t.Error("a phantom T-old task was created")
	}
	got := OrphanStepConflicts(&s, b.events)
	if len(got) != 1 || got[0].Events[0] != stray.ID {
		t.Errorf("orphan steps %v, want only %s", got, stray.ID)
	}
}

// Review finding 4: routing and conflict events are never orphan steps.
func TestOrphanStepExemptions_DImportedEventTimeOrder(t *testing.T) {
	b := newLog(t)
	b.add(event.RoutedToOwnerType, event.RoleLoop, "loop", event.KindTask, "T-ghost", event.RoutedToOwner{RouteID: "r1", To: "orchestrator"})
	b.add(event.RouteResolvedType, event.RoleOrchestrator, "o", event.KindTask, "T-ghost", event.RouteResolved{RouteID: "r1", Note: "x"})
	b.add(event.ConflictDetectedType, event.RoleLoop, "loop", event.KindDecision, "D-ghost", event.ConflictDetected{Rule: "lane"})
	s := b.state()
	if got := OrphanStepConflicts(&s, b.events); len(got) != 0 {
		t.Errorf("orphan steps: %v", got)
	}
}

// D-imported-event-time-order (re-review finding 1): a later step moves only
// after its own prerequisite; otherwise time order holds. decide → apply →
// supersede keeps the applied info, even if the apply was stamped before the
// decide.
func TestDecideApplySupersede_DImportedEventTimeOrder(t *testing.T) {
	for _, applyFirst := range []bool{false, true} {
		b := newLog(t)
		b.raise(record("D-x", "T-a", "s1", 2), "s1")
		decided := b.decide("D-x", "a")
		applied := b.apply("D-x")
		b.add(event.DecisionSupersededType, event.RoleLead, "lead", event.KindDecision, "D-x", event.DecisionSuperseded{By: "D-y"})
		if applyFirst { // apply stamped before its decide; supersede still last
			b.events[1] = restamp(t, decided, t0.Add(3*time.Minute+30*time.Second))
			b.events[2] = restamp(t, applied, t0.Add(2*time.Minute+30*time.Second))
		}
		s := b.state()
		d := s.Decisions["D-x"]
		if d.Status != StatusSuperseded || d.AppliedOn == "" || d.Applied == nil {
			t.Errorf("applyFirst=%v: status=%s applied_on=%q", applyFirst, d.Status, d.AppliedOn)
		}
		var types []event.Type
		for _, e := range SortEvents(b.events) {
			types = append(types, e.Type)
		}
		want := []event.Type{event.DecisionRaisedType, event.DecisionDecidedType, event.DecisionAppliedType, event.DecisionSupersededType}
		if !slices.Equal(types, want) {
			t.Errorf("applyFirst=%v: order %v, want %v", applyFirst, types, want)
		}
	}
	// Aliases are per kind (re-review finding 6): task T-new and decision
	// D-new both declare alias OLD; a decide addressed to decision OLD,
	// stamped before D-new was raised, still follows D-new's raise.
	b := newLog(t)
	b.lax = true
	b.add(event.TaskDraftedType, event.RoleOrchestrator, "orch", event.KindTask, "T-new",
		event.TaskDrafted{Title: "t", Kind: event.TaskKindBuild, Aliases: []string{"OLD"}})
	dec := b.decide("OLD", "a")
	rec := record("D-new", "T-new", "s1", 2)
	rec.Aliases = []string{"OLD"}
	raise := b.raise(rec, "s1")
	var pos = map[string]int{}
	for i, e := range SortEvents(b.events) {
		pos[e.ID] = i
	}
	if pos[dec.ID] < pos[raise.ID] {
		t.Error("a decision event addressed to an alias was grouped under the task's alias")
	}
}

// D-imported-event-time-order: a step whose prerequisite never exists is
// reported by rule orphan-step, never silently dropped.
func TestOrphanStep_DImportedEventTimeOrder(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	b.raise(record("D-x", "T-a", "s1", 2), "s1")
	applyNoDecide := b.apply("D-x")
	decideNoRaise := b.decide("D-ghost", "a")
	approveNoDraft := b.approve("T-ghost", "h")
	answerNoAsk := b.add(event.QuestionAnsweredType, event.RoleLead, "lead", event.KindTask, "T-a",
		event.QuestionAnswered{QuestionID: "q-none", Text: "?"})
	s := b.state()
	if s.Decisions["D-x"].AppliedOn != "" {
		t.Error("an apply with no decide took effect")
	}
	got := OrphanStepConflicts(&s, b.events)
	var evs []string
	for _, c := range got {
		if c.Rule != RuleOrphanStep {
			t.Errorf("rule %s", c.Rule)
		}
		evs = append(evs, c.Events...)
	}
	want := []string{applyNoDecide.ID, decideNoRaise.ID, approveNoDraft.ID, answerNoAsk.ID}
	slices.Sort(want)
	slices.Sort(evs)
	if !slices.Equal(evs, want) {
		t.Errorf("orphan-step events %v, want %v", evs, want)
	}
	if !slices.Contains(conflictRules(Conflicts(&s, b.events, ConflictInputs{})), RuleOrphanStep) {
		t.Error("Conflicts does not run orphan-step")
	}
}

// D-contract-version-recording (versioned-command): ContractVersioned
// records a new version that waits for ContractFrozen; frozen-text-changed
// compares with the latest frozen version and accepts the pending one.
func TestContractVersioned_DContractVersionRecording(t *testing.T) {
	b := newLog(t)
	b.draftContract("C-a")
	b.freeze("C-a", 4, "h4")
	b.add(event.ContractVersionedType, event.RoleOrchestrator, "orch", event.KindContract, "C-a",
		event.ContractVersioned{Version: 5, ByDecision: "D-x", TextSHA256: "h5"})
	s := b.state()
	c := s.Contracts["C-a"]
	if c.Version != 4 || c.Pending == nil || c.Pending.Version != 5 {
		t.Fatalf("C-a = %+v", c)
	}
	path := ".ekumen/contracts/C-a.md"
	if got := FrozenTextChangedConflicts(&s, map[string]string{path: "h5"}); len(got) != 0 {
		t.Errorf("pending version text reported: %v", got)
	}
	if got := FrozenTextChangedConflicts(&s, map[string]string{path: "other"}); len(got) != 1 {
		t.Errorf("unrecorded text not reported: %v", got)
	}
	acts := LeadActions(&s, Observed{}, t0, nil)
	if len(acts) != 1 || acts[0].Action != ActionFreeze || acts[0].Subject.ID != "C-a" {
		t.Errorf("actions %+v, want freeze C-a", acts)
	}
	// Re-review finding 4: no freeze action while the contract has open blockers.
	blocked := newLog(t)
	blocked.events = append(blocked.events, b.events...)
	blocked.n = b.n
	blocked.add(event.ContractBlockersChangedType, event.RoleOrchestrator, "orch", event.KindContract, "C-a",
		event.ContractBlockersChanged{Added: []string{"D-open"}})
	bs := blocked.state()
	if acts := LeadActions(&bs, Observed{}, t0, nil); len(acts) != 0 {
		t.Errorf("blocked pending version: actions %+v", acts)
	}
	b.freeze("C-a", 5, "h5")
	s = b.state()
	if c := s.Contracts["C-a"]; c.Version != 5 || c.Pending != nil {
		t.Errorf("after freeze C-a = %+v", c)
	}
	if got := FrozenTextChangedConflicts(&s, map[string]string{path: "h4"}); len(got) != 1 {
		t.Errorf("old text after the new freeze not reported: %v", got)
	}
}

// D-post-switch-hand-edits (prose-carve-out): SpineApproved is the lead's;
// spine-unapproved fires when spine.md no longer matches the latest one.
func TestSpineUnapproved_DPostSwitchHandEdits(t *testing.T) {
	b := newLog(t)
	s := b.state()
	if got := SpineUnapprovedConflicts(&s, map[string]string{SpineFile: "x"}); len(got) != 0 {
		t.Errorf("reported before any approval: %v", got)
	}
	b.add(event.SpineApprovedType, event.RoleLead, "lead", event.KindProject, event.ProjectSubjectID, event.SpineApproved{TextSHA256: "s1"})
	second := b.add(event.SpineApprovedType, event.RoleLead, "lead", event.KindProject, event.ProjectSubjectID, event.SpineApproved{TextSHA256: "s2"})
	s = b.state()
	if got := SpineUnapprovedConflicts(&s, map[string]string{SpineFile: "s2"}); len(got) != 0 {
		t.Errorf("approved text reported: %v", got)
	}
	got := SpineUnapprovedConflicts(&s, map[string]string{SpineFile: "s1"})
	if len(got) != 1 || got[0].Rule != RuleSpineUnapproved || got[0].Events[0] != second.ID {
		t.Errorf("got %v, want spine-unapproved on the latest approval", got)
	}
	for _, role := range event.Roles {
		e := event.Event{Type: event.SpineApprovedType, Actor: event.Actor{Role: role, ID: "x"},
			Subject: event.Subject{Kind: event.KindProject, ID: event.ProjectSubjectID}, Data: json.RawMessage(`{}`)}
		if err := CheckLane(e); (err == nil) != (role == event.RoleLead) {
			t.Errorf("SpineApproved by %s: %v", role, err)
		}
	}
}

// D-apply-amend-lane (loop-lane): TaskAmended with data.by_decision is in
// the loop's lane; other amendments stay with the orchestrator.
func TestApplyAmendLane_DApplyAmendLane(t *testing.T) {
	sub := event.Subject{Kind: event.KindTask, ID: "T-a"}
	cases := []struct {
		role event.Role
		data string
		ok   bool
	}{
		{event.RoleLoop, `{"file":"f","text_sha256":"h","note":"applies D-x","by_decision":"D-x"}`, true},
		{event.RoleLoop, `{"file":"f","text_sha256":"h","note":"x"}`, false},
		{event.RoleOrchestrator, `{"file":"f","text_sha256":"h","note":"x"}`, true},
		{event.RoleOrchestrator, `{"file":"f","text_sha256":"h","note":"applies D-x","by_decision":"D-x"}`, true},
		{event.RoleSession, `{"file":"f","text_sha256":"h","note":"x","by_decision":"D-x"}`, false},
		{event.RoleLead, `{"file":"f","text_sha256":"h","note":"x","by_decision":"D-x"}`, false},
	}
	for _, c := range cases {
		e := event.Event{ID: "01JD8Z3K6Q4M2V7X9T1B5N0RCE", Type: event.TaskAmendedType, Actor: event.Actor{Role: c.role, ID: "x"},
			Subject: sub, Data: json.RawMessage(c.data)}
		if err := CheckLane(e); (err == nil) != c.ok {
			t.Errorf("TaskAmended by %s with %s: %v, want ok=%v", c.role, c.data, err, c.ok)
		}
	}
	// With state (review finding 2): by_decision must be a decided
	// decision that gates the amended task.
	b := newLog(t)
	b.draftTask("T-a")
	b.draftTask("T-b")
	b.draftTask("T-c", "D-x")
	rec := record("D-x", "T-a", "s1", 2)
	rec.Blocks = []string{"T-a"}
	b.add(event.DecisionRaisedType, event.RoleOrchestrator, "orch", event.KindDecision, "D-x", rec)
	b.add(event.DecisionRaisedType, event.RoleOrchestrator, "orch", event.KindDecision, "D-y", func() event.DecisionRaised {
		r := record("D-y", "T-b", "s1", 2)
		r.Blocks = []string{"T-b"}
		return r
	}())
	b.decide("D-x", "a")
	amend := func(task, by string) event.Event {
		return b.add(event.TaskAmendedType, event.RoleLoop, "main", event.KindTask, task,
			event.TaskAmended{File: "f", TextSHA256: "h", Note: "applies " + by, ByDecision: by})
	}
	okA, okC, unrelated, pending, ghost := amend("T-a", "D-x"), amend("T-c", "D-x"), amend("T-b", "D-x"), amend("T-b", "D-y"), amend("T-a", "D-none")
	s := b.state()
	for _, c := range []struct {
		e  event.Event
		ok bool
	}{{okA, true}, {okC, true}, {unrelated, false}, {pending, false}, {ghost, false}} {
		if err := CheckLaneIn(&s, c.e); (err == nil) != c.ok {
			t.Errorf("TaskAmended on %s by %s: %v, want ok=%v", c.e.Subject.ID, string(c.e.Data), err, c.ok)
		}
	}
	if s.Tasks["T-a"].Amendments[0].ByDecision != "D-x" {
		t.Errorf("by_decision not kept: %+v", s.Tasks["T-a"].Amendments)
	}
}

// T-audit F-2026-11-20-2: the lead's next moves (approve, freeze, apply).
func TestLeadActions_AuditF2(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-ready")              // brief, no blockers: approve
	b.draftTask("T-nobrief")            // no brief: not listed
	b.draftTask("T-blocked", "C-draft") // blocked by a draft contract
	b.draftContract("C-draft")          // no blockers: freeze
	b.draftContract("C-waits")
	b.add(event.ContractBlockersChangedType, event.RoleOrchestrator, "orch", event.KindContract, "C-waits",
		event.ContractBlockersChanged{Added: []string{"T-ready"}})
	b.raise(record("D-old", "T-ready", "s1", 2), "s1")
	b.decide("D-old", "a") // decided at t0+8m
	b.raise(record("D-new", "T-ready", "s1", 2), "s1")
	b.decide("D-new", "a")
	b.raise(record("D-done", "T-ready", "s1", 2), "s1")
	b.decide("D-done", "a")
	b.apply("D-done")
	s := b.state()
	now := t0.Add(24*time.Hour + 9*time.Minute) // D-old a day old, D-new not
	hasBrief := func(id string) bool { return id != "T-nobrief" }
	var got []string
	for _, a := range LeadActions(&s, Observed{}, now, hasBrief) {
		got = append(got, a.Action+" "+a.Subject.ID)
		if a.Action == ActionApply {
			if a.Command != "ekumen reconcile --as loop:<session>" {
				t.Errorf("apply command %q", a.Command)
			}
		} else if !strings.Contains(a.Command, a.Subject.ID) {
			t.Errorf("command %q", a.Command)
		}
	}
	want := []string{"approve T-ready", "freeze C-draft", "apply D-old"}
	if !slices.Equal(got, want) {
		t.Errorf("actions %v, want %v", got, want)
	}
}

// Brief T-reconcile job 4: a task drafted without a brief can never reach
// the lead's approve action, so the orchestrator's inbox lists it (brief),
// with the brief path in the command; approved, deferred, done and briefed
// tasks are not listed, and a refused recorded path is named.
func TestOrchestratorBriefAction_InboxGaps(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-nobrief")
	b.draftTask("T-brief")
	b.draftTask("T-approved")
	b.approve("T-approved", "h")
	b.draftTask("T-deferred")
	b.add(event.TaskDeferredType, event.RoleOrchestrator, "orch", event.KindTask, "T-deferred", event.TaskDeferred{Reason: "later"})
	b.draftTask("T-done")
	b.draftTask("T-badpath")
	s := b.state()
	obs := Observed{Tasks: map[string]TaskObservation{"T-done": {PR: PRMerged}}}
	lookup := func(id string) (string, bool, error) {
		if id == "T-badpath" {
			return "", false, errors.New("leads outside .ekumen/")
		}
		return ".ekumen/briefs/" + id + "/brief.md", id == "T-brief", nil
	}
	got := OrchestratorActions(&s, obs, lookup)
	if len(got) != 2 || got[0].Subject.ID != "T-badpath" || got[1].Subject.ID != "T-nobrief" {
		t.Fatalf("orchestrator actions %+v", got)
	}
	if !strings.Contains(got[0].Reason, "leads outside .ekumen/") {
		t.Errorf("refused path not named: %+v", got[0])
	}
	if a := got[1]; a.Action != ActionBrief || a.Command != "write .ekumen/briefs/T-nobrief/brief.md" || a.Event != s.Tasks["T-nobrief"].Drafts[0].Event {
		t.Errorf("brief action %+v", a)
	}
	hasBrief := func(id string) bool { return id == "T-brief" }
	for _, a := range LeadActions(&s, Observed{}, t0, hasBrief) {
		if a.Subject.ID == "T-nobrief" {
			t.Errorf("lead action for a task with no brief: %+v", a)
		}
	}
}
