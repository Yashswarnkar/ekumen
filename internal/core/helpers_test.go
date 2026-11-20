package core

import (
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

var t0 = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)

// logBuilder makes events with increasing, deterministic ULIDs.
type logBuilder struct {
	t      *testing.T
	n      int
	events []event.Event
	lax    bool // allow invalid envelopes, to test the reducer on them
}

func newLog(t *testing.T) *logBuilder { return &logBuilder{t: t} }

func (b *logBuilder) add(typ event.Type, role event.Role, actor string, kind event.SubjectKind, subject string, data any) event.Event {
	b.t.Helper()
	b.n++
	at := t0.Add(time.Duration(b.n) * time.Minute)
	var ent [10]byte
	ent[9] = byte(b.n)
	id, err := event.ULIDFromParts(uint64(at.UnixMilli()), ent)
	if err != nil {
		b.t.Fatal(err)
	}
	e, err := event.New(id, typ, at, event.Actor{Role: role, ID: actor}, event.Subject{Kind: kind, ID: subject}, data)
	if err != nil {
		b.t.Fatal(err)
	}
	if err := e.Validate(); err != nil && !b.lax {
		b.t.Fatalf("builder made an invalid event: %v", err)
	}
	b.events = append(b.events, e)
	return e
}

func (b *logBuilder) state() State { return Reduce(b.events) }

// Shorthands for common events.

func (b *logBuilder) draftTask(id string, blockedBy ...string) event.Event {
	return b.add(event.TaskDraftedType, event.RoleOrchestrator, "orch", event.KindTask, id, event.TaskDrafted{
		Title: id, Kind: event.TaskKindBuild, Brief: ".ekumen/briefs/" + id + "/brief.md", Repo: "ekumen",
		BaseBranch: "main", Branch: "build/" + id, Worktree: ".worktrees/" + id,
		BlockedBy: orEmpty(blockedBy), TierGrants: []string{"push_own_branch"},
	})
}

func (b *logBuilder) approve(id, sha string) event.Event {
	return b.add(event.TaskApprovedType, event.RoleLead, "lead", event.KindTask, id,
		event.TaskApproved{TierGrants: []string{"push_own_branch"}, TextSHA256: sha})
}

func (b *logBuilder) attach(task, session string) event.Event {
	return b.add(event.TaskSessionAttachedType, event.RoleLoop, "loop", event.KindTask, task,
		event.TaskSessionAttached{Session: session, Adapter: "test"})
}

func (b *logBuilder) draftContract(id string) event.Event {
	return b.add(event.ContractDraftedType, event.RoleOrchestrator, "orch", event.KindContract, id,
		event.ContractDrafted{Title: id, Path: ".ekumen/contracts/" + id + ".md"})
}

func (b *logBuilder) freeze(id string, version int, sha string) event.Event {
	return b.add(event.ContractFrozenType, event.RoleLead, "lead", event.KindContract, id,
		event.ContractFrozen{Version: version, TextSHA256: sha})
}

func ptr(s string) *string { return &s }

// record returns a valid decision record raised by session for task.
func record(id, task, session string, tier int) event.DecisionRaised {
	r := event.DecisionRaised{
		ID: id, Title: "Pick one", Tier: tier,
		RaisedBy: event.RaisedBy{Task: ptr(task), Session: session, On: "2026-11-20"},
		Question: "Which?",
		Options:  []event.Option{{ID: "a", Tradeoff: "A"}, {ID: "b", Tradeoff: "B"}},
		Blocks:   []string{}, ContractImpact: []string{},
		Context: event.DecisionContext{Summary: "Because."},
	}
	if tier == 1 {
		r.DefaultIfNoAnswer, r.Deadline = ptr("a"), ptr("2026-11-27")
	}
	return r
}

func (b *logBuilder) raise(rec event.DecisionRaised, session string) event.Event {
	return b.add(event.DecisionRaisedType, event.RoleSession, session, event.KindDecision, rec.ID, rec)
}

func (b *logBuilder) decide(id, answer string) event.Event {
	return b.add(event.DecisionDecidedType, event.RoleLead, "lead", event.KindDecision, id,
		event.DecisionDecided{Answer: answer})
}

func (b *logBuilder) apply(id string) event.Event {
	return b.add(event.DecisionAppliedType, event.RoleLoop, "loop", event.KindDecision, id, event.DecisionApplied{})
}
