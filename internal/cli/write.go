package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/project"
	"github.com/Yashswarnkar/ekumen/internal/store/files"
)

// writeResult is the C-cli-json §1 write output.
type writeResult struct {
	OK     bool     `json:"ok"`
	Events []string `json:"events"`
	Paths  []string `json:"paths"`
}

func (r *run) mkEvent(a event.Actor, typ event.Type, sub event.Subject, data any) (event.Event, error) {
	now := r.now()
	id, err := r.env.NewID(now)
	if err != nil {
		return event.Event{}, errf(CodeInternal, "new event id: %v", err)
	}
	e, err := event.New(id, typ, now, a, sub, data)
	if err != nil {
		return event.Event{}, errf(CodeInternal, "%v", err)
	}
	return e, nil
}

// probe checks the actor's lane for an event type and subject before any
// validation or file work, so a refusal is reported as a lane error.
func (r *run) probe(s *project.Snapshot, a event.Actor, typ event.Type, sub event.Subject) error {
	e := event.Event{Schema: event.SchemaVersion, Type: typ, At: r.now(), Actor: a, Subject: sub, Data: json.RawMessage("{}")}
	return r.checkLane(s, e)
}

// admit checks an event before it is written: its lane (C-event-log v3
// §3), the orchestrator's role claim (C-cli-json §1), and the envelope.
func (r *run) admit(s *project.Snapshot, e event.Event) error {
	if err := r.checkLane(s, e); err != nil {
		return err
	}
	if err := e.Validate(); err != nil {
		return errf(CodeValidation, "invalid event: %v", err)
	}
	return nil
}

// checkLane is admit without the envelope check, for probing before a
// file is written.
func (r *run) checkLane(s *project.Snapshot, e event.Event) error {
	if e.Actor.Role == event.RoleOrchestrator && e.Type != event.RoleClaimedType && e.Type != event.RoleReleasedType &&
		!s.HasClaim(event.RoleOrchestrator, e.Actor.ID) {
		holders := s.ClaimHolders(event.RoleOrchestrator)
		msg := "run `ekumen claim orchestrator` first"
		if len(holders) > 0 {
			msg = "the orchestrator role is held by " + strings.Join(holders, ", ")
		}
		return errf(CodeLane, "orchestrator writes need a live role claim: %s is not holding it; %s", e.Actor.ID, msg)
	}
	if err := s.CheckLane(e); err != nil {
		return errf(CodeLane, "refused: %v", err)
	}
	return nil
}

// commit appends events (all or none), then regenerates projections.
// written are files already created for this write (an amendment or
// handoff), removed again if the events can't be recorded.
func (r *run) commit(s *project.Snapshot, evs []event.Event, written []string, human string) error {
	var appended []string
	rollback := func() {
		for _, p := range append(appended, written...) {
			os.Remove(p)
		}
	}
	for _, e := range evs {
		if err := r.admit(s, e); err != nil {
			rollback()
			return err
		}
		b, err := event.Encode(e)
		if err != nil {
			rollback()
			return errf(CodeInternal, "encode: %v", err)
		}
		p, err := s.Store.Append(b)
		if err != nil {
			rollback()
			return errf(CodeInternal, "append: %v", err)
		}
		appended = append(appended, p)
	}
	res := writeResult{OK: true, Events: []string{}, Paths: []string{}}
	for _, e := range evs {
		res.Events = append(res.Events, e.ID)
	}
	for _, p := range append(written, appended...) {
		res.Paths = append(res.Paths, s.Rel(p))
	}
	if len(evs) > 0 {
		fresh, err := r.load()
		if err == nil {
			var ps []string
			ps, err = fresh.WriteProjections()
			res.Paths = append(res.Paths, ps...)
		}
		if err != nil {
			r.warn("events recorded, but projections were not regenerated: %v (run `ekumen project`)", err)
		}
	}
	r.out(res, func(w io.Writer) {
		fmt.Fprintln(w, human)
		for _, p := range res.Paths {
			fmt.Fprintf(w, "  %s\n", p)
		}
	})
	return nil
}

// strList is a repeatable, comma-separated flag.
type strList []string

func (l *strList) String() string { return strings.Join(*l, ",") }
func (l *strList) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*l = append(*l, p)
		}
	}
	return nil
}

func validationFrom(err error) error {
	var ve core.ValidationError
	if errors.As(err, &ve) && len(ve) > 0 {
		return fieldErr(CodeValidation, ve[0].Field, "%v", ve)
	}
	return errf(CodeValidation, "%v", err)
}

// ---- decisions ----

var recordFields = []string{"id", "aliases", "title", "tier", "raised_by", "question", "options", "recommendation",
	"default_if_no_answer", "deadline", "blocks", "contract_impact", "context"}

func (r *run) decisionRaise(args []string) error {
	fs := r.newFlags("decision raise")
	file := fs.String("file", "", "decision record (YAML), - for stdin")
	if _, err := r.parse(fs, args, 0, 0, "decision raise --file <d.yaml>"); err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	raw, err := r.readInput("file", *file)
	if err != nil {
		return err
	}
	v, err := yamlToJSON(raw)
	if err != nil {
		return errf(CodeValidation, "%s: %v", *file, err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return errf(CodeValidation, "%s: a decision file is one YAML mapping (one record per file)", *file)
	}
	for k := range m {
		if !slices.Contains(recordFields, k) && !slices.Contains(event.DecisionLifecycleFields, k) {
			return fieldErr(CodeValidation, k, "%s: unknown field %q (C-decision-record §1)", *file, k)
		}
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	// Fill raised_by from the actor when it is left out.
	today := r.now().Format(time.DateOnly)
	rb, _ := m["raised_by"].(map[string]any)
	if m["raised_by"] == nil {
		rb = map[string]any{"task": nil}
		if own := s.State.SessionTasks(a.ID); (a.Role == event.RoleSession || a.Role == event.RoleIntegrator) && len(own) == 1 {
			rb["task"] = own[0]
		}
		m["raised_by"] = rb
	}
	if rb != nil {
		if _, ok := rb["session"]; !ok {
			rb["session"] = a.ID
		}
		if _, ok := rb["on"]; !ok {
			rb["on"] = today
		}
		if _, ok := rb["task"]; !ok {
			rb["task"] = nil
		}
	}
	id, _ := m["id"].(string)
	data, err := json.Marshal(m)
	if err != nil {
		return errf(CodeValidation, "%v", err)
	}
	e, err := r.mkEvent(a, event.DecisionRaisedType, event.Subject{Kind: event.KindDecision, ID: id}, json.RawMessage(data))
	if err != nil {
		return err
	}
	if _, err := e.Payload(); err != nil {
		return errf(CodeValidation, "%s: %v", *file, err)
	}
	if event.KindOf(id) == event.KindDecision {
		if err := r.checkLane(s, e); err != nil {
			return err
		}
	}
	if err := core.ValidateDecisionRaised(&s.State, e); err != nil {
		return validationFrom(err)
	}
	return r.commit(s, []event.Event{e}, nil, "raised "+id)
}

func (r *run) decisionAnswer(args []string) error {
	fs := r.newFlags("decision answer")
	note := fs.String("note", "", "note; required with `other`")
	pos, err := r.parse(fs, args, 2, 2, "decision answer <id> <option|other> [--note t]")
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	id, err := r.findDecision(s, pos[0])
	if err != nil {
		return err
	}
	if err := r.probe(s, a, event.DecisionDecidedType, event.Subject{Kind: event.KindDecision, ID: id}); err != nil {
		return err
	}
	d := s.State.Decisions[id]
	answer := pos[1]
	switch {
	case d.Status != core.StatusPending:
		return fieldErr(CodeValidation, "id", "%s is already %s; decisions are never reopened (raise one that supersedes it)", id, d.Status)
	case answer == event.AnswerOther && strings.TrimSpace(*note) == "":
		return fieldErr(CodeValidation, "note", "answer `other` needs --note with the choice")
	case answer != event.AnswerOther && !slices.ContainsFunc(d.Options, func(o event.Option) bool { return o.ID == answer }):
		var ids []string
		for _, o := range d.Options {
			ids = append(ids, o.ID)
		}
		return fieldErr(CodeValidation, "answer", "%q is not an option of %s (options: %s, or other)", answer, id, strings.Join(ids, ", "))
	}
	e, err := r.mkEvent(a, event.DecisionDecidedType, event.Subject{Kind: event.KindDecision, ID: id}, event.DecisionDecided{Answer: answer, Note: *note})
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, fmt.Sprintf("decided %s: %s", id, answer))
}

func (r *run) decisionSupersede(args []string) error {
	fs := r.newFlags("decision supersede")
	by := fs.String("by", "", "the decision that replaces it")
	note := fs.String("note", "", "note")
	pos, err := r.parse(fs, args, 1, 1, "decision supersede <id> --by <id> [--note t]")
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	if *by == "" {
		return fieldErr(CodeUsage, "by", "--by <decision id> is required")
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	id, err := r.findDecision(s, pos[0])
	if err != nil {
		return err
	}
	if err := r.probe(s, a, event.DecisionSupersededType, event.Subject{Kind: event.KindDecision, ID: id}); err != nil {
		return err
	}
	byID, err := r.findDecision(s, *by)
	if err != nil {
		return err
	}
	if byID == id {
		return fieldErr(CodeValidation, "by", "a decision cannot supersede itself")
	}
	if st := s.State.Decisions[id].Status; st == core.StatusSuperseded {
		return fieldErr(CodeValidation, "id", "%s is already superseded", id)
	}
	e, err := r.mkEvent(a, event.DecisionSupersededType, event.Subject{Kind: event.KindDecision, ID: id}, event.DecisionSuperseded{By: byID, Note: *note})
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, fmt.Sprintf("superseded %s by %s", id, byID))
}

// ---- questions ----

func (r *run) questionAsk(args []string) error {
	fs := r.newFlags("question ask")
	text := fs.String("text", "", "the question")
	to := fs.String("to", "", "addressee: a role or session (default: C-event-log §4)")
	pos, err := r.parse(fs, args, 1, 1, "question ask <subject id> --text t [--to x]")
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*text) == "" {
		return fieldErr(CodeUsage, "text", "--text is required")
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	sid := s.State.Resolve(pos[0])
	kind := event.KindOf(sid)
	if kind != event.KindTask && kind != event.KindDecision && kind != event.KindContract {
		return fieldErr(CodeValidation, "subject", "questions are asked about a task, decision or contract, not %q", pos[0])
	}
	if !s.State.Has(sid) {
		return fieldErr(CodeNotFound, "subject", "no %s %q", kind, pos[0])
	}
	sub := event.Subject{Kind: kind, ID: sid}
	e, err := r.mkEvent(a, event.QuestionAskedType, sub, nil)
	if err != nil {
		return err
	}
	qid := "q-" + strings.ToLower(e.ID)
	data, _ := json.Marshal(event.QuestionAsked{QuestionID: qid, Text: *text, To: project.NormalizeOwner(*to)})
	e.Data = data
	return r.commit(s, []event.Event{e}, nil, fmt.Sprintf("asked %s on %s %s", qid, kind, sid))
}

func (r *run) questionAnswer(args []string) error {
	fs := r.newFlags("question answer")
	text := fs.String("text", "", "the answer")
	pos, err := r.parse(fs, args, 1, 1, "question answer <question id> --text t")
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*text) == "" {
		return fieldErr(CodeUsage, "text", "--text is required")
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	sub, q, ok := s.FindQuestion(pos[0])
	if !ok {
		return fieldErr(CodeNotFound, "id", "no question %q", pos[0])
	}
	if q.Answered() {
		return fieldErr(CodeValidation, "id", "question %s is already answered", q.ID)
	}
	e, err := r.mkEvent(a, event.QuestionAnsweredType, sub, event.QuestionAnswered{QuestionID: q.ID, Text: *text})
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, "answered "+q.ID)
}

// ---- tasks ----

type taskFile struct {
	ID         string   `json:"id"`
	Aliases    []string `json:"aliases"`
	Title      string   `json:"title"`
	Kind       string   `json:"kind"`
	Brief      string   `json:"brief"`
	Repo       string   `json:"repo"`
	BaseBranch string   `json:"base_branch"`
	Branch     string   `json:"branch"`
	Worktree   string   `json:"worktree"`
	BlockedBy  []string `json:"blocked_by"`
	TierGrants []string `json:"tier_grants"`
	Session    string   `json:"session"`
}

// taskDrafted adds the optional `session` of C-event-log v3 §6, which
// event.TaskDrafted does not have yet.
type taskDrafted struct {
	event.TaskDrafted
	Session string `json:"session,omitempty"`
}

func (r *run) taskDraft(args []string) error {
	fs := r.newFlags("task draft")
	file := fs.String("file", "", "task (YAML), - for stdin")
	if _, err := r.parse(fs, args, 0, 0, "task draft --file <t.yaml>"); err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	raw, err := r.readInput("file", *file)
	if err != nil {
		return err
	}
	v, err := yamlToJSON(raw)
	if err != nil {
		return errf(CodeValidation, "%s: %v", *file, err)
	}
	var t taskFile
	if err := decodeStrict(v, &t); err != nil {
		if f := unknownField(err); f != "" {
			return fieldErr(CodeValidation, f, "%s: unknown field %q", *file, f)
		}
		return errf(CodeValidation, "%s: %v", *file, err)
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	if err := r.probe(s, a, event.TaskDraftedType, event.Subject{Kind: event.KindTask, ID: t.ID}); err != nil {
		return err
	}
	switch {
	case event.ValidateIDOfKind(t.ID, event.KindTask) != nil:
		return fieldErr(CodeValidation, "id", "%v", event.ValidateIDOfKind(t.ID, event.KindTask))
	case s.State.Has(t.ID) || s.State.Resolve(t.ID) != t.ID:
		return fieldErr(CodeValidation, "id", "%q is already in use", t.ID)
	case strings.TrimSpace(t.Title) == "":
		return fieldErr(CodeValidation, "title", "required")
	case !slices.Contains([]string{event.TaskKindResearch, event.TaskKindContract, event.TaskKindBuild}, t.Kind):
		return fieldErr(CodeValidation, "kind", "must be research, contract or build, got %q", t.Kind)
	}
	for i, al := range t.Aliases {
		if err := event.ValidateAlias(al); err != nil {
			return fieldErr(CodeValidation, fmt.Sprintf("aliases[%d]", i), "%v", err)
		}
		if s.State.Has(al) {
			return fieldErr(CodeValidation, fmt.Sprintf("aliases[%d]", i), "%q is already in use", al)
		}
	}
	for i, b := range t.BlockedBy {
		rb := s.State.Resolve(b)
		if event.ValidateID(rb) != nil {
			return fieldErr(CodeValidation, fmt.Sprintf("blocked_by[%d]", i), "%q is not an id or known alias", b)
		}
		if !s.State.Has(rb) {
			r.warn("blocked_by %s names nothing yet; it blocks until it exists and clears", b)
		}
		t.BlockedBy[i] = rb
	}
	if t.Brief == "" {
		t.Brief = "briefs/" + t.ID + "/brief.md"
	}
	d := taskDrafted{TaskDrafted: event.TaskDrafted{Title: t.Title, Kind: t.Kind, Brief: t.Brief, Repo: t.Repo,
		BaseBranch: t.BaseBranch, Branch: t.Branch, Worktree: t.Worktree, BlockedBy: orEmpty(t.BlockedBy),
		TierGrants: orEmpty(t.TierGrants), Aliases: t.Aliases}, Session: t.Session}
	e, err := r.mkEvent(a, event.TaskDraftedType, event.Subject{Kind: event.KindTask, ID: t.ID}, d)
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, "drafted "+t.ID)
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (r *run) briefPath(s *project.Snapshot, id string) string {
	if b := s.State.Tasks[id].Brief; b != "" {
		return s.ResolvePath(b)
	}
	return filepath.Join(s.EkumenDir(), "briefs", id, "brief.md")
}

func (r *run) taskApprove(args []string) error {
	fs := r.newFlags("task approve")
	var grants strList
	fs.Var(&grants, "grants", "final tier grants, comma-separated (default: as drafted)")
	pos, err := r.parse(fs, args, 1, 1, "task approve <id> [--grants a,b]")
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	id, err := r.findTask(s, pos[0])
	if err != nil {
		return err
	}
	if err := r.probe(s, a, event.TaskApprovedType, event.Subject{Kind: event.KindTask, ID: id}); err != nil {
		return err
	}
	t := s.State.Tasks[id]
	if t.Approved {
		return fieldErr(CodeValidation, "id", "%s was approved on %s; §A is frozen (amend it instead)", id, t.ApprovedOn)
	}
	sum, err := files.HashFile(r.briefPath(s, id))
	if err != nil {
		return fieldErr(CodeNotFound, "brief", "%s: cannot hash the brief: %v", id, err)
	}
	final := t.TierGrants
	if flagSet(fs, "grants") {
		final = grants
	}
	e, err := r.mkEvent(a, event.TaskApprovedType, event.Subject{Kind: event.KindTask, ID: id},
		event.TaskApproved{TierGrants: orEmpty(final), TextSHA256: sum})
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, "approved "+id)
}

func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// authorSlug makes an entry-name author from an actor id.
func authorSlug(id string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(id), "-"), "-")
}

// taskEntry is `task amend` (amend true) and `task handoff`.
func (r *run) taskEntry(args []string, amend bool) error {
	name, usage := "task handoff", "task handoff <id> --text-file f"
	if amend {
		name, usage = "task amend", "task amend <id> --text-file f [--note t]"
	}
	fs := r.newFlags(name)
	textFile := fs.String("text-file", "", "markdown file, - for stdin")
	note := fs.String("note", "", "note recorded on the event (amend only)")
	pos, err := r.parse(fs, args, 1, 1, usage)
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	body, err := r.readInput("text-file", *textFile)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return fieldErr(CodeValidation, "text-file", "the file is empty")
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	id, err := r.findTask(s, pos[0])
	if err != nil {
		return err
	}
	sub := event.Subject{Kind: event.KindTask, ID: id}
	var (
		typ  = event.TaskHandoffType
		kind = files.Handoffs
		h    Handoff
	)
	if amend {
		typ, kind = event.TaskAmendedType, files.Amendments
	} else {
		h = ParseHandoff(string(body))
		if h.StatusLine == "" {
			return fieldErr(CodeValidation, "STATUS", "a handoff needs a STATUS section with a first line (sections found: %s)", strings.Join(h.Sections, ", "))
		}
	}
	// Check the lane before writing the file.
	if err := r.probe(s, a, typ, sub); err != nil {
		return err
	}
	author := authorSlug(a.ID)
	if author == "" {
		return errf(CodeValidation, "actor id %q gives an empty file-name slug", a.ID)
	}
	path, sum, err := s.Store.WriteEntry(id, kind, author, body)
	if err != nil {
		return errf(CodeInternal, "write %s: %v", kind, err)
	}
	rel, _ := s.Store.Rel(path)
	var data any = event.TaskHandoff{File: rel, TextSHA256: sum, StatusLine: h.StatusLine,
		HasFollowups: h.HasFollowups, HasDecisions: h.HasDecisions, HasDeviations: h.HasDeviations}
	if amend {
		data = event.TaskAmended{File: rel, TextSHA256: sum, Note: *note}
	}
	e, err := r.mkEvent(a, typ, sub, data)
	if err != nil {
		os.Remove(path)
		return err
	}
	verb := "handoff"
	if amend {
		verb = "amendment"
	}
	return r.commit(s, []event.Event{e}, []string{path}, fmt.Sprintf("%s recorded for %s", verb, id))
}

func (r *run) taskBlock(args []string) error {
	fs := r.newFlags("task block")
	var add, remove strList
	fs.Var(&add, "add", "ids to add to blocked_by (repeatable, comma-separated)")
	fs.Var(&remove, "remove", "ids to remove from blocked_by")
	reason := fs.String("reason", "", "why")
	pos, err := r.parse(fs, args, 1, 1, "task block <id> [--add x] [--remove y] --reason t")
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	if len(add) == 0 && len(remove) == 0 {
		return errf(CodeUsage, "task block: pass --add and/or --remove")
	}
	if strings.TrimSpace(*reason) == "" {
		return fieldErr(CodeUsage, "reason", "--reason is required")
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	id, err := r.findTask(s, pos[0])
	if err != nil {
		return err
	}
	if err := r.probe(s, a, event.TaskBlockersChangedType, event.Subject{Kind: event.KindTask, ID: id}); err != nil {
		return err
	}
	current := map[string]string{} // resolved → as written
	for _, b := range s.State.Tasks[id].BlockedBy {
		current[s.State.Resolve(b)] = b
	}
	added, removed := []string{}, []string{}
	for _, x := range add {
		rx := s.State.Resolve(x)
		if event.ValidateID(rx) != nil {
			return fieldErr(CodeValidation, "add", "%q is not an id or known alias", x)
		}
		if rx == id {
			return fieldErr(CodeValidation, "add", "a task cannot block itself")
		}
		if _, ok := current[rx]; ok {
			r.warn("%s already blocks %s", rx, id)
			continue
		}
		if !s.State.Has(rx) {
			r.warn("%s names nothing yet; it blocks until it exists and clears", x)
		}
		added = append(added, rx)
	}
	for _, x := range remove {
		rx := s.State.Resolve(x)
		w, ok := current[rx]
		if !ok {
			return fieldErr(CodeValidation, "remove", "%s is not in %s's blocked_by", x, id)
		}
		removed = append(removed, w)
	}
	if len(added) == 0 && len(removed) == 0 {
		return errf(CodeValidation, "nothing to change")
	}
	e, err := r.mkEvent(a, event.TaskBlockersChangedType, event.Subject{Kind: event.KindTask, ID: id},
		event.TaskBlockersChanged{Added: added, Removed: removed, Reason: *reason})
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, "blockers changed for "+id)
}

// ---- contracts, routes, roles ----

var frontVersionRe = regexp.MustCompile(`(?m)^version:\s*(\d+)\s*(#.*)?$`)

// frontMatterVersion reads `version: N` from a markdown file's front matter.
func frontMatterVersion(b []byte) int {
	text := string(b)
	if !strings.HasPrefix(text, "---") {
		return 0
	}
	end := strings.Index(text[3:], "\n---")
	if end < 0 {
		return 0
	}
	if m := frontVersionRe.FindStringSubmatch(text[3 : 3+end]); m != nil {
		v, _ := strconv.Atoi(m[1])
		return v
	}
	return 0
}

func (r *run) contractFreeze(args []string) error {
	fs := r.newFlags("contract freeze")
	pos, err := r.parse(fs, args, 1, 1, "contract freeze <id>")
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	id := s.State.Resolve(pos[0])
	c, ok := s.State.Contracts[id]
	if !ok || c.DraftedEvent == "" {
		return fieldErr(CodeNotFound, "id", "no contract %q", pos[0])
	}
	if err := r.probe(s, a, event.ContractFrozenType, event.Subject{Kind: event.KindContract, ID: id}); err != nil {
		return err
	}
	if c.Path == "" {
		return fieldErr(CodeValidation, "path", "contract %s has no file to freeze", id)
	}
	text, err := os.ReadFile(s.ResolvePath(c.Path))
	if err != nil {
		return fieldErr(CodeNotFound, "path", "read %s: %v", c.Path, err)
	}
	sum, _ := files.HashFile(s.ResolvePath(c.Path))
	version := frontMatterVersion(text)
	if version == 0 {
		version = c.Version + 1
	}
	for _, f := range append(slices.Clone(c.Freezes), c.Versions...) {
		if f.Version != version {
			continue
		}
		if f.TextSHA256 == sum {
			return fieldErr(CodeValidation, "version", "%s version %d is already frozen with this text", id, version)
		}
		return fieldErr(CodeConflict, "version", "%s version %d was frozen with different text; bump `version` in the front matter (a change needs a tier-2 decision)", id, version)
	}
	e, err := r.mkEvent(a, event.ContractFrozenType, event.Subject{Kind: event.KindContract, ID: id},
		event.ContractFrozen{Version: version, TextSHA256: sum})
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, fmt.Sprintf("froze %s version %d", id, version))
}

func (r *run) routeResolve(args []string) error {
	fs := r.newFlags("route resolve")
	note := fs.String("note", "", "what was done")
	pos, err := r.parse(fs, args, 1, 1, "route resolve <route id> --note t")
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*note) == "" {
		return fieldErr(CodeUsage, "note", "--note is required")
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	rt, ok := s.Routes()[pos[0]]
	if !ok {
		return fieldErr(CodeNotFound, "id", "no route %q", pos[0])
	}
	if !rt.Open {
		return fieldErr(CodeValidation, "id", "route %s is already closed", rt.ID)
	}
	e, err := r.mkEvent(a, event.RouteResolvedType, rt.Subject, event.RouteResolved{RouteID: rt.ID, Note: *note})
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, "resolved "+rt.ID)
}

func (r *run) claim(args []string, take bool) error {
	name := "release"
	if take {
		name = "claim"
	}
	fs := r.newFlags(name)
	pos, err := r.parse(fs, args, 1, 1, name+" <role>")
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	role := event.Role(pos[0])
	if !role.Known() || role == event.RoleLoop || role == event.RoleSession {
		return fieldErr(CodeUsage, "role", "cannot %s role %q (lead, orchestrator or integrator)", name, pos[0])
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	holders := s.ClaimHolders(role)
	held := slices.Contains(holders, a.ID)
	typ := event.RoleReleasedType
	switch {
	case take && held:
		r.out(writeResult{OK: true, Events: []string{}, Paths: []string{}}, func(w io.Writer) {
			fmt.Fprintf(w, "%s already holds %s\n", a.ID, role)
		})
		return nil
	case take && len(holders) > 0:
		return errf(CodeLane, "role %s is held by %s; it must be released first", role, strings.Join(holders, ", "))
	case take:
		typ = event.RoleClaimedType
	case !held:
		return errf(CodeLane, "%s does not hold role %s", a.ID, role)
	}
	var data any = event.RoleReleased{Role: role, Session: a.ID}
	if take {
		data = event.RoleClaimed{Role: role, Session: a.ID}
	}
	e, err := r.mkEvent(a, typ, event.Subject{Kind: event.KindProject, ID: event.ProjectSubjectID}, data)
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, fmt.Sprintf("%s %s for %s", map[bool]string{true: "claimed", false: "released"}[take], role, a.ID))
}
