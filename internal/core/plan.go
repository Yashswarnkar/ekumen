package core

import (
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

// Plan is one pass of the control loop: the actions to take, and what the
// loop reports but does not do.
type Plan struct {
	Actions []LoopAction `json:"actions"`
	// Ready are tasks whose derived status is ready, after the actions.
	Ready []string `json:"ready"`
	// NotStartable are tasks that are approved only because branch or
	// worktree is empty (PlaceholderDecision).
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
	Events    []event.Event
	Observed  Observed
	Now       time.Time
	Conflicts ConflictInputs
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
//  3. route: RoutedToOwner for every entry Route returns over the log.
//  4. conflict: for each conflict Conflicts returns, a tier-2 decision
//     (ConflictDecision) unless this conflict's decision is already
//     raised, then ConflictDetected.
//
// Each stage plans against the state with every earlier action's events
// reduced in, so the plan is closed: once its events are written, planning
// again returns no actions. It never starts or messages sessions.
func PlanPass(in PlanInput) (Plan, error) {
	p := &planner{in: in, events: slices.Clone(in.Events)}
	p.state = Reduce(p.events)
	if err := p.defaults(); err != nil {
		return Plan{}, err
	}
	if err := p.applies(); err != nil {
		return Plan{}, err
	}
	if err := p.routes(); err != nil {
		return Plan{}, err
	}
	if err := p.conflicts(); err != nil {
		return Plan{}, err
	}
	return p.report(), nil
}

type planner struct {
	in        PlanInput
	events    []event.Event
	state     State
	n         int
	actions   []LoopAction
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

func (p *planner) defaults() error {
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
			return err
		}
		p.add(LoopAction{Kind: LoopDefault, Subject: sub, Events: []event.Event{e},
			Detail: fmt.Sprintf("default %s to `%s`: deadline %s passed", id, *d.DefaultIfNoAnswer, *d.Deadline)})
	}
	return nil
}

func (p *planner) applies() error {
	for _, id := range sortedKeys(p.state.Decisions) {
		d := p.state.Decisions[id]
		if len(d.Raises) == 0 || d.AppliedOn != "" || (d.Status != StatusDecided && d.Status != StatusDefaulted) {
			continue
		}
		eff, err := ApplyEffectOf(&p.state, p.events, p.in.Observed, id, p.in.Now)
		if err != nil {
			return err
		}
		a := LoopAction{Kind: LoopApply, Subject: event.Subject{Kind: event.KindDecision, ID: id}}
		for _, tid := range eff.Amend {
			e, err := p.newEvent(event.TaskAmendedType, event.Subject{Kind: event.KindTask, ID: tid},
				event.TaskAmended{Note: "applies " + id, ByDecision: id})
			if err != nil {
				return err
			}
			a.Events = append(a.Events, e)
			a.Amendments = append(a.Amendments, PlannedAmendment{Task: tid, Event: e.ID, Text: eff.Line + "\n"})
		}
		e, err := p.newEvent(event.DecisionAppliedType, a.Subject,
			event.DecisionApplied{AmendedTasks: eff.Tasks, UnblockedTasks: eff.Unblocked, NotifiedSessions: []string{}})
		if err != nil {
			return err
		}
		a.Events = append(a.Events, e)
		a.Detail = fmt.Sprintf("apply %s (%s `%s`): amend %s; unblock %s", id, d.Status, d.Answer, listOrNone(eff.Amend), listOrNone(eff.Unblocked))
		p.unblocked = appendUnique(p.unblocked, eff.Unblocked...)
		for _, tid := range eff.Tasks {
			if s := p.state.Tasks[tid].Session; s != "" {
				p.notify = appendUnique(p.notify, s)
			}
		}
		p.add(a)
	}
	return nil
}

func (p *planner) routes() error {
	for _, r := range Route(&p.state, p.events) {
		e, err := p.newEvent(event.RoutedToOwnerType, r.Subject, r.RoutedToOwner)
		if err != nil {
			return err
		}
		p.add(LoopAction{Kind: LoopRoute, Subject: r.Subject, Events: []event.Event{e},
			Detail: fmt.Sprintf("route %s (%s %s) to %s: %s", r.RouteID, r.Subject.Kind, r.Subject.ID, r.To, r.Reason)})
	}
	return nil
}

func (p *planner) conflicts() error {
	for _, c := range Conflicts(&p.state, p.events, p.in.Conflicts) {
		rec := ConflictDecision(&p.state, p.events, c, p.in.Loop.ID, p.in.Now)
		sub := event.Subject{Kind: event.KindDecision, ID: rec.ID}
		a := LoopAction{Kind: LoopConflict, Subject: sub, Detail: fmt.Sprintf("conflict %s: %s", c.Rule, c.Detail)}
		if d, ok := p.state.Decisions[rec.ID]; !ok || len(d.Raises) == 0 {
			if err := ValidateDecision(rec, &p.state); err != nil {
				return fmt.Errorf("conflict decision %s: %w", rec.ID, err)
			}
			e, err := p.newEvent(event.DecisionRaisedType, sub, rec)
			if err != nil {
				return err
			}
			a.Events = append(a.Events, e)
		}
		e, err := p.newEvent(event.ConflictDetectedType, sub, event.ConflictDetected{Rule: c.Rule, Events: c.Events})
		if err != nil {
			return err
		}
		a.Events = append(a.Events, e)
		p.add(a)
	}
	return nil
}

func (p *planner) report() Plan {
	out := Plan{Actions: orEmptyActions(p.actions), Ready: []string{}, NotStartable: []string{},
		Unblocked: orEmpty(p.unblocked), Notify: orEmpty(p.notify)}
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
	slices.Sort(out.Unblocked)
	slices.Sort(out.Notify)
	return out
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
// conflict: D-conflict-<rule>-<suffix>, the suffix being the last
// characters of the conflict's lowest event id, lowercased. A ULID's
// leading characters are its timestamp, shared by events minutes apart;
// its trailing ones are random. The suffix grows from 6 characters when
// the id is taken by a decision raised for another conflict.
func ConflictDecisionID(s *State, events []event.Event, c Conflict) string {
	low := strings.ToLower(lowestEvent(c))
	want := conflictEvidence(events, c)
	id := ""
	for n := 6; n <= 10; n += 2 {
		id = "D-conflict-" + c.Rule + "-" + low[max(0, len(low)-n):]
		d, ok := s.Decisions[id]
		if !ok || len(d.Raises) == 0 || slices.Equal(d.Context.Evidence, want) {
			return id
		}
	}
	return id
}

// ConflictDecision is the tier-2 decision the loop raises for a conflict
// (C-event-log §5): options fix and accept, no recommendation, no blocks,
// the conflict's event files as evidence, raised by the loop session for
// no task.
func ConflictDecision(s *State, events []event.Event, c Conflict, session string, now time.Time) event.DecisionRaised {
	id := ConflictDecisionID(s, events, c)
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
	}
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
