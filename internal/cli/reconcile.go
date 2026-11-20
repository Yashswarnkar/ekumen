package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/project"
)

type reconcileResult struct {
	OK           bool              `json:"ok"`
	DryRun       bool              `json:"dry_run"`
	Observed     reconcileObserved `json:"observed"`
	Actions      []reconcileAction `json:"actions"`
	Ready        []readyTask       `json:"ready"`
	NotStartable []notStartable    `json:"not_startable"`
	Unblocked    []string          `json:"unblocked"`
	Notify       []string          `json:"notify"`
	Events       []string          `json:"events"`
	Paths        []string          `json:"paths"`
	Planned      []event.Event     `json:"planned,omitempty"`
}

type reconcileObserved struct {
	PRs string `json:"prs"`
}

type reconcileAction struct {
	Kind    string          `json:"kind"`
	Subject project.Subject `json:"subject"`
	Detail  string          `json:"detail"`
	Events  []string        `json:"events"`
	Paths   []string        `json:"paths"`
}

type readyTask struct {
	ID       string  `json:"id"`
	Branch   string  `json:"branch"`
	Worktree string  `json:"worktree"`
	Session  *string `json:"session"`
	Brief    *string `json:"brief"`
}

type notStartable struct {
	ID       string   `json:"id"`
	Missing  []string `json:"missing"`
	Decision string   `json:"decision"`
}

// reconcile runs one pass of the control loop (D-control-loop): it plans
// with core.PlanPass and writes each action as its own all-or-none write,
// in plan order, admitting each against the state with the earlier
// actions absorbed. Pull requests are observed once, at load; projections
// are regenerated once, at the end, from that same observation.
func (r *run) reconcile(args []string) error {
	fs := r.newFlags("reconcile")
	dry := fs.Bool("dry-run", false, "plan and report; write nothing")
	if _, err := r.parse(fs, args, 0, 0, "reconcile [--dry-run]"); err != nil {
		return err
	}
	a, given, err := r.actor(!*dry)
	if err != nil {
		return err
	}
	if given && a.Role != event.RoleLoop {
		return errf(CodeLane, "refused: reconcile writes in the loop's lane; run it as --as loop:<session>, not %s", project.ActorString(a))
	}
	if !given {
		a = event.Actor{Role: event.RoleLoop, ID: "loop"}
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	now := r.now()
	plan, err := core.PlanPass(core.PlanInput{Events: s.Events, Observed: s.Observed, Now: now,
		Conflicts: core.ConflictInputs{TextSHA256: s.FrozenHashes()}, Loop: a, NewID: r.env.NewID})
	if err != nil {
		return errf(CodeInternal, "plan: %v", err)
	}
	res := reconcileResult{OK: true, DryRun: *dry, Observed: reconcileObserved{PRs: s.PRsObserved},
		Actions: []reconcileAction{}, Unblocked: plan.Unblocked, Notify: plan.Notify, Events: []string{}, Paths: []string{}}
	if *dry {
		res.Planned = []event.Event{}
		for _, act := range plan.Actions {
			ra := reconcileAction{Kind: act.Kind, Subject: subjectView(act.Subject), Detail: act.Detail, Events: []string{}, Paths: []string{}}
			for _, e := range act.Events {
				ra.Events = append(ra.Events, e.ID)
				res.Events = append(res.Events, e.ID)
			}
			res.Planned = append(res.Planned, act.Events...)
			res.Actions = append(res.Actions, ra)
		}
	} else if err := r.execute(s, a, plan, &res); err != nil {
		return err
	}
	r.report(s, plan, &res)
	r.out(res, func(w io.Writer) { printReconcile(w, res) })
	return nil
}

// execute writes the plan's actions and the projections. A failure
// partway exits 5 and names the events already recorded; the next pass
// completes the work.
func (r *run) execute(s *project.Snapshot, a event.Actor, plan core.Plan, res *reconcileResult) error {
	for i, act := range plan.Actions {
		ra, err := r.executeAction(s, a, i, act)
		if err != nil {
			recorded := strings.Join(res.Events, ", ")
			if recorded == "" {
				recorded = "none"
			}
			if len(res.Events) > 0 {
				if _, perr := s.WriteProjections(); perr != nil {
					r.warn("projections not regenerated: %v; run `ekumen project`", perr)
				}
			}
			return errf(CodeInternal, "reconcile stopped at action %d of %d (%s %s): %v; events recorded before it: %s; run reconcile again to finish",
				i+1, len(plan.Actions), act.Kind, act.Subject.ID, err, recorded)
		}
		res.Actions = append(res.Actions, ra)
		res.Events = append(res.Events, ra.Events...)
		res.Paths = append(res.Paths, ra.Paths...)
	}
	if len(res.Events) == 0 {
		return nil
	}
	ps, err := s.WriteProjections()
	res.Paths = append(res.Paths, ps...)
	if err != nil {
		r.warn("%v", err)
		return errf(CodeInternal, "events recorded (%s), projections not regenerated; run `ekumen project`", strings.Join(res.Events, ", "))
	}
	return nil
}

func (r *run) executeAction(s *project.Snapshot, a event.Actor, i int, act core.LoopAction) (reconcileAction, error) {
	ra := reconcileAction{Kind: act.Kind, Subject: subjectView(act.Subject), Detail: act.Detail, Events: []string{}, Paths: []string{}}
	if r.env.beforeAction != nil {
		if err := r.env.beforeAction(i); err != nil {
			return ra, err
		}
	}
	evs := append([]event.Event(nil), act.Events...)
	var written []string
	for _, am := range act.Amendments {
		p, rel, sum, err := r.writeAmendment(s, a, am.Task, []byte(am.Text))
		if err == nil {
			written = append(written, p)
			err = fillAmendment(evs, am.Event, rel, sum)
		}
		if err != nil {
			for _, p := range written {
				os.Remove(p)
			}
			return ra, err
		}
	}
	appended, err := r.appendEvents(s, evs, written)
	if err != nil {
		return ra, err
	}
	s.Absorb(evs...)
	for _, e := range evs {
		ra.Events = append(ra.Events, e.ID)
	}
	for _, p := range append(written, appended...) {
		ra.Paths = append(ra.Paths, s.Rel(p))
	}
	return ra, nil
}

func fillAmendment(evs []event.Event, id, file, sum string) error {
	for i, e := range evs {
		if e.ID != id {
			continue
		}
		var d event.TaskAmended
		if err := json.Unmarshal(e.Data, &d); err != nil {
			return errf(CodeInternal, "planned amendment %s: %v", id, err)
		}
		d.File, d.TextSHA256 = file, sum
		evs[i].Data, _ = json.Marshal(d)
		return nil
	}
	return errf(CodeInternal, "planned amendment %s has no event", id)
}

// report fills what the pass reports but does not do. Ready is unknown
// (null) when pull requests were not fully observed.
func (r *run) report(s *project.Snapshot, plan core.Plan, res *reconcileResult) {
	if s.PRsObserved == project.ObserveOK {
		res.Ready = []readyTask{}
		for _, id := range plan.Ready {
			t := s.State.Tasks[id]
			v := s.TaskView(id)
			res.Ready = append(res.Ready, readyTask{ID: id, Branch: t.Branch, Worktree: t.Worktree, Session: v.Session, Brief: v.Brief})
		}
	} else {
		r.warn("pull requests %s: ready tasks unknown; unblocked_tasks derived without them", s.PRsObserved)
	}
	res.NotStartable = []notStartable{}
	for _, id := range plan.NotStartable {
		t := s.State.Tasks[id]
		var missing []string
		if t.Branch == "" {
			missing = append(missing, "branch")
		}
		if t.Worktree == "" {
			missing = append(missing, "worktree")
		}
		res.NotStartable = append(res.NotStartable, notStartable{ID: id, Missing: missing, Decision: core.PlaceholderDecision})
	}
	if len(res.Notify) > 0 && !res.DryRun {
		r.warn("no session messaging exists yet; tell %s yourself", strings.Join(res.Notify, ", "))
	}
}

func subjectView(s event.Subject) project.Subject {
	return project.Subject{Kind: string(s.Kind), ID: s.ID}
}

func printReconcile(w io.Writer, res reconcileResult) {
	head := "reconcile"
	if res.DryRun {
		head += " --dry-run (nothing written)"
	}
	fmt.Fprintf(w, "%s: %d action(s), %d event(s); pull requests observed: %s\n", head, len(res.Actions), len(res.Events), res.Observed.PRs)
	for _, a := range res.Actions {
		fmt.Fprintf(w, "  %-8s %s\n", a.Kind, a.Detail)
	}
	if res.Ready == nil {
		fmt.Fprintln(w, "ready: unknown (pull requests not fully observed)")
	} else {
		var ids []string
		for _, t := range res.Ready {
			ids = append(ids, t.ID)
		}
		fmt.Fprintf(w, "ready: %s\n", orNone(ids))
	}
	var ns []string
	for _, t := range res.NotStartable {
		ns = append(ns, t.ID)
	}
	fmt.Fprintf(w, "not startable (no branch or worktree, %s): %s\n", core.PlaceholderDecision, orNone(ns))
	fmt.Fprintf(w, "unblocked: %s\n", orNone(res.Unblocked))
	fmt.Fprintf(w, "notify: %s\n", orNone(res.Notify))
	for _, p := range res.Paths {
		fmt.Fprintf(w, "  %s\n", p)
	}
}

func orNone(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}
