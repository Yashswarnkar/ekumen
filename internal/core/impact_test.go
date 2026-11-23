package core

import (
	"slices"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// GitHub #63: a decision named only in a task's blocked_by gates it, so
// applying the decision amends that task, after those in `blocks`.
func TestApplyAmendsBlockedBySide_Issue63(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-listed")
	b.draftTask("T-named", "D-x")
	b.draftTask("T-other")
	rec := record("D-x", "T-listed", "s1", 2)
	rec.Blocks = []string{"T-listed"}
	leadRaise(b, rec)
	b.decide("D-x", "b")
	s := b.state()
	if got := Gates(&s, "D-x"); !slices.Equal(got, []string{"T-listed", "T-named"}) {
		t.Errorf("Gates = %v", got)
	}
	eff, err := ApplyEffectOf(&s, b.events, Observed{}, "D-x", t0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(eff.Tasks, []string{"T-listed", "T-named"}) || !slices.Equal(eff.Amend, []string{"T-listed", "T-named"}) {
		t.Errorf("effect %+v", eff)
	}
	// Only blocked_by names it, as for T-loop-hardening's decisions.
	b2 := newLog(t)
	b2.draftTask("T-named", "D-y")
	leadRaise(b2, record("D-y", "T-named", "s1", 2))
	b2.decide("D-y", "b")
	s2 := b2.state()
	eff, err = ApplyEffectOf(&s2, b2.events, Observed{}, "D-y", t0)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(eff.Amend, []string{"T-named"}) {
		t.Errorf("blocked_by-only gate amends %v, want [T-named]", eff.Amend)
	}
}

// T-inbox-v1 (#37): impact counts unfinished tasks only, separates direct
// from transitive, leaves contract_impact as context, and gives paths.
func TestImpactOf_Issue37(t *testing.T) {
	b := newLog(t)
	b.draftContract("C-api")
	b.draftTask("T-done", "C-api") // reached only through contract_impact, and done
	b.draftTask("T-wait", "D-wide")
	b.draftTask("T-next", "T-wait")
	b.draftTask("T-after-done", "T-done")
	b.draftTask("T-deferred", "T-wait")
	b.add(event.TaskDeferredType, event.RoleOrchestrator, "orch", event.KindTask, "T-deferred", event.TaskDeferred{})
	rec := record("D-wide", "T-wait", "s1", 2)
	rec.ContractImpact = []string{"C-api"}
	leadRaise(b, rec)
	s := b.state()
	obs := Observed{Tasks: map[string]TaskObservation{"T-done": {PR: PRMerged}}}

	im := ImpactOf(&s, obs, "D-wide")
	if !slices.Equal(im.Direct, []string{"T-wait"}) || !slices.Equal(im.Unblocks, []string{"T-next", "T-wait"}) ||
		!slices.Equal(im.ContractContext, []string{"C-api"}) {
		t.Errorf("impact %+v", im)
	}
	if len(im.Paths) != 2 || !slices.Equal(im.Paths[0], []string{"D-wide", "T-wait"}) ||
		!slices.Equal(im.Paths[1], []string{"D-wide", "T-wait", "T-next"}) {
		t.Errorf("paths %v", im.Paths)
	}
	// A done task holds up nothing, even with dependents.
	if im := ImpactOf(&s, obs, "T-done"); len(im.Unblocks) != 0 {
		t.Errorf("done task impact %v", im.Unblocks)
	}
	// T-next waits on T-wait, which waits on the lead's answer.
	ws := WaitsOn(&s, obs, "T-next")
	if len(ws) != 1 || ws[0].ID != "T-wait" || len(ws[0].On) != 1 || ws[0].On[0].ID != "D-wide" || ws[0].On[0].Why != WaitAnswer {
		t.Errorf("WaitsOn = %+v", ws)
	}
	// Once decided and applied, nothing waits on the decision.
	b.decide("D-wide", "b")
	b.add(event.DecisionAppliedType, event.RoleLoop, "loop", event.KindDecision, "D-wide", event.DecisionApplied{})
	s = b.state()
	if im := ImpactOf(&s, obs, "D-wide"); len(im.Unblocks) != 0 || !slices.Equal(im.ContractContext, []string{"C-api"}) {
		t.Errorf("applied decision impact %+v", im)
	}
}
