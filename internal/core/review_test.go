package core

import (
	"fmt"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

func (b *logBuilder) draftTaskAs(actor, id string, aliases []string, blockedBy ...string) event.Event {
	return b.add(event.TaskDraftedType, event.RoleOrchestrator, actor, event.KindTask, id, event.TaskDrafted{
		Title: id, Kind: event.TaskKindBuild, Brief: ".ekumen/briefs/" + id + "/brief.md", Repo: "ekumen",
		BaseBranch: "main", Branch: "build/" + id, Worktree: ".worktrees/" + id,
		BlockedBy: orEmpty(blockedBy), TierGrants: []string{"push_own_branch"}, Aliases: aliases,
	})
}

func TestAliasCannotHijackAnItem(t *testing.T) {
	b := newLog(t)
	b.draftTaskAs("orch", "T-a", []string{"D-012", "T-b", "T0"})
	b.raise(record("D-012", "T-a", "s1", 2), "s1")
	b.draftTask("T-b")
	b.draftTaskAs("other", "T-a", []string{"T9"})
	s := b.state()
	for alias, want := range map[string]string{"D-012": "D-012", "T-b": "T-b", "T9": "T9", "T0": "T-a"} {
		if got := s.Resolve(alias); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", alias, got, want)
		}
	}
	if !s.Tasks["T-b"].Drafted() || len(s.Tasks["T-a"].Drafts) != 2 {
		t.Errorf("T-b drafted=%v, T-a drafts=%d", s.Tasks["T-b"].Drafted(), len(s.Tasks["T-a"].Drafts))
	}
	if d := s.Decisions["D-012"]; d == nil || len(d.Raises) != 1 {
		t.Errorf("D-012 not raised on its own id: %+v", d)
	}
	if len(s.Skipped) != 3 {
		t.Errorf("skipped = %+v, want the 3 refused aliases", s.Skipped)
	}
}

func TestKindlessAliasResolvesPerKind(t *testing.T) {
	b := newLog(t)
	b.lax = true
	b.draftTaskAs("orch", "T-a", []string{"R1"})
	b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindDecision, "R1", event.QuestionAsked{QuestionID: "q1", Text: "?"})
	s := b.state()
	if _, ok := s.Decisions["T-a"]; ok {
		t.Error("a decision event on R1 landed on task T-a's id")
	}
}

func TestBlockersCompareResolvedIDs(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-x", "T0", "C1")
	b.draftTaskAs("orch", "T-dep", []string{"T0"})
	b.add(event.ContractDraftedType, event.RoleOrchestrator, "orch", event.KindContract, "C-y",
		event.ContractDrafted{Title: "y", Path: "contracts/C-y.md", BlockedBy: []string{"T0"}})
	b.add(event.ContractDraftedType, event.RoleOrchestrator, "orch", event.KindContract, "C-x",
		event.ContractDrafted{Title: "x", Path: "contracts/C-x.md", Aliases: []string{"C1"}})
	b.add(event.TaskBlockersChangedType, event.RoleOrchestrator, "orch", event.KindTask, "T-x",
		event.TaskBlockersChanged{Added: []string{"T-dep"}, Removed: []string{"T-dep"}, Reason: "done"})
	b.add(event.ContractBlockersChangedType, event.RoleOrchestrator, "orch", event.KindContract, "C-y",
		event.ContractBlockersChanged{Removed: []string{"T-dep"}, Reason: "done"})
	s := b.state()
	if got := s.Tasks["T-x"].BlockedBy; len(got) != 1 || got[0] != "C-x" {
		t.Errorf("T-x blocked_by = %v, want [C-x]", got)
	}
	if got := s.Contracts["C-y"].BlockedBy; len(got) != 0 {
		t.Errorf("C-y blocked_by = %v, want []", got)
	}
}

func TestActorIsRoleNamesMatchRoleOnly(t *testing.T) {
	cases := []struct {
		a    event.Actor
		to   string
		want bool
	}{
		{event.Actor{Role: event.RoleSession, ID: "lead"}, "lead", false},
		{event.Actor{Role: event.RoleSession, ID: "orchestrator"}, "orchestrator", false},
		{event.Actor{Role: event.RoleIntegrator, ID: "loop"}, "loop", false},
		{event.Actor{Role: event.RoleLead, ID: "yash"}, "lead", true},
		{event.Actor{Role: event.RoleSession, ID: "t-core"}, "t-core", true},
		{event.Actor{Role: event.RoleIntegrator, ID: "t-e2e"}, "t-e2e", true},
		{event.Actor{Role: event.RoleOrchestrator, ID: "main"}, "main", true},
		{event.Actor{Role: event.RoleLoop, ID: "main"}, "main", true},
		{event.Actor{Role: event.RoleLead, ID: "t-core"}, "t-core", false},
		{event.Actor{Role: event.RoleLead, ID: ""}, "", false},
	}
	for _, c := range cases {
		if got := ActorIs(c.a, c.to); got != c.want {
			t.Errorf("ActorIs(%s:%s, %q) = %v, want %v", c.a.Role, c.a.ID, c.to, got, c.want)
		}
	}

	b := newLog(t)
	b.draftContract("C-x")
	b.add(event.QuestionAskedType, event.RoleSession, "s1", event.KindContract, "C-x", event.QuestionAsked{QuestionID: "q1", Text: "?", To: "lead"})
	ans := b.add(event.QuestionAnsweredType, event.RoleSession, "lead", event.KindContract, "C-x", event.QuestionAnswered{QuestionID: "q1", Text: "!"})
	s := b.state()
	if err := CheckLaneIn(&s, ans); err == nil {
		t.Error("session:lead answered the lead's question")
	}
}

func TestAddresseeMatchesRouteOwnerWithoutSession(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-x")
	q := b.add(event.QuestionAskedType, event.RoleLead, "lead", event.KindTask, "T-x", event.QuestionAsked{QuestionID: "q1", Text: "?"})
	ans := b.add(event.QuestionAnsweredType, event.RoleOrchestrator, "main", event.KindTask, "T-x", event.QuestionAnswered{QuestionID: "q1", Text: "!"})
	s := Reduce(b.events[:2])
	owner, _ := s.RouteOwner(RouteID(q.ID))
	question, _ := s.FindQuestion(q.Subject, "q1")
	if got := s.Addressee(q.Subject, question); got != owner {
		t.Errorf("Addressee = %q, RouteOwner = %q", got, owner)
	}
	if err := CheckLaneIn(&s, ans); err != nil {
		t.Errorf("the route owner could not answer: %v", err)
	}
}

func TestStatusDerivationIsNotExponential(t *testing.T) {
	b := newLog(t)
	const layers = 24
	name := func(l, i int) string { return fmt.Sprintf("T-l%d-%d", l, i) }
	for l := range layers {
		var below []string
		if l > 0 {
			below = []string{name(l-1, 0), name(l-1, 1)}
		}
		for i := range 2 {
			b.draftTask(name(l, i), below...)
			b.approve(name(l, i), "h")
		}
	}
	s := b.state()
	start := time.Now()
	statuses := TaskStatuses(&s, Observed{})
	top := DeriveTaskStatus(&s, Observed{}, name(layers-1, 0))
	open := OpenBlockers(&s, Observed{}, name(layers-1, 1))
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("deriving took %v", d)
	}
	if top != TaskBlocked || statuses[name(0, 0)] != TaskReady || len(open) != 2 {
		t.Errorf("top=%s bottom=%s open=%v", top, statuses[name(0, 0)], open)
	}
}

func TestStatusCyclesStayBlocked(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a", "T-b", "C-x")
	b.draftTask("T-b", "T-a")
	b.approve("T-a", "h")
	b.approve("T-b", "h")
	b.add(event.ContractDraftedType, event.RoleOrchestrator, "orch", event.KindContract, "C-x",
		event.ContractDrafted{Title: "x", Path: "contracts/C-x.md", BlockedBy: []string{"C-y"}})
	b.add(event.ContractDraftedType, event.RoleOrchestrator, "orch", event.KindContract, "C-y",
		event.ContractDrafted{Title: "y", Path: "contracts/C-y.md", BlockedBy: []string{"C-x"}})
	b.freeze("C-x", 1, "h")
	b.freeze("C-y", 1, "h")
	s := b.state()
	for id, st := range TaskStatuses(&s, Observed{}) {
		if st != TaskBlocked {
			t.Errorf("%s = %s, want blocked", id, st)
		}
	}
	if got := ContractOpenBlockers(&s, Observed{}, "C-x"); len(got) != 1 || got[0] != "C-y" {
		t.Errorf("C-x open blockers = %v", got)
	}
}

func TestGhostItemsNeverClearBlockers(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-x", "X-ghost", "C-ghost", "T-ghost")
	b.approve("T-x", "h")
	b.add(event.ExternalResolvedType, event.RoleOrchestrator, "orch", event.KindExternal, "X-ghost", event.ExternalResolved{Note: "?"})
	b.freeze("C-ghost", 1, "h")
	b.approve("T-ghost", "h")
	s := b.state()
	obs := Observed{Tasks: map[string]TaskObservation{"T-ghost": {PR: PRMerged}}}
	if got := OpenBlockers(&s, obs, "T-x"); len(got) != 3 {
		t.Errorf("open blockers = %v, want the three ghosts", got)
	}
	for _, id := range []string{"X-ghost", "C-ghost", "T-ghost"} {
		if s.Has(id) {
			t.Errorf("Has(%s) for an item never created", id)
		}
	}
	if got := len(OrphanStepConflicts(&s, b.events)); got != 3 {
		t.Errorf("orphan-step conflicts = %d, want 3", got)
	}
}
