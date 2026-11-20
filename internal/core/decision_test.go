package core

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

func validationState(t *testing.T) State {
	b := newLog(t)
	b.add(event.TaskDraftedType, event.RoleOrchestrator, "orch", event.KindTask, "T-core",
		event.TaskDrafted{Title: "core", Aliases: []string{"T1"}, BlockedBy: []string{}})
	b.draftContract("C-event-log")
	b.raise(record("D-taken", "T-core", "t-core", 2), "t-core")
	return b.state()
}

// C-decision-record §1 and §2: one case per rule; the error names the field.
func TestValidateDecision_DecisionRecord2(t *testing.T) {
	s := validationState(t)
	tests := []struct {
		name  string
		edit  func(*event.DecisionRaised)
		field string
		rule  int
	}{
		{"valid tier 2", func(*event.DecisionRaised) {}, "", 0},
		{"valid tier 1", func(r *event.DecisionRaised) {
			r.Tier, r.DefaultIfNoAnswer, r.Deadline = 1, ptr("a"), ptr("2026-11-27")
		}, "", 0},
		{"C-event-log v3 §1: pre-contract alias like D-012", func(r *event.DecisionRaised) { r.Aliases = []string{"D-012"} }, "", 0},
		{"valid with alias block and contract impact", func(r *event.DecisionRaised) {
			r.Blocks, r.ContractImpact = []string{"T1"}, []string{"C-event-log"}
		}, "", 0},

		{"rule 1: tier 1 without default", func(r *event.DecisionRaised) { r.Tier, r.Deadline = 1, ptr("2026-11-27") }, "default_if_no_answer", 1},
		{"rule 1: tier 1 without deadline", func(r *event.DecisionRaised) { r.Tier, r.DefaultIfNoAnswer = 1, ptr("a") }, "deadline", 1},
		{"rule 2: tier 2 with default", func(r *event.DecisionRaised) { r.DefaultIfNoAnswer = ptr("a") }, "default_if_no_answer", 2},
		{"rule 3: contract impact at tier 1", func(r *event.DecisionRaised) {
			r.Tier, r.DefaultIfNoAnswer, r.Deadline = 1, ptr("a"), ptr("2026-11-27")
			r.ContractImpact = []string{"C-event-log"}
		}, "contract_impact", 3},
		{"rule 4: recommendation not an option", func(r *event.DecisionRaised) { r.Recommendation = ptr("z") }, "recommendation", 4},
		{"rule 4: default not an option", func(r *event.DecisionRaised) {
			r.Tier, r.DefaultIfNoAnswer, r.Deadline = 1, ptr("z"), ptr("2026-11-27")
		}, "default_if_no_answer", 4},
		{"rule 5: summary over 150 words", func(r *event.DecisionRaised) {
			r.Context.Summary = strings.Repeat("word ", 151)
		}, "context.summary", 5},
		{"rule 6: blocks a missing task", func(r *event.DecisionRaised) { r.Blocks = []string{"T-nope"} }, "blocks[0]", 6},
		{"rule 6: blocks a contract", func(r *event.DecisionRaised) { r.Blocks = []string{"C-event-log"} }, "blocks[0]", 6},
		{"rule 6: impacts a missing contract", func(r *event.DecisionRaised) { r.ContractImpact = []string{"C-nope"} }, "contract_impact[0]", 6},
		{"rule 7: bad id", func(r *event.DecisionRaised) { r.ID = "D_bad" }, "id", 7},
		{"rule 7: not a decision id", func(r *event.DecisionRaised) { r.ID = "T-core-two" }, "id", 7},
		{"rule 7: id in use", func(r *event.DecisionRaised) { r.ID = "D-taken" }, "id", 7},
		{"rule 7: alias in use", func(r *event.DecisionRaised) { r.Aliases = []string{"T1"} }, "aliases[0]", 7},
		{"rule 7: alias is an id in use", func(r *event.DecisionRaised) { r.Aliases = []string{"D-taken"} }, "aliases[0]", 7},
		{"C-event-log §1: alias with a slash", func(r *event.DecisionRaised) { r.Aliases = []string{"D/012"} }, "aliases[0]", 0},

		{"§1 title required", func(r *event.DecisionRaised) { r.Title = "" }, "title", 0},
		{"§1 title ≤ 100", func(r *event.DecisionRaised) { r.Title = strings.Repeat("é", 101) }, "title", 0},
		{"§1 tier 1 or 2", func(r *event.DecisionRaised) { r.Tier = 3 }, "tier", 0},
		{"§1 raised_by.session", func(r *event.DecisionRaised) { r.RaisedBy.Session = "" }, "raised_by.session", 0},
		{"§1 raised_by.on is a date", func(r *event.DecisionRaised) { r.RaisedBy.On = "yesterday" }, "raised_by.on", 0},
		{"§1 raised_by.task is a task", func(r *event.DecisionRaised) { r.RaisedBy.Task = ptr("C-event-log") }, "raised_by.task", 0},
		{"§1 question", func(r *event.DecisionRaised) { r.Question = " " }, "question", 0},
		{"§1 at least 2 options", func(r *event.DecisionRaised) { r.Options = r.Options[:1] }, "options", 0},
		{"§1 option id slug", func(r *event.DecisionRaised) { r.Options[1].ID = "Big B" }, "options[1].id", 0},
		{"§1 option id unique", func(r *event.DecisionRaised) { r.Options[1].ID = "a" }, "options[1].id", 0},
		{"§1 option tradeoff", func(r *event.DecisionRaised) { r.Options[0].Tradeoff = "" }, "options[0].tradeoff", 0},
		{"§1 deadline is a date", func(r *event.DecisionRaised) { r.Deadline = ptr("soon") }, "deadline", 0},
		{"§1 blocks required", func(r *event.DecisionRaised) { r.Blocks = nil }, "blocks", 0},
		{"§1 contract_impact required", func(r *event.DecisionRaised) { r.ContractImpact = nil }, "contract_impact", 0},
		{"§1 summary required", func(r *event.DecisionRaised) { r.Context.Summary = "" }, "context.summary", 0},
		{"§1 prior decisions are decisions", func(r *event.DecisionRaised) { r.Context.PriorDecisions = []string{"T-core"} }, "context.prior_decisions[0]", 0},
		{"§1 evidence not empty", func(r *event.DecisionRaised) { r.Context.Evidence = []string{""} }, "context.evidence[0]", 0},
	}
	for _, tt := range tests {
		rec := record("D-new", "T-core", "t-core", 2)
		rec.Options = slices.Clone(rec.Options)
		tt.edit(&rec)
		err := ValidateDecision(rec, &s)
		if tt.field == "" {
			if err != nil {
				t.Errorf("%s: unexpected error %v", tt.name, err)
			}
			continue
		}
		ve, ok := err.(ValidationError)
		if !ok {
			t.Errorf("%s: got %v, want a ValidationError", tt.name, err)
			continue
		}
		found := false
		for _, fe := range ve {
			if fe.Field == tt.field && fe.Rule == tt.rule {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: want field %s rule %d, got %v", tt.name, tt.field, tt.rule, err)
		}
	}
}

func TestSummaryAt150WordsIsValid_DecisionRecord2Rule5(t *testing.T) {
	s := validationState(t)
	rec := record("D-new", "T-core", "t-core", 2)
	rec.Context.Summary = strings.TrimSpace(strings.Repeat("word ", 150))
	if err := ValidateDecision(rec, &s); err != nil {
		t.Error(err)
	}
}

// C-event-log §6: DecisionRaised is "the record minus lifecycle fields", and
// its id is the subject.
func TestValidateDecisionRaised_DecisionRecord1Lifecycle(t *testing.T) {
	s := validationState(t)
	b := newLog(t)
	good := b.raise(record("D-new", "T-core", "t-core", 2), "t-core")
	if err := ValidateDecisionRaised(&s, good); err != nil {
		t.Errorf("valid event: %v", err)
	}

	withStatus := good
	var m map[string]any
	_ = json.Unmarshal(good.Data, &m)
	m["status"], m["answer"] = "decided", "a"
	withStatus.Data, _ = json.Marshal(m)
	err := ValidateDecisionRaised(&s, withStatus)
	ve, _ := err.(ValidationError)
	if !slices.Contains(ve.Fields(), "status") || !slices.Contains(ve.Fields(), "answer") {
		t.Errorf("lifecycle fields accepted: %v", err)
	}

	mismatch := good
	mismatch.Subject.ID = "D-other"
	if err := ValidateDecisionRaised(&s, mismatch); err == nil || !strings.Contains(err.Error(), "subject.id") {
		t.Errorf("id/subject mismatch: %v", err)
	}
	if ValidateDecisionRaised(&s, b.draftTask("T-z")) == nil {
		t.Error("accepted a non-DecisionRaised event")
	}
}
