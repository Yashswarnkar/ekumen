package core

import (
	"slices"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// TaskStatus is a derived task status (spine §3). It is never stored.
type TaskStatus string

// Task statuses (spine §3).
const (
	TaskDraft     TaskStatus = "draft"
	TaskApproved  TaskStatus = "approved"
	TaskBlocked   TaskStatus = "blocked"
	TaskReady     TaskStatus = "ready"
	TaskRunning   TaskStatus = "running"
	TaskInReview  TaskStatus = "in_review"
	TaskDone      TaskStatus = "done"
	TaskDeferred  TaskStatus = "deferred"
	taskStatusNil TaskStatus = ""
)

// PRState is an observed pull request state.
type PRState string

// Observed PR states. PRNone means no PR was found.
const (
	PRNone   PRState = ""
	PRDraft  PRState = "draft"
	PROpen   PRState = "open" // open and ready for review
	PRMerged PRState = "merged"
	PRClosed PRState = "closed" // closed without merging
)

// SessionStatus is an observed session status (spine §6, agent layer).
type SessionStatus string

// Observed session statuses. SessionNone means no session was found.
const (
	SessionNone       SessionStatus = ""
	SessionRunning    SessionStatus = "running"
	SessionIdle       SessionStatus = "idle"
	SessionNeedsInput SessionStatus = "needs_input"
	SessionDone       SessionStatus = "done"
	SessionUnknown    SessionStatus = "unknown"
)

// TaskObservation is what the caller observed about one task this pass.
type TaskObservation struct {
	PR      PRState       `json:"pr"`
	Session SessionStatus `json:"session"`
}

// Observed holds facts read fresh on each pass (PR and session state). They
// are inputs, never events (C-event-log §7 rule 2). A task missing from
// Tasks has no PR and no session.
type Observed struct {
	Tasks map[string]TaskObservation `json:"tasks"`
}

func (o Observed) task(id string) TaskObservation { return o.Tasks[id] }

// DeriveTaskStatus returns a task's status from events plus observations.
// The first matching rule wins:
//
//  1. PR merged                                        → done
//  2. TaskDeferred                                     → deferred
//  3. not approved (or never drafted)                  → draft
//  4. PR open for review                               → in_review
//  5. draft PR, or session running/idle/needs_input    → running
//  6. any open blocker (see OpenBlockers)              → blocked
//  7. branch or worktree not set, so it can't start    → approved
//  8. otherwise                                        → ready
//
// Rule 7 is this package's reading of the spine's `approved` status, raised
// as decision draft D-task-status-approved. An unknown id gives "".
func DeriveTaskStatus(s *State, obs Observed, id string) TaskStatus {
	return deriveTaskStatus(s, obs, s.Resolve(id), map[string]bool{})
}

func deriveTaskStatus(s *State, obs Observed, id string, visiting map[string]bool) TaskStatus {
	t, ok := s.Tasks[id]
	if !ok {
		return taskStatusNil
	}
	o := obs.task(id)
	switch {
	case o.PR == PRMerged:
		return TaskDone
	case t.Deferred:
		return TaskDeferred
	case !t.Approved || !t.Drafted():
		return TaskDraft
	case o.PR == PROpen:
		return TaskInReview
	case o.PR == PRDraft || o.Session == SessionRunning || o.Session == SessionIdle || o.Session == SessionNeedsInput:
		return TaskRunning
	case len(openBlockers(s, obs, id, visiting)) > 0:
		return TaskBlocked
	case t.Branch == "" || t.Worktree == "":
		return TaskApproved
	}
	return TaskReady
}

// TaskStatuses derives the status of every task.
func TaskStatuses(s *State, obs Observed) map[string]TaskStatus {
	out := make(map[string]TaskStatus, len(s.Tasks))
	for id := range s.Tasks {
		out[id] = DeriveTaskStatus(s, obs, id)
	}
	return out
}

// OpenBlockers lists, sorted, the ids still blocking a task:
//   - each id in blocked_by that is not cleared: a task that is not done,
//     a decision that is neither applied nor superseded, a contract that is
//     not frozen, an external dependency that is not resolved, or an id that
//     names nothing (it can never clear);
//   - each decision whose `blocks` names the task and that is neither applied
//     nor superseded (C-decision-record §1, §3);
//   - each unresolved external dependency whose `blocks` names the task.
//
// A superseded decision stops blocking: it is never applied, and the
// decision that supersedes it carries the gate (raised as part of decision
// draft D-blocker-semantics). Task blockers are followed through their own
// status; a cycle counts as open.
func OpenBlockers(s *State, obs Observed, id string) []string {
	return openBlockers(s, obs, s.Resolve(id), map[string]bool{})
}

func openBlockers(s *State, obs Observed, id string, visiting map[string]bool) []string {
	t, ok := s.Tasks[id]
	if !ok {
		return nil
	}
	if visiting[id] {
		return []string{id} // cycle
	}
	visiting[id] = true
	defer delete(visiting, id)

	var open []string
	for _, raw := range t.BlockedBy {
		b := s.Resolve(raw)
		if !blockerCleared(s, obs, b, visiting) {
			open = appendUnique(open, b)
		}
	}
	for _, did := range sortedKeys(s.Decisions) {
		d := s.Decisions[did]
		if decisionGates(d) && slices.ContainsFunc(d.Blocks, func(b string) bool { return s.Resolve(b) == id }) {
			open = appendUnique(open, did)
		}
	}
	for _, xid := range sortedKeys(s.Externals) {
		x := s.Externals[xid]
		if !x.Resolved && slices.ContainsFunc(x.Blocks, func(b string) bool { return s.Resolve(b) == id }) {
			open = appendUnique(open, xid)
		}
	}
	slices.Sort(open)
	return open
}

// decisionGates reports whether a raised decision still gates its tasks.
func decisionGates(d *Decision) bool {
	return len(d.Raises) > 0 && d.AppliedOn == "" && d.Status != StatusSuperseded
}

func blockerCleared(s *State, obs Observed, id string, visiting map[string]bool) bool {
	switch event.KindOf(id) {
	case event.KindTask:
		if visiting[id] {
			return false
		}
		return deriveTaskStatus(s, obs, id, visiting) == TaskDone
	case event.KindDecision:
		d, ok := s.Decisions[id]
		return ok && len(d.Raises) > 0 && !decisionGates(d)
	case event.KindContract:
		c, ok := s.Contracts[id]
		return ok && c.Status == ContractFrozen
	case event.KindExternal:
		x, ok := s.Externals[id]
		return ok && x.Resolved
	}
	return false
}
