package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Kinds of loop action, in the order a pass takes them (D-control-loop).
const (
	LoopDefault  = "default"
	LoopApply    = "apply"
	LoopRoute    = "route"
	LoopConflict = "conflict"
)

// PlaceholderDecision is why an approved task with no branch or worktree
// cannot start.
const PlaceholderDecision = "D-placeholder-task-fields"

// LoopAction is one idempotent action of a pass. Its events are written
// in order, and the last one marks the action done: DecisionApplied after
// the amendments, ConflictDetected after the conflict's decision.
type LoopAction struct {
	Kind    string        `json:"kind"`
	Subject event.Subject `json:"subject"`
	Detail  string        `json:"detail"`
	Events  []event.Event `json:"events"`
	// Amendments are the §B entries of an apply, one per TaskAmended in
	// Events. The executor writes each file and sets File and TextSHA256
	// on its event.
	Amendments []PlannedAmendment `json:"amendments,omitempty"`
}

// PlannedAmendment is a §B entry an apply writes.
type PlannedAmendment struct {
	Task  string `json:"task"`
	Event string `json:"event"`
	Text  string `json:"text"`
}

// Observation completeness, as PlanInput.Observation.
const (
	ObservationOK          = "ok"
	ObservationUnavailable = "unavailable"
	ObservationTruncated   = "truncated"
)

// Reasons an action is held.
const (
	HeldObservation = "observation incomplete: unblocked_tasks would be recorded without knowing which tasks are done"
	HeldContested   = "contested: decided with different answers (decided-twice); the lead resolves the conflict first"
)

// Skip is an action the plan did not take, with why.
type Skip struct {
	Kind    string        `json:"kind"`
	Subject event.Subject `json:"subject"`
	Reason  string        `json:"reason"`
}

// Plan is one pass of the control loop: the actions to take, and what the
// loop reports but does not do.
type Plan struct {
	Actions []LoopAction `json:"actions"`
	// Held are actions deliberately not taken this pass (HeldObservation,
	// HeldContested); a later pass takes them.
	Held []Skip `json:"held"`
	// Skipped are actions that could not be planned (an error); the rest
	// of the pass goes on.
	Skipped []Skip `json:"skipped"`
	// Ready are tasks whose derived status is ready, after the actions.
	// Nil when the observation is not ObservationOK: unknown.
	Ready []string `json:"ready"`
	// NotStartable are tasks that are approved only because branch or
	// worktree is empty (PlaceholderDecision). Nil when unknown.
	NotStartable []string `json:"not_startable"`
	// Unblocked are the tasks the applies unblocked.
	Unblocked []string `json:"unblocked"`
	// Notify are the sessions of amended tasks; no session messaging
	// exists yet.
	Notify []string `json:"notify"`
}

// Empty reports whether the plan takes no action.
func (p Plan) Empty() bool { return len(p.Actions) == 0 }

// PlanInput is everything one pass reads. The planner does no I/O.
type PlanInput struct {
	// Events is the whole log.
	Events   []event.Event
	Observed Observed
	// Observation says whether Observed is complete: ObservationOK, or
	// ObservationUnavailable / ObservationTruncated. Empty counts as
	// unavailable.
	Observation string
	Now         time.Time
	Conflicts   ConflictInputs
	// Loop is the actor the events are written as (role loop).
	Loop event.Actor
	// NewID makes event ids. Nil gives deterministic ids from Now.
	NewID func(time.Time) (string, error)
}

// PlanPass plans one pass of the control loop (D-control-loop, spine §6):
//
//  1. default: each pending tier-1 decision whose deadline is before
//     today (UTC) gets DecisionDefaulted with its default_if_no_answer.
//     The deadline day itself is still answerable (Urgency's "overdue").
//  2. apply: each decided or defaulted decision that is not applied or
//     superseded, as `decision apply` does (ApplyEffectOf): a TaskAmended
//     per drafted task in blocks not yet amended for it, then
//     DecisionApplied. Defaults of this pass are applied in this pass.
//     Held, not planned, when the observation is incomplete (DecisionApplied
//     records unblocked_tasks for good) or the decision is contested
//     (C-event-log §5: detected, never resolved silently).
//  3. route: RoutedToOwner for every entry Route returns over the log.
//  4. conflict: for each conflict Conflicts returns, a tier-2 decision
//     (ConflictDecision) unless this conflict's decision is already
//     raised, then ConflictDetected.
//
// Each stage plans against the state with every earlier action's events
// reduced in, so the plan is closed: once its events are written, planning
// again returns no actions (held ones aside). An action that cannot be
// planned is listed in Skipped and the pass goes on. It never starts or
// messages sessions.
func PlanPass(in PlanInput) Plan {
	p := &planner{in: in, events: slices.Clone(in.Events)}
	p.state = Reduce(p.events)
	p.defaults()
	p.applies()
	p.routes()
	p.conflicts()
	return p.report()
}

type planner struct {
	in        PlanInput
	events    []event.Event
	state     State
	n         int
	actions   []LoopAction
	held      []Skip
	skipped   []Skip
	unblocked []string
	notify    []string
}

func (p *planner) newEvent(typ event.Type, sub event.Subject, data any) (event.Event, error) {
	now := p.in.Now.UTC()
	var id string
	var err error
	if p.in.NewID != nil {
		id, err = p.in.NewID(now)
	} else {
		p.n++
		var ent [10]byte
		ent[8], ent[9] = byte(p.n>>8), byte(p.n)
		id, err = event.ULIDFromParts(uint64(now.UnixMilli()), ent)
	}
	if err != nil {
		return event.Event{}, fmt.Errorf("new event id: %w", err)
	}
	return event.New(id, typ, now, p.in.Loop, sub, data)
}

func (p *planner) add(a LoopAction) {
	p.actions = append(p.actions, a)
	p.events = append(p.events, a.Events...)
	p.state = Reduce(p.events)
}

func (p *planner) skip(kind string, sub event.Subject, err error) {
	p.skipped = append(p.skipped, Skip{Kind: kind, Subject: sub, Reason: err.Error()})
}

func (p *planner) defaults() {
	today := p.in.Now.UTC().Format(time.DateOnly)
	for _, id := range sortedKeys(p.state.Decisions) {
		d := p.state.Decisions[id]
		if len(d.Raises) == 0 || d.Status != StatusPending || d.Tier != 1 ||
			d.DefaultIfNoAnswer == nil || d.Deadline == nil || !validDate(*d.Deadline) || *d.Deadline >= today {
			continue
		}
		sub := event.Subject{Kind: event.KindDecision, ID: id}
		e, err := p.newEvent(event.DecisionDefaultedType, sub, event.DecisionDefaulted{Answer: *d.DefaultIfNoAnswer, Deadline: *d.Deadline})
		if err != nil {
			p.skip(LoopDefault, sub, err)
			continue
		}
		p.add(LoopAction{Kind: LoopDefault, Subject: sub, Events: []event.Event{e},
			Detail: fmt.Sprintf("default %s to `%s`: deadline %s passed", id, *d.DefaultIfNoAnswer, *d.Deadline)})
	}
}

// Contested reports whether a decision has DecisionDecided events with
// different answers (rule decided-twice).
func Contested(d *Decision) bool {
	for i := range d.Answers {
		for j := i + 1; j < len(d.Answers); j++ {
			a, b := d.Answers[i], d.Answers[j]
			if a.Answer != b.Answer || (a.Answer == event.AnswerOther && a.Note != b.Note) {
				return true
			}
		}
	}
	return false
}

func (p *planner) applies() {
	for _, id := range sortedKeys(p.state.Decisions) {
		d := p.state.Decisions[id]
		if len(d.Raises) == 0 || d.AppliedOn != "" || (d.Status != StatusDecided && d.Status != StatusDefaulted) {
			continue
		}
		sub := event.Subject{Kind: event.KindDecision, ID: id}
		switch {
		case Contested(d):
			p.held = append(p.held, Skip{Kind: LoopApply, Subject: sub, Reason: HeldContested})
			continue
		case p.in.Observation != ObservationOK:
			p.held = append(p.held, Skip{Kind: LoopApply, Subject: sub, Reason: HeldObservation})
			continue
		}
		a, eff, err := p.applyAction(id, sub)
		if err != nil {
			p.skip(LoopApply, sub, err)
			continue
		}
		a.Detail = fmt.Sprintf("apply %s (%s `%s`): amend %s; unblock %s", id, d.Status, d.Answer, listOrNone(eff.Amend), listOrNone(eff.Unblocked))
		p.unblocked = appendUnique(p.unblocked, eff.Unblocked...)
		for _, tid := range eff.Tasks {
			if s := p.state.Tasks[tid].Session; s != "" {
				p.notify = appendUnique(p.notify, s)
			}
		}
		p.add(a)
	}
}

func (p *planner) applyAction(id string, sub event.Subject) (LoopAction, ApplyEffect, error) {
	a := LoopAction{Kind: LoopApply, Subject: sub}
	eff, err := ApplyEffectOf(&p.state, p.events, p.in.Observed, id, p.in.Now)
	if err != nil {
		return a, eff, err
	}
	for _, tid := range eff.Amend {
		e, err := p.newEvent(event.TaskAmendedType, event.Subject{Kind: event.KindTask, ID: tid},
			event.TaskAmended{Note: "applies " + id, ByDecision: id})
		if err != nil {
			return a, eff, err
		}
		a.Events = append(a.Events, e)
		a.Amendments = append(a.Amendments, PlannedAmendment{Task: tid, Event: e.ID, Text: eff.Line + "\n"})
	}
	e, err := p.newEvent(event.DecisionAppliedType, sub,
		event.DecisionApplied{AmendedTasks: eff.Tasks, UnblockedTasks: eff.Unblocked, NotifiedSessions: []string{}})
	if err != nil {
		return a, eff, err
	}
	a.Events = append(a.Events, e)
	return a, eff, nil
}

func (p *planner) routes() {
	for _, r := range Route(&p.state, p.events) {
		e, err := p.newEvent(event.RoutedToOwnerType, r.Subject, r.RoutedToOwner)
		if err != nil {
			p.skip(LoopRoute, r.Subject, err)
			continue
		}
		p.add(LoopAction{Kind: LoopRoute, Subject: r.Subject, Events: []event.Event{e},
			Detail: fmt.Sprintf("route %s (%s %s) to %s: %s", r.RouteID, r.Subject.Kind, r.Subject.ID, r.To, r.Reason)})
	}
}

func (p *planner) conflicts() {
	for _, c := range Conflicts(&p.state, p.events, p.in.Conflicts) {
		rec, err := ConflictDecision(&p.state, p.events, c, p.in.Loop.ID, p.in.Now)
		if err != nil {
			p.skip(LoopConflict, event.Subject{Kind: event.KindProject, ID: event.ProjectSubjectID}, err)
			continue
		}
		sub := event.Subject{Kind: event.KindDecision, ID: rec.ID}
		a := LoopAction{Kind: LoopConflict, Subject: sub, Detail: fmt.Sprintf("conflict %s: %s", c.Rule, c.Detail)}
		if err := p.conflictEvents(&a, rec, c); err != nil {
			p.skip(LoopConflict, sub, err)
			continue
		}
		p.add(a)
	}
}

func (p *planner) conflictEvents(a *LoopAction, rec event.DecisionRaised, c Conflict) error {
	if d, ok := p.state.Decisions[rec.ID]; !ok || len(d.Raises) == 0 {
		if err := ValidateDecision(rec, &p.state); err != nil {
			return fmt.Errorf("conflict decision %s: %w", rec.ID, err)
		}
		e, err := p.newEvent(event.DecisionRaisedType, a.Subject, rec)
		if err != nil {
			return err
		}
		a.Events = append(a.Events, e)
	}
	e, err := p.newEvent(event.ConflictDetectedType, a.Subject, event.ConflictDetected{Rule: c.Rule, Events: c.Events})
	if err != nil {
		return err
	}
	a.Events = append(a.Events, e)
	return nil
}

func (p *planner) report() Plan {
	out := Plan{Actions: orEmptyActions(p.actions), Held: orEmptySkips(p.held), Skipped: orEmptySkips(p.skipped),
		Unblocked: orEmpty(p.unblocked), Notify: orEmpty(p.notify)}
	slices.Sort(out.Unblocked)
	slices.Sort(out.Notify)
	if p.in.Observation != ObservationOK {
		return out
	}
	out.Ready, out.NotStartable = []string{}, []string{}
	d := newDeriver(&p.state, p.in.Observed)
	for _, id := range sortedKeys(p.state.Tasks) {
		if !p.state.Tasks[id].Drafted() {
			continue
		}
		switch d.taskStatus(id) {
		case TaskReady:
			out.Ready = append(out.Ready, id)
		case TaskApproved:
			out.NotStartable = append(out.NotStartable, id)
		}
	}
	return out
}

func orEmptySkips(s []Skip) []Skip {
	if s == nil {
		return []Skip{}
	}
	return s
}

func orEmptyActions(a []LoopAction) []LoopAction {
	if a == nil {
		return []LoopAction{}
	}
	return a
}

func listOrNone(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

// Conflict decision options.
const (
	ConflictFix    = "fix"
	ConflictAccept = "accept"
)

// ConflictDecisionID is the id of the decision the loop raises for a
// conflict: D-conflict-<rule>-<the first 6 hex digits of
// sha256("<rule>|<sorted event ids, comma-joined>")>. It depends only on
// the conflict, never on the order conflicts are found. The suffix grows
// by 2 digits only when the id is taken by a decision raised for another
// conflict (a hash collision); when 10 digits are taken too it is an
// error, and the planner skips the conflict rather than misattribute it. A conflict whose event set grows (a third
// answer to a decision decided twice) is a new conflict, with a new
// decision.
func ConflictDecisionID(s *State, events []event.Event, c Conflict) (string, error) {
	evs := slices.Clone(c.Events)
	slices.Sort(evs)
	sum := sha256.Sum256([]byte(c.Rule + "|" + strings.Join(evs, ",")))
	h := hex.EncodeToString(sum[:])
	want := conflictEvidence(events, c)
	for n := 6; n <= 10; n += 2 {
		id := "D-conflict-" + c.Rule + "-" + h[:n]
		d, ok := s.Decisions[id]
		if !ok || len(d.Raises) == 0 || slices.Equal(d.Context.Evidence, want) {
			return id, nil
		}
	}
	return "", fmt.Errorf("every id for this %s conflict (D-conflict-%s-%s…) is taken by another conflict's decision", c.Rule, c.Rule, h[:10])
}

// ConflictDecision is the tier-2 decision the loop raises for a conflict
// (C-event-log §5): options fix and accept, no recommendation, no blocks,
// the conflict's event files as evidence, raised by the loop session for
// no task.
func ConflictDecision(s *State, events []event.Event, c Conflict, session string, now time.Time) (event.DecisionRaised, error) {
	id, err := ConflictDecisionID(s, events, c)
	if err != nil {
		return event.DecisionRaised{}, err
	}
	summary := fmt.Sprintf("The control loop found a %s conflict (C-event-log §5) in events %s: %s",
		c.Rule, strings.Join(c.Events, ", "), c.Detail)
	if w := strings.Fields(summary); len(w) > MaxSummaryWords {
		summary = strings.Join(w[:MaxSummaryWords-1], " ") + " …"
	}
	title := fmt.Sprintf("Fix or accept the %s conflict at %s", c.Rule, lowestEvent(c))
	return event.DecisionRaised{
		ID: id, Title: title, Tier: 2,
		RaisedBy: event.RaisedBy{Task: nil, Session: session, On: now.UTC().Format(time.DateOnly)},
		Question: fmt.Sprintf("Rule %s: %s. Fix it, or accept it as recorded?", c.Rule, c.Detail),
		Options: []event.Option{
			{ID: ConflictFix, Tradeoff: "Correct it with new events, or restore the changed file. The log reads right again; someone does the fix."},
			{ID: ConflictAccept, Tradeoff: "Record it and change nothing. Nothing to do, but the contradiction stays in the log."},
		},
		Blocks: []string{}, ContractImpact: []string{},
		Context: event.DecisionContext{Summary: summary, Evidence: conflictEvidence(events, c)},
	}, nil
}

func lowestEvent(c Conflict) string {
	if len(c.Events) == 0 {
		return ""
	}
	return slices.Min(c.Events)
}

// conflictEvidence lists the files of a conflict's events, as repo paths.
func conflictEvidence(events []event.Event, c Conflict) []string {
	out := []string{}
	for _, id := range c.Events {
		for _, e := range events {
			if e.ID == id {
				out = append(out, fmt.Sprintf(".ekumen/events/%s/%s/%s.%s.json", e.Subject.Kind, e.Subject.ID, e.ID, e.Type))
				break
			}
		}
	}
	return out
}
