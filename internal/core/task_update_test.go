package core

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

func (b *logBuilder) update(id string, fields map[string]string, reason string) event.Event {
	d := event.TaskUpdated{Reason: reason}
	for k, v := range fields {
		switch k {
		case "title":
			d.Title = &v
		case "brief":
			d.Brief = &v
		case "repo":
			d.Repo = &v
		case "base_branch":
			d.BaseBranch = &v
		case "branch":
			d.Branch = &v
		case "worktree":
			d.Worktree = &v
		}
	}
	return b.add(event.TaskUpdatedType, event.RoleOrchestrator, "orch", event.KindTask, id, d)
}

func placeholder(b *logBuilder, id string) {
	b.add(event.TaskDraftedType, event.RoleOrchestrator, "orch", event.KindTask, id, event.TaskDrafted{
		Title: id, Kind: event.TaskKindBuild, BlockedBy: []string{}, TierGrants: []string{}})
}

func TestTaskUpdatedBeforeApprovalAppliesAtOnce(t *testing.T) {
	b := newLog(t)
	placeholder(b, "T-a")
	b.update("T-a", map[string]string{"branch": "build/t-a", "worktree": ".worktrees/t-a"}, "fill placeholders")
	s := b.state()
	ta := s.Tasks["T-a"]
	if ta.Branch != "build/t-a" || ta.Worktree != ".worktrees/t-a" || ta.PendingUpdate != nil {
		t.Fatalf("task = %+v", ta)
	}
	if got := DeriveTaskStatus(&s, Observed{}, "T-a"); got != TaskDraft {
		t.Errorf("status %s, want draft", got)
	}
	b.approve("T-a", "h1")
	s = b.state()
	if got := DeriveTaskStatus(&s, Observed{}, "T-a"); got != TaskReady {
		t.Errorf("status %s, want ready", got)
	}
}

func TestTaskUpdatedAfterApprovalWaitsForTheLead(t *testing.T) {
	b := newLog(t)
	placeholder(b, "T-a")
	first := b.approve("T-a", "h1")
	u1 := b.update("T-a", map[string]string{"branch": "build/old", "repo": "ekumen"}, "first")
	u2 := b.update("T-a", map[string]string{"branch": "build/t-a", "worktree": ".worktrees/t-a"}, "second")
	s := b.state()
	ta := s.Tasks["T-a"]
	if ta.Branch != "" || ta.PendingUpdate == nil {
		t.Fatalf("update applied before approval: %+v", ta)
	}
	want := map[string]string{"repo": "ekumen", "branch": "build/t-a", "worktree": ".worktrees/t-a"}
	if !maps.Equal(ta.PendingUpdate.Fields, want) || ta.PendingUpdate.Reason != "second" ||
		!slices.Equal(ta.PendingUpdate.Events, []string{u1.ID, u2.ID}) {
		t.Errorf("pending = %+v", ta.PendingUpdate)
	}
	if got := DeriveTaskStatus(&s, Observed{}, "T-a"); got != TaskApproved {
		t.Errorf("pending update changed status to %s", got)
	}
	acts := LeadActions(&s, Observed{}, t0, nil)
	if len(acts) != 1 || acts[0].Update == nil || acts[0].Command != "ekumen task approve T-a" || acts[0].Event != u2.ID {
		t.Errorf("lead actions = %+v", acts)
	}

	b.add(event.TaskApprovedType, event.RoleLead, "lead", event.KindTask, "T-a",
		event.TaskApproved{TierGrants: []string{"restack"}, TextSHA256: "h2"})
	s = b.state()
	ta = s.Tasks["T-a"]
	if ta.PendingUpdate != nil || ta.Branch != "build/t-a" || ta.Repo != "ekumen" || ta.Worktree != ".worktrees/t-a" {
		t.Fatalf("not accepted: %+v", ta)
	}
	if !slices.Equal(ta.TierGrants, []string{"restack"}) || ta.ApprovedSHA256 != "h1" || ta.ApprovedEvent != first.ID {
		t.Errorf("grants %v, sha %s, event %s: an unchanged §A keeps its first hash", ta.TierGrants, ta.ApprovedSHA256, ta.ApprovedEvent)
	}
	if got := DeriveTaskStatus(&s, Observed{}, "T-a"); got != TaskReady {
		t.Errorf("status %s, want ready", got)
	}
	if acts := LeadActions(&s, Observed{}, t0, nil); len(acts) != 0 {
		t.Errorf("action left after approve: %+v", acts)
	}
}

func TestTaskApprovedWithoutPendingUpdateStillFirstWins(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	first := b.approve("T-a", "h1")
	b.approve("T-a", "h2")
	s := b.state()
	if ta := s.Tasks["T-a"]; ta.ApprovedEvent != first.ID || ta.ApprovedSHA256 != "h1" {
		t.Errorf("second approval changed the task: %+v", ta)
	}
}

func TestTaskUpdatedBriefMoveTakesTheNewHash(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	b.approve("T-a", "h1")
	b.update("T-a", map[string]string{"brief": "briefs/T-a/v2.md"}, "move")
	re := b.approve("T-a", "h2")
	s := b.state()
	ta := s.Tasks["T-a"]
	if ta.Brief != "briefs/T-a/v2.md" || ta.ApprovedSHA256 != "h2" || ta.ApprovedEvent != re.ID {
		t.Errorf("task = %+v", ta)
	}
	if got := FrozenTextChangedConflicts(&s, map[string]string{"briefs/T-a/v2.md": "h2"}); len(got) != 0 {
		t.Errorf("moved brief reported: %v", got)
	}
}

func TestTaskUpdatedAfterCloseIsReported(t *testing.T) {
	b := newLog(t)
	b.draftTask("T-a")
	b.add(event.TaskDeferredType, event.RoleOrchestrator, "orch", event.KindTask, "T-a", event.TaskDeferred{Reason: "later"})
	late := b.update("T-a", map[string]string{"title": "New"}, "rename")
	b.draftTask("T-b")
	b.approve("T-b", "h")
	pend := b.update("T-b", map[string]string{"title": "New"}, "rename")
	s := b.state()
	if s.Tasks["T-a"].Title == "New" {
		t.Error("update applied to a deferred task")
	}
	obs := Observed{Tasks: map[string]TaskObservation{"T-b": {PR: PRMerged}}}
	got := UpdateAfterCloseConflicts(&s, obs)
	if len(got) != 2 || got[0].Events[0] != late.ID || got[1].Events[0] != pend.ID {
		t.Errorf("conflicts = %+v", got)
	}
	if acts := LeadActions(&s, obs, t0, nil); len(acts) != 0 {
		t.Errorf("done task still offered: %+v", acts)
	}
	if got := UpdateAfterCloseConflicts(&s, Observed{}); len(got) != 1 {
		t.Errorf("pending on an open task reported: %+v", got)
	}
}

func TestTaskUpdatedLaneAndValidation(t *testing.T) {
	for _, role := range event.Roles {
		e := event.Event{Type: event.TaskUpdatedType, Actor: event.Actor{Role: role, ID: "x"},
			Subject: event.Subject{Kind: event.KindTask, ID: "T-a"}, Data: json.RawMessage(`{"title":"x","reason":"r"}`)}
		if err := CheckLane(e); (err == nil) != (role == event.RoleOrchestrator) {
			t.Errorf("TaskUpdated by %s: %v", role, err)
		}
	}
	b := newLog(t)
	b.lax = true
	b.draftTask("T-a")
	empty := ""
	bad := b.add(event.TaskUpdatedType, event.RoleOrchestrator, "orch", event.KindTask, "T-a", event.TaskUpdated{Branch: &empty, Reason: "r"})
	none := b.add(event.TaskUpdatedType, event.RoleOrchestrator, "orch", event.KindTask, "T-a", event.TaskUpdated{Reason: "r"})
	s := b.state()
	if s.Tasks["T-a"].Branch != "build/T-a" {
		t.Error("empty value applied")
	}
	var skipped []string
	for _, sk := range s.Skipped {
		skipped = append(skipped, sk.Event)
	}
	if !slices.Contains(skipped, bad.ID) || !slices.Contains(skipped, none.ID) {
		t.Errorf("skipped = %v", s.Skipped)
	}
	if bad.Validate() == nil || none.Validate() == nil {
		t.Error("Validate accepts an empty update")
	}
}

func TestTaskUpdatedOrphanStep(t *testing.T) {
	b := newLog(t)
	u := b.update("T-x", map[string]string{"title": "x"}, "r")
	s := b.state()
	got := OrphanStepConflicts(&s, b.events)
	if len(got) != 1 || got[0].Events[0] != u.ID {
		t.Errorf("got %v", got)
	}
}
