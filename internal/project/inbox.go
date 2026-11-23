package project

import (
	"cmp"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/observe"
)

// BranchAnswerGate is the decision that says how a decision raised on a
// branch is answered (raised by T-inbox-v1). Until it is made, branch
// decisions are shown but cannot be answered.
const BranchAnswerGate = "D-branch-decision-answer"

// BranchDecision is a decision raised on an open pull request's branch and
// not in the local log (GitHub #38).
type BranchDecision struct {
	ID     string
	Branch string
	PR     int
	// Raise is the DecisionRaised event read from the branch.
	Raise event.Event
	// State is the local state plus Raise, for impact.
	State core.State
}

// BranchDecisions reads, once per snapshot and read-only, the decisions
// raised on the head branches of open and draft pull requests that are not
// in the local log. A decision on several branches of a stack is credited
// to the lowest PR number. It is empty when observation is unavailable or
// the observer cannot read branches.
func (s *Snapshot) BranchDecisions() []BranchDecision {
	if s.branchRead {
		return s.branches
	}
	s.branchRead = true
	br, ok := s.observer.(observe.BranchReader)
	if !ok || s.PRList == nil {
		return nil
	}
	prOf := map[string]int{}
	var branches []string
	for _, p := range s.PRList {
		if p.State != observe.StateOpen && p.State != observe.StateDraft || p.Branch == "" {
			continue
		}
		if _, ok := prOf[p.Branch]; !ok {
			branches = append(branches, p.Branch)
		}
		if n, ok := prOf[p.Branch]; !ok || p.Number < n {
			prOf[p.Branch] = p.Number
		}
	}
	slices.Sort(branches)
	known := func(name string) bool {
		_, ok := s.byID[strings.SplitN(name, ".", 2)[0]]
		return ok
	}
	files, err := br.DecisionRaises(s.ctx, branches, known)
	if err != nil {
		s.warn("decisions on open branches unknown: " + err.Error())
		return nil
	}
	found := map[string]*BranchDecision{}
	for _, b := range branches {
		for _, f := range files[b] {
			e, err := event.Decode(f.Body)
			if err == nil {
				err = e.Validate()
			}
			if err != nil || e.Type != event.DecisionRaisedType || known(path.Base(f.Path)) {
				if err != nil {
					s.warn(fmt.Sprintf("%s on branch %s: %v", f.Path, b, err))
				}
				continue
			}
			id := e.Subject.ID
			if s.State.Has(id) {
				continue // raised on main too, under the same id
			}
			if cur, ok := found[id]; ok && cur.PR <= prOf[b] {
				continue
			}
			found[id] = &BranchDecision{ID: id, Branch: b, PR: prOf[b], Raise: e}
		}
	}
	for _, id := range sortedKeys(found) {
		bd := found[id]
		bd.State = core.Reduce(append(slices.Clone(s.Events), bd.Raise))
		s.branches = append(s.branches, *bd)
	}
	return s.branches
}

// FindBranchDecision returns the branch decision with this id.
func (s *Snapshot) FindBranchDecision(id string) (BranchDecision, bool) {
	for _, b := range s.BranchDecisions() {
		if b.ID == id {
			return b, true
		}
	}
	return BranchDecision{}, false
}

// ErrBranchDecision is wrapped when a command refuses a decision that is
// only on a branch.
var ErrBranchDecision = errors.New("raised on a branch, not on main")

// BranchAnswerBlocked explains why a branch decision cannot be answered
// yet, naming BranchAnswerGate.
func (b BranchDecision) BranchAnswerBlocked() error {
	return fmt.Errorf("%s is %w (branch %s, PR #%d). Answering it waits on %s, which decides how an answer on main "+
		"refers to a decision that is not on main yet (see `ekumen why %s`)", b.ID, ErrBranchDecision, b.Branch, b.PR,
		BranchAnswerGate, BranchAnswerGate)
}

// ---- inbox ----

// Inbox lists what waits on owner (a role name or session name), ranked,
// each item saying what to do, why, what it unblocks and the command:
//
//   - for the lead: pending decisions, including those raised on open
//     branches (BranchDecisions), by priority (unfinished tasks held up ×
//     urgency, core.ImpactOf), then earliest deadline, then id; then
//     unrecorded conflicts; then the lead's next moves (kind "action",
//     core.LeadActions: approve, freeze, apply);
//   - for the orchestrator: its next moves (core.OrchestratorActions);
//   - then, for everyone, open routed items (questions and handoffs),
//     oldest first.
func (s *Snapshot) Inbox(owner string) []InboxItem {
	out := []InboxItem{}
	if owner == string(event.RoleLead) {
		out = append(out, s.decisionItems()...)
		for _, c := range s.Conflicts() {
			out = append(out, s.fill(InboxItem{Kind: "conflict", ID: "conflict-" + c.Rule + "-" + strings.ToLower(first(c.Events)),
				Title: c.Detail, Reason: "rule " + c.Rule, CreatedAt: s.atOf(last(c.Events)), Command: "ekumen check",
				What: "Resolve a conflict in the log: " + c.Detail,
				Why:  "Two records disagree (rule " + c.Rule + "). Ekumen never settles this on its own, and derived state may be wrong until it is fixed."}))
		}
		out = append(out, s.actionItems(core.LeadActions(&s.State, s.Observed, s.Now, s.hasBrief))...)
	}
	if owner == string(event.RoleOrchestrator) {
		out = append(out, s.OrchestratorActions()...)
	}
	var routes []InboxItem
	for _, r := range s.Routes() {
		if !r.Open || r.To != owner {
			continue
		}
		sub := &Subject{string(r.Subject.Kind), s.State.Resolve(r.Subject.ID)}
		item := InboxItem{Kind: "route", ID: r.ID, Reason: r.Reason, CreatedAt: s.atOf(r.FromEvent), Subject: sub,
			Command: fmt.Sprintf("ekumen route resolve %s --note \"…\"", r.ID),
			What:    fmt.Sprintf("Read and resolve %s about %s", r.ID, sub.ID),
			Why:     "It was routed to you (" + r.Reason + ") and stays here until you resolve it."}
		if from, ok := s.byID[r.FromEvent]; ok {
			switch from.Type {
			case event.QuestionAskedType:
				var q event.QuestionAsked
				if p, err := from.Payload(); err == nil {
					q = *p.(*event.QuestionAsked)
				}
				item.Kind, item.ID, item.Title = "question", q.QuestionID, q.Text
				item.What = fmt.Sprintf("Answer %s's question about %s: %s", ActorString(from.Actor), sub.ID, oneLine(q.Text))
				item.Why = ActorString(from.Actor) + " asked and is waiting for the answer."
				item.Command = fmt.Sprintf("ekumen question answer %s --text \"…\"", q.QuestionID)
			case event.TaskHandoffType:
				var h event.TaskHandoff
				if p, err := from.Payload(); err == nil {
					h = *p.(*event.TaskHandoff)
				}
				item.Title = fmt.Sprintf("%s handoff: %s", sub.ID, h.StatusLine)
				item.What = fmt.Sprintf("Read %s's handoff and fold it in: %s", sub.ID, oneLine(h.StatusLine))
				item.Why = "The session handed off with items for you (" + r.Reason + ")."
			}
		}
		routes = append(routes, s.fill(item))
	}
	slices.SortFunc(routes, func(a, b InboxItem) int {
		return cmp.Or(cmp.Compare(a.CreatedAt, b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return append(out, routes...)
}

// decisionItems are the pending decisions on main and on open branches,
// ranked.
func (s *Snapshot) decisionItems() []InboxItem {
	type ranked struct {
		item     InboxItem
		deadline *string
	}
	var all []ranked
	add := func(st *core.State, id string, bd *BranchDecision) {
		d := st.Decisions[id]
		im := core.ImpactOf(st, s.Observed, id)
		item := InboxItem{Kind: "decision", ID: id, Title: d.Title,
			Priority:  float64(len(im.Unblocks) * core.Urgency(d.Deadline, s.Now)),
			Reason:    fmt.Sprintf("holds up %d unfinished task%s", len(im.Unblocks), plural(len(im.Unblocks))),
			CreatedAt: s.atOf(d.Raises[0].Event),
			What:      fmt.Sprintf("Decide %s: %s", id, d.Title),
			Why:       s.whyText(st, im, "decided") + dueText(d, s.Now),
			Command:   fmt.Sprintf("ekumen decision answer %s <option>", id),
		}
		if d.Deadline != nil {
			item.Reason += ", due " + *d.Deadline
		}
		for _, o := range d.Options {
			item.Options = append(item.Options, o.ID)
		}
		if d.Recommendation != nil {
			item.Recommendation = *d.Recommendation
		}
		if bd != nil {
			no := false
			item.CreatedAt = bd.Raise.At.UTC().Format(time.RFC3339)
			item.OnBranch, item.PR, item.Answerable, item.BlockedBy = bd.Branch, bd.PR, &no, BranchAnswerGate
			item.Reason += fmt.Sprintf(", raised on branch %s in PR #%d", bd.Branch, bd.PR)
			item.Command = "none yet: answering waits on " + BranchAnswerGate
		}
		setImpact(&item, im)
		all = append(all, ranked{item, d.Deadline})
	}
	for _, r := range core.PendingDecisions(&s.State, s.Observed, s.Now) {
		add(&s.State, r.ID, nil)
	}
	for _, bd := range s.BranchDecisions() {
		if d := bd.State.Decisions[bd.ID]; d != nil && d.Status == core.StatusPending {
			add(&bd.State, bd.ID, &bd)
		}
	}
	slices.SortStableFunc(all, func(a, b ranked) int {
		if c := cmp.Compare(b.item.Priority, a.item.Priority); c != 0 {
			return c
		}
		switch da, db := a.deadline, b.deadline; {
		case da != nil && db == nil:
			return -1
		case da == nil && db != nil:
			return 1
		case da != nil && db != nil && *da != *db:
			return cmp.Compare(*da, *db)
		}
		return cmp.Compare(a.item.ID, b.item.ID)
	})
	out := []InboxItem{}
	for _, r := range all {
		out = append(out, r.item)
	}
	return out
}

func setImpact(item *InboxItem, im core.Impact) {
	item.Unblocks, item.Path, item.Direct, item.ContractContext = im.Unblocks, im.Paths, im.Direct, im.ContractContext
}

// fill makes the list fields of an item empty lists rather than null.
func (s *Snapshot) fill(i InboxItem) InboxItem {
	if i.Unblocks == nil {
		i.Unblocks = []string{}
	}
	if i.Path == nil {
		i.Path = [][]string{}
	}
	if i.Direct == nil {
		i.Direct = []string{}
	}
	if i.ContractContext == nil {
		i.ContractContext = []string{}
	}
	return i
}

// OrchestratorActions are the orchestrator's action items
// (core.OrchestratorActions): tasks drafted without a brief.
func (s *Snapshot) OrchestratorActions() []InboxItem {
	return s.actionItems(core.OrchestratorActions(&s.State, s.Observed, s.briefLookup))
}

func (s *Snapshot) actionItems(as []core.LeadAction) []InboxItem {
	out := []InboxItem{}
	for _, a := range as {
		item := InboxItem{Kind: "action", ID: a.Subject.ID, Title: s.actionTitle(a), Reason: a.Reason,
			CreatedAt: s.atOf(a.Event), Subject: &Subject{string(a.Subject.Kind), a.Subject.ID},
			Action: a.Action, Command: a.Command}
		item.What = item.Title
		switch a.Action {
		case core.ActionApprove:
			if a.Update != nil {
				item.Why = "The task's details changed after you approved it; the change takes effect once you accept it."
			} else {
				item.Why = "Its brief is written and nothing blocks it, so it can start as soon as you approve it."
			}
		case core.ActionFreeze:
			im := core.ImpactOf(&s.State, s.Observed, a.Subject.ID)
			setImpact(&item, im)
			item.Why = capFirst(a.Reason) + ". " + s.whyText(&s.State, im, "frozen")
		case core.ActionApply:
			im := core.ImpactOf(&s.State, s.Observed, a.Subject.ID)
			setImpact(&item, im)
			item.Why = "It was " + a.Reason + ", so the briefs it changes are not amended yet. " + s.whyText(&s.State, im, "applied")
		case core.ActionBrief:
			item.Why = "The lead can't approve a task without a brief."
		}
		out = append(out, s.fill(item))
	}
	return out
}

// whyText says in plain words what waits on an item: "T-x can't start
// until this is decided." or "3 unfinished tasks wait on this: …".
func (s *Snapshot) whyText(st *core.State, im core.Impact, done string) string {
	verb := func(t string) string {
		switch core.DeriveTaskStatus(st, s.Observed, t) {
		case core.TaskRunning, core.TaskInReview:
			return "can't finish"
		}
		return "can't start"
	}
	switch len(im.Unblocks) {
	case 0:
		return "Nothing unfinished waits on this."
	case 1:
		t := im.Unblocks[0]
		if len(im.Direct) == 1 {
			return fmt.Sprintf("%s %s until this is %s.", t, verb(t), done)
		}
		p := im.Paths[0]
		return fmt.Sprintf("%s %s until this is %s (it waits on %s).", t, verb(t), done, strings.Join(p[1:len(p)-1], ", which waits on "))
	}
	var through []string
	for _, t := range im.Unblocks {
		if !slices.Contains(im.Direct, t) {
			through = append(through, t)
		}
	}
	msg := fmt.Sprintf("%d unfinished tasks wait on this: %s directly", len(im.Unblocks), joinAnd(im.Direct))
	if len(through) > 0 {
		msg += fmt.Sprintf(", and %s through them", joinAnd(through))
	}
	return msg + "."
}

// dueText is " Due …" for a decision with a deadline, naming the default
// a tier-1 decision falls back to.
func dueText(d *core.Decision, now time.Time) string {
	if d.Deadline == nil {
		return ""
	}
	due, err := time.Parse(time.DateOnly, *d.Deadline)
	if err != nil {
		return ""
	}
	today, _ := time.Parse(time.DateOnly, now.UTC().Format(time.DateOnly))
	days := int(due.Sub(today).Hours() / 24)
	when := fmt.Sprintf("in %d days", days)
	switch {
	case days < 0:
		when = fmt.Sprintf("overdue by %d day%s", -days, plural(-days))
	case days == 0:
		when = "today"
	case days == 1:
		when = "tomorrow"
	}
	out := fmt.Sprintf(" Due %s (%s).", *d.Deadline, when)
	if d.DefaultIfNoAnswer != nil {
		out += fmt.Sprintf(" If it is not answered by then, %s is chosen for you.", *d.DefaultIfNoAnswer)
	}
	return out
}

func joinAnd(ids []string) string {
	switch len(ids) {
	case 0:
		return ""
	case 1:
		return ids[0]
	}
	return strings.Join(ids[:len(ids)-1], ", ") + " and " + ids[len(ids)-1]
}

func capFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		s = s[:117] + "…"
	}
	return s
}

// OnBranch returns a copy of the snapshot whose state includes the branch
// decision's raise, so decision views work for it. Nothing is written.
func (s *Snapshot) OnBranch(bd BranchDecision) *Snapshot {
	cp := *s
	cp.State = bd.State
	cp.byID = make(map[string]event.Event, len(s.byID)+1)
	for k, v := range s.byID {
		cp.byID[k] = v
	}
	cp.byID[bd.Raise.ID] = bd.Raise
	cp.Events = append(slices.Clone(s.Events), bd.Raise)
	return &cp
}

// ---- why ----

// Why is `ekumen why <id>`: what an item waits on and what waits on it,
// counting unfinished work only.
type Why struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Status string `json:"status"`
	// OnBranch and PR are set for a decision raised on an open branch.
	OnBranch string `json:"on_branch,omitempty"`
	PR       int    `json:"pr,omitempty"`
	// Next says what would move the item on.
	Next string `json:"next"`
	// WaitsOn are its open blockers, each with its own (core.WaitsOn).
	WaitsOn []core.Wait `json:"waits_on"`
	// Why, Direct, Unblocks, Path and ContractContext describe what waits
	// on it (core.ImpactOf), as in InboxItem.
	Why             string     `json:"why"`
	Direct          []string   `json:"direct"`
	Unblocks        []string   `json:"unblocks"`
	Path            [][]string `json:"path"`
	ContractContext []string   `json:"contract_context"`
	// Statuses are the derived statuses of every task named above.
	Statuses map[string]string `json:"statuses"`
}

// WhyOf builds the Why view for id: a task, decision (on main or on an
// open branch), contract or external dependency. It wraps ErrNotFound for
// anything else.
func (s *Snapshot) WhyOf(id string) (Why, error) {
	st := &s.State
	rid := st.Resolve(id)
	w := Why{ID: rid, Kind: string(event.KindOf(rid)), Statuses: map[string]string{}}
	switch {
	case st.Has(rid):
	default:
		bd, ok := s.FindBranchDecision(rid)
		if !ok {
			return Why{}, fmt.Errorf("%q: %w", id, ErrNotFound)
		}
		st, w.OnBranch, w.PR = &bd.State, bd.Branch, bd.PR
	}
	switch event.KindOf(rid) {
	case event.KindTask:
		t := st.Tasks[rid]
		if !t.Drafted() {
			return Why{}, fmt.Errorf("%q: %w", id, ErrNotFound)
		}
		w.Title, w.Status = t.Title, string(core.DeriveTaskStatus(st, s.Observed, rid))
		w.Next = s.taskNext(rid, core.TaskStatus(w.Status))
		if w.Status == string(core.TaskDraft) && len(core.OpenBlockers(st, s.Observed, rid)) > 0 {
			w.Next = "its open blockers clearing (below), then the lead approving it"
		}
	case event.KindDecision:
		d := st.Decisions[rid]
		w.Title, w.Status = d.Title, d.Status
		switch {
		case w.OnBranch != "":
			w.Next = fmt.Sprintf("the lead's answer, which waits on %s: it is raised on branch %s (PR #%d), not on main", BranchAnswerGate, w.OnBranch, w.PR)
		case d.Status == core.StatusPending:
			w.Next = "the lead's answer: ekumen decision answer " + rid + " <option>"
		case d.Status == core.StatusSuperseded:
			w.Next = "nothing: it was superseded by " + st.Resolve(d.SupersededBy) + ", which carries the gate"
		case d.AppliedOn != "":
			w.Status += ", applied " + d.AppliedOn
			w.Next = "nothing: it is applied and no longer blocks anything"
		default:
			w.Next = "the loop applying it: ekumen reconcile --as loop:<session>"
		}
	case event.KindContract:
		c := st.Contracts[rid]
		w.Title, w.Status = c.Title, c.Status
		switch {
		case c.Status != core.ContractFrozen:
			w.Next = "the lead freezing it: ekumen contract freeze " + rid
		case len(core.ContractOpenBlockers(st, s.Observed, rid)) > 0:
			w.Next = "its open blockers clearing (below)"
		default:
			w.Next = "nothing: it is frozen with no open blockers"
		}
	case event.KindExternal:
		x := st.Externals[rid]
		w.Title, w.Status, w.Next = x.What, "open", "its owner ("+x.OwnerTeam+") delivering it, needed by "+x.NeededBy
		if x.Resolved {
			w.Status, w.Next = "resolved", "nothing: it is resolved"
		}
	default:
		return Why{}, fmt.Errorf("%q: %w", id, ErrNotFound)
	}
	w.WaitsOn = core.WaitsOn(st, s.Observed, rid)
	im := core.ImpactOf(st, s.Observed, rid)
	w.Why = s.whyText(st, im, map[string]string{"decision": "decided", "contract": "frozen", "task": "done", "external": "resolved"}[w.Kind])
	w.Direct, w.Unblocks, w.Path, w.ContractContext = im.Direct, im.Unblocks, im.Paths, im.ContractContext
	var note func([]core.Wait)
	note = func(ws []core.Wait) {
		for _, x := range ws {
			if event.KindOf(x.ID) == event.KindTask && st.Has(x.ID) {
				w.Statuses[x.ID] = string(core.DeriveTaskStatus(st, s.Observed, x.ID))
			}
			note(x.On)
		}
	}
	note(w.WaitsOn)
	for _, t := range im.Unblocks {
		w.Statuses[t] = string(core.DeriveTaskStatus(st, s.Observed, t))
	}
	return w, nil
}

func (s *Snapshot) taskNext(id string, st core.TaskStatus) string {
	switch st {
	case core.TaskDraft:
		return "the lead approving it: ekumen task approve " + id
	case core.TaskBlocked:
		return "its open blockers clearing (below)"
	case core.TaskApproved:
		return "a branch and worktree, so a session can start it"
	case core.TaskReady:
		return "a session starting it"
	case core.TaskRunning:
		if l, ok := s.Links[id]; ok && l.State == observe.StateDraft {
			return fmt.Sprintf("its session finishing and marking draft PR #%d ready for review", l.Number)
		}
		return "its session finishing and opening a PR for review"
	case core.TaskInReview:
		return "its PR being reviewed and merged"
	case core.TaskDone:
		return "nothing: it is done"
	case core.TaskDeferred:
		return "nothing: it is deferred"
	}
	return ""
}
