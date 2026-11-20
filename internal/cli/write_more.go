package cli

// Writes added by T-cli-gaps (C-cli-json v2 §3): decision apply, task
// defer and attach, contract draft and block, external add, update and
// resolve.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/project"
	"github.com/Yashswarnkar/ekumen/internal/store/files"
)

// noop prints a successful write that recorded nothing (an idempotent
// repeat).
func (r *run) noop(human string) error {
	r.out(writeResult{OK: true, Events: []string{}, Paths: []string{}}, func(w io.Writer) {
		fmt.Fprintln(w, human)
	})
	return nil
}

// ---- decision apply ----

// decisionApply writes DecisionApplied for a decided or defaulted
// decision, plus one §B amendment (TaskAmended) for each task in its
// `blocks` (C-decision-record §3, D-apply-before-loop). Applying twice
// writes nothing.
//
// Both are written by the loop (C-event-log v5 §3, D-apply-amend-lane):
// TaskAmended with data.by_decision set is in the loop's lane, so no
// orchestrator claim is needed. Until the loop exists the orchestrator
// runs this as loop:<its session> (D-apply-before-loop).
func (r *run) decisionApply(args []string) error {
	fs := r.newFlags("decision apply")
	textFile := fs.String("amend-text-file", "", "§B amendment text for each blocked task, - for stdin (default: a generated line)")
	pos, err := r.parse(fs, args, 1, 1, "decision apply <id> [--amend-text-file f]")
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	var text []byte
	if *textFile != "" {
		if text, err = r.readInput("amend-text-file", *textFile); err != nil {
			return err
		}
		if len(strings.TrimSpace(string(text))) == 0 {
			return fieldErr(CodeValidation, "amend-text-file", "the file is empty")
		}
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	id, err := r.findDecision(s, pos[0])
	if err != nil {
		return err
	}
	sub := event.Subject{Kind: event.KindDecision, ID: id}
	if err := r.probe(s, a, event.DecisionAppliedType, sub); err != nil {
		return err
	}
	d := s.State.Decisions[id]
	switch {
	case d.AppliedOn != "":
		return r.noop(fmt.Sprintf("%s was already applied on %s; nothing to do", id, d.AppliedOn))
	case d.Status == core.StatusSuperseded:
		return fieldErr(CodeValidation, "id", "%s is superseded; a superseded decision is never applied (C-decision-record §3)", id)
	case d.Status != core.StatusDecided && d.Status != core.StatusDefaulted:
		return fieldErr(CodeValidation, "id", "%s is %s; only a decided or defaulted decision is applied", id, d.Status)
	}

	var tasks []string
	for _, b := range d.Blocks {
		tid := s.State.Resolve(b)
		if t, ok := s.State.Tasks[tid]; !ok || !t.Drafted() {
			r.warn("%s blocks %s, which is not a drafted task; no amendment for it", id, b)
			continue
		}
		if !slices.Contains(tasks, tid) {
			tasks = append(tasks, tid)
		}
	}

	if text != nil && len(tasks) > 1 {
		if f, err := ParseEntryFront(string(text)); err == nil && f.Present {
			return fieldErr(CodeValidation, "front_matter", "--amend-text-file has front matter, but %s amends %d tasks (%s); drop it and each amendment gets its own", id, len(tasks), strings.Join(tasks, ", "))
		}
	}
	var (
		evs     []event.Event
		written []string
	)
	cleanup := func() {
		for _, p := range written {
			os.Remove(p)
		}
	}
	for _, tid := range tasks {
		body := text
		if body == nil {
			body = []byte(applyLine(d) + "\n")
		}
		body, err := r.entryBody(s, a, tid, body)
		if err != nil {
			cleanup()
			return err
		}
		p, sum, err := s.Store.WriteEntryAt(tid, files.Amendments, authorSlug(a.ID), r.now(), body)
		if err != nil {
			cleanup()
			return errf(CodeInternal, "write amendment for %s: %v", tid, err)
		}
		written = append(written, p)
		rel, _ := s.Store.Rel(p)
		e, err := r.mkEvent(a, event.TaskAmendedType, event.Subject{Kind: event.KindTask, ID: tid},
			event.TaskAmended{File: rel, TextSHA256: sum, Note: "applies " + id, ByDecision: id})
		if err != nil {
			cleanup()
			return err
		}
		evs = append(evs, e)
	}
	applied, err := r.mkEvent(a, event.DecisionAppliedType, sub, event.DecisionApplied{
		AmendedTasks: orEmpty(tasks), UnblockedTasks: []string{}, NotifiedSessions: []string{}})
	if err != nil {
		cleanup()
		return err
	}
	// Unblocked: tasks blocked before and not after.
	after := core.Reduce(append(slices.Clone(s.Events), append(slices.Clone(evs), applied)...))
	// The apply must take effect (T-audit F-2026-11-20-1): refuse rather
	// than record an event the reducer would ignore.
	if after.Decisions[id].AppliedOn == "" {
		cleanup()
		return errf(CodeInternal, "applying %s would not take effect in the log; nothing recorded", id)
	}
	var unblocked []string
	for _, tid := range s.TaskIDs() {
		if s.TaskStatus(tid) == core.TaskBlocked && core.DeriveTaskStatus(&after, s.Observed, tid) != core.TaskBlocked {
			unblocked = append(unblocked, tid)
		}
	}
	applied.Data, _ = json.Marshal(event.DecisionApplied{AmendedTasks: orEmpty(tasks), UnblockedTasks: orEmpty(unblocked), NotifiedSessions: []string{}})
	if len(tasks) > 0 {
		r.warn("no session messaging exists yet; tell the sessions of %s yourself", strings.Join(tasks, ", "))
	}
	return r.commit(s, append(evs, applied), written, fmt.Sprintf("applied %s: amended %d task(s), unblocked %d", id, len(tasks), len(unblocked)))
}

// applyLine is the generated §B amendment for an applied decision.
func applyLine(d *core.Decision) string {
	line := fmt.Sprintf("Applied %s (%s): `%s`, %s on %s.", d.ID, d.Title, d.Answer, d.Status, d.DecidedOn)
	if n := strings.TrimSpace(d.AnswerNote); n != "" {
		line += " Note: " + n
		if !strings.HasSuffix(n, ".") {
			line += "."
		}
	}
	return line + " See .ekumen/decisions/" + d.ID + ".yaml."
}

// ---- task defer / attach ----

func (r *run) taskDefer(args []string) error {
	fs := r.newFlags("task defer")
	reason := fs.String("reason", "", "why the task is parked")
	pos, err := r.parse(fs, args, 1, 1, "task defer <id> --reason t")
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
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
	sub := event.Subject{Kind: event.KindTask, ID: id}
	if err := r.probe(s, a, event.TaskDeferredType, sub); err != nil {
		return err
	}
	if t := s.State.Tasks[id]; t.Deferred {
		return fieldErr(CodeValidation, "id", "%s is already deferred (%s)", id, t.DeferredReason)
	}
	e, err := r.mkEvent(a, event.TaskDeferredType, sub, event.TaskDeferred{Reason: *reason})
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, "deferred "+id)
}

func (r *run) taskAttach(args []string) error {
	fs := r.newFlags("task attach")
	session := fs.String("session", "", "the session that owns the task")
	adapter := fs.String("adapter", "", "the agent adapter the session runs under (optional)")
	pos, err := r.parse(fs, args, 1, 1, "task attach <id> --session s [--adapter a]")
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	sess := project.NormalizeOwner(strings.TrimSpace(*session))
	if sess == "" {
		return fieldErr(CodeUsage, "session", "--session is required")
	}
	if strings.ContainsAny(sess, " \t\n:/") || event.Role(sess).Known() {
		return fieldErr(CodeValidation, "session", "%q is not a session name", *session)
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
	if err := r.probe(s, a, event.TaskSessionAttachedType, sub); err != nil {
		return err
	}
	if t := s.State.Tasks[id]; t.Session == sess && t.Adapter == *adapter {
		return r.noop(fmt.Sprintf("%s is already attached to %s", id, sess))
	}
	e, err := r.mkEvent(a, event.TaskSessionAttachedType, sub, event.TaskSessionAttached{Session: sess, Adapter: *adapter})
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, fmt.Sprintf("attached %s to session %s", id, sess))
}

// ---- blockers (tasks and contracts) ----

// blockerChange works out a TaskBlockersChanged or ContractBlockersChanged
// from --add and --remove against the current blocked_by.
func (r *run) blockerChange(s *project.Snapshot, self string, blockedBy []string, add, remove []string) (added, removed []string, err error) {
	current := map[string]string{} // resolved → as written
	for _, b := range blockedBy {
		current[s.State.Resolve(b)] = b
	}
	added, removed = []string{}, []string{}
	for _, x := range add {
		rx := s.State.Resolve(x)
		if event.ValidateID(rx) != nil {
			return nil, nil, fieldErr(CodeValidation, "add", "%q is not an id or known alias", x)
		}
		if rx == self {
			return nil, nil, fieldErr(CodeValidation, "add", "%s cannot block itself", self)
		}
		if _, ok := current[rx]; ok {
			r.warn("%s already blocks %s", rx, self)
			continue
		}
		if !s.State.Has(rx) {
			r.warn("%s names nothing yet; it blocks until it exists and clears", x)
		}
		added = append(added, rx)
	}
	for _, x := range remove {
		w, ok := current[s.State.Resolve(x)]
		if !ok {
			return nil, nil, fieldErr(CodeValidation, "remove", "%s is not in %s's blocked_by", x, self)
		}
		removed = append(removed, w)
	}
	if len(added) == 0 && len(removed) == 0 {
		return nil, nil, errf(CodeValidation, "nothing to change")
	}
	return added, removed, nil
}

// ---- contracts ----

type contractFile struct {
	ID        string   `json:"id"`
	Aliases   []string `json:"aliases"`
	Title     string   `json:"title"`
	Path      string   `json:"path"`
	BlockedBy []string `json:"blocked_by"`
}

func (r *run) findContract(s *project.Snapshot, id string) (string, error) {
	cid := s.State.Resolve(id)
	if c, ok := s.State.Contracts[cid]; !ok || c.DraftedEvent == "" {
		return "", fieldErr(CodeNotFound, "id", "no contract %q", id)
	}
	return cid, nil
}

func (r *run) contractDraft(args []string) error {
	fs := r.newFlags("contract draft")
	file := fs.String("file", "", "contract record (YAML), - for stdin")
	textFile := fs.String("text-file", "", "the contract's markdown body, copied to its path (optional if the file is there)")
	if _, err := r.parse(fs, args, 0, 0, "contract draft --file <c.yaml> [--text-file body.md]"); err != nil {
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
	var body []byte
	if *textFile != "" {
		if *file == "-" && *textFile == "-" {
			return fieldErr(CodeUsage, "text-file", "--file and --text-file cannot both be stdin")
		}
		if body, err = r.readInput("text-file", *textFile); err != nil {
			return err
		}
	}
	var c contractFile
	if err := r.decodeRecord(*file, raw, &c); err != nil {
		return err
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	sub := event.Subject{Kind: event.KindContract, ID: c.ID}
	if err := r.probe(s, a, event.ContractDraftedType, sub); err != nil {
		return err
	}
	switch {
	case event.ValidateIDOfKind(c.ID, event.KindContract) != nil:
		return fieldErr(CodeValidation, "id", "%v", event.ValidateIDOfKind(c.ID, event.KindContract))
	case s.State.Resolve(c.ID) != c.ID || s.State.Contracts[c.ID] != nil && s.State.Contracts[c.ID].DraftedEvent != "":
		return fieldErr(CodeValidation, "id", "%q is already in use", c.ID)
	case strings.TrimSpace(c.Title) == "":
		return fieldErr(CodeValidation, "title", "required")
	}
	if err := r.checkAliases(s, c.Aliases); err != nil {
		return err
	}
	blockedBy, err := r.resolveIDs(s, "blocked_by", c.BlockedBy)
	if err != nil {
		return err
	}
	if c.Path == "" {
		c.Path = "contracts/" + c.ID + ".md"
	}
	clean := path.Clean(filepath.ToSlash(c.Path))
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, project.Dir+"/") || !strings.HasSuffix(clean, ".md") {
		return fieldErr(CodeValidation, "path", "%q: want a .md path relative to %s/, e.g. contracts/%s.md", c.Path, project.Dir, c.ID)
	}
	c.Path = clean
	full := filepath.Join(s.EkumenDir(), filepath.FromSlash(clean))
	var written []string
	if body != nil {
		if len(strings.TrimSpace(string(body))) == 0 {
			return fieldErr(CodeValidation, "text-file", "the file is empty")
		}
		if err := createFile(full, body); err != nil {
			if errors.Is(err, os.ErrExist) {
				return fieldErr(CodeValidation, "path", "%s already exists; drop --text-file to draft the contract from it", s.Rel(full))
			}
			return errf(CodeInternal, "write %s: %v", s.Rel(full), err)
		}
		written = append(written, full)
	} else if _, err := os.Stat(full); err != nil {
		return fieldErr(CodeValidation, "path", "%s does not exist; pass --text-file with the contract's body", s.Rel(full))
	}
	e, err := r.mkEvent(a, event.ContractDraftedType, sub, event.ContractDrafted{Title: c.Title, Path: c.Path, Aliases: c.Aliases, BlockedBy: blockedBy})
	if err != nil {
		for _, p := range written {
			os.Remove(p)
		}
		return err
	}
	return r.commit(s, []event.Event{e}, written, "drafted "+c.ID)
}

// createFile writes a new file and never replaces one.
func createFile(p string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), files.DirMode); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, files.FileMode)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(p)
		return err
	}
	return f.Close()
}

func (r *run) contractBlock(args []string) error {
	fs := r.newFlags("contract block")
	var add, remove strList
	fs.Var(&add, "add", "ids to add to blocked_by (repeatable, comma-separated)")
	fs.Var(&remove, "remove", "ids to remove from blocked_by")
	reason := fs.String("reason", "", "why")
	pos, err := r.parse(fs, args, 1, 1, "contract block <id> [--add x] [--remove y] --reason t")
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	if len(add) == 0 && len(remove) == 0 {
		return errf(CodeUsage, "contract block: pass --add and/or --remove")
	}
	if strings.TrimSpace(*reason) == "" {
		return fieldErr(CodeUsage, "reason", "--reason is required")
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	id, err := r.findContract(s, pos[0])
	if err != nil {
		return err
	}
	sub := event.Subject{Kind: event.KindContract, ID: id}
	if err := r.probe(s, a, event.ContractBlockersChangedType, sub); err != nil {
		return err
	}
	added, removed, err := r.blockerChange(s, id, s.State.Contracts[id].BlockedBy, add, remove)
	if err != nil {
		return err
	}
	e, err := r.mkEvent(a, event.ContractBlockersChangedType, sub, event.ContractBlockersChanged{Added: added, Removed: removed, Reason: *reason})
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, "blockers changed for "+id)
}

// ---- external dependencies ----

// externalFields are the ExternalAdded fields `external update` may change.
var externalFields = []string{"what", "owner_team", "asked_on", "needed_by", "blocks"}

type externalFile struct {
	ID        string   `json:"id"`
	Aliases   []string `json:"aliases"`
	What      string   `json:"what"`
	OwnerTeam string   `json:"owner_team"`
	AskedOn   string   `json:"asked_on"`
	NeededBy  string   `json:"needed_by"`
	Blocks    []string `json:"blocks"`
}

func (r *run) findExternal(s *project.Snapshot, id string) (string, error) {
	xid := s.State.Resolve(id)
	if x, ok := s.State.Externals[xid]; !ok || x.AddedEvent == "" {
		return "", fieldErr(CodeNotFound, "id", "no external dependency %q", id)
	}
	return xid, nil
}

// checkExternal validates the fields an external dependency must have
// (spine §3: an owner, an asked-on date and a needed-by date).
func checkExternal(x externalFile) error {
	switch {
	case strings.TrimSpace(x.What) == "":
		return fieldErr(CodeValidation, "what", "required")
	case strings.TrimSpace(x.OwnerTeam) == "":
		return fieldErr(CodeValidation, "owner_team", "required")
	}
	for name, v := range map[string]string{"asked_on": x.AskedOn, "needed_by": x.NeededBy} {
		if _, err := time.Parse(time.DateOnly, v); err != nil {
			return fieldErr(CodeValidation, name, "want a UTC date YYYY-MM-DD, got %q", v)
		}
	}
	return nil
}

// externalBlocks resolves `blocks`, which names tasks.
func (r *run) externalBlocks(s *project.Snapshot, ids []string) ([]string, error) {
	out := []string{}
	for i, b := range ids {
		rb := s.State.Resolve(b)
		if event.ValidateIDOfKind(rb, event.KindTask) != nil {
			return nil, fieldErr(CodeValidation, fmt.Sprintf("blocks[%d]", i), "%q is not a task id or known alias", b)
		}
		if !s.State.Has(rb) {
			r.warn("blocks %s names no task yet", b)
		}
		if !slices.Contains(out, rb) {
			out = append(out, rb)
		}
	}
	return out, nil
}

func (r *run) externalAdd(args []string) error {
	fs := r.newFlags("external add")
	file := fs.String("file", "", "external dependency (YAML), - for stdin")
	if _, err := r.parse(fs, args, 0, 0, "external add --file f"); err != nil {
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
	var x externalFile
	if err := r.decodeRecord(*file, raw, &x); err != nil {
		return err
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	sub := event.Subject{Kind: event.KindExternal, ID: x.ID}
	if err := r.probe(s, a, event.ExternalAddedType, sub); err != nil {
		return err
	}
	switch {
	case event.ValidateIDOfKind(x.ID, event.KindExternal) != nil:
		return fieldErr(CodeValidation, "id", "%v", event.ValidateIDOfKind(x.ID, event.KindExternal))
	case s.State.Has(x.ID) || s.State.Resolve(x.ID) != x.ID:
		return fieldErr(CodeValidation, "id", "%q is already in use", x.ID)
	}
	if err := checkExternal(x); err != nil {
		return err
	}
	if err := r.checkAliases(s, x.Aliases); err != nil {
		return err
	}
	blocks, err := r.externalBlocks(s, x.Blocks)
	if err != nil {
		return err
	}
	e, err := r.mkEvent(a, event.ExternalAddedType, sub, event.ExternalAdded{What: x.What, OwnerTeam: x.OwnerTeam,
		AskedOn: x.AskedOn, NeededBy: x.NeededBy, Blocks: blocks, Aliases: x.Aliases})
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, "added "+x.ID)
}

func (r *run) externalUpdate(args []string) error {
	fs := r.newFlags("external update")
	file := fs.String("file", "", "the fields to change (YAML), - for stdin")
	pos, err := r.parse(fs, args, 1, 1, "external update <id> --file f")
	if err != nil {
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
		return errf(CodeValidation, "%s: want a YAML mapping of the fields to change", *file)
	}
	for k := range m {
		if !slices.Contains(externalFields, k) {
			return fieldErr(CodeValidation, k, "%s: %q cannot be updated (fields: %s)", *file, k, strings.Join(externalFields, ", "))
		}
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	id, err := r.findExternal(s, pos[0])
	if err != nil {
		return err
	}
	sub := event.Subject{Kind: event.KindExternal, ID: id}
	if err := r.probe(s, a, event.ExternalUpdatedType, sub); err != nil {
		return err
	}
	cur := s.State.Externals[id]
	if cur.Resolved {
		return fieldErr(CodeValidation, "id", "%s is resolved", id)
	}
	// Apply the change to a copy, validate the result, keep what changed.
	next := externalFile{ID: id, What: cur.What, OwnerTeam: cur.OwnerTeam, AskedOn: cur.AskedOn, NeededBy: cur.NeededBy, Blocks: cur.Blocks}
	if err := decodeStrict(m, &next); err != nil {
		return errf(CodeValidation, "%s: %v", *file, err)
	}
	if err := checkExternal(next); err != nil {
		return err
	}
	if _, set := m["blocks"]; set {
		if next.Blocks, err = r.externalBlocks(s, next.Blocks); err != nil {
			return err
		}
	}
	values := map[string]any{"what": next.What, "owner_team": next.OwnerTeam, "asked_on": next.AskedOn, "needed_by": next.NeededBy, "blocks": orEmpty(next.Blocks)}
	olds := map[string]any{"what": cur.What, "owner_team": cur.OwnerTeam, "asked_on": cur.AskedOn, "needed_by": cur.NeededBy, "blocks": orEmpty(cur.Blocks)}
	fields := map[string]json.RawMessage{}
	for _, k := range externalFields {
		if _, set := m[k]; !set {
			continue
		}
		nb, _ := json.Marshal(values[k])
		ob, _ := json.Marshal(olds[k])
		if string(nb) != string(ob) {
			fields[k] = nb
		}
	}
	if len(fields) == 0 {
		return errf(CodeValidation, "nothing to change")
	}
	e, err := r.mkEvent(a, event.ExternalUpdatedType, sub, event.ExternalUpdated{Fields: fields})
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, "updated "+id)
}

func (r *run) externalResolve(args []string) error {
	fs := r.newFlags("external resolve")
	note := fs.String("note", "", "how it was resolved")
	pos, err := r.parse(fs, args, 1, 1, "external resolve <id> --note t")
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
	id, err := r.findExternal(s, pos[0])
	if err != nil {
		return err
	}
	sub := event.Subject{Kind: event.KindExternal, ID: id}
	if err := r.probe(s, a, event.ExternalResolvedType, sub); err != nil {
		return err
	}
	if s.State.Externals[id].Resolved {
		return fieldErr(CodeValidation, "id", "%s is already resolved", id)
	}
	e, err := r.mkEvent(a, event.ExternalResolvedType, sub, event.ExternalResolved{Note: *note})
	if err != nil {
		return err
	}
	return r.commit(s, []event.Event{e}, nil, "resolved "+id)
}

// ---- shared input helpers ----

// decodeRecord parses a YAML record file into dst, refusing unknown fields.
func (r *run) decodeRecord(name string, raw []byte, dst any) error {
	v, err := yamlToJSON(raw)
	if err != nil {
		return errf(CodeValidation, "%s: %v", name, err)
	}
	if _, ok := v.(map[string]any); !ok {
		return errf(CodeValidation, "%s: want one YAML mapping", name)
	}
	if err := decodeStrict(v, dst); err != nil {
		if f := unknownField(err); f != "" {
			return fieldErr(CodeValidation, f, "%s: unknown field %q", name, f)
		}
		return errf(CodeValidation, "%s: %v", name, err)
	}
	return nil
}

func (r *run) checkAliases(s *project.Snapshot, aliases []string) error {
	for i, al := range aliases {
		if err := event.ValidateAlias(al); err != nil {
			return fieldErr(CodeValidation, fmt.Sprintf("aliases[%d]", i), "%v", err)
		}
		if s.State.Has(al) || s.State.Resolve(al) != al {
			return fieldErr(CodeValidation, fmt.Sprintf("aliases[%d]", i), "%q is already in use", al)
		}
	}
	return nil
}

// resolveIDs resolves a list of blocker ids or aliases; ids that name
// nothing yet are allowed with a warning (they block until they clear).
func (r *run) resolveIDs(s *project.Snapshot, field string, ids []string) ([]string, error) {
	var out []string
	for i, b := range ids {
		rb := s.State.Resolve(b)
		if event.ValidateID(rb) != nil {
			return nil, fieldErr(CodeValidation, fmt.Sprintf("%s[%d]", field, i), "%q is not an id or known alias", b)
		}
		if !s.State.Has(rb) {
			r.warn("%s %s names nothing yet; it blocks until it exists and clears", field, b)
		}
		if !slices.Contains(out, rb) {
			out = append(out, rb)
		}
	}
	return out, nil
}
