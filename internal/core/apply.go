package core

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Reasons ApplyEffectOf refuses a decision.
var (
	ErrApplied    = errors.New("already applied")
	ErrSuperseded = errors.New("superseded; a superseded decision is never applied (C-decision-record §3)")
	ErrNotDecided = errors.New("only a decided or defaulted decision is applied")
	ErrNoEffect   = errors.New("the apply would not take effect in the log")
)

// ApplyEffect is what applying a decision writes (C-decision-record §3,
// D-apply-before-loop): a §B amendment for each task in Amend, then
// DecisionApplied {amended_tasks: Tasks, unblocked_tasks: Unblocked}.
type ApplyEffect struct {
	Decision string `json:"decision"`
	// Tasks are the drafted tasks in the decision's `blocks`, resolved,
	// in order: amended_tasks.
	Tasks []string `json:"tasks"`
	// Amend are the Tasks not yet amended for this decision.
	Amend []string `json:"amend"`
	// NotTasks are entries of `blocks` that name no drafted task; they get
	// no amendment.
	NotTasks []string `json:"not_tasks"`
	// Unblocked are the tasks blocked before the apply and not after.
	Unblocked []string `json:"unblocked"`
	// Line is the generated §B amendment text.
	Line string `json:"line"`
}

// ApplyEffectOf works out what applying decision id writes, from the log
// events (reduced into s) and this pass's observations. An amendment
// counts as made for the decision when its by_decision names it, or (for
// amendments written before D-apply-amend-lane) its note is
// "applies <id>".
func ApplyEffectOf(s *State, events []event.Event, obs Observed, id string, now time.Time) (ApplyEffect, error) {
	id = s.ResolveAs(event.KindDecision, id)
	d, ok := s.Decisions[id]
	switch {
	case !ok || len(d.Raises) == 0:
		return ApplyEffect{}, fmt.Errorf("%s: no such decision", id)
	case d.AppliedOn != "":
		return ApplyEffect{}, fmt.Errorf("%s: %w on %s", id, ErrApplied, d.AppliedOn)
	case d.Status == StatusSuperseded:
		return ApplyEffect{}, fmt.Errorf("%s: %w", id, ErrSuperseded)
	case d.Status != StatusDecided && d.Status != StatusDefaulted:
		return ApplyEffect{}, fmt.Errorf("%s is %s: %w", id, d.Status, ErrNotDecided)
	}
	eff := ApplyEffect{Decision: id, Tasks: []string{}, Amend: []string{}, NotTasks: []string{}, Unblocked: []string{}, Line: ApplyLine(d)}
	for _, b := range d.Blocks {
		tid := s.Resolve(b)
		if t, ok := s.Tasks[tid]; !ok || !t.Drafted() {
			eff.NotTasks = append(eff.NotTasks, b)
			continue
		}
		if slices.Contains(eff.Tasks, tid) {
			continue
		}
		eff.Tasks = append(eff.Tasks, tid)
		if !amendedFor(s.Tasks[tid], id) {
			eff.Amend = append(eff.Amend, tid)
		}
	}
	applied, err := event.New(syntheticID(now), event.DecisionAppliedType, now.UTC(), event.Actor{Role: event.RoleLoop, ID: "loop"},
		event.Subject{Kind: event.KindDecision, ID: id}, event.DecisionApplied{})
	if err != nil {
		return ApplyEffect{}, err
	}
	after := Reduce(append(slices.Clone(events), applied))
	if after.Decisions[id].AppliedOn == "" {
		return ApplyEffect{}, fmt.Errorf("%s: %w", id, ErrNoEffect)
	}
	before, next := newDeriver(s, obs), newDeriver(&after, obs)
	for _, tid := range sortedKeys(s.Tasks) {
		if s.Tasks[tid].Drafted() && before.taskStatus(tid) == TaskBlocked && next.taskStatus(tid) != TaskBlocked {
			eff.Unblocked = append(eff.Unblocked, tid)
		}
	}
	return eff, nil
}

func amendedFor(t *Task, id string) bool {
	return slices.ContainsFunc(t.Amendments, func(a Entry) bool { return a.ByDecision == id || a.Note == "applies "+id })
}

// ApplyLine is the generated §B amendment for an applied decision.
func ApplyLine(d *Decision) string {
	line := fmt.Sprintf("Applied %s (%s): `%s`, %s on %s.", d.ID, d.Title, d.Answer, d.Status, d.DecidedOn)
	if n := strings.TrimSpace(d.AnswerNote); n != "" {
		line += " Note: " + n
		if !strings.HasSuffix(n, ".") {
			line += "."
		}
	}
	return line + " See .ekumen/decisions/" + d.ID + ".yaml."
}

// syntheticID is an event id for reducing a what-if event that is never
// written.
func syntheticID(now time.Time) string {
	id, _ := event.ULIDFromParts(uint64(now.UnixMilli()), [10]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	return id
}
