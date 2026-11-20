package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
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
	Held         []reconcileSkip   `json:"held"`
	Skipped      []reconcileSkip   `json:"skipped"`
	Ready        []readyTask       `json:"ready"`
	NotStartable []notStartable    `json:"not_startable"`
	Unblocked    []string          `json:"unblocked"`
	Notify       []string          `json:"notify"`
	// OrchestratorActions are the orchestrator's action items (brief).
	OrchestratorActions []project.InboxItem `json:"orchestrator_actions"`
	Events              []string            `json:"events"`
	Paths               []string            `json:"paths"`
	Planned             []event.Event       `json:"planned,omitempty"`
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

type reconcileSkip struct {
	Kind    string          `json:"kind"`
	Subject project.Subject `json:"subject"`
	Reason  string          `json:"reason"`
	Code    string          `json:"code,omitempty"`
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

// heldByOther is why an action is dropped when another writer already did
// it between this pass's load and its write.
const heldByOther = "already done by another writer since this pass loaded the log"

// heldNotDecided is why an apply is dropped when the default it was
// planned after was not written.
const heldNotDecided = "still pending: its default was not written this pass"

// reconcile runs one pass of the control loop (D-control-loop): it plans
// with core.PlanPass and writes each action as its own all-or-none write,
// in plan order, admitting each against the state with the earlier
// actions absorbed. Pull requests are observed once, at load; projections
// are regenerated once, at the end, from that same observation. An action
// that fails is reported in skipped and the pass goes on.
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
	plan := core.PlanPass(core.PlanInput{Events: s.Events, Observed: s.Observed, Observation: s.PRsObserved, Now: r.now(),
		Conflicts: core.ConflictInputs{TextSHA256: s.FrozenHashes()}, Loop: a, NewID: r.env.NewID})
	res := reconcileResult{OK: true, DryRun: *dry, Observed: reconcileObserved{PRs: s.PRsObserved},
		Actions: []reconcileAction{}, Held: skipViews(plan.Held, ""), Skipped: skipViews(plan.Skipped, CodeValidation),
		Unblocked: plan.Unblocked, Notify: plan.Notify, Events: []string{}, Paths: []string{}}
	var failure *Error
	if *dry {
		res.Planned = []event.Event{}
		for _, act := range plan.Actions {
			ra := actionView(act)
			for _, e := range act.Events {
				ra.Events = append(ra.Events, e.ID)
				res.Events = append(res.Events, e.ID)
			}
			res.Planned = append(res.Planned, act.Events...)
			res.Actions = append(res.Actions, ra)
		}
	} else {
		failure = r.execute(s, a, plan, &res)
	}
	r.report(s, plan, &res)
	if failure != nil {
		return failure
	}
	if len(res.Skipped) > 0 {
		failure = &Error{Code: CodeValidation}
		for _, sk := range res.Skipped {
			if sk.Code == CodeInternal {
				failure.Code = CodeInternal
			}
		}
	}
	if failure != nil {
		res.OK = false
		failure.Silent = true
	}
	r.out(res, func(w io.Writer) { printReconcile(w, res) })
	if failure != nil {
		return failure
	}
	return nil
}

// execute writes the plan's actions, then the projections. An action
// that fails is rolled back, listed in res.Skipped with its error code,
// and the next action goes on; the next pass retries it.
func (r *run) execute(s *project.Snapshot, a event.Actor, plan core.Plan, res *reconcileResult) *Error {
	for _, act := range plan.Actions {
		ra, held, err := r.executeAction(s, a, act)
		switch {
		case err != nil:
			code := CodeInternal
			var ce *Error
			if errors.As(err, &ce) {
				code = ce.Code
			}
			res.Skipped = append(res.Skipped, reconcileSkip{Kind: act.Kind, Subject: subjectView(act.Subject), Reason: err.Error(), Code: code})
		case held != "":
			res.Held = append(res.Held, reconcileSkip{Kind: act.Kind, Subject: subjectView(act.Subject), Reason: held})
		default:
			res.Actions = append(res.Actions, ra)
			res.Events = append(res.Events, ra.Events...)
			res.Paths = append(res.Paths, ra.Paths...)
		}
	}
	if len(res.Events) == 0 {
		return nil
	}
	ps, err := s.WriteProjections()
	res.Paths = append(res.Paths, ps...)
	if err != nil {
		r.warn("%v", err)
		return projectionsErr(res.Events, res.Paths)
	}
	return nil
}

// executeAction writes one action, all or none. Before a default or an
// apply it reads the log again and drops the action (held) when another
// writer has done it since the load (issue #12 has no lock yet).
func (r *run) executeAction(s *project.Snapshot, a event.Actor, act core.LoopAction) (reconcileAction, string, error) {
	ra := actionView(act)
	if r.env.beforeAction != nil {
		if err := r.env.beforeAction(act); err != nil {
			return ra, "", err
		}
	}
	if act.Kind == core.LoopDefault || act.Kind == core.LoopApply {
		if err := s.Refresh(); err != nil {
			return ra, "", errf(CodeInternal, "%v", err)
		}
		if doneElsewhere(&s.State, act) {
			return ra, heldByOther, nil
		}
		if d := s.State.Decisions[act.Subject.ID]; act.Kind == core.LoopApply && d != nil {
			switch {
			case d.Status == core.StatusPending:
				return ra, heldNotDecided, nil
			case core.Contested(d):
				return ra, core.HeldContested, nil
			}
		}
		if act.Kind == core.LoopApply {
			var err error
			if act, err = r.replanApply(s, a, act); err != nil {
				return ra, "", err
			}
			ra.Detail = act.Detail
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
			return ra, "", err
		}
	}
	appended, err := r.appendEvents(s, evs, written)
	if err != nil {
		return ra, "", err
	}
	s.Absorb(evs...)
	for _, e := range evs {
		ra.Events = append(ra.Events, e.ID)
	}
	for _, p := range append(written, appended...) {
		ra.Paths = append(ra.Paths, s.Rel(p))
	}
	return ra, "", nil
}

// replanApply works out an apply again against the log as it is now, so
// that DecisionApplied never records unblocked_tasks that assumed an
// earlier action of this pass which was held or failed. Planned event ids
// are kept.
func (r *run) replanApply(s *project.Snapshot, a event.Actor, act core.LoopAction) (core.LoopAction, error) {
	id := act.Subject.ID
	eff, err := core.ApplyEffectOf(&s.State, s.Events, s.Observed, id, r.now())
	if err != nil {
		return act, errf(CodeValidation, "%v", err)
	}
	planned := map[string]event.Event{}
	for _, e := range act.Events {
		key := string(e.Type)
		if e.Type == event.TaskAmendedType {
			key = e.Subject.ID
		}
		planned[key] = e
	}
	mk := func(key string, typ event.Type, sub event.Subject, data any) (event.Event, error) {
		if p, ok := planned[key]; ok {
			e, err := event.New(p.ID, typ, p.At, a, sub, data)
			if err != nil {
				return e, errf(CodeInternal, "%v", err)
			}
			return e, nil
		}
		return r.mkEvent(a, typ, sub, data)
	}
	out := core.LoopAction{Kind: act.Kind, Subject: act.Subject}
	for _, tid := range eff.Amend {
		e, err := mk(tid, event.TaskAmendedType, event.Subject{Kind: event.KindTask, ID: tid},
			event.TaskAmended{Note: "applies " + id, ByDecision: id})
		if err != nil {
			return act, err
		}
		out.Events = append(out.Events, e)
		out.Amendments = append(out.Amendments, core.PlannedAmendment{Task: tid, Event: e.ID, Text: eff.Line + "\n"})
	}
	e, err := mk(string(event.DecisionAppliedType), event.DecisionAppliedType, act.Subject,
		event.DecisionApplied{AmendedTasks: eff.Tasks, UnblockedTasks: eff.Unblocked, NotifiedSessions: []string{}})
	if err != nil {
		return act, err
	}
	out.Events = append(out.Events, e)
	d := s.State.Decisions[id]
	out.Detail = fmt.Sprintf("apply %s (%s `%s`): amend %s; unblock %s", id, d.Status, d.Answer, orNone(eff.Amend), orNone(eff.Unblocked))
	return out, nil
}

// doneElsewhere reports whether the log already has what a default or
// apply would write: the decision is no longer pending (default), or is
// applied, or one of the tasks it would amend is amended for it (apply).
func doneElsewhere(st *core.State, act core.LoopAction) bool {
	d, ok := st.Decisions[act.Subject.ID]
	if !ok {
		return false
	}
	if act.Kind == core.LoopDefault {
		return d.Status != core.StatusPending
	}
	if d.AppliedOn != "" {
		return true
	}
	for _, am := range act.Amendments {
		if t, ok := st.Tasks[am.Task]; ok && core.AmendedFor(t, d.ID) {
			return true
		}
	}
	return false
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

// report fills what the pass reports but does not do. A dry run reports
// what the plan expects. A pass reports what it did: ready and
// not-startable tasks from the log as written, unblocked tasks and
// sessions to notify from the DecisionApplied events it wrote. Ready and
// not_startable are unknown (null) when pull requests were not fully
// observed.
func (r *run) report(s *project.Snapshot, plan core.Plan, res *reconcileResult) {
	ready, waiting := plan.Ready, plan.NotStartable
	if !res.DryRun {
		res.Unblocked, res.Notify = written(s, res.Events)
		if ready != nil {
			ready, waiting = []string{}, []string{}
			for _, id := range s.TaskIDs() {
				switch s.TaskStatus(id) {
				case core.TaskReady:
					ready = append(ready, id)
				case core.TaskApproved:
					waiting = append(waiting, id)
				}
			}
		}
	}
	if ready == nil {
		r.warn("pull requests %s: applies held, ready and not-startable tasks unknown", s.PRsObserved)
	} else {
		res.Ready = []readyTask{}
		for _, id := range ready {
			t := s.State.Tasks[id]
			v := s.TaskView(id)
			res.Ready = append(res.Ready, readyTask{ID: id, Branch: t.Branch, Worktree: t.Worktree, Session: v.Session, Brief: v.Brief})
		}
	}
	if waiting != nil {
		res.NotStartable = []notStartable{}
		for _, id := range waiting {
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
	}
	res.OrchestratorActions = s.OrchestratorActions()
	if len(res.Notify) > 0 && !res.DryRun {
		r.warn("no session messaging exists yet; tell %s yourself", strings.Join(res.Notify, ", "))
	}
}

// written lists, sorted, the tasks unblocked by the DecisionApplied events
// among ids, and the sessions of the tasks they amended.
func written(s *project.Snapshot, ids []string) (unblocked, notify []string) {
	unblocked, notify = []string{}, []string{}
	for _, id := range ids {
		e, ok := s.Event(id)
		if !ok || e.Type != event.DecisionAppliedType {
			continue
		}
		var d event.DecisionApplied
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		for _, t := range d.UnblockedTasks {
			if !slices.Contains(unblocked, t) {
				unblocked = append(unblocked, t)
			}
		}
		for _, t := range d.AmendedTasks {
			if task, ok := s.State.Tasks[t]; ok && task.Session != "" && !slices.Contains(notify, task.Session) {
				notify = append(notify, task.Session)
			}
		}
	}
	slices.Sort(unblocked)
	slices.Sort(notify)
	return unblocked, notify
}

func actionView(act core.LoopAction) reconcileAction {
	return reconcileAction{Kind: act.Kind, Subject: subjectView(act.Subject), Detail: act.Detail, Events: []string{}, Paths: []string{}}
}

func skipViews(ss []core.Skip, code string) []reconcileSkip {
	out := []reconcileSkip{}
	for _, s := range ss {
		out = append(out, reconcileSkip{Kind: s.Kind, Subject: subjectView(s.Subject), Reason: s.Reason, Code: code})
	}
	return out
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
	for _, h := range res.Held {
		fmt.Fprintf(w, "  held     %s %s: %s\n", h.Kind, h.Subject.ID, h.Reason)
	}
	for _, sk := range res.Skipped {
		fmt.Fprintf(w, "  skipped  %s %s (%s): %s\n", sk.Kind, sk.Subject.ID, sk.Code, sk.Reason)
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
	if res.NotStartable != nil {
		var ns []string
		for _, t := range res.NotStartable {
			ns = append(ns, t.ID)
		}
		fmt.Fprintf(w, "not startable (no branch or worktree, %s): %s\n", core.PlaceholderDecision, orNone(ns))
	}
	fmt.Fprintf(w, "unblocked: %s\n", orNone(res.Unblocked))
	fmt.Fprintf(w, "notify: %s\n", orNone(res.Notify))
	for _, a := range res.OrchestratorActions {
		fmt.Fprintf(w, "orchestrator: %s (%s)\n", a.Title, a.Command)
	}
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
