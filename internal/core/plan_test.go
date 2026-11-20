package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

var loopActor = event.Actor{Role: event.RoleLoop, ID: "main"}

func planAt(t *testing.T, events []event.Event, now time.Time) Plan {
	t.Helper()
	return PlanPass(PlanInput{Events: events, Now: now, Loop: loopActor, Observation: ObservationOK})
}

// written returns the log after a plan's events are recorded, with the
// amendment files filled in as the executor would.
func written(t *testing.T, events []event.Event, p Plan) []event.Event {
	t.Helper()
	out := slices.Clone(events)
	for _, a := range p.Actions {
		for _, e := range a.Events {
			if e.Type == event.TaskAmendedType {
				var d event.TaskAmended
				if err := json.Unmarshal(e.Data, &d); err != nil {
					t.Fatal(err)
				}
				d.File, d.TextSHA256 = "briefs/"+e.Subject.ID+"/amendments/x.md", "h"
				e.Data, _ = json.Marshal(d)
			}
			if err := CheckLaneIn(ptrState(Reduce(out)), e); err != nil {
				t.Fatalf("planned %s outside the loop's lane: %v", e.Type, err)
			}
			out = append(out, e)
		}
	}
	return out
}

func ptrState(s State) *State { return &s }

// closed checks that planning again after the plan is written plans
// nothing, and returns the new log.
func closed(t *testing.T, events []event.Event, p Plan, now time.Time) []event.Event {
	t.Helper()
	after := written(t, events, p)
	if again := planAt(t, after, now); !again.Empty() {
		t.Fatalf("plan is not closed; second pass plans %v", kinds(again))
	}
	return after
}

func kinds(p Plan) []string {
	var out []string
	for _, a := range p.Actions {
		out = append(out, a.Kind+" "+a.Subject.ID)
	}
	return out
}

func eventTypes(a LoopAction) []event.Type {
	var out []event.Type
	for _, e := range a.Events {
		out = append(out, e.Type)
	}
	return out
}

func leadRaise(b *logBuilder, rec event.DecisionRaised) event.Event {
	return b.add(event.DecisionRaisedType, event.RoleLead, "lead", event.KindDecision, rec.ID, rec)
}

func tierOne(b *logBuilder, id, deadline string, blocks ...string) {
	rec := record(id, "T-a", "s1", 1)
	rec.Deadline, rec.Blocks = ptr(deadline), orEmpty(blocks)
	leadRaise(b, rec)
}

// C-decision-record §1/§3: a tier-1 decision defaults only after its
// deadline day; the deadline day itself is still answerable (Urgency's
// "overdue" starts the day after).
func TestPlanDefaultOnlyAfterDeadlineDay_DecisionRecord3(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	tierOne(b, "D-x", "2026-11-27")
	deadlineDay := time.Date(2026, 11, 27, 23, 59, 0, 0, time.UTC)
	if p := planAt(t, b.events, deadlineDay); !p.Empty() {
		t.Fatalf("on the deadline day the plan is %v, want nothing", kinds(p))
	}
	if u := Urgency(ptr("2026-11-27"), deadlineDay); u == 6 {
		t.Fatal("Urgency calls the deadline day overdue")
	}
	next := deadlineDay.Add(time.Minute)
	p := planAt(t, b.events, next)
	if got := kinds(p); !slices.Equal(got, []string{"default D-x", "apply D-x"}) {
		t.Fatalf("after the deadline: %v", got)
	}
	var d event.DecisionDefaulted
	json.Unmarshal(p.Actions[0].Events[0].Data, &d)
	if d.Answer != "a" || d.Deadline != "2026-11-27" {
		t.Errorf("DecisionDefaulted %+v", d)
	}
	after := closed(t, b.events, p, next)
	if s := Reduce(after); s.Decisions["D-x"].Status != StatusDefaulted || s.Decisions["D-x"].AppliedOn == "" {
		t.Errorf("D-x is %s, applied_on %q", s.Decisions["D-x"].Status, s.Decisions["D-x"].AppliedOn)
	}
}

// D-control-loop: a default and its apply belong to one pass; the apply
// is planned against the state with the default reduced in, and unblocks
// the task it gates.
func TestPlanDefaultThenApplyOnePass_DControlLoop(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	b.approve("T-a", "h")
	b.attach("T-a", "s-a")
	tierOne(b, "D-x", "2026-11-20", "T-a")
	now := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	if st := DeriveTaskStatus(ptrState(b.state()), Observed{}, "T-a"); st != TaskBlocked {
		t.Fatalf("T-a %s before the pass", st)
	}
	p := planAt(t, b.events, now)
	if got := kinds(p); !slices.Equal(got, []string{"default D-x", "apply D-x"}) {
		t.Fatalf("plan %v", got)
	}
	if got := eventTypes(p.Actions[1]); !slices.Equal(got, []event.Type{event.TaskAmendedType, event.DecisionAppliedType}) {
		t.Errorf("apply writes %v", got)
	}
	if !slices.Equal(p.Unblocked, []string{"T-a"}) || !slices.Equal(p.Notify, []string{"s-a"}) || !slices.Equal(p.Ready, []string{"T-a"}) {
		t.Errorf("unblocked %v notify %v ready %v", p.Unblocked, p.Notify, p.Ready)
	}
	if am := p.Actions[1].Amendments; len(am) != 1 || !strings.Contains(am[0].Text, "Applied D-x (Pick one): `a`, defaulted on 2026-11-21.") {
		t.Errorf("amendment %+v", am)
	}
	closed(t, b.events, p, now)
}

// D-apply-before-loop: the loop applies as `decision apply` does
// (ApplyEffectOf), skips tasks already amended for the decision, and an
// applied decision is never planned again.
func TestPlanApplyIdempotent_DApplyBeforeLoop(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	b.draftTask("T-b")
	rec := record("D-x", "T-a", "s1", 2)
	rec.Blocks = []string{"T-a", "T-b", "T-none"}
	leadRaise(b, rec)
	b.decide("D-x", "b")
	b.add(event.TaskAmendedType, event.RoleLoop, "loop", event.KindTask, "T-a",
		event.TaskAmended{File: "briefs/T-a/amendments/1.md", TextSHA256: "h", Note: "applies D-x", ByDecision: "D-x"})

	s := b.state()
	eff, err := ApplyEffectOf(&s, b.events, Observed{}, "D-x", t0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(eff.Tasks, []string{"T-a", "T-b"}) || !slices.Equal(eff.Amend, []string{"T-b"}) || !slices.Equal(eff.NotTasks, []string{"T-none"}) {
		t.Errorf("effect %+v", eff)
	}
	p := planAt(t, b.events, t0)
	if got := kinds(p); !slices.Equal(got, []string{"apply D-x"}) {
		t.Fatalf("plan %v", got)
	}
	a := p.Actions[0]
	var applied event.DecisionApplied
	json.Unmarshal(a.Events[len(a.Events)-1].Data, &applied)
	if !slices.Equal(applied.AmendedTasks, eff.Tasks) || !slices.Equal(applied.UnblockedTasks, eff.Unblocked) || applied.NotifiedSessions == nil {
		t.Errorf("DecisionApplied %+v, want the effect %+v", applied, eff)
	}
	if len(a.Amendments) != 1 || a.Amendments[0].Task != "T-b" || a.Amendments[0].Text != eff.Line+"\n" {
		t.Errorf("amendments %+v", a.Amendments)
	}
	after := closed(t, b.events, p, t0)
	as := Reduce(after)
	if _, err := ApplyEffectOf(&as, after, Observed{}, "D-x", t0); err == nil || !strings.Contains(err.Error(), ErrApplied.Error()) {
		t.Errorf("applying again: %v", err)
	}
}

// C-event-log §4: the loop writes RoutedToOwner for every routable entry
// (handoffs with follow-ups, unanswered questions), with the route ids
// Route gives, and never routes an entry twice.
func TestPlanRoutes_EventLog4(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	b.attach("T-a", "s-a")
	h := b.add(event.TaskHandoffType, event.RoleSession, "s-a", event.KindTask, "T-a",
		event.TaskHandoff{File: "briefs/T-a/handoffs/1.md", TextSHA256: "h", StatusLine: "done", HasFollowups: true})
	b.add(event.TaskHandoffType, event.RoleSession, "s-a", event.KindTask, "T-a",
		event.TaskHandoff{File: "briefs/T-a/handoffs/2.md", TextSHA256: "h", StatusLine: "done"})
	q := b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindTask, "T-a",
		event.QuestionAsked{QuestionID: "q-1", Text: "why?"})
	p := planAt(t, b.events, t0)
	if got := kinds(p); !slices.Equal(got, []string{"route T-a", "route T-a"}) {
		t.Fatalf("plan %v", got)
	}
	want := map[string]string{RouteID(h.ID): "orchestrator", RouteID(q.ID): "s-a"}
	for _, a := range p.Actions {
		var r event.RoutedToOwner
		json.Unmarshal(a.Events[0].Data, &r)
		if want[r.RouteID] != r.To || a.Events[0].Type != event.RoutedToOwnerType || a.Events[0].Actor != loopActor {
			t.Errorf("route %+v by %v", r, a.Events[0].Actor)
		}
	}
	after := closed(t, b.events, p, t0)
	if s := Reduce(after); len(s.Routes) != 2 {
		t.Errorf("routes %v", s.Routes)
	}
}

// C-event-log §5: a conflict becomes a tier-2 decision with a
// deterministic id, then ConflictDetected; a pass interrupted between the
// two writes only ConflictDetected next time.
func TestPlanConflict_EventLog5(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	leadRaise(b, record("D-x", "T-a", "s1", 2))
	d1 := b.decide("D-x", "a")
	d2 := b.decide("D-x", "b")
	b.apply("D-x")
	p := planAt(t, b.events, t0)
	if got := kinds(p); len(got) != 1 || !strings.HasPrefix(got[0], "conflict D-conflict-decided-twice-") {
		t.Fatalf("plan %v", got)
	}
	a := p.Actions[0]
	sum := sha256.Sum256([]byte("decided-twice|" + d1.ID + "," + d2.ID))
	wantID := "D-conflict-decided-twice-" + hex.EncodeToString(sum[:])[:6]
	if a.Subject.ID != wantID || event.ValidateID(wantID) != nil {
		t.Errorf("decision id %s, want %s", a.Subject.ID, wantID)
	}
	if again := planAt(t, b.events, t0.Add(time.Hour)); again.Actions[0].Subject.ID != wantID {
		t.Errorf("id not deterministic: %s", again.Actions[0].Subject.ID)
	}
	if got := eventTypes(a); !slices.Equal(got, []event.Type{event.DecisionRaisedType, event.ConflictDetectedType}) {
		t.Fatalf("conflict writes %v", got)
	}
	var rec event.DecisionRaised
	json.Unmarshal(a.Events[0].Data, &rec)
	if rec.Tier != 2 || rec.Recommendation != nil || rec.DefaultIfNoAnswer != nil || rec.RaisedBy.Task != nil ||
		rec.RaisedBy.Session != "main" || len(rec.Blocks) != 0 || len(rec.ContractImpact) != 0 ||
		len(rec.Options) != 2 || rec.Options[0].ID != ConflictFix || rec.Options[1].ID != ConflictAccept ||
		len(rec.Context.Evidence) != 2 || !strings.Contains(rec.Context.Summary, "decided-twice") {
		t.Errorf("conflict decision %+v", rec)
	}
	var cd event.ConflictDetected
	json.Unmarshal(a.Events[1].Data, &cd)
	if cd.Rule != RuleDecidedTwice || !slices.Equal(cd.Events, []string{d1.ID, d2.ID}) {
		t.Errorf("ConflictDetected %+v", cd)
	}
	closed(t, b.events, p, t0)

	crashed := append(slices.Clone(b.events), a.Events[0])
	p2 := planAt(t, crashed, t0)
	if len(p2.Actions) != 1 || !slices.Equal(eventTypes(p2.Actions[0]), []event.Type{event.ConflictDetectedType}) || p2.Actions[0].Subject.ID != wantID {
		t.Fatalf("after a crash between the two writes: %v", kinds(p2))
	}
	closed(t, crashed, p2, t0)
}

// Spine §3 and D-placeholder-task-fields: the loop reports ready tasks and
// tasks approved only for want of a branch or worktree; it starts nothing.
func TestPlanReadinessAndNotStartable_DPlaceholderTaskFields(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-ready")
	b.approve("T-ready", "h")
	b.add(event.TaskDraftedType, event.RoleOrchestrator, "orch", event.KindTask, "T-bare",
		event.TaskDrafted{Title: "bare", Kind: event.TaskKindBuild, BlockedBy: []string{}, TierGrants: []string{}})
	b.approve("T-bare", "h")
	b.draftTask("T-blocked", "T-ready")
	b.approve("T-blocked", "h")
	b.draftTask("T-draft")
	p := planAt(t, b.events, t0)
	if !p.Empty() || !slices.Equal(p.Ready, []string{"T-ready"}) || !slices.Equal(p.NotStartable, []string{"T-bare"}) {
		t.Errorf("ready %v not_startable %v actions %v", p.Ready, p.NotStartable, kinds(p))
	}
	for _, e := range b.events {
		if e.Type == event.TaskSessionAttachedType {
			t.Error("the loop attached a session")
		}
	}
}

// Issue #15 (review): DecisionApplied records unblocked_tasks for good, so
// without a complete observation the loop holds applies, and reports ready
// and not-startable tasks as unknown; defaults and routes still go ahead.
// T-a is blocked by D-x and by T-b, whose PR is merged.
func TestPlanHoldsAppliesWithoutObservation_Issue15(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-b")
	b.approve("T-b", "h")
	b.draftTask("T-a", "T-b")
	b.approve("T-a", "h")
	rec := record("D-x", "T-a", "s1", 2)
	rec.Blocks = []string{"T-a"}
	leadRaise(b, rec)
	b.decide("D-x", "a")
	tierOne(b, "D-late", "2026-11-19")
	b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindTask, "T-a", event.QuestionAsked{QuestionID: "q-1", Text: "?", To: "orchestrator"})
	merged := Observed{Tasks: map[string]TaskObservation{"T-b": {PR: PRMerged}}}

	ok := PlanPass(PlanInput{Events: b.events, Now: t0, Loop: loopActor, Observation: ObservationOK, Observed: merged})
	if got := kinds(ok); !slices.Equal(got, []string{"default D-late", "apply D-late", "apply D-x", "route T-a"}) {
		t.Fatalf("observed plan %v", got)
	}
	if !slices.Equal(ok.Unblocked, []string{"T-a"}) || !slices.Equal(ok.Ready, []string{"T-a"}) {
		t.Errorf("observed: unblocked %v ready %v", ok.Unblocked, ok.Ready)
	}

	for _, obs := range []string{ObservationUnavailable, ObservationTruncated, ""} {
		p := PlanPass(PlanInput{Events: b.events, Now: t0, Loop: loopActor, Observation: obs})
		if got := kinds(p); !slices.Equal(got, []string{"default D-late", "route T-a"}) {
			t.Errorf("%q: plan %v", obs, got)
		}
		var held []string
		for _, h := range p.Held {
			held = append(held, h.Subject.ID)
			if h.Reason != HeldObservation {
				t.Errorf("%q: held %+v", obs, h)
			}
		}
		if !slices.Equal(held, []string{"D-late", "D-x"}) || p.Ready != nil || p.NotStartable != nil || len(p.Unblocked) != 0 {
			t.Errorf("%q: held %v ready %v not_startable %v unblocked %v", obs, held, p.Ready, p.NotStartable, p.Unblocked)
		}
	}
}

// C-event-log §5 (detected, never resolved silently): a decision decided
// with two different answers is not applied; the apply is held and the
// conflict is raised for the lead.
func TestPlanHoldsContestedDecision_EventLog5(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	rec := record("D-x", "T-a", "s1", 2)
	rec.Blocks = []string{"T-a"}
	leadRaise(b, rec)
	b.decide("D-x", "a")
	b.decide("D-x", "b")
	p := planAt(t, b.events, t0)
	if got := kinds(p); len(got) != 1 || !strings.HasPrefix(got[0], "conflict D-conflict-decided-twice-") {
		t.Fatalf("plan %v", got)
	}
	if len(p.Held) != 1 || p.Held[0].Subject.ID != "D-x" || p.Held[0].Reason != HeldContested {
		t.Errorf("held %+v", p.Held)
	}
	after := written(t, b.events, p)
	if again := planAt(t, after, t0); !again.Empty() || len(again.Held) != 1 {
		t.Errorf("after the conflict is recorded: %v, held %v", kinds(again), again.Held)
	}
}

// The conflict decision id is a hash of the rule and the sorted event ids:
// the same conflict gets the same id whatever else is in the log or the
// order it is found in; a conflict whose events grow (a third answer) is a
// new conflict with a new decision.
func TestConflictDecisionIDIsAHashOfTheConflict_EventLog5(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	leadRaise(b, record("D-x", "T-a", "s1", 2))
	d1 := b.decide("D-x", "a")
	d2 := b.decide("D-x", "b")
	s := b.state()
	c := Conflict{Rule: RuleDecidedTwice, Events: []string{d2.ID, d1.ID}}
	id, _ := ConflictDecisionID(&s, b.events, c)
	if id2, _ := ConflictDecisionID(&s, b.events, Conflict{Rule: RuleDecidedTwice, Events: []string{d1.ID, d2.ID}}); id != id2 {
		t.Error("id depends on event order")
	}
	if event.ValidateID(id) != nil || len(id) != len("D-conflict-decided-twice-")+6 {
		t.Errorf("id %s", id)
	}
	first := written(t, b.events, planAt(t, b.events, t0))
	b.events = first
	b.decide("D-x", "a2")
	p := planAt(t, b.events, t0)
	if got := kinds(p); len(got) != 1 || p.Actions[0].Subject.ID == id || len(p.Actions[0].Events) != 2 {
		t.Fatalf("third answer: plan %v (first decision %s)", got, id)
	}
	closed(t, b.events, p, t0)
}

// Review: one action that cannot be planned is skipped with its reason;
// the other actions of the pass are still planned.
func TestPlanSkipsAnActionAndGoesOn(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	tierOne(b, "D-one", "2026-11-18")
	tierOne(b, "D-two", "2026-11-18")
	calls := 0
	newID := func(at time.Time) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("no id")
		}
		return event.ULIDFromParts(uint64(at.UnixMilli()), [10]byte{9: byte(calls)})
	}
	p := PlanPass(PlanInput{Events: b.events, Now: t0, Loop: loopActor, Observation: ObservationOK, NewID: newID})
	if got := kinds(p); !slices.Equal(got, []string{"default D-two", "apply D-two"}) {
		t.Errorf("plan %v", got)
	}
	if len(p.Skipped) != 1 || p.Skipped[0].Subject.ID != "D-one" || p.Skipped[0].Kind != LoopDefault || !strings.Contains(p.Skipped[0].Reason, "no id") {
		t.Errorf("skipped %+v", p.Skipped)
	}
}

// Review: when every id width is taken by other conflicts' decisions, the
// conflict is skipped with a reason, never attributed to another decision.
func TestConflictDecisionIDAllTaken_EventLog5(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	leadRaise(b, record("D-x", "T-a", "s1", 2))
	d1 := b.decide("D-x", "a")
	d2 := b.decide("D-x", "b")
	sum := sha256.Sum256([]byte("decided-twice|" + d1.ID + "," + d2.ID))
	h := hex.EncodeToString(sum[:])
	for _, n := range []int{6, 8, 10} {
		rec := record("D-conflict-decided-twice-"+h[:n], "T-a", "s1", 2)
		rec.Context.Evidence = []string{"other"}
		leadRaise(b, rec)
	}
	s := b.state()
	if id, err := ConflictDecisionID(&s, b.events, Conflict{Rule: RuleDecidedTwice, Events: []string{d1.ID, d2.ID}}); err == nil {
		t.Fatalf("got %s, want an error", id)
	}
	p := planAt(t, b.events, t0)
	if len(p.Skipped) != 1 || p.Skipped[0].Kind != LoopConflict || !strings.Contains(p.Skipped[0].Reason, "taken") {
		t.Errorf("skipped %+v, actions %v", p.Skipped, kinds(p))
	}
}
