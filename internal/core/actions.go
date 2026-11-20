package core

import (
	"fmt"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Lead action kinds (C-cli-json v3 InboxItem.action): the lead's next moves
// that are not decisions (T-audit finding F-2026-11-20-2).
const (
	ActionApprove = "approve" // a drafted task awaiting approval
	ActionFreeze  = "freeze"  // a draft contract, or a recorded new version, awaiting freeze
	ActionApply   = "apply"   // a decided decision not applied after a day
	ActionBrief   = "brief"   // a drafted task with no brief.md (the orchestrator's)
)

// ApplyAfter is how long a decided or defaulted decision may wait for
// `decision apply` before it shows up as a lead action.
const ApplyAfter = 24 * time.Hour

// LeadAction is one next move for the lead.
type LeadAction struct {
	Action  string        `json:"action"`
	Subject event.Subject `json:"subject"`
	// Event is the event the action follows from (the draft, the version
	// or the decision), for its time.
	Event  string `json:"event"`
	Reason string `json:"reason"`
	// Command is the ekumen command that takes the action.
	Command string `json:"command"`
}

// LeadActions lists the lead's next moves, in this order (each by id):
//
//  1. approve: tasks drafted, not approved or deferred, whose brief exists
//     (hasBrief) and that have no open blockers (OpenBlockers);
//  2. freeze: draft contracts with no open blockers (ContractOpenBlockers),
//     and frozen contracts with a newer version recorded by
//     ContractVersioned that is not frozen yet;
//  3. apply: decided or defaulted decisions not applied ApplyAfter or more
//     after they were decided. The command runs the loop, which applies
//     every such decision.
//
// They rank after decisions in the lead's inbox (C-cli-json v3). Applying
// is the loop's lane: `ekumen reconcile` as loop:<session>.
func LeadActions(s *State, obs Observed, now time.Time, hasBrief func(taskID string) bool) []LeadAction {
	out := []LeadAction{}
	for _, id := range sortedKeys(s.Tasks) {
		t := s.Tasks[id]
		if !t.Drafted() || t.Approved || t.Deferred || (hasBrief != nil && !hasBrief(id)) {
			continue
		}
		if len(OpenBlockers(s, obs, id)) > 0 {
			continue
		}
		out = append(out, LeadAction{Action: ActionApprove, Subject: event.Subject{Kind: event.KindTask, ID: id},
			Event: t.Drafts[0].Event, Reason: "drafted with a brief and no open blockers",
			Command: "ekumen task approve " + id})
	}
	for _, id := range sortedKeys(s.Contracts) {
		c := s.Contracts[id]
		sub := event.Subject{Kind: event.KindContract, ID: id}
		switch {
		case c.DraftedEvent == "", c.Path == "": // nothing to freeze
		case c.Status == ContractDraft && len(ContractOpenBlockers(s, obs, id)) == 0:
			out = append(out, LeadAction{Action: ActionFreeze, Subject: sub, Event: c.DraftedEvent,
				Reason: "draft contract with no open blockers", Command: "ekumen contract freeze " + id})
		case c.Pending != nil && c.Status == ContractFrozen && len(ContractOpenBlockers(s, obs, id)) == 0:
			out = append(out, LeadAction{Action: ActionFreeze, Subject: sub, Event: c.Pending.Event,
				Reason:  fmt.Sprintf("version %d recorded by %s, not frozen yet", c.Pending.Version, c.Pending.ByDecision),
				Command: "ekumen contract freeze " + id})
		}
	}
	for _, id := range sortedKeys(s.Decisions) {
		d := s.Decisions[id]
		if len(d.Raises) == 0 || d.AppliedOn != "" || d.DecidedEvent == "" ||
			(d.Status != StatusDecided && d.Status != StatusDefaulted) {
			continue
		}
		at, err := event.ULIDTime(d.DecidedEvent)
		if err != nil || now.Sub(at) < ApplyAfter {
			continue
		}
		out = append(out, LeadAction{Action: ActionApply, Subject: event.Subject{Kind: event.KindDecision, ID: id},
			Event: d.DecidedEvent, Reason: fmt.Sprintf("%s on %s, not applied yet", d.Status, d.DecidedOn),
			Command: "ekumen reconcile --as loop:<session>"})
	}
	return out
}

// OrchestratorActions lists the orchestrator's next moves that are not
// routed entries, by id: brief, for each task drafted, not approved or
// deferred, whose brief.md does not exist (hasBrief), so it can never
// reach the lead's approve action. Like LeadActions, they are derived,
// not stored.
func OrchestratorActions(s *State, hasBrief func(taskID string) bool) []LeadAction {
	out := []LeadAction{}
	for _, id := range sortedKeys(s.Tasks) {
		t := s.Tasks[id]
		if !t.Drafted() || t.Approved || t.Deferred || hasBrief == nil || hasBrief(id) {
			continue
		}
		out = append(out, LeadAction{Action: ActionBrief, Subject: event.Subject{Kind: event.KindTask, ID: id},
			Event: t.Drafts[0].Event, Reason: "drafted without a brief",
			Command: "write .ekumen/briefs/" + id + "/brief.md"})
	}
	return out
}
