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
	return newDeriver(s, obs).taskStatus(s.Resolve(id))
}

// deriver derives statuses for one call. cleared memoizes blockerCleared
// by id; onStack marks the tasks and contracts whose blockers are being
// listed, so a cycle counts as open. A result computed while a cycle was
// on the stack is still safe to keep: an item is only reported cleared
// when every blocker it depends on is, which no member of a cycle is.
type deriver struct {
	s       *State
	obs     Observed
	cleared map[string]bool
	onStack map[string]bool
}

func newDeriver(s *State, obs Observed) *deriver {
	return &deriver{s: s, obs: obs, cleared: map[string]bool{}, onStack: map[string]bool{}}
}

func (d *deriver) taskStatus(id string) TaskStatus {
	t, ok := d.s.Tasks[id]
	if !ok {
		return taskStatusNil
	}
	o := d.obs.task(id)
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
	case len(d.openBlockers(id)) > 0:
		return TaskBlocked
	case t.Branch == "" || t.Worktree == "":
		return TaskApproved
	}
	return TaskReady
}

// TaskStatuses derives the status of every task.
func TaskStatuses(s *State, obs Observed) map[string]TaskStatus {
	out := make(map[string]TaskStatus, len(s.Tasks))
	d := newDeriver(s, obs)
	for id := range s.Tasks {
		out[id] = d.taskStatus(id)
	}
	return out
}

// OpenBlockers lists, sorted, the ids still blocking a task:
//   - each id in blocked_by that is not cleared: a task that is not done,
//     a decision that is neither applied nor superseded, a contract that is
//     not frozen or still has open blockers of its own (C-event-log v4,
//     D-contract-blockers; see ContractOpenBlockers), an external dependency that is not resolved, or an id whose
//     creating event is not in the log (it can never clear);
//   - each decision whose `blocks` names the task and that is neither applied
//     nor superseded (C-decision-record §1, §3);
//   - each unresolved external dependency whose `blocks` names the task.
//
// A superseded decision stops blocking: it is never applied, and the
// decision that supersedes it carries the gate (raised as part of decision
// draft D-blocker-semantics). Task blockers are followed through their own
// status; a cycle counts as open.
func OpenBlockers(s *State, obs Observed, id string) []string {
	return newDeriver(s, obs).openBlockers(s.Resolve(id))
}

func (d *deriver) openBlockers(id string) []string {
	s := d.s
	t, ok := s.Tasks[id]
	if !ok {
		return nil
	}
	if d.onStack[id] {
		return []string{id}
	}
	d.onStack[id] = true
	defer delete(d.onStack, id)

	var open []string
	for _, raw := range t.BlockedBy {
		b := s.Resolve(raw)
		if !d.blockerCleared(b) {
			open = appendUnique(open, b)
		}
	}
	for _, did := range sortedKeys(s.Decisions) {
		dec := s.Decisions[did]
		if decisionGates(dec) && slices.ContainsFunc(dec.Blocks, func(b string) bool { return s.Resolve(b) == id }) {
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

func (d *deriver) blockerCleared(id string) bool {
	if d.onStack[id] {
		return false
	}
	if v, ok := d.cleared[id]; ok {
		return v
	}
	v := d.clear(id)
	d.cleared[id] = v
	return v
}

// clear is blockerCleared without the memo. A task is done only when its
// PR is merged (DeriveTaskStatus rule 1), so its own blockers never matter.
func (d *deriver) clear(id string) bool {
	s := d.s
	switch event.KindOf(id) {
	case event.KindTask:
		t, ok := s.Tasks[id]
		return ok && t.Drafted() && d.obs.task(id).PR == PRMerged
	case event.KindDecision:
		dec, ok := s.Decisions[id]
		return ok && len(dec.Raises) > 0 && !decisionGates(dec)
	case event.KindContract:
		c, ok := s.Contracts[id]
		if !ok || c.DraftedEvent == "" || c.Status != ContractFrozen {
			return false
		}
		return len(d.contractOpenBlockers(id)) == 0
	case event.KindExternal:
		x, ok := s.Externals[id]
		return ok && x.AddedEvent != "" && x.Resolved
	}
	return false
}

// ContractOpenBlockers lists, sorted, the ids in a contract's blocked_by
// that are not cleared (same rules as OpenBlockers). A task blocked by the
// contract stays blocked while this is non-empty, even once the contract is
// frozen (C-event-log v4 §6, D-contract-blockers). A cycle counts as open.
func ContractOpenBlockers(s *State, obs Observed, id string) []string {
	return newDeriver(s, obs).contractOpenBlockers(s.Resolve(id))
}

func (d *deriver) contractOpenBlockers(id string) []string {
	c, ok := d.s.Contracts[id]
	if !ok {
		return nil
	}
	if d.onStack[id] {
		return []string{id}
	}
	d.onStack[id] = true
	defer delete(d.onStack, id)
	var open []string
	for _, raw := range c.BlockedBy {
		b := d.s.Resolve(raw)
		if !d.blockerCleared(b) {
			open = appendUnique(open, b)
		}
	}
	slices.Sort(open)
	return open
}
