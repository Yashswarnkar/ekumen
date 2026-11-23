package core

import (
	"cmp"
	"slices"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Gates lists, sorted, the drafted tasks a decision gates directly: the
// tasks in its `blocks`, and the tasks whose blocked_by names it. Either
// side alone makes the gate (GitHub #63), so impact, ranking, `why` and
// applying all use this union. It ignores whether the decision still
// gates; see Impact for that.
func Gates(s *State, decisionID string) []string {
	id := s.Resolve(decisionID)
	d, ok := s.Decisions[id]
	if !ok {
		return nil
	}
	var out []string
	for _, b := range d.Blocks {
		if t, ok := s.Tasks[s.Resolve(b)]; ok && t.Drafted() {
			out = appendUnique(out, s.Resolve(b))
		}
	}
	for _, tid := range sortedKeys(s.Tasks) {
		t := s.Tasks[tid]
		if t.Drafted() && slices.ContainsFunc(t.BlockedBy, func(b string) bool { return s.Resolve(b) == id }) {
			out = appendUnique(out, tid)
		}
	}
	slices.Sort(out)
	return out
}

// Impact is what waits on one item (a task, decision, contract or
// external dependency), counting unfinished work only.
type Impact struct {
	ID string `json:"id"`
	// Direct are the unfinished tasks that name the item themselves (for a
	// decision: Gates), sorted.
	Direct []string `json:"direct"`
	// Unblocks are every unfinished task that waits on the item, directly
	// or through other unfinished tasks and the contracts they block,
	// sorted. A task is unfinished when it is drafted and neither done nor
	// deferred (DeriveTaskStatus, with observations).
	Unblocks []string `json:"unblocks"`
	// Paths has one chain per task in Unblocks: the item, each task or
	// contract in between, then the task. Each chain is a shortest one;
	// shorter chains come first, then by the task's id.
	Paths [][]string `json:"path"`
	// ContractContext is a decision's contract_impact: contracts the
	// answer may change. It is context only and never counts toward rank.
	ContractContext []string `json:"contract_context"`
}

// ImpactOf works out what waits on id (C-decision-record §1 "tasks held
// up", as narrowed by T-inbox-v1 and raised as D-priority-open-tasks):
//
//   - nothing waits on an item that no longer blocks: a decision applied
//     or superseded, a task that is done or deferred, a contract frozen
//     with no open blockers, a resolved external dependency;
//   - the direct dependents are the tasks and contracts whose blocked_by
//     names the item, plus a decision's or external's `blocks` (for a
//     decision, the tasks among them are Gates);
//   - from there, the walk follows blocked_by downstream through unfinished
//     tasks and through contracts, never through a done or deferred task,
//     and never through a decision's contract_impact.
//
// Only tasks are counted.
func ImpactOf(s *State, obs Observed, id string) Impact {
	id = s.Resolve(id)
	im := Impact{ID: id, Direct: []string{}, Unblocks: []string{}, Paths: [][]string{}, ContractContext: []string{}}
	d := newDeriver(s, obs)
	if dec, ok := s.Decisions[id]; ok {
		for _, c := range dec.ContractImpact {
			im.ContractContext = appendUnique(im.ContractContext, s.Resolve(c))
		}
		if !decisionGates(dec) {
			return im
		}
	} else if !s.Has(id) || d.blockerCleared(id) || d.finished(id) {
		return im
	}
	parent := map[string]string{id: ""}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range d.dependents(cur) {
			if _, seen := parent[next]; seen {
				continue
			}
			if event.KindOf(next) == event.KindTask && d.finished(next) {
				continue
			}
			parent[next] = cur
			queue = append(queue, next)
			if event.KindOf(next) == event.KindTask {
				im.Unblocks = append(im.Unblocks, next)
				if cur == id {
					im.Direct = append(im.Direct, next)
				}
			}
		}
	}
	slices.Sort(im.Unblocks)
	slices.Sort(im.Direct)
	for _, t := range im.Unblocks {
		var chain []string
		for n := t; n != ""; n = parent[n] {
			chain = append(chain, n)
		}
		slices.Reverse(chain)
		im.Paths = append(im.Paths, chain)
	}
	slices.SortStableFunc(im.Paths, func(a, b []string) int { return cmp.Compare(len(a), len(b)) })
	return im
}

// finished reports whether a task is done or deferred, or not a drafted
// task at all, so nothing is held up by it or waits through it.
func (d *deriver) finished(id string) bool {
	t, ok := d.s.Tasks[id]
	if !ok {
		return event.KindOf(id) == event.KindTask
	}
	if !t.Drafted() {
		return true
	}
	st := d.taskStatus(id)
	return st == TaskDone || st == TaskDeferred
}

// dependents lists, sorted, the tasks and contracts that wait on id
// directly: those whose blocked_by names it, plus a decision's or an
// external dependency's `blocks`.
func (d *deriver) dependents(id string) []string {
	s := d.s
	out := Gates(s, id)
	names := func(blockedBy []string) bool {
		return slices.ContainsFunc(blockedBy, func(b string) bool { return s.Resolve(b) == id })
	}
	for _, tid := range sortedKeys(s.Tasks) {
		if t := s.Tasks[tid]; t.Drafted() && names(t.BlockedBy) {
			out = appendUnique(out, tid)
		}
	}
	for _, cid := range sortedKeys(s.Contracts) {
		if names(s.Contracts[cid].BlockedBy) {
			out = appendUnique(out, cid)
		}
	}
	if x, ok := s.Externals[id]; ok {
		for _, b := range x.Blocks {
			if t, ok := s.Tasks[s.Resolve(b)]; ok && t.Drafted() {
				out = appendUnique(out, s.Resolve(b))
			}
		}
	}
	slices.Sort(out)
	return out
}

// Wait is one open blocker of an item and, in turn, what it waits on.
type Wait struct {
	ID string `json:"id"`
	// Why says, in a few words, why the blocker is still open.
	Why string `json:"why"`
	// On are the blocker's own open blockers. A blocker already shown
	// higher up the chain is listed with Repeat set and no On.
	On     []Wait `json:"on"`
	Repeat bool   `json:"repeat,omitempty"`
}

// Why each kind of blocker is still open.
const (
	WaitAnswer     = "waits on the lead's answer"
	WaitApply      = "decided, waits to be applied"
	WaitTask       = "not done yet"
	WaitDraft      = "contract still a draft, waits to be frozen"
	WaitContract   = "contract frozen, but its own blockers are open"
	WaitExternal   = "external dependency not resolved"
	WaitMissing    = "named but never created, so it can never clear"
	WaitCycleShown = "already shown above"
)

// WaitsOn lists what id waits on: its open blockers (OpenBlockers for a
// task, ContractOpenBlockers for a contract), each with its own open
// blockers, recursively. A decision or external dependency waits on no
// other item.
func WaitsOn(s *State, obs Observed, id string) []Wait {
	d := newDeriver(s, obs)
	return d.waits(s.Resolve(id), map[string]bool{s.Resolve(id): true})
}

func (d *deriver) waits(id string, shown map[string]bool) []Wait {
	var open []string
	switch event.KindOf(id) {
	case event.KindTask:
		open = d.openBlockers(id)
	case event.KindContract:
		open = d.contractOpenBlockers(id)
	}
	out := []Wait{}
	for _, b := range open {
		w := Wait{ID: b, Why: d.waitWhy(b), On: []Wait{}}
		if shown[b] {
			w.Repeat, w.Why = true, WaitCycleShown
		} else {
			shown[b] = true
			w.On = d.waits(b, shown)
		}
		out = append(out, w)
	}
	return out
}

func (d *deriver) waitWhy(id string) string {
	s := d.s
	if !s.Has(id) {
		return WaitMissing
	}
	switch event.KindOf(id) {
	case event.KindDecision:
		if dec := s.Decisions[id]; dec.Status == StatusPending {
			return WaitAnswer
		}
		return WaitApply
	case event.KindContract:
		if c := s.Contracts[id]; c.DraftedEvent == "" {
			return WaitMissing
		} else if c.Status != ContractFrozen {
			return WaitDraft
		}
		return WaitContract
	case event.KindExternal:
		return WaitExternal
	}
	if t := s.Tasks[id]; !t.Drafted() {
		return WaitMissing
	}
	return WaitTask + " (" + string(d.taskStatus(id)) + ")"
}
