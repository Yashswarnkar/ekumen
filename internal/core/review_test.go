package core

import (
	"testing"

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
