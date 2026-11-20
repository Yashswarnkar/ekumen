package legacyimport

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Mismatch is a difference between the reduced events and a hand-written
// record. Any mismatch fails the import.
type Mismatch struct {
	Record string `json:"record"`
	Field  string `json:"field"`
	Want   string `json:"want"` // the hand-written value
	Got    string `json:"got"`  // reduced from events
}

func (m Mismatch) String() string {
	return fmt.Sprintf("%s: %s: hand-written %q, events give %q", m.Record, m.Field, m.Want, m.Got)
}

// observedFromRegistry turns the registry's hand-written statuses into the
// observations the core needs (C-event-log §7 rule 2: PR and session state
// are observed, never events). done → PR merged, in_review → PR open,
// running → a running session. Every other status must then follow from the
// events alone.
func observedFromRegistry(in *Inputs) core.Observed {
	obs := core.Observed{Tasks: map[string]core.TaskObservation{}}
	for _, t := range in.Tasks {
		switch t.Status {
		case "done":
			obs.Tasks[t.ID] = core.TaskObservation{PR: core.PRMerged}
		case "in_review":
			obs.Tasks[t.ID] = core.TaskObservation{PR: core.PROpen}
		case "running":
			obs.Tasks[t.ID] = core.TaskObservation{Session: core.SessionRunning}
		}
	}
	return obs
}

// verify reduces the log with core.Reduce and compares it with every
// hand-written record: each decision's status, answer, note, decided and
// applied dates; each task's status, approval, blockers and grants; each
// contract's status and version; and every alias.
func verify(b *builder, s *core.State) []Mismatch {
	var out []Mismatch
	add := func(rec, field, want, got string) {
		if want != got {
			out = append(out, Mismatch{rec, field, want, got})
		}
	}

	for _, d := range b.in.Decisions {
		id, _ := b.ids.newID(d.ID)
		got, ok := s.Decisions[id]
		if !ok || len(got.Raises) == 0 {
			out = append(out, Mismatch{d.File, "id", id, "(missing)"})
			continue
		}
		want := d.Status
		if want == "" {
			want = core.StatusPending
		}
		add(d.File, "status", want, got.Status)
		add(d.File, "answer", d.Answer, got.Answer)
		if d.Status == "decided" {
			add(d.File, "answer_note", d.AnswerNote, got.AnswerNote)
			by := d.DecidedBy
			if by == "lead" {
				by = b.lead.ID
			}
			if by != "" {
				add(d.File, "decided_by", by, got.DecidedBy)
			}
		}
		if d.Status == "decided" || d.Status == "defaulted" {
			add(d.File, "decided_on", strings.TrimSpace(d.DecidedOn), got.DecidedOn)
		}
		if d.SupersededBy != "" {
			sb, _ := b.ids.newID(d.SupersededBy)
			add(d.File, "superseded_by", sb, got.SupersededBy)
		}
		add(d.File, "applied_on", strings.TrimSpace(d.AppliedOn), got.AppliedOn)
		add(d.File, "title", d.Title, got.Title)
		add(d.File, "tier", fmt.Sprint(d.Tier), fmt.Sprint(got.Tier))
		if id != d.ID {
			add(d.File, "alias "+d.ID, id, s.Resolve(d.ID))
		}
	}

	obs := observedFromRegistry(b.in)
	for _, t := range b.in.Tasks {
		rec := "registry.yaml task " + t.OldID
		got, ok := s.Tasks[t.ID]
		if !ok || !got.Drafted() {
			out = append(out, Mismatch{rec, "id", t.ID, "(missing)"})
			continue
		}
		add(rec, "status", t.Status, string(core.DeriveTaskStatus(s, obs, t.ID)))
		add(rec, "approved", fmt.Sprint(t.Approved != nil), fmt.Sprint(got.Approved))
		add(rec, "title", t.Title, got.Title)
		add(rec, "kind", t.Kind, got.Kind)
		wantBlocked := slices.Clone(b.refs(rec, orEmpty(t.BlockedBy)))
		gotBlocked := slices.Clone(got.BlockedBy)
		slices.Sort(wantBlocked)
		slices.Sort(gotBlocked)
		add(rec, "blocked_by", strings.Join(wantBlocked, ","), strings.Join(gotBlocked, ","))
		add(rec, "tier_grants", strings.Join(orEmpty(t.TierGrants), ","), strings.Join(got.TierGrants, ","))
		add(rec, "session", t.Session, got.Session)
		if t.ID != t.OldID {
			add(rec, "alias "+t.OldID, t.ID, s.Resolve(t.OldID))
		}
		if br := b.briefs[t.ID]; br != nil {
			add(rec, "amendments", fmt.Sprint(len(br.Amendments)), fmt.Sprint(len(got.Amendments)))
			add(rec, "handoffs", fmt.Sprint(len(br.Handoffs)), fmt.Sprint(len(got.Handoffs)))
		}
	}

	for _, c := range b.in.Contracts {
		rec := "contract " + c.ID
		got, ok := s.Contracts[c.ID]
		if !ok {
			out = append(out, Mismatch{rec, "id", c.ID, "(missing)"})
			continue
		}
		want := c.Status
		if want != core.ContractFrozen {
			want = core.ContractDraft // planned and draft are both "not frozen"
		}
		add(rec, "status", want, got.Status)
		if c.RegistryStatus != "" && c.File != "" {
			add(rec, "registry status", c.RegistryStatus, c.Status)
		}
		if c.Status == core.ContractFrozen {
			add(rec, "version", fmt.Sprint(max(c.Version, 1)), fmt.Sprint(got.Version))
		}
		for _, a := range c.Aliases {
			add(rec, "alias "+a, c.ID, s.Resolve(a))
		}
	}

	for _, sk := range s.Skipped {
		out = append(out, Mismatch{"event " + sk.Event, "reduce", "applied", "skipped: " + sk.Reason})
	}
	return out
}

// RecordProblem is a hand-written record that breaks a C-decision-record §2
// rule. It is reported, never fixed: the brief forbids editing records.
type RecordProblem struct {
	Record string `json:"record"`
	Field  string `json:"field"`
	Rule   int    `json:"rule,omitempty"`
	Text   string `json:"text"`
}

// selfExcluded is the log as an IDLookup, minus the decision being checked,
// so rule 7 ("id already in use") does not trip on the record itself.
type selfExcluded struct {
	s    *core.State
	self string
}

func (x selfExcluded) Has(id string) bool {
	if x.s.Resolve(id) == x.self {
		return false
	}
	return x.s.Has(id)
}

func (x selfExcluded) Resolve(id string) string { return x.s.Resolve(id) }

// checkRecords validates every imported DecisionRaised against
// C-decision-record §2.
func checkRecords(s *core.State, events []event.Event, sources map[string]string) []RecordProblem {
	var out []RecordProblem
	for _, e := range events {
		if e.Type != event.DecisionRaisedType {
			continue
		}
		p, err := e.Payload()
		if err != nil {
			continue
		}
		rec := *p.(*event.DecisionRaised)
		err = core.ValidateDecision(rec, selfExcluded{s, e.Subject.ID})
		var ve core.ValidationError
		if errors.As(err, &ve) {
			for _, fe := range ve {
				out = append(out, RecordProblem{Record: sources[e.ID], Field: fe.Field, Rule: fe.Rule, Text: fe.Msg})
			}
		}
	}
	return out
}

// lanesAllowedByV3 reports whether a lane conflict found by today's core is
// an event C-event-log v3 allows: v3 lets the lead and the orchestrator
// raise decisions (D-lane-gaps), which internal/core (built on v1) does not
// yet know.
func lanesAllowedByV3(s *core.State, c core.Conflict, byID map[string]event.Event) bool {
	if c.Rule != "lane" || len(c.Events) != 1 {
		return false
	}
	e, ok := byID[c.Events[0]]
	if !ok || e.Type != event.DecisionRaisedType {
		return false
	}
	switch e.Actor.Role {
	case event.RoleLead, event.RoleOrchestrator, event.RoleLoop:
		return true
	}
	return false
}
