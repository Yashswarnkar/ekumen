package project

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Object shapes of C-cli-json §4. Field order follows the contract; extra
// fields (a minor change, §5) come last.

// PRView is an observed pull request.
type PRView struct {
	Number int    `json:"number"`
	State  string `json:"state"`
	Checks string `json:"checks"`
	URL    string `json:"url,omitempty"`
	Title  string `json:"title,omitempty"`
}

// ObservedView is a task's observed state. A nil *ObservedView is "unknown".
type ObservedView struct {
	PR *PRView `json:"pr"`
}

// Task is the C-cli-json Task shape.
type Task struct {
	ID            string        `json:"id"`
	Aliases       []string      `json:"aliases"`
	Title         string        `json:"title"`
	Kind          string        `json:"kind"`
	Status        string        `json:"status"`
	BlockedBy     []string      `json:"blocked_by"`
	BlockedByOpen []string      `json:"blocked_by_open"`
	ApprovedOn    *string       `json:"approved_on"`
	Branch        *string       `json:"branch"`
	Session       *string       `json:"session"`
	TierGrants    []string      `json:"tier_grants"`
	Observed      *ObservedView `json:"observed"`
	Brief         *string       `json:"brief"`
	// PendingUpdate is a TaskUpdated waiting for the lead's `task approve`
	// (D-placeholder-task-fields).
	PendingUpdate *PendingUpdate `json:"pending_update,omitempty"`
}

// PendingUpdate shows the proposed fields next to their current values.
type PendingUpdate struct {
	Fields  map[string]string `json:"fields"`
	Current map[string]string `json:"current"`
	Reason  string            `json:"reason"`
	Events  []string          `json:"events"`
}

// RaisedBy is raised_by with ids resolved.
type RaisedBy struct {
	Task    *string `json:"task"`
	Session string  `json:"session"`
	On      string  `json:"on"`
}

// Context is a decision's context.
type Context struct {
	Summary        string   `json:"summary"`
	Evidence       []string `json:"evidence"`
	PriorDecisions []string `json:"prior_decisions"`
}

// Applied is a decision's applied info.
type Applied struct {
	AmendedTasks     []string `json:"amended_tasks"`
	UnblockedTasks   []string `json:"unblocked_tasks"`
	NotifiedSessions []string `json:"notified_sessions"`
}

// RecordQuestion is one entry of a decision's questions thread
// (C-decision-record §1).
type RecordQuestion struct {
	ID         string  `json:"id"`
	At         string  `json:"at"`
	By         string  `json:"by"`
	To         string  `json:"to"`
	Text       string  `json:"text"`
	Answer     *string `json:"answer"`
	AnsweredBy *string `json:"answered_by"`
	AnsweredAt *string `json:"answered_at"`
}

// Decision is every C-decision-record §1 field plus priority and holds_up.
type Decision struct {
	ID                string           `json:"id"`
	Aliases           []string         `json:"aliases"`
	Title             string           `json:"title"`
	Tier              int              `json:"tier"`
	RaisedBy          RaisedBy         `json:"raised_by"`
	Question          string           `json:"question"`
	Options           []event.Option   `json:"options"`
	Recommendation    *string          `json:"recommendation"`
	DefaultIfNoAnswer *string          `json:"default_if_no_answer"`
	Deadline          *string          `json:"deadline"`
	Blocks            []string         `json:"blocks"`
	ContractImpact    []string         `json:"contract_impact"`
	Context           Context          `json:"context"`
	Status            string           `json:"status"`
	Questions         []RecordQuestion `json:"questions"`
	Answer            *string          `json:"answer"`
	AnswerNote        *string          `json:"answer_note"`
	DecidedBy         *string          `json:"decided_by"`
	DecidedOn         *string          `json:"decided_on"`
	SupersededBy      *string          `json:"superseded_by"`
	AppliedOn         *string          `json:"applied_on"`
	Applied           *Applied         `json:"applied"`
	Priority          float64          `json:"priority"`
	HoldsUp           []string         `json:"holds_up"`
}

// Subject names what a question is about.
type Subject struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Question is the C-cli-json Question shape.
type Question struct {
	ID         string  `json:"id"`
	Subject    Subject `json:"subject"`
	AskedBy    string  `json:"asked_by"`
	To         string  `json:"to"`
	Text       string  `json:"text"`
	AskedAt    string  `json:"asked_at"`
	Answer     *string `json:"answer"`
	AnsweredBy *string `json:"answered_by"`
	AnsweredAt *string `json:"answered_at"`
}

// InboxItem is the C-cli-json InboxItem shape.
type InboxItem struct {
	Kind      string   `json:"kind"`
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Priority  float64  `json:"priority"`
	Reason    string   `json:"reason"`
	CreatedAt string   `json:"created_at"`
	Subject   *Subject `json:"subject,omitempty"`
	// Action and Command are set on kind "action" items (C-cli-json v3):
	// approve, freeze or apply, and the command that does it.
	Action  string `json:"action,omitempty"`
	Command string `json:"command,omitempty"`
}

// Node is a graph node.
type Node struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Status string `json:"status"`
	// WaitingOn is set on decision nodes: "lead" for a pending decision or
	// one whose apply is in the lead's inbox (core.LeadActions), "loop" for
	// one decided or defaulted, not applied, and not yet the lead's.
	WaitingOn string `json:"waiting_on,omitempty"`
}

// Who a gating decision waits on (graph Node.WaitingOn).
const (
	WaitingOnLead = "lead"
	WaitingOnLoop = "loop"
)

// Edge says From is needed by To.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Entry is an amendment or handoff file.
type Entry struct {
	Path   string `json:"path"`
	Author string `json:"author"`
	At     string `json:"at"`
	SHA256 string `json:"sha256"`
	Text   string `json:"text"`
}

// Conflict is the C-cli-json Conflict shape.
type Conflict struct {
	Rule    string   `json:"rule"`
	Events  []string `json:"events"`
	Message string   `json:"message"`
}

// Contract is a contract as listed in projections.
type Contract struct {
	ID      string   `json:"id"`
	Aliases []string `json:"aliases"`
	Title   string   `json:"title"`
	Path    *string  `json:"path"`
	Status  string   `json:"status"`
	Version int      `json:"version"`
	// PendingVersion is a newer version recorded by `contract version`
	// and not frozen yet (C-cli-json v3).
	PendingVersion *int `json:"pending_version,omitempty"`
}

// External is an external dependency as listed in projections.
type External struct {
	ID        string   `json:"id"`
	Aliases   []string `json:"aliases"`
	What      string   `json:"what"`
	OwnerTeam string   `json:"owner_team"`
	AskedOn   string   `json:"asked_on"`
	NeededBy  string   `json:"needed_by"`
	Blocks    []string `json:"blocks"`
	Resolved  bool     `json:"resolved"`
}

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func list(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *Snapshot) resolveAll(ids []string) []string {
	out := []string{}
	for _, id := range ids {
		r := s.State.Resolve(id)
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// ActorString formats an actor as role:id.
func ActorString(a event.Actor) string { return string(a.Role) + ":" + a.ID }

// FormatOwner shows an addressee: role names as is, session names as
// session:<name>.
func FormatOwner(to string) string {
	if to == "" || event.Role(to).Known() {
		return to
	}
	return "session:" + to
}

// ---- tasks ----

// TaskIDs lists drafted tasks, sorted.
func (s *Snapshot) TaskIDs() []string {
	var out []string
	for _, id := range sortedKeys(s.State.Tasks) {
		if s.State.Tasks[id].Drafted() {
			out = append(out, id)
		}
	}
	return out
}

// RootPath turns a path recorded in an event (relative to .ekumen/, or to
// the project root) into the form CLI output uses: relative to the
// project root, e.g. ".ekumen/briefs/T-core/brief.md" (C-cli-json v2 §1).
// A path ResolvePath refuses is returned as recorded.
func (s *Snapshot) RootPath(p string) string {
	rp, err := s.ResolvePath(p)
	if err != nil || rp == "" {
		return p
	}
	return s.rel(rp)
}

// TaskView builds the Task shape for a drafted task. Its brief path is
// relative to the project root.
func (s *Snapshot) TaskView(id string) Task {
	t := s.State.Tasks[id]
	v := Task{
		ID: id, Aliases: list(t.Aliases), Title: t.Title, Kind: t.Kind,
		Status: string(s.TaskStatus(id)), BlockedBy: s.resolveAll(t.BlockedBy),
		BlockedByOpen: list(core.OpenBlockers(&s.State, s.Observed, id)),
		ApprovedOn:    ptr(t.ApprovedOn), Branch: ptr(t.Branch), Session: ptr(t.Session),
		TierGrants: list(t.TierGrants), Brief: ptr(s.RootPath(t.Brief)),
	}
	if u := t.PendingUpdate; u != nil {
		cur := map[string]string{"title": t.Title, "brief": t.Brief, "repo": t.Repo,
			"base_branch": t.BaseBranch, "branch": t.Branch, "worktree": t.Worktree}
		v.PendingUpdate = &PendingUpdate{Fields: u.Fields, Current: map[string]string{}, Reason: u.Reason, Events: u.Events}
		for k := range u.Fields {
			v.PendingUpdate.Current[k] = cur[k]
		}
	}
	if s.PRs != nil {
		v.Observed = &ObservedView{}
		if pr, ok := s.PRs[t.Branch]; ok && t.Branch != "" {
			v.Observed.PR = &PRView{Number: pr.Number, State: pr.State, Checks: pr.Checks, URL: pr.URL, Title: pr.Title}
		}
	}
	return v
}

// Tasks lists every task, optionally only those with a status.
func (s *Snapshot) Tasks(status string) []Task {
	out := []Task{}
	for _, id := range s.TaskIDs() {
		v := s.TaskView(id)
		if status == "" || v.Status == status {
			out = append(out, v)
		}
	}
	return out
}

// ---- decisions ----

// DecisionIDs lists raised decisions, sorted.
func (s *Snapshot) DecisionIDs() []string {
	var out []string
	for _, id := range sortedKeys(s.State.Decisions) {
		if len(s.State.Decisions[id].Raises) > 0 {
			out = append(out, id)
		}
	}
	return out
}

// HoldsUp lists what a decision holds up: its contract_impact plus the
// tasks core.BlockedByDecision reaches, sorted.
func (s *Snapshot) HoldsUp(id string) []string {
	d := s.State.Decisions[id]
	out := s.resolveAll(d.ContractImpact)
	for _, t := range core.BlockedByDecision(&s.State, id) {
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return out
}

func (s *Snapshot) questionsView(sub event.Subject, qs []core.Question) []RecordQuestion {
	out := []RecordQuestion{}
	for _, q := range qs {
		rq := RecordQuestion{ID: q.ID, At: q.At, By: s.actorOf(q.Event, q.By), To: FormatOwner(s.State.Addressee(sub, q)), Text: q.Text}
		if q.Answered() {
			rq.Answer, rq.AnsweredBy, rq.AnsweredAt = &q.Answer, ptr(s.actorOf(q.AnswerEvent, q.AnsweredBy)), ptr(q.AnsweredAt)
		}
		out = append(out, rq)
	}
	return out
}

func (s *Snapshot) actorOf(eventID, fallback string) string {
	if e, ok := s.byID[eventID]; ok {
		return ActorString(e.Actor)
	}
	return fallback
}

// DecisionView builds the Decision shape for a raised decision.
func (s *Snapshot) DecisionView(id string) Decision {
	d := s.State.Decisions[id]
	v := Decision{
		ID: id, Aliases: list(d.Aliases), Title: d.Title, Tier: d.Tier,
		RaisedBy: RaisedBy{Session: d.RaisedBy.Session, On: d.RaisedBy.On},
		Question: d.Question, Options: d.Options, Recommendation: d.Recommendation,
		DefaultIfNoAnswer: d.DefaultIfNoAnswer, Deadline: d.Deadline,
		Blocks: s.resolveAll(d.Blocks), ContractImpact: s.resolveAll(d.ContractImpact),
		Context: Context{Summary: d.Context.Summary, Evidence: list(d.Context.Evidence),
			PriorDecisions: s.resolveAll(d.Context.PriorDecisions)},
		Status:    d.Status,
		Questions: s.questionsView(event.Subject{Kind: event.KindDecision, ID: id}, d.Questions),
		Answer:    ptr(d.Answer), AnswerNote: ptr(d.AnswerNote), DecidedBy: ptr(d.DecidedBy),
		DecidedOn: ptr(d.DecidedOn), SupersededBy: ptr(s.State.Resolve(d.SupersededBy)), AppliedOn: ptr(d.AppliedOn),
		HoldsUp: s.HoldsUp(id),
	}
	if v.Options == nil {
		v.Options = []event.Option{}
	}
	if d.RaisedBy.Task != nil {
		t := s.State.Resolve(*d.RaisedBy.Task)
		v.RaisedBy.Task = &t
	}
	if d.Applied != nil {
		v.Applied = &Applied{list(d.Applied.AmendedTasks), list(d.Applied.UnblockedTasks), list(d.Applied.NotifiedSessions)}
	}
	if d.Status == core.StatusPending {
		v.Priority = float64(core.Priority(&s.State, id, s.Now))
	}
	return v
}

// Decisions lists every decision, optionally only those with a status.
func (s *Snapshot) Decisions(status string) []Decision {
	out := []Decision{}
	for _, id := range s.DecisionIDs() {
		if status == "" || s.State.Decisions[id].Status == status {
			out = append(out, s.DecisionView(id))
		}
	}
	return out
}

// ---- questions ----

// Questions lists every question on every subject, oldest first.
func (s *Snapshot) Questions() []Question {
	out := []Question{}
	add := func(kind event.SubjectKind, id string, qs []core.Question) {
		sub := event.Subject{Kind: kind, ID: id}
		for _, q := range qs {
			v := Question{ID: q.ID, Subject: Subject{string(kind), id}, AskedBy: s.actorOf(q.Event, q.By),
				To: FormatOwner(s.State.Addressee(sub, q)), Text: q.Text, AskedAt: q.At}
			if q.Answered() {
				v.Answer, v.AnsweredBy, v.AnsweredAt = &q.Answer, ptr(s.actorOf(q.AnswerEvent, q.AnsweredBy)), ptr(q.AnsweredAt)
			}
			out = append(out, v)
		}
	}
	for _, id := range sortedKeys(s.State.Tasks) {
		add(event.KindTask, id, s.State.Tasks[id].Questions)
	}
	for _, id := range sortedKeys(s.State.Decisions) {
		add(event.KindDecision, id, s.State.Decisions[id].Questions)
	}
	for _, id := range sortedKeys(s.State.Contracts) {
		add(event.KindContract, id, s.State.Contracts[id].Questions)
	}
	slices.SortStableFunc(out, func(a, b Question) int { return cmp.Or(cmp.Compare(a.AskedAt, b.AskedAt), cmp.Compare(a.ID, b.ID)) })
	return out
}

// FindQuestion finds a question by id on any subject.
func (s *Snapshot) FindQuestion(qid string) (event.Subject, core.Question, bool) {
	for _, q := range s.Questions() {
		if q.ID == qid {
			sub := event.Subject{Kind: event.SubjectKind(q.Subject.Kind), ID: q.Subject.ID}
			cq, ok := s.State.FindQuestion(sub, qid)
			return sub, cq, ok
		}
	}
	return event.Subject{}, core.Question{}, false
}

// NormalizeOwner turns "session:x" into "x"; role names and bare session
// names are returned as is.
func NormalizeOwner(o string) string {
	if r, id, ok := strings.Cut(o, ":"); ok {
		if r == string(event.RoleSession) || r == string(event.RoleIntegrator) {
			return id
		}
		if event.Role(r).Known() {
			return r
		}
	}
	return o
}

// ---- inbox ----

// Inbox lists what waits on owner (a role name or session name), ranked:
// for the lead, pending decisions by priority (core.PendingDecisions), then
// unrecorded conflicts, then the lead's next moves (kind "action",
// core.LeadActions: approve, freeze, apply); for the orchestrator, its
// next moves (core.OrchestratorActions: brief); then, for everyone, open
// routed items (questions and handoffs), oldest first.
func (s *Snapshot) Inbox(owner string) []InboxItem {
	out := []InboxItem{}
	if owner == string(event.RoleLead) {
		for _, r := range core.PendingDecisions(&s.State, s.Now) {
			d := s.State.Decisions[r.ID]
			reason := fmt.Sprintf("blocks %d task%s", r.Blocked, plural(r.Blocked))
			if d.Deadline != nil {
				reason += ", due " + *d.Deadline
			}
			out = append(out, InboxItem{Kind: "decision", ID: r.ID, Title: d.Title, Priority: float64(r.Priority),
				Reason: reason, CreatedAt: s.atOf(d.Raises[0].Event)})
		}
		for _, c := range s.Conflicts() {
			out = append(out, InboxItem{Kind: "conflict", ID: "conflict-" + c.Rule + "-" + strings.ToLower(first(c.Events)),
				Title: c.Detail, Reason: "rule " + c.Rule, CreatedAt: s.atOf(last(c.Events))})
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
		item := InboxItem{Kind: "route", ID: r.ID, Reason: r.Reason, CreatedAt: s.atOf(r.FromEvent), Subject: sub}
		if from, ok := s.byID[r.FromEvent]; ok {
			switch from.Type {
			case event.QuestionAskedType:
				var q event.QuestionAsked
				if p, err := from.Payload(); err == nil {
					q = *p.(*event.QuestionAsked)
				}
				item.Kind, item.ID, item.Title = "question", q.QuestionID, q.Text
			case event.TaskHandoffType:
				var h event.TaskHandoff
				if p, err := from.Payload(); err == nil {
					h = *p.(*event.TaskHandoff)
				}
				item.Title = fmt.Sprintf("%s handoff: %s", sub.ID, h.StatusLine)
			}
		}
		routes = append(routes, item)
	}
	slices.SortFunc(routes, func(a, b InboxItem) int {
		return cmp.Or(cmp.Compare(a.CreatedAt, b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return append(out, routes...)
}

// OrchestratorActions are the orchestrator's action items
// (core.OrchestratorActions): tasks drafted without a brief.
func (s *Snapshot) OrchestratorActions() []InboxItem {
	return s.actionItems(core.OrchestratorActions(&s.State, s.Observed, s.briefLookup))
}

func (s *Snapshot) actionItems(as []core.LeadAction) []InboxItem {
	out := []InboxItem{}
	for _, a := range as {
		out = append(out, InboxItem{Kind: "action", ID: a.Subject.ID, Title: s.actionTitle(a), Reason: a.Reason,
			CreatedAt: s.atOf(a.Event), Subject: &Subject{string(a.Subject.Kind), a.Subject.ID},
			Action: a.Action, Command: a.Command})
	}
	return out
}

func (s *Snapshot) briefLookup(id string) (string, bool, error) {
	p, err := s.BriefPath(id)
	if err != nil {
		return "", false, err
	}
	return s.rel(p), exists(p), nil
}

// hasBrief reports whether a task's brief file exists.
func (s *Snapshot) hasBrief(id string) bool {
	if _, ok := s.State.Tasks[id]; !ok {
		return false
	}
	p, err := s.BriefPath(id)
	return err == nil && exists(p)
}

// BriefPath is the file path of a task's §A: the brief recorded by
// TaskDrafted, else briefs/<id>/brief.md under .ekumen/. A recorded path
// ResolvePath refuses is an error.
func (s *Snapshot) BriefPath(id string) (string, error) {
	if t, ok := s.State.Tasks[id]; ok && t.Brief != "" {
		return s.ResolvePath(t.Brief)
	}
	return filepath.Join(s.EkumenDir(), "briefs", id, "brief.md"), nil
}

// AllTaskIDs lists every task id in the state, sorted, drafted or not
// (an orphan step can record entries on a task never drafted).
func (s *Snapshot) AllTaskIDs() []string { return sortedKeys(s.State.Tasks) }

func (s *Snapshot) actionTitle(a core.LeadAction) string {
	title := ""
	switch a.Subject.Kind {
	case event.KindTask:
		title = s.State.Tasks[a.Subject.ID].Title
	case event.KindContract:
		title = s.State.Contracts[a.Subject.ID].Title
	case event.KindDecision:
		title = s.State.Decisions[a.Subject.ID].Title
	}
	if a.Update != nil {
		return fmt.Sprintf("Approve update to %s: %s (%s)", a.Subject.ID, FormatUpdate(a.Update.Fields), a.Update.Reason)
	}
	verb := map[string]string{core.ActionApprove: "Approve", core.ActionFreeze: "Freeze", core.ActionApply: "Apply",
		core.ActionBrief: "Write the brief for"}[a.Action]
	return fmt.Sprintf("%s %s: %s", verb, a.Subject.ID, title)
}

// FormatUpdate lists update fields as "name value", in contract order.
func FormatUpdate(f map[string]string) string {
	var parts []string
	for _, k := range event.TaskUpdateFields {
		if v, ok := f[k]; ok {
			parts = append(parts, k+" "+v)
		}
	}
	return strings.Join(parts, ", ")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func last(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}

func (s *Snapshot) atOf(eventID string) string {
	if e, ok := s.byID[eventID]; ok {
		return e.At.UTC().Format(time.RFC3339)
	}
	return ""
}

// OpenRoutes counts open routes, recorded or pending.
func (s *Snapshot) OpenRoutes() int {
	n := 0
	for _, r := range s.Routes() {
		if r.Open {
			n++
		}
	}
	return n
}

// ---- graph ----

// Graph returns the task graph: drafted tasks, contracts, external
// dependencies, and decisions that still gate (raised, not applied, not
// superseded). An edge runs from what is needed to what needs it.
func (s *Snapshot) Graph() ([]Node, []Edge) {
	nodes := []Node{}
	in := map[string]bool{}
	addNode := func(n Node) { nodes = append(nodes, n); in[n.ID] = true }
	for _, id := range s.TaskIDs() {
		addNode(Node{ID: id, Kind: "task", Title: s.State.Tasks[id].Title, Status: string(s.TaskStatus(id))})
	}
	for _, id := range sortedKeys(s.State.Contracts) {
		c := s.State.Contracts[id]
		addNode(Node{ID: id, Kind: "contract", Title: c.Title, Status: c.Status})
	}
	applyDue := map[string]bool{}
	for _, a := range core.LeadActions(&s.State, s.Observed, s.Now, s.hasBrief) {
		if a.Action == core.ActionApply {
			applyDue[a.Subject.ID] = true
		}
	}
	var gating []string
	for _, id := range s.DecisionIDs() {
		d := s.State.Decisions[id]
		if d.AppliedOn == "" && d.Status != core.StatusSuperseded {
			gating = append(gating, id)
			waiting := WaitingOnLoop
			if d.Status == core.StatusPending || applyDue[id] {
				waiting = WaitingOnLead
			}
			addNode(Node{ID: id, Kind: "decision", Title: d.Title, Status: d.Status, WaitingOn: waiting})
		}
	}
	for _, id := range sortedKeys(s.State.Externals) {
		x := s.State.Externals[id]
		st := "open"
		if x.Resolved {
			st = "resolved"
		}
		addNode(Node{ID: id, Kind: "external", Title: x.What, Status: st})
	}
	edges := []Edge{}
	seen := map[Edge]bool{}
	add := func(from, to string) {
		e := Edge{s.State.Resolve(from), s.State.Resolve(to)}
		if in[e.From] && in[e.To] && e.From != e.To && !seen[e] {
			seen[e] = true
			edges = append(edges, e)
		}
	}
	for _, id := range s.TaskIDs() {
		for _, b := range s.State.Tasks[id].BlockedBy {
			add(b, id)
		}
	}
	for _, id := range gating {
		d := s.State.Decisions[id]
		for _, t := range d.Blocks {
			add(id, t)
		}
		for _, c := range d.ContractImpact {
			add(id, c)
		}
	}
	for _, id := range sortedKeys(s.State.Externals) {
		for _, t := range s.State.Externals[id].Blocks {
			add(id, t)
		}
	}
	slices.SortFunc(edges, func(a, b Edge) int { return cmp.Or(cmp.Compare(a.From, b.From), cmp.Compare(a.To, b.To)) })
	return nodes, edges
}

// ---- contracts, externals, conflicts ----

// ContractViews lists every contract.
func (s *Snapshot) ContractViews() []Contract {
	out := []Contract{}
	for _, id := range sortedKeys(s.State.Contracts) {
		c := s.State.Contracts[id]
		v := Contract{ID: id, Aliases: list(c.Aliases), Title: c.Title, Path: ptr(c.Path), Status: c.Status, Version: c.Version}
		if c.Pending != nil {
			n := c.Pending.Version
			v.PendingVersion = &n
		}
		out = append(out, v)
	}
	return out
}

// ExternalViews lists every external dependency.
func (s *Snapshot) ExternalViews() []External {
	out := []External{}
	for _, id := range sortedKeys(s.State.Externals) {
		x := s.State.Externals[id]
		out = append(out, External{ID: id, Aliases: list(x.Aliases), What: x.What, OwnerTeam: x.OwnerTeam,
			AskedOn: x.AskedOn, NeededBy: x.NeededBy, Blocks: s.resolveAll(x.Blocks), Resolved: x.Resolved})
	}
	return out
}

// ConflictViews runs the conflict rules and returns C-cli-json Conflicts.
func (s *Snapshot) ConflictViews() []Conflict {
	out := []Conflict{}
	for _, c := range s.Conflicts() {
		out = append(out, Conflict{Rule: c.Rule, Events: list(c.Events), Message: c.Detail})
	}
	return out
}
