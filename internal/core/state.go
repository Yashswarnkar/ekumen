// Package core is Ekumen's pure domain: lanes, decision validation, the
// reducer from events to state, and the derivations on that state (task
// status, decision priority, inboxes, routing, conflicts). It never reads
// files, the network or processes (spine invariant 9); anything observed
// (PR state, session status, file hashes, the clock) is passed in.
//
// See README.md for a map from contract sections to functions.
package core

import (
	"cmp"
	"encoding/json"
	"slices"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Decision statuses (C-decision-record §1, §3).
const (
	StatusPending    = "pending"
	StatusDecided    = "decided"
	StatusDefaulted  = "defaulted"
	StatusSuperseded = "superseded"
)

// Contract statuses (C-event-log front matter: draft | frozen).
const (
	ContractDraft  = "draft"
	ContractFrozen = "frozen"
)

// State is everything derived from the event log. Build it with Reduce.
// Maps are keyed by slug id. Id lists inside records (blocked_by, blocks,
// ...) are kept as written; use Resolve to follow aliases.
type State struct {
	Tasks     map[string]*Task     `json:"tasks"`
	Decisions map[string]*Decision `json:"decisions"`
	Contracts map[string]*Contract `json:"contracts"`
	Externals map[string]*External `json:"externals"`
	// Routes holds every routed item, open or closed, by route id.
	Routes map[string]*RouteRecord `json:"routes"`
	// Claims are the role claims not yet released, in event order.
	Claims []Claim `json:"claims"`
	// EarlyResolutions are RouteResolved events whose route has no
	// RoutedToOwner yet, by route id. When the RoutedToOwner arrives, the
	// route is closed by the resolution and it leaves this map.
	EarlyResolutions map[string]Resolution `json:"early_resolutions"`
	// Conflicts are the ConflictDetected events already written.
	Conflicts []RecordedConflict `json:"conflicts"`
	// Aliases maps an older id to its slug id (C-event-log §1).
	Aliases map[string]string `json:"aliases"`
	// SpineApprovals are the SpineApproved events, in log order
	// (C-event-log v5, D-post-switch-hand-edits).
	SpineApprovals []SpineApproval `json:"spine_approvals"`
	// Skipped lists events the reducer could not use: unknown types (C-event-log
	// §9) or payloads that do not decode.
	Skipped []SkippedEvent `json:"skipped"`
}

// SpineApproval is one SpineApproved.
type SpineApproval struct {
	Event      string `json:"event"`
	TextSHA256 string `json:"text_sha256"`
}

// Writer is one event that created an item, with who wrote it. Several
// writers for one id is the id-collision rule (C-event-log §5).
type Writer struct {
	Event string      `json:"event"`
	Actor event.Actor `json:"actor"`
}

// Entry is one §B amendment.
type Entry struct {
	Event      string `json:"event"`
	At         string `json:"at"`
	File       string `json:"file"`
	TextSHA256 string `json:"text_sha256"`
	Note       string `json:"note,omitempty"`
	ByDecision string `json:"by_decision,omitempty"`
}

// Handoff is one §C entry.
type Handoff struct {
	Event         string `json:"event"`
	At            string `json:"at"`
	By            string `json:"by"`
	File          string `json:"file"`
	TextSHA256    string `json:"text_sha256"`
	StatusLine    string `json:"status_line"`
	HasFollowups  bool   `json:"has_followups"`
	HasDecisions  bool   `json:"has_decisions"`
	HasDeviations bool   `json:"has_deviations"`
}

// Question is one QuestionAsked and its answer, if any.
type Question struct {
	ID          string `json:"id"`
	Event       string `json:"event"`
	At          string `json:"at"`
	By          string `json:"by"`
	To          string `json:"to"`
	Text        string `json:"text"`
	Answer      string `json:"answer,omitempty"`
	AnsweredBy  string `json:"answered_by,omitempty"`
	AnsweredAt  string `json:"answered_at,omitempty"`
	AnswerEvent string `json:"answer_event,omitempty"`
}

// Answered reports whether the question has an answer.
func (q Question) Answered() bool { return q.AnswerEvent != "" }

// Task is a task as derived from events. Its status is not stored; see
// TaskStatus.
type Task struct {
	ID         string   `json:"id"`
	Aliases    []string `json:"aliases,omitempty"`
	Title      string   `json:"title"`
	Kind       string   `json:"kind"`
	Brief      string   `json:"brief"`
	Repo       string   `json:"repo"`
	BaseBranch string   `json:"base_branch"`
	Branch     string   `json:"branch"`
	Worktree   string   `json:"worktree"`
	BlockedBy  []string `json:"blocked_by"`
	TierGrants []string `json:"tier_grants"`

	Drafts []Writer `json:"drafts"`

	Approved       bool   `json:"approved"`
	ApprovedEvent  string `json:"approved_event,omitempty"`
	ApprovedOn     string `json:"approved_on,omitempty"`
	ApprovedSHA256 string `json:"approved_sha256,omitempty"`

	Amendments []Entry   `json:"amendments"`
	Handoffs   []Handoff `json:"handoffs"`

	// Session is the session from the latest TaskSessionAttached, or else
	// from TaskDrafted.session. DraftSession is TaskDrafted.session.
	Session      string `json:"session,omitempty"`
	DraftSession string `json:"draft_session,omitempty"`
	Adapter      string `json:"adapter,omitempty"`

	Deferred       bool   `json:"deferred"`
	DeferredReason string `json:"deferred_reason,omitempty"`

	Questions []Question `json:"questions"`
}

// Drafted reports whether a TaskDrafted was seen for the task.
func (t *Task) Drafted() bool { return len(t.Drafts) > 0 }

// Applied is a decision's applied info (C-decision-record §1).
type Applied struct {
	AmendedTasks     []string `json:"amended_tasks"`
	UnblockedTasks   []string `json:"unblocked_tasks"`
	NotifiedSessions []string `json:"notified_sessions"`
}

// Answer is one DecisionDecided, kept for the decided-twice rule.
type Answer struct {
	Event  string `json:"event"`
	By     string `json:"by"`
	Answer string `json:"answer"`
	Note   string `json:"note,omitempty"`
}

// Decision is a decision record with its lifecycle fields
// (C-decision-record §1). The embedded record comes from the first
// DecisionRaised for the id.
type Decision struct {
	event.DecisionRaised

	Raises []Writer `json:"raises"`

	Status     string     `json:"status"`
	Questions  []Question `json:"questions"`
	Answer     string     `json:"answer,omitempty"`
	AnswerNote string     `json:"answer_note,omitempty"`
	DecidedBy  string     `json:"decided_by,omitempty"`
	DecidedOn  string     `json:"decided_on,omitempty"`
	// DecidedEvent is the DecisionDecided or DecisionDefaulted that set
	// the answer.
	DecidedEvent string   `json:"decided_event,omitempty"`
	SupersededBy string   `json:"superseded_by,omitempty"`
	AppliedOn    string   `json:"applied_on,omitempty"`
	Applied      *Applied `json:"applied,omitempty"`

	// Answers lists every DecisionDecided in event order, including ones
	// that came after the first (decided-twice, C-event-log §5).
	Answers []Answer `json:"answers"`
}

// Freeze is one ContractFrozen or ContractVersioned.
type Freeze struct {
	Event      string `json:"event"`
	Version    int    `json:"version"`
	TextSHA256 string `json:"text_sha256"`
	ByDecision string `json:"by_decision,omitempty"` // set for ContractVersioned
}

// Contract is a contract as derived from events.
type Contract struct {
	ID           string   `json:"id"`
	Aliases      []string `json:"aliases,omitempty"`
	Title        string   `json:"title"`
	Path         string   `json:"path"`
	DraftedEvent string   `json:"drafted_event,omitempty"`
	// BlockedBy are the ids the contract waits on (C-event-log v4,
	// D-contract-blockers).
	BlockedBy []string `json:"blocked_by"`
	// Status is draft until the first ContractFrozen.
	Status string `json:"status"`
	// Version and TextSHA256 are those of the highest ContractFrozen
	// (C-event-log v5: a ContractVersioned does not freeze by itself).
	Version    int    `json:"version"`
	TextSHA256 string `json:"text_sha256,omitempty"`
	// Pending is the highest ContractVersioned above Version: a new
	// version recorded by a decision and waiting for `contract freeze`
	// (D-contract-version-recording). Nil when none.
	Pending *Freeze `json:"pending,omitempty"`
	// Freezes lists ContractFrozen events; Versions lists ContractVersioned.
	Freezes   []Freeze   `json:"freezes"`
	Versions  []Freeze   `json:"versions"`
	Questions []Question `json:"questions"`
}

// External is an external dependency (spine §3).
type External struct {
	ID           string   `json:"id"`
	Aliases      []string `json:"aliases,omitempty"`
	AddedEvent   string   `json:"added_event,omitempty"`
	What         string   `json:"what"`
	OwnerTeam    string   `json:"owner_team"`
	AskedOn      string   `json:"asked_on"`
	NeededBy     string   `json:"needed_by"`
	Blocks       []string `json:"blocks"`
	Resolved     bool     `json:"resolved"`
	ResolvedNote string   `json:"resolved_note,omitempty"`
}

// RouteRecord is an item routed to an owner (C-event-log §4).
type RouteRecord struct {
	ID        string        `json:"id"`
	Event     string        `json:"event"`
	Subject   event.Subject `json:"subject"`
	To        string        `json:"to"`
	FromEvent string        `json:"from_event"`
	Reason    string        `json:"reason"`
	Open      bool          `json:"open"`
	// ClosedBy is the RouteResolved or QuestionAnswered event that closed it.
	ClosedBy string `json:"closed_by,omitempty"`
	Note     string `json:"note,omitempty"`
}

// Claim is a held role (C-event-log §3).
type Claim struct {
	Role    event.Role `json:"role"`
	Session string     `json:"session"`
	Event   string     `json:"event"`
}

// Resolution is one RouteResolved.
type Resolution struct {
	Event string `json:"event"`
	Note  string `json:"note,omitempty"`
}

// RecordedConflict is a ConflictDetected already in the log.
type RecordedConflict struct {
	Event  string   `json:"event"`
	Rule   string   `json:"rule"`
	Events []string `json:"events"`
}

// SkippedEvent is an event the reducer ignored, and why.
type SkippedEvent struct {
	Event  string `json:"event"`
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

func newState() State {
	return State{
		Tasks:     map[string]*Task{},
		Decisions: map[string]*Decision{},
		Contracts: map[string]*Contract{},
		Externals: map[string]*External{},
		Routes:    map[string]*RouteRecord{},
		Claims:    []Claim{},
		Conflicts: []RecordedConflict{},

		EarlyResolutions: map[string]Resolution{},
		Aliases:          map[string]string{},
		SpineApprovals:   []SpineApproval{},
		Skipped:          []SkippedEvent{},
	}
}

// LifecycleRank is an event type's lifecycle step (C-event-log v5 §2,
// D-imported-event-time-order): 0 drafted/raised, 1 approved/frozen/asked,
// 2 amended/answered/decided/defaulted/superseded/handoff, 3 applied.
// Types the decision does not name sit where their lifecycle puts them
// (see the table in README.md). Unknown types come last.
func LifecycleRank(t event.Type) int {
	switch t {
	case event.TaskDraftedType, event.DecisionRaisedType, event.ContractDraftedType, event.ExternalAddedType:
		return 0
	case event.TaskApprovedType, event.ContractFrozenType, event.QuestionAskedType,
		event.TaskSessionAttachedType, event.RoleClaimedType, event.SpineApprovedType:
		return 1
	case event.TaskAmendedType, event.QuestionAnsweredType, event.DecisionDecidedType,
		event.DecisionDefaultedType, event.DecisionSupersededType, event.TaskBlockersChangedType,
		event.ContractBlockersChangedType, event.ContractVersionedType, event.TaskHandoffType,
		event.TaskDeferredType, event.ExternalUpdatedType, event.RoutedToOwnerType:
		return 2
	case event.DecisionAppliedType, event.ExternalResolvedType, event.RouteResolvedType,
		event.RoleReleasedType, event.ConflictDetectedType:
		return 3
	}
	return 4
}

// step is an event's position within its subject: its LifecycleRank,
// except for project-wide events (role claims and releases, spine
// approvals, conflicts), which are not one item's lifecycle and keep plain
// time order.
func step(e event.Event) int {
	if e.Subject.Kind == event.KindProject {
		return 0
	}
	return LifecycleRank(e.Type)
}

// ulidTime is the millisecond-timestamp part of a ULID (its first 10
// characters), which sorts as text. Malformed ids use the whole id.
func ulidTime(id string) string {
	if len(id) >= 10 {
		return id[:10]
	}
	return id
}

// SortEvents returns a copy of events in log order (C-event-log v5 §2,
// D-imported-event-time-order: causal within a subject):
//
//   - Within one subject (kind and id), events are ordered by lifecycle
//     step first (see step), then by the timestamp in the id. An apply
//     therefore always comes after its decide, and an approval after its
//     draft, whatever their clocks said (backdated imports, clock skew).
//     Events of one step keep time order.
//   - Across subjects, events are ordered by time: the subjects' sequences
//     are merged by always taking the earliest next event.
//
// Remaining ties break by LifecycleRank, id, type and encoded bytes, so the
// order is total and the same for any permutation of the input. Exact
// duplicates (same encoding) are dropped.
func SortEvents(events []event.Event) []event.Event {
	type keyed struct {
		e   event.Event
		enc string
	}
	ks := make([]keyed, 0, len(events))
	for _, e := range events {
		b, err := json.Marshal(e)
		if err != nil {
			b = nil
		}
		ks = append(ks, keyed{e, string(b)})
	}
	byTime := func(a, b keyed) int {
		return cmp.Or(
			cmp.Compare(ulidTime(a.e.ID), ulidTime(b.e.ID)),
			cmp.Compare(LifecycleRank(a.e.Type), LifecycleRank(b.e.Type)),
			cmp.Compare(a.e.ID, b.e.ID),
			cmp.Compare(a.e.Type, b.e.Type),
			cmp.Compare(a.enc, b.enc),
		)
	}
	slices.SortStableFunc(ks, byTime)
	// Drop exact duplicates.
	uniq := ks[:0]
	for i, k := range ks {
		if i > 0 && k.enc != "" && k.enc == ks[i-1].enc {
			continue
		}
		uniq = append(uniq, k)
	}
	// One sequence per subject, in causal order. An event addressed to an
	// alias belongs to the aliased item's sequence, so it is ordered after
	// the event that creates the item (and the alias).
	type subjectKey struct {
		kind event.SubjectKind
		id   string
	}
	aliases := map[string]string{}
	for _, k := range uniq {
		for _, a := range creatingAliases(k.e) {
			if _, taken := aliases[a]; !taken && a != k.e.Subject.ID {
				aliases[a] = k.e.Subject.ID
			}
		}
	}
	var order []subjectKey
	seqs := map[subjectKey][]keyed{}
	for _, k := range uniq {
		id := k.e.Subject.ID
		if to, ok := aliases[id]; ok {
			id = to
		}
		sk := subjectKey{k.e.Subject.Kind, id}
		if _, ok := seqs[sk]; !ok {
			order = append(order, sk)
		}
		seqs[sk] = append(seqs[sk], k)
	}
	for _, sk := range order {
		slices.SortStableFunc(seqs[sk], func(a, b keyed) int {
			return cmp.Or(cmp.Compare(step(a.e), step(b.e)), byTime(a, b))
		})
	}
	// Merge: repeatedly take the earliest head (a binary heap of subjects).
	out := make([]event.Event, 0, len(uniq))
	heads := make([]int, len(order))
	head := func(i int) keyed { return seqs[order[i]][heads[i]] }
	less := func(h []int, a, b int) bool { return byTime(head(h[a]), head(h[b])) < 0 }
	down := func(h []int, i int) {
		for {
			l, m := 2*i+1, i
			if l < len(h) && less(h, l, m) {
				m = l
			}
			if r := l + 1; r < len(h) && less(h, r, m) {
				m = r
			}
			if m == i {
				return
			}
			h[i], h[m] = h[m], h[i]
			i = m
		}
	}
	h := make([]int, len(order))
	for i := range h {
		h[i] = i
	}
	for i := len(h)/2 - 1; i >= 0; i-- {
		down(h, i)
	}
	for len(h) > 0 {
		i := h[0]
		out = append(out, head(i).e)
		heads[i]++
		if heads[i] == len(seqs[order[i]]) {
			h[0] = h[len(h)-1]
			h = h[:len(h)-1]
		}
		down(h, 0)
	}
	return out
}

// creatingAliases returns the aliases an item-creating event declares
// (TaskDrafted, DecisionRaised, ContractDrafted, ExternalAdded), or nil.
func creatingAliases(e event.Event) []string {
	switch e.Type {
	case event.TaskDraftedType, event.DecisionRaisedType, event.ContractDraftedType, event.ExternalAddedType:
	default:
		return nil
	}
	p, err := e.Payload()
	if err != nil {
		return nil
	}
	switch d := p.(type) {
	case *event.TaskDrafted:
		return d.Aliases
	case *event.DecisionRaised:
		return d.Aliases
	case *event.ContractDrafted:
		return d.Aliases
	case *event.ExternalAdded:
		return d.Aliases
	}
	return nil
}

// Reduce folds an event log into State. Events are applied in log order
// (SortEvents) whatever the input order, so any permutation of the same events gives the
// same State. Reduce never fails: events it cannot use are listed in
// State.Skipped, and events outside their lane are applied anyway and left
// for Conflicts to report (C-event-log §5: detect, never resolve silently).
//
// Lifecycle rules applied here (C-decision-record §3): only a pending
// decision can be decided or defaulted, and the first such event wins;
// a decision can be superseded from any status; DecisionApplied sets
// applied_on once, and only on a decided or defaulted decision.
func Reduce(events []event.Event) State {
	s := newState()
	for _, e := range SortEvents(events) {
		s.apply(e)
	}
	s.finish()
	return s
}

func day(t time.Time) string { return t.UTC().Format(time.DateOnly) }

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func (s *State) skip(e event.Event, reason string) {
	s.Skipped = append(s.Skipped, SkippedEvent{Event: e.ID, Type: string(e.Type), Reason: reason})
}

// Resolve maps an alias to its slug id. Anything else is returned unchanged.
func (s *State) Resolve(id string) string {
	if to, ok := s.Aliases[id]; ok {
		return to
	}
	return id
}

func (s *State) addAliases(id string, aliases []string) {
	for _, a := range aliases {
		if _, taken := s.Aliases[a]; !taken && a != id {
			s.Aliases[a] = id
		}
	}
}

// Has reports whether an item with this id or alias exists: a drafted task,
// a raised decision, a contract or an external dependency.
func (s *State) Has(id string) bool {
	id = s.Resolve(id)
	switch event.KindOf(id) {
	case event.KindTask:
		t, ok := s.Tasks[id]
		return ok && t.Drafted()
	case event.KindDecision:
		d, ok := s.Decisions[id]
		return ok && len(d.Raises) > 0
	case event.KindContract:
		_, ok := s.Contracts[id]
		return ok
	case event.KindExternal:
		_, ok := s.Externals[id]
		return ok
	}
	return false
}

func (s *State) task(id string) *Task {
	id = s.Resolve(id)
	t, ok := s.Tasks[id]
	if !ok {
		t = &Task{ID: id, BlockedBy: []string{}, TierGrants: []string{}, Drafts: []Writer{},
			Amendments: []Entry{}, Handoffs: []Handoff{}, Questions: []Question{}}
		s.Tasks[id] = t
	}
	return t
}

func (s *State) decision(id string) *Decision {
	id = s.Resolve(id)
	d, ok := s.Decisions[id]
	if !ok {
		d = &Decision{Raises: []Writer{}, Status: StatusPending, Questions: []Question{}, Answers: []Answer{}}
		d.ID = id
		s.Decisions[id] = d
	}
	return d
}

func (s *State) contract(id string) *Contract {
	id = s.Resolve(id)
	c, ok := s.Contracts[id]
	if !ok {
		c = &Contract{ID: id, Status: ContractDraft, BlockedBy: []string{}, Freezes: []Freeze{}, Versions: []Freeze{}, Questions: []Question{}}
		s.Contracts[id] = c
	}
	return c
}

func (s *State) external(id string) *External {
	id = s.Resolve(id)
	x, ok := s.Externals[id]
	if !ok {
		x = &External{ID: id, Blocks: []string{}}
		s.Externals[id] = x
	}
	return x
}

// questionsOf returns the question list of the subject, or nil for a
// subject that cannot hold questions.
func (s *State) questionsOf(sub event.Subject) *[]Question {
	switch sub.Kind {
	case event.KindTask:
		return &s.task(sub.ID).Questions
	case event.KindDecision:
		return &s.decision(sub.ID).Questions
	case event.KindContract:
		return &s.contract(sub.ID).Questions
	}
	return nil
}

// FindQuestion returns the question with this id on the subject.
func (s *State) FindQuestion(sub event.Subject, questionID string) (Question, bool) {
	var qs []Question
	id := s.Resolve(sub.ID)
	switch sub.Kind {
	case event.KindTask:
		if t, ok := s.Tasks[id]; ok {
			qs = t.Questions
		}
	case event.KindDecision:
		if d, ok := s.Decisions[id]; ok {
			qs = d.Questions
		}
	case event.KindContract:
		if c, ok := s.Contracts[id]; ok {
			qs = c.Questions
		}
	}
	for _, q := range qs {
		if q.ID == questionID {
			return q, true
		}
	}
	return Question{}, false
}

// questionByEvent finds a question by the id of its QuestionAsked event.
func (s *State) questionByEvent(eventID string) (event.Subject, Question, bool) {
	for _, id := range sortedKeys(s.Tasks) {
		for _, q := range s.Tasks[id].Questions {
			if q.Event == eventID {
				return event.Subject{Kind: event.KindTask, ID: id}, q, true
			}
		}
	}
	for _, id := range sortedKeys(s.Decisions) {
		for _, q := range s.Decisions[id].Questions {
			if q.Event == eventID {
				return event.Subject{Kind: event.KindDecision, ID: id}, q, true
			}
		}
	}
	for _, id := range sortedKeys(s.Contracts) {
		for _, q := range s.Contracts[id].Questions {
			if q.Event == eventID {
				return event.Subject{Kind: event.KindContract, ID: id}, q, true
			}
		}
	}
	return event.Subject{}, Question{}, false
}

func sortedKeys[K ~string, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func (s *State) apply(e event.Event) {
	p, err := e.Payload()
	if err != nil {
		s.skip(e, err.Error())
		return
	}
	switch d := p.(type) {
	case *event.TaskDrafted:
		s.addAliases(e.Subject.ID, d.Aliases)
		t := s.task(e.Subject.ID)
		t.Drafts = append(t.Drafts, Writer{Event: e.ID, Actor: e.Actor})
		if len(t.Drafts) > 1 {
			return // first draft wins; later ones are id-collision or repeats
		}
		t.Aliases = d.Aliases
		t.Title, t.Kind, t.Brief, t.Repo = d.Title, d.Kind, d.Brief, d.Repo
		t.BaseBranch, t.Branch, t.Worktree = d.BaseBranch, d.Branch, d.Worktree
		t.BlockedBy = appendUnique(t.BlockedBy, d.BlockedBy...)
		t.TierGrants = orEmpty(d.TierGrants)
		t.DraftSession = d.Session
		if t.Session == "" {
			t.Session = d.Session // a TaskSessionAttached still wins
		}
	case *event.TaskApproved:
		t := s.task(e.Subject.ID)
		if t.Approved {
			return
		}
		t.Approved, t.ApprovedEvent, t.ApprovedOn = true, e.ID, day(e.At)
		t.ApprovedSHA256 = d.TextSHA256
		t.TierGrants = orEmpty(d.TierGrants)
	case *event.TaskAmended:
		t := s.task(e.Subject.ID)
		t.Amendments = append(t.Amendments, Entry{Event: e.ID, At: stamp(e.At), File: d.File, TextSHA256: d.TextSHA256, Note: d.Note, ByDecision: d.ByDecision})
	case *event.TaskBlockersChanged:
		t := s.task(e.Subject.ID)
		t.BlockedBy = appendUnique(t.BlockedBy, d.Added...)
		t.BlockedBy = slices.DeleteFunc(t.BlockedBy, func(b string) bool { return slices.Contains(d.Removed, b) })
	case *event.TaskSessionAttached:
		t := s.task(e.Subject.ID)
		t.Session, t.Adapter = d.Session, d.Adapter
		if t.Session == "" {
			t.Session = t.DraftSession
		}
	case *event.TaskHandoff:
		t := s.task(e.Subject.ID)
		t.Handoffs = append(t.Handoffs, Handoff{Event: e.ID, At: stamp(e.At), By: e.Actor.ID, File: d.File,
			TextSHA256: d.TextSHA256, StatusLine: d.StatusLine, HasFollowups: d.HasFollowups,
			HasDecisions: d.HasDecisions, HasDeviations: d.HasDeviations})
	case *event.TaskDeferred:
		t := s.task(e.Subject.ID)
		t.Deferred, t.DeferredReason = true, d.Reason
	case *event.DecisionRaised:
		s.addAliases(e.Subject.ID, d.Aliases)
		dec := s.decision(e.Subject.ID)
		dec.Raises = append(dec.Raises, Writer{Event: e.ID, Actor: e.Actor})
		if len(dec.Raises) > 1 {
			return
		}
		id := dec.ID
		dec.DecisionRaised = *d
		dec.ID = id // the subject id is authoritative
	case *event.DecisionDecided:
		dec := s.decision(e.Subject.ID)
		dec.Answers = append(dec.Answers, Answer{Event: e.ID, By: e.Actor.ID, Answer: d.Answer, Note: d.Note})
		if dec.Status != StatusPending {
			return
		}
		dec.Status, dec.Answer, dec.AnswerNote = StatusDecided, d.Answer, d.Note
		dec.DecidedBy, dec.DecidedOn, dec.DecidedEvent = e.Actor.ID, day(e.At), e.ID
	case *event.DecisionDefaulted:
		dec := s.decision(e.Subject.ID)
		if dec.Status != StatusPending {
			return
		}
		dec.Status, dec.Answer = StatusDefaulted, d.Answer
		dec.DecidedBy, dec.DecidedOn, dec.DecidedEvent = e.Actor.ID, day(e.At), e.ID
	case *event.DecisionSuperseded:
		dec := s.decision(e.Subject.ID)
		if dec.Status == StatusSuperseded {
			return
		}
		dec.Status, dec.SupersededBy = StatusSuperseded, d.By
	case *event.DecisionApplied:
		dec := s.decision(e.Subject.ID)
		if dec.AppliedOn != "" || (dec.Status != StatusDecided && dec.Status != StatusDefaulted) {
			return
		}
		dec.AppliedOn = day(e.At)
		dec.Applied = &Applied{AmendedTasks: orEmpty(d.AmendedTasks), UnblockedTasks: orEmpty(d.UnblockedTasks),
			NotifiedSessions: orEmpty(d.NotifiedSessions)}
	case *event.QuestionAsked:
		qs := s.questionsOf(e.Subject)
		if qs == nil {
			s.skip(e, "questions are only recorded on a task, decision or contract")
			return
		}
		for _, q := range *qs {
			if q.ID == d.QuestionID {
				s.skip(e, "question id already used on this subject")
				return
			}
		}
		*qs = append(*qs, Question{ID: d.QuestionID, Event: e.ID, At: stamp(e.At), By: e.Actor.ID, To: d.To, Text: d.Text})
	case *event.QuestionAnswered:
		qs := s.questionsOf(e.Subject)
		if qs == nil {
			s.skip(e, "questions are only recorded on a task, decision or contract")
			return
		}
		for i := range *qs {
			q := &(*qs)[i]
			if q.ID != d.QuestionID || q.Answered() {
				continue
			}
			q.Answer, q.AnsweredBy, q.AnsweredAt, q.AnswerEvent = d.Text, e.Actor.ID, stamp(e.At), e.ID
			for _, r := range s.Routes {
				if r.FromEvent == q.Event && r.Open {
					r.Open, r.ClosedBy = false, e.ID
				}
			}
			return
		}
	case *event.ContractDrafted:
		s.addAliases(e.Subject.ID, d.Aliases)
		c := s.contract(e.Subject.ID)
		if c.DraftedEvent != "" {
			return
		}
		c.DraftedEvent, c.Title, c.Path, c.Aliases = e.ID, d.Title, d.Path, d.Aliases
		c.BlockedBy = appendUnique(c.BlockedBy, d.BlockedBy...)
	case *event.ContractBlockersChanged:
		c := s.contract(e.Subject.ID)
		c.BlockedBy = appendUnique(c.BlockedBy, d.Added...)
		c.BlockedBy = slices.DeleteFunc(c.BlockedBy, func(b string) bool { return slices.Contains(d.Removed, b) })
	case *event.ContractFrozen:
		c := s.contract(e.Subject.ID)
		c.Freezes = append(c.Freezes, Freeze{Event: e.ID, Version: d.Version, TextSHA256: d.TextSHA256})
	case *event.ContractVersioned:
		c := s.contract(e.Subject.ID)
		c.Versions = append(c.Versions, Freeze{Event: e.ID, Version: d.Version, TextSHA256: d.TextSHA256, ByDecision: d.ByDecision})
	case *event.ExternalAdded:
		s.addAliases(e.Subject.ID, d.Aliases)
		x := s.external(e.Subject.ID)
		if x.AddedEvent != "" {
			return
		}
		x.AddedEvent, x.Aliases, x.What, x.OwnerTeam = e.ID, d.Aliases, d.What, d.OwnerTeam
		x.AskedOn, x.NeededBy, x.Blocks = d.AskedOn, d.NeededBy, orEmpty(d.Blocks)
	case *event.ExternalUpdated:
		x := s.external(e.Subject.ID)
		for _, k := range sortedKeys(d.Fields) {
			raw := d.Fields[k]
			var err error
			switch k {
			case "what":
				err = json.Unmarshal(raw, &x.What)
			case "owner_team":
				err = json.Unmarshal(raw, &x.OwnerTeam)
			case "asked_on":
				err = json.Unmarshal(raw, &x.AskedOn)
			case "needed_by":
				err = json.Unmarshal(raw, &x.NeededBy)
			case "blocks":
				err = json.Unmarshal(raw, &x.Blocks)
				x.Blocks = orEmpty(x.Blocks)
			}
			if err != nil {
				s.skip(e, "field "+k+": "+err.Error())
			}
		}
	case *event.ExternalResolved:
		x := s.external(e.Subject.ID)
		if !x.Resolved {
			x.Resolved, x.ResolvedNote = true, d.Note
		}
	case *event.RoutedToOwner:
		if _, ok := s.Routes[d.RouteID]; ok {
			return // the loop is idempotent; a repeat changes nothing
		}
		r := &RouteRecord{ID: d.RouteID, Event: e.ID, Subject: e.Subject, To: d.To, FromEvent: d.FromEvent, Reason: d.Reason, Open: true}
		if _, q, ok := s.questionByEvent(d.FromEvent); ok && q.Answered() {
			r.Open, r.ClosedBy = false, q.AnswerEvent
		}
		if res, ok := s.EarlyResolutions[d.RouteID]; ok {
			if r.Open {
				r.Open, r.ClosedBy, r.Note = false, res.Event, res.Note
			}
			delete(s.EarlyResolutions, d.RouteID)
		}
		s.Routes[d.RouteID] = r
	case *event.RouteResolved:
		r, ok := s.Routes[d.RouteID]
		switch {
		case ok && r.Open:
			r.Open, r.ClosedBy, r.Note = false, e.ID, d.Note
		case !ok:
			// Resolved before the loop routed it: keep the first
			// resolution and close the route when it appears.
			if _, seen := s.EarlyResolutions[d.RouteID]; !seen {
				s.EarlyResolutions[d.RouteID] = Resolution{Event: e.ID, Note: d.Note}
			}
		}
	case *event.RoleClaimed:
		s.Claims = append(s.Claims, Claim{Role: d.Role, Session: d.Session, Event: e.ID})
	case *event.RoleReleased:
		s.Claims = slices.DeleteFunc(s.Claims, func(c Claim) bool { return c.Role == d.Role && c.Session == d.Session })
	case *event.SpineApproved:
		s.SpineApprovals = append(s.SpineApprovals, SpineApproval{Event: e.ID, TextSHA256: d.TextSHA256})
	case *event.ConflictDetected:
		evs := slices.Clone(d.Events)
		slices.Sort(evs)
		s.Conflicts = append(s.Conflicts, RecordedConflict{Event: e.ID, Rule: d.Rule, Events: orEmpty(evs)})
	}
}

// finish computes values that depend on the whole log.
func (s *State) finish() {
	for _, c := range s.Contracts {
		var best *Freeze
		for _, f := range c.Freezes {
			if best == nil || f.Version > best.Version {
				best = &f
			}
		}
		if best != nil {
			c.Status, c.Version, c.TextSHA256 = ContractFrozen, best.Version, best.TextSHA256
		}
		c.Pending = nil
		for _, f := range c.Versions {
			if f.Version > c.Version && (c.Pending == nil || f.Version >= c.Pending.Version) {
				c.Pending = &f
			}
		}
	}
}

func appendUnique(list []string, add ...string) []string {
	for _, a := range add {
		if !slices.Contains(list, a) {
			list = append(list, a)
		}
	}
	return list
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
