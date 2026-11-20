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
	// Update is the pending update an approve action accepts, if any.
	Update *PendingUpdate `json:"update,omitempty"`
}

// LeadActions lists the lead's next moves, in this order (each by id):
//
//  1. approve: tasks drafted, not approved or deferred, whose brief exists
//     (hasBrief) and that have no open blockers (OpenBlockers); then
//     approved tasks, not deferred or done, with a pending update
//     (D-placeholder-task-fields), which `task approve` accepts;
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
	for _, id := range sortedKeys(s.Tasks) {
		t := s.Tasks[id]
		if !t.Approved || t.PendingUpdate == nil || t.Deferred || obs.task(id).PR == PRMerged {
			continue
		}
		u := t.PendingUpdate
		out = append(out, LeadAction{Action: ActionApprove, Subject: event.Subject{Kind: event.KindTask, ID: id},
			Event: u.Events[len(u.Events)-1], Reason: "update proposed after approval",
			Command: "ekumen task approve " + id, Update: u})
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

// BriefLookup says where a task's brief.md is (as a path to show), whether
// it exists, and err when the recorded brief path is refused.
type BriefLookup func(taskID string) (path string, exists bool, err error)

// OrchestratorActions lists the orchestrator's next moves that are not
// routed entries, by id: brief, for each task drafted, not approved,
// deferred or done, whose brief does not exist, so it can never reach the
// lead's approve action. A refused recorded brief path is named in the
// reason. Like LeadActions, they are derived, not stored.
func OrchestratorActions(s *State, obs Observed, brief BriefLookup) []LeadAction {
	out := []LeadAction{}
	if brief == nil {
		return out
	}
	for _, id := range sortedKeys(s.Tasks) {
		t := s.Tasks[id]
		if !t.Drafted() || t.Approved || t.Deferred || obs.task(id).PR == PRMerged {
			continue
		}
		path, exists, err := brief(id)
		if exists && err == nil {
			continue
		}
		a := LeadAction{Action: ActionBrief, Subject: event.Subject{Kind: event.KindTask, ID: id},
			Event: t.Drafts[0].Event, Reason: "drafted without a brief", Command: "write " + path}
		if err != nil {
			a.Reason = fmt.Sprintf("drafted without a usable brief: the recorded brief path is refused (%v)", err)
			a.Command = "write .ekumen/briefs/" + id + "/brief.md, and fix the recorded brief path"
		}
		out = append(out, a)
	}
	return out
}
