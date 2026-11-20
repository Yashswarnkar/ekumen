package core

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Conflict rules (C-event-log §5).
const (
	RuleLane              = "lane"
	RuleRole              = "role"
	RuleDecidedTwice      = "decided-twice"
	RuleFrozenTwice       = "frozen-twice"
	RuleFrozenTextChanged = "frozen-text-changed"
	RuleIDCollision       = "id-collision"
	// RuleOrphanStep: a lifecycle step whose prerequisite never exists, e.g.
	// an apply with no decide (C-event-log v5, D-imported-event-time-order).
	RuleOrphanStep = "orphan-step"
	// RuleSpineUnapproved: spine.md does not match the latest SpineApproved
	// (C-event-log v5, D-post-switch-hand-edits).
	RuleSpineUnapproved = "spine-unapproved"
)

// SpineFile is the spine's path relative to .ekumen/. Its hash is passed in
// ConflictInputs.TextSHA256 under this key.
const SpineFile = "spine.md"

// ConflictRules lists every rule in the order of C-event-log §5.
var ConflictRules = []string{RuleLane, RuleRole, RuleDecidedTwice, RuleFrozenTwice, RuleFrozenTextChanged, RuleIDCollision,
	RuleOrphanStep, RuleSpineUnapproved}

// Conflict is a contradiction to report with ConflictDetected {rule,
// events}. Detail explains it for the decision the loop raises.
type Conflict struct {
	Rule   string   `json:"rule"`
	Events []string `json:"events"` // sorted
	Detail string   `json:"detail"`
}

// ConflictInputs are the observed facts the conflict rules need. The core
// never reads files or checks processes; the caller supplies them.
type ConflictInputs struct {
	// TextSHA256 maps a path as recorded in events (a brief's §A, a
	// contract body), or SpineFile, to the sha256 of its current text. A
	// path that is missing is not checked.
	TextSHA256 map[string]string
	// AliveSessions says which sessions are alive. Nil means "unknown":
	// every claim counts as live.
	AliveSessions map[string]bool
}

// Conflicts runs every rule of C-event-log §5 and returns the conflicts not
// already recorded by a ConflictDetected in s, in the rule order of §5. s is the state of the whole log; events are the events to
// lane-check (normally the whole log, or the ones new this pass). Events that
// don't contradict are never conflicts: e.g. two questions at once both
// stand.
func Conflicts(s *State, events []event.Event, in ConflictInputs) []Conflict {
	var all []Conflict
	all = append(all, LaneConflicts(s, events)...)
	all = append(all, RoleConflicts(s, in.AliveSessions)...)
	all = append(all, DecidedTwiceConflicts(s)...)
	all = append(all, FrozenTwiceConflicts(s)...)
	all = append(all, FrozenTextChangedConflicts(s, in.TextSHA256)...)
	all = append(all, IDCollisionConflicts(s)...)
	all = append(all, OrphanStepConflicts(s, events)...)
	all = append(all, SpineUnapprovedConflicts(s, in.TextSHA256)...)

	seen := map[string]bool{}
	for _, r := range s.Conflicts {
		seen[conflictKey(r.Rule, r.Events)] = true
	}
	out := []Conflict{}
	for _, c := range all {
		k := conflictKey(c.Rule, c.Events)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, c)
	}
	return out
}

func conflictKey(rule string, events []string) string {
	evs := slices.Clone(events)
	slices.Sort(evs)
	return rule + "|" + strings.Join(evs, ",")
}

func newConflict(rule, detail string, events ...string) Conflict {
	evs := slices.Clone(events)
	slices.Sort(evs)
	return Conflict{Rule: rule, Events: slices.Compact(evs), Detail: detail}
}

// LaneConflicts is rule `lane`: an event written outside its lane, for
// example one that arrived through a git merge and bypassed the CLI. It uses
// CheckLaneIn against the whole-log state. Events of unknown types are
// skipped (C-event-log §9).
func LaneConflicts(s *State, events []event.Event) []Conflict {
	var out []Conflict
	for _, e := range SortEvents(events) {
		if !e.Type.Known() {
			continue
		}
		if err := CheckLaneIn(s, e); err != nil {
			out = append(out, newConflict(RuleLane, err.Error(), e.ID))
		}
	}
	return out
}

// RoleConflicts is rule `role`: two live claims on one role by different
// sessions. alive nil counts every claim as live.
func RoleConflicts(s *State, alive map[string]bool) []Conflict {
	byRole := map[event.Role][]Claim{}
	for _, c := range s.Claims {
		if alive == nil || alive[c.Session] {
			byRole[c.Role] = append(byRole[c.Role], c)
		}
	}
	var out []Conflict
	for _, role := range sortedKeys(byRole) {
		claims := byRole[role]
		var sessions, evs []string
		for _, c := range claims {
			sessions = appendUnique(sessions, c.Session)
			evs = append(evs, c.Event)
		}
		if len(sessions) > 1 {
			out = append(out, newConflict(RuleRole, fmt.Sprintf("role %q claimed by %v", role, sessions), evs...))
		}
	}
	return out
}

// DecidedTwiceConflicts is rule `decided-twice`: two DecisionDecided with
// different answers for one decision. Answers differ when the option id
// differs, or both are `other` with different notes.
func DecidedTwiceConflicts(s *State) []Conflict {
	var out []Conflict
	for _, id := range sortedKeys(s.Decisions) {
		as := s.Decisions[id].Answers
		var evs []string
		for i := range as {
			for j := i + 1; j < len(as); j++ {
				if as[i].Answer != as[j].Answer || (as[i].Answer == event.AnswerOther && as[i].Note != as[j].Note) {
					evs = appendUnique(evs, as[i].Event, as[j].Event)
				}
			}
		}
		if len(evs) > 0 {
			out = append(out, newConflict(RuleDecidedTwice, fmt.Sprintf("decision %s decided with different answers", id), evs...))
		}
	}
	return out
}

// FrozenTwiceConflicts is rule `frozen-twice`: two ContractFrozen for one
// version with different text hashes.
func FrozenTwiceConflicts(s *State) []Conflict {
	var out []Conflict
	for _, id := range sortedKeys(s.Contracts) {
		byVersion := map[int][]Freeze{}
		var versions []int
		for _, f := range s.Contracts[id].Freezes {
			if _, ok := byVersion[f.Version]; !ok {
				versions = append(versions, f.Version)
			}
			byVersion[f.Version] = append(byVersion[f.Version], f)
		}
		slices.Sort(versions)
		for _, v := range versions {
			fs := byVersion[v]
			var evs []string
			for i := range fs {
				for j := i + 1; j < len(fs); j++ {
					if fs[i].TextSHA256 != fs[j].TextSHA256 {
						evs = appendUnique(evs, fs[i].Event, fs[j].Event)
					}
				}
			}
			if len(evs) > 0 {
				out = append(out, newConflict(RuleFrozenTwice, fmt.Sprintf("contract %s version %d frozen with different text", id, v), evs...))
			}
		}
	}
	return out
}

// FrozenTextChangedConflicts is rule `frozen-text-changed`: the current
// text of an approved §A (the task's brief path) or of a frozen contract
// (its path) no longer matches the hash recorded by TaskApproved, or by the
// latest frozen version of the contract (C-event-log v5). A contract whose
// text matches a newer version recorded by ContractVersioned and not yet
// frozen is not a conflict: it waits for `contract freeze`. Hashes come
// from the caller; paths it doesn't supply are not checked.
func FrozenTextChangedConflicts(s *State, current map[string]string) []Conflict {
	var out []Conflict
	for _, id := range sortedKeys(s.Tasks) {
		t := s.Tasks[id]
		if !t.Approved || t.Brief == "" {
			continue
		}
		if h, ok := current[t.Brief]; ok && h != t.ApprovedSHA256 {
			out = append(out, newConflict(RuleFrozenTextChanged,
				fmt.Sprintf("task %s: %s changed since approval", id, t.Brief), t.ApprovedEvent))
		}
	}
	for _, id := range sortedKeys(s.Contracts) {
		c := s.Contracts[id]
		if c.Status != ContractFrozen || c.Path == "" {
			continue
		}
		if h, ok := current[c.Path]; ok && h != c.TextSHA256 && (c.Pending == nil || h != c.Pending.TextSHA256) {
			out = append(out, newConflict(RuleFrozenTextChanged,
				fmt.Sprintf("contract %s: %s changed since version %d was frozen", id, c.Path, c.Version), c.freezeEvent()))
		}
	}
	return out
}

// freezeEvent returns the event that recorded the current frozen version.
func (c *Contract) freezeEvent() string {
	for _, f := range c.Freezes {
		if f.Version == c.Version && f.TextSHA256 == c.TextSHA256 {
			return f.Event
		}
	}
	return ""
}

// IDCollisionConflicts is rule `id-collision`: two DecisionRaised or two
// TaskDrafted for the same id from different writers (actor role and id).
// The same writer repeating itself is not a collision.
func IDCollisionConflicts(s *State) []Conflict {
	var out []Conflict
	check := func(kind, id string, ws []Writer) {
		var writers, evs []string
		for _, w := range ws {
			writers = appendUnique(writers, string(w.Actor.Role)+":"+w.Actor.ID)
			evs = append(evs, w.Event)
		}
		if len(writers) > 1 {
			out = append(out, newConflict(RuleIDCollision, fmt.Sprintf("%s %s created by %v", kind, id, writers), evs...))
		}
	}
	for _, id := range sortedKeys(s.Decisions) {
		check("decision", id, s.Decisions[id].Raises)
	}
	for _, id := range sortedKeys(s.Tasks) {
		check("task", id, s.Tasks[id].Drafts)
	}
	return out
}

// OrphanStepConflicts is rule `orphan-step` (C-event-log v5,
// D-imported-event-time-order): an event whose lifecycle prerequisite is
// nowhere in the log. The reducer orders steps causally within a subject,
// so a step is never dropped for arriving "too early"; one whose
// prerequisite never exists is reported here instead of being ignored:
//
//   - any task event other than TaskDrafted (and questions on a task) with
//     no TaskDrafted;
//   - any decision event other than DecisionRaised with no DecisionRaised,
//     and DecisionApplied with no DecisionDecided or DecisionDefaulted;
//   - any contract event other than ContractDrafted with no ContractDrafted;
//   - ExternalUpdated or ExternalResolved with no ExternalAdded;
//   - QuestionAnswered for a question never asked on that subject.
//
// Routing, role and conflict events (RoutedToOwner, RouteResolved,
// ConflictDetected, and the project-wide ones) have no prerequisite here: a
// route may be resolved before it is written (State.EarlyResolutions).
// Subject ids are resolved through aliases. Conflicts come out ordered by
// event id; events need not be sorted.
func OrphanStepConflicts(s *State, events []event.Event) []Conflict {
	var out []Conflict
	for _, e := range events {
		switch {
		case !e.Type.Known(), e.Type == event.RoutedToOwnerType, e.Type == event.RouteResolvedType,
			e.Type == event.ConflictDetectedType:
			continue
		}
		id := s.Resolve(e.Subject.ID)
		missing := ""
		switch e.Subject.Kind {
		case event.KindTask:
			if t, ok := s.Tasks[id]; e.Type != event.TaskDraftedType && (!ok || !t.Drafted()) {
				missing = "TaskDrafted"
			}
		case event.KindDecision:
			d, ok := s.Decisions[id]
			switch {
			case e.Type != event.DecisionRaisedType && (!ok || len(d.Raises) == 0):
				missing = "DecisionRaised"
			case e.Type == event.DecisionAppliedType && d.DecidedEvent == "":
				missing = "DecisionDecided or DecisionDefaulted"
			}
		case event.KindContract:
			if c, ok := s.Contracts[id]; e.Type != event.ContractDraftedType && (!ok || c.DraftedEvent == "") {
				missing = "ContractDrafted"
			}
		case event.KindExternal:
			if x, ok := s.Externals[id]; e.Type != event.ExternalAddedType && (!ok || x.AddedEvent == "") {
				missing = "ExternalAdded"
			}
		}
		if missing == "" && e.Type == event.QuestionAnsweredType {
			if p, err := e.Payload(); err == nil {
				if _, ok := s.FindQuestion(e.Subject, p.(*event.QuestionAnswered).QuestionID); !ok {
					missing = "QuestionAsked"
				}
			}
		}
		if missing != "" {
			out = append(out, newConflict(RuleOrphanStep,
				fmt.Sprintf("%s %s on %s %s has no %s", e.Type, e.ID, e.Subject.Kind, e.Subject.ID, missing), e.ID))
		}
	}
	slices.SortFunc(out, func(a, b Conflict) int { return strings.Compare(a.Events[0], b.Events[0]) })
	return slices.CompactFunc(out, func(a, b Conflict) bool { return a.Events[0] == b.Events[0] })
}

// SpineUnapprovedConflicts is rule `spine-unapproved` (C-event-log v5,
// D-post-switch-hand-edits): the spine's current text does not match the
// latest SpineApproved. Before the first approval there is nothing to
// compare against, so nothing is reported; nor when the caller does not
// supply the spine's hash.
func SpineUnapprovedConflicts(s *State, current map[string]string) []Conflict {
	if len(s.SpineApprovals) == 0 {
		return nil
	}
	h, ok := current[SpineFile]
	if !ok {
		return nil
	}
	last := s.SpineApprovals[len(s.SpineApprovals)-1]
	if h == last.TextSHA256 {
		return nil
	}
	return []Conflict{newConflict(RuleSpineUnapproved,
		fmt.Sprintf("%s changed since the lead approved it (run `ekumen spine approve` once the lead has)", SpineFile), last.Event)}
}
