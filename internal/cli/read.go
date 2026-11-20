package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/mapview"
	"github.com/Yashswarnkar/ekumen/internal/project"
	"github.com/Yashswarnkar/ekumen/internal/store/files"
)

// owner is the inbox owner for an actor: its session name for a session
// or integrator, otherwise its role.
func owner(a event.Actor) string {
	if a.Role == event.RoleSession || a.Role == event.RoleIntegrator {
		return a.ID
	}
	return string(a.Role)
}

func (r *run) status(args []string) error {
	fs := r.newFlags("status")
	if _, err := r.parse(fs, args, 0, 0, "status"); err != nil {
		return err
	}
	a, ok, err := r.actor(false)
	if err != nil {
		return err
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	who := string(event.RoleLead)
	if ok {
		who = owner(a)
	}
	inbox := s.Inbox(who)
	if len(inbox) > 5 {
		inbox = inbox[:5]
	}
	counts := s.Counts()
	v := struct {
		Project     string              `json:"project"`
		GeneratedAt string              `json:"generated_at"`
		Counts      project.StateCounts `json:"counts"`
		InboxTop    []project.InboxItem `json:"inbox_top"`
	}{s.ProjectName(), r.now().Format(time.RFC3339), counts, inbox}
	r.out(v, func(w io.Writer) {
		fmt.Fprintf(w, "%s · %s\n", s.ProjectName(), r.now().Format(time.RFC3339))
		var parts []string
		for _, k := range []string{"draft", "approved", "blocked", "ready", "running", "in_review", "done", "deferred"} {
			if n := counts.TasksByStatus[k]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, k))
			}
		}
		fmt.Fprintf(w, "tasks: %s\n", strings.Join(parts, ", "))
		fmt.Fprintf(w, "decisions open: %d · routes open: %d · conflicts: %d\n", counts.DecisionsOpen, counts.RoutesOpen, counts.Conflicts)
		fmt.Fprintf(w, "inbox (%s):\n", who)
		printInbox(w, inbox)
	})
	return nil
}

func printInbox(w io.Writer, items []project.InboxItem) {
	if len(items) == 0 {
		fmt.Fprintln(w, "  nothing waiting")
	}
	for _, i := range items {
		fmt.Fprintf(w, "  %-9s %-28s p=%-4g %s (%s)\n", i.Kind, i.ID, i.Priority, i.Title, i.Reason)
	}
}

func (r *run) inbox(args []string) error {
	fs := r.newFlags("inbox")
	forWho := fs.String("for", "", "lead, orchestrator or a session (default: your own role)")
	if _, err := r.parse(fs, args, 0, 0, "inbox [--for lead|orchestrator|<session>]"); err != nil {
		return err
	}
	who := project.NormalizeOwner(*forWho)
	if who == "" {
		a, ok, err := r.actor(false)
		if err != nil {
			return err
		}
		if !ok {
			return errf(CodeUsage, "inbox: pass --for, --as or set EKUMEN_ACTOR")
		}
		who = owner(a)
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	items := s.Inbox(who)
	r.out(struct {
		For   string              `json:"for"`
		Items []project.InboxItem `json:"items"`
	}{who, items}, func(w io.Writer) {
		fmt.Fprintf(w, "inbox for %s:\n", who)
		printInbox(w, items)
	})
	return nil
}

func (r *run) taskList(args []string) error {
	fs := r.newFlags("task list")
	status := fs.String("status", "", "only tasks with this status")
	if _, err := r.parse(fs, args, 0, 0, "task list [--status s]"); err != nil {
		return err
	}
	if *status != "" && !slices.Contains(taskStatuses, *status) {
		return fieldErr(CodeUsage, "status", "unknown task status %q (one of %s)", *status, strings.Join(taskStatuses, ", "))
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	tasks := s.Tasks(*status)
	r.out(map[string]any{"tasks": tasks}, func(w io.Writer) {
		for _, t := range tasks {
			fmt.Fprintf(w, "%-22s %-10s %s", t.ID, t.Status, t.Title)
			if len(t.BlockedByOpen) > 0 {
				fmt.Fprintf(w, "  [blocked by %s]", strings.Join(t.BlockedByOpen, ", "))
			}
			fmt.Fprintln(w)
		}
	})
	return nil
}

var taskStatuses = []string{"draft", "approved", "blocked", "ready", "running", "in_review", "done", "deferred"}

func (r *run) findTask(s *project.Snapshot, id string) (string, error) {
	tid := s.State.Resolve(id)
	if t, ok := s.State.Tasks[tid]; !ok || !t.Drafted() {
		return "", fieldErr(CodeNotFound, "id", "no task %q", id)
	}
	return tid, nil
}

func (r *run) findDecision(s *project.Snapshot, id string) (string, error) {
	did := s.State.Resolve(id)
	if d, ok := s.State.Decisions[did]; !ok || len(d.Raises) == 0 {
		return "", fieldErr(CodeNotFound, "id", "no decision %q", id)
	}
	return did, nil
}

// entries builds the Entry shape for brief files, taking author and time
// from the event that recorded each file when there is one, else from the
// file name.
func entries(s *project.Snapshot, es []files.Entry, recorded map[string]event.Event) []project.Entry {
	out := []project.Entry{}
	for _, e := range es {
		v := project.Entry{Path: s.Rel(e.Path), SHA256: e.SHA256, Text: string(e.Body)}
		if ev, ok := recorded[e.Rel]; ok {
			v.Author, v.At = project.ActorString(ev.Actor), ev.At.UTC().Format(time.RFC3339)
		} else {
			ts, slug, _ := strings.Cut(strings.TrimSuffix(e.Name, ".md"), "-")
			if t, err := time.Parse(files.TimestampFormat, ts); err == nil {
				v.At = t.UTC().Format(time.RFC3339)
			}
			v.Author = slug
		}
		out = append(out, v)
	}
	return out
}

// recordedEntries maps an entry's path (relative to .ekumen/) to the
// TaskAmended or TaskHandoff event that recorded it.
func recordedEntries(s *project.Snapshot, task string) map[string]event.Event {
	out := map[string]event.Event{}
	t := s.State.Tasks[task]
	for _, a := range t.Amendments {
		if e, ok := s.Event(a.Event); ok {
			out[strings.TrimPrefix(a.File, project.Dir+"/")] = e
		}
	}
	for _, h := range t.Handoffs {
		if e, ok := s.Event(h.Event); ok {
			out[strings.TrimPrefix(h.File, project.Dir+"/")] = e
		}
	}
	return out
}

func (r *run) taskShow(args []string) error {
	fs := r.newFlags("task show")
	pos, err := r.parse(fs, args, 1, 1, "task show <id>")
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
	brief := briefView{Amendments: []project.Entry{}, Handoffs: []project.Entry{}}
	b, warns, err := s.Store.ReadBrief(id)
	switch {
	case errors.Is(err, os.ErrNotExist):
		r.warn("task %s has no %s", id, filepath.ToSlash(filepath.Join(project.Dir, "briefs", id, "brief.md")))
	case err != nil:
		return errf(CodeInternal, "read brief: %v", err)
	default:
		rec := recordedEntries(s, id)
		brief = briefView{Text: string(b.Text), Amendments: entries(s, b.Amendments, rec), Handoffs: entries(s, b.Handoffs, rec)}
	}
	for _, w := range warns {
		r.warn("%s", w.String())
	}
	t := s.TaskView(id)
	r.out(struct {
		Task  project.Task `json:"task"`
		Brief briefView    `json:"brief"`
	}{t, brief}, func(w io.Writer) {
		fmt.Fprintf(w, "%s · %s\nstatus: %s\n", t.ID, t.Title, t.Status)
		if len(t.BlockedByOpen) > 0 {
			fmt.Fprintf(w, "blocked by: %s\n", strings.Join(t.BlockedByOpen, ", "))
		}
		fmt.Fprintf(w, "\n%s\n", brief.Text)
		for _, e := range brief.Amendments {
			fmt.Fprintf(w, "--- amendment %s by %s (%s)\n%s\n", e.At, e.Author, e.Path, e.Text)
		}
		for _, e := range brief.Handoffs {
			fmt.Fprintf(w, "--- handoff %s by %s (%s)\n%s\n", e.At, e.Author, e.Path, e.Text)
		}
	})
	return nil
}

// briefView is the `brief` of `task show`.
type briefView struct {
	Text       string          `json:"text"`
	Amendments []project.Entry `json:"amendments"`
	Handoffs   []project.Entry `json:"handoffs"`
}

func (r *run) decisionList(args []string) error {
	fs := r.newFlags("decision list")
	status := fs.String("status", "", "pending, decided, defaulted or superseded")
	if _, err := r.parse(fs, args, 0, 0, "decision list [--status s]"); err != nil {
		return err
	}
	if *status != "" && !slices.Contains([]string{core.StatusPending, core.StatusDecided, core.StatusDefaulted, core.StatusSuperseded}, *status) {
		return fieldErr(CodeUsage, "status", "unknown decision status %q", *status)
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	ds := s.Decisions(*status)
	r.out(map[string]any{"decisions": ds}, func(w io.Writer) {
		for _, d := range ds {
			ans := ""
			if d.Answer != nil {
				ans = " → " + *d.Answer
			}
			fmt.Fprintf(w, "%-34s t%d %-10s p=%-4g %s%s\n", d.ID, d.Tier, d.Status, d.Priority, d.Title, ans)
		}
	})
	return nil
}

func (r *run) decisionShow(args []string) error {
	fs := r.newFlags("decision show")
	pos, err := r.parse(fs, args, 1, 1, "decision show <id>")
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
	d := s.DecisionView(id)
	r.out(map[string]any{"decision": d}, func(w io.Writer) {
		b, _ := s.DecisionYAML(id)
		w.Write(b)
	})
	return nil
}

func (r *run) questionList(args []string) error {
	fs := r.newFlags("question list")
	forWho := fs.String("for", "", "only questions addressed to this role or session")
	open := fs.Bool("open", false, "only unanswered questions")
	if _, err := r.parse(fs, args, 0, 0, "question list [--for x] [--open]"); err != nil {
		return err
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	who := project.NormalizeOwner(*forWho)
	out := []project.Question{}
	for _, q := range s.Questions() {
		if *open && q.Answer != nil {
			continue
		}
		if who != "" && project.NormalizeOwner(q.To) != who {
			continue
		}
		out = append(out, q)
	}
	r.out(map[string]any{"questions": out}, func(w io.Writer) {
		for _, q := range out {
			state := "open"
			if q.Answer != nil {
				state = "answered: " + *q.Answer
			}
			fmt.Fprintf(w, "%s on %s %s, %s → %s: %s [%s]\n", q.ID, q.Subject.Kind, q.Subject.ID, q.AskedBy, q.To, q.Text, state)
		}
	})
	return nil
}

func (r *run) graph(args []string) error {
	fs := r.newFlags("graph")
	if _, err := r.parse(fs, args, 0, 0, "graph"); err != nil {
		return err
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	nodes, edges := s.Graph()
	r.out(struct {
		Nodes []project.Node `json:"nodes"`
		Edges []project.Edge `json:"edges"`
	}{nodes, edges}, func(w io.Writer) {
		for _, n := range nodes {
			fmt.Fprintf(w, "%-9s %-28s %-10s %s\n", n.Kind, n.ID, n.Status, n.Title)
		}
		for _, e := range edges {
			fmt.Fprintf(w, "%s -> %s\n", e.From, e.To)
		}
	})
	return nil
}

func (r *run) mapCmd(args []string) error {
	fs := r.newFlags("map")
	outPath := fs.String("out", "", "HTML file to write")
	fragment := fs.Bool("fragment", false, "write the page without <!doctype>, <html>, <head> and <body>, for embedding")
	linkBase := fs.String("link-base", "", "URL prefix for links to files (default: from the origin remote; none if unknown)")
	if _, err := r.parse(fs, args, 0, 0, "map --out <file.html> [--fragment] [--link-base url]"); err != nil {
		return err
	}
	if *outPath == "" {
		return fieldErr(CodeUsage, "out", "map: --out <file.html> is required")
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	nodes, edges := s.Graph()
	commit := ""
	if r.env.Commit != nil {
		commit = r.env.Commit(s.Root)
	}
	// Links point at the commit the map was made from, or else at origin's
	// default branch; with neither, no links (never a guessed branch).
	base := *linkBase
	if base == "" {
		ref := commit
		if ref == "" {
			ref = strings.TrimPrefix(r.git(s.Root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"), "origin/")
		}
		if base = mapview.LinkBase(r.remote(s.Root), ref); base != "" {
			base += r.git(s.Root, "rev-parse", "--show-prefix")
		}
	}
	render := mapview.Render
	if *fragment {
		render = mapview.RenderFragment
	}
	page, err := render(mapview.Data{
		GeneratedAt: r.now().Format(time.RFC3339), Commit: commit, Project: s.ProjectName(),
		Observed: s.PRs != nil, Counts: s.Counts(), Inbox: s.Inbox(string(event.RoleLead)),
		Nodes: nodes, Edges: edges, Tasks: s.Tasks(""), Decisions: s.Decisions(""),
		Links: mapLinks(s, base),
	})
	if err != nil {
		return errf(CodeInternal, "render map: %v", err)
	}
	p := *outPath
	if !filepath.IsAbs(p) {
		p = filepath.Join(r.env.Dir, p)
	}
	if err := os.WriteFile(p, page, 0o644); err != nil {
		return errf(CodeInternal, "write map: %v", err)
	}
	r.out(struct {
		OK    bool     `json:"ok"`
		Paths []string `json:"paths"`
	}{true, []string{s.Rel(p)}}, func(w io.Writer) {
		fmt.Fprintf(w, "wrote %s: %d nodes, %d edges, %d inbox items\n", s.Rel(p), len(nodes), len(edges), len(s.Inbox(string(event.RoleLead))))
	})
	return nil
}

// mapLinks builds the map's links to source files under base (paths
// relative to the project root): decisions to their YAML, tasks to their
// brief and latest handoff, contracts to their body. Empty base: no links.
func mapLinks(s *project.Snapshot, base string) map[string]mapview.Link {
	out := map[string]mapview.Link{}
	if base == "" {
		return out
	}
	url := func(p string) string { return mapview.FileURL(base, p) }
	for _, id := range s.DecisionIDs() {
		out[id] = mapview.Link{Source: url(project.Dir + "/" + project.DecisionsDir + "/" + id + ".yaml")}
	}
	for _, id := range s.TaskIDs() {
		t := s.State.Tasks[id]
		l := mapview.Link{Source: url(s.Rel(s.BriefPath(id)))}
		if n := len(t.Handoffs); n > 0 {
			l.Handoff = url(s.RootPath(t.Handoffs[n-1].File))
		}
		out[id] = l
	}
	for _, c := range s.ContractViews() {
		if c.Path != nil {
			out[c.ID] = mapview.Link{Source: url(s.RootPath(*c.Path))}
		}
	}
	return out
}

func (r *run) check(args []string) error {
	fs := r.newFlags("check")
	if _, err := r.parse(fs, args, 0, 0, "check"); err != nil {
		return err
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	invalid := append([]project.Invalid{}, s.Invalid...)
	// Brief entry files.
	briefs, _ := os.ReadDir(filepath.Join(s.EkumenDir(), "briefs"))
	for _, d := range briefs {
		if !d.IsDir() || event.ValidateIDOfKind(d.Name(), event.KindTask) != nil {
			continue
		}
		_, warns, err := s.Store.ReadBrief(d.Name())
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			invalid = append(invalid, project.Invalid{Path: s.Rel(filepath.Join(s.EkumenDir(), "briefs", d.Name())), Error: err.Error()})
		}
		for _, w := range warns {
			invalid = append(invalid, project.Invalid{Path: s.Rel(w.Path), Error: w.Reason})
		}
	}
	// Recorded entry files must still exist.
	recorded := map[string]bool{}
	for _, id := range s.AllTaskIDs() {
		t := s.State.Tasks[id]
		for _, f := range append(entryFiles(t.Amendments), handoffFiles(t.Handoffs)...) {
			recorded[s.RootPath(f)] = true
			if _, err := os.Stat(s.ResolvePath(f)); err != nil {
				invalid = append(invalid, project.Invalid{Path: s.RootPath(f), Error: "recorded by an event but missing"})
			}
		}
	}
	// And every entry file must be recorded by one (orphan-entry).
	invalid = append(invalid, orphanEntries(s, briefs, recorded)...)
	conflicts := s.ConflictViews()
	ok := len(conflicts) == 0 && len(invalid) == 0
	r.out(struct {
		OK        bool               `json:"ok"`
		Conflicts []project.Conflict `json:"conflicts"`
		Invalid   []project.Invalid  `json:"invalid"`
	}{ok, conflicts, invalid}, func(w io.Writer) {
		for _, c := range conflicts {
			fmt.Fprintf(w, "conflict %s: %s (%s)\n", c.Rule, c.Message, strings.Join(c.Events, ", "))
		}
		for _, i := range invalid {
			fmt.Fprintf(w, "invalid %s: %s\n", i.Path, i.Error)
		}
		if ok {
			fmt.Fprintf(w, "ok: %d events, no conflicts, no invalid files\n", len(s.Events))
		}
	})
	switch {
	case len(conflicts) > 0:
		return &Error{Code: CodeConflict, Silent: true}
	case len(invalid) > 0:
		return &Error{Code: CodeValidation, Silent: true}
	}
	return nil
}

// orphanEntries reports amendment and handoff files that no TaskAmended or
// TaskHandoff records (check rule orphan-entry, T-audit F-2026-11-20-3).
func orphanEntries(s *project.Snapshot, briefs []os.DirEntry, recorded map[string]bool) []project.Invalid {
	var out []project.Invalid
	for _, d := range briefs {
		if !d.IsDir() {
			continue
		}
		for _, kind := range []string{string(files.Amendments), string(files.Handoffs)} {
			dir := filepath.Join(s.EkumenDir(), "briefs", d.Name(), kind)
			ents, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range ents {
				if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
					continue
				}
				rel := s.Rel(filepath.Join(dir, e.Name()))
				if !recorded[rel] {
					out = append(out, project.Invalid{Path: rel,
						Error: "orphan-entry: no TaskAmended or TaskHandoff records this file (write entries with `ekumen task amend` or `ekumen task handoff`)"})
				}
			}
		}
	}
	return out
}

func entryFiles(es []core.Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.File)
	}
	return out
}

func handoffFiles(hs []core.Handoff) []string {
	var out []string
	for _, h := range hs {
		out = append(out, h.File)
	}
	return out
}

func (r *run) projectCmd(args []string) error {
	fs := r.newFlags("project")
	checkOnly := fs.Bool("check", false, "write nothing; fail if a committed per-item projection does not match the events")
	if _, err := r.parse(fs, args, 0, 0, "project [--check]"); err != nil {
		return err
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	if *checkOnly {
		return r.projectCheck(s)
	}
	paths, err := s.WriteProjections()
	if errors.Is(err, project.ErrNoEvents) {
		r.warn("%v", err)
		err = nil
	}
	if err != nil {
		return errf(CodeInternal, "projections: %v", err)
	}
	if paths == nil {
		paths = []string{}
	}
	r.out(map[string]any{"ok": true, "events": []string{}, "paths": paths}, func(w io.Writer) {
		fmt.Fprintf(w, "projections: %d file(s) changed\n", len(paths))
		for _, p := range paths {
			fmt.Fprintf(w, "  %s\n", p)
		}
	})
	return nil
}

// projectCheck is `ekumen project --check` (D-projections-in-git): the
// committed decisions/<id>.yaml must match the events. registry.yaml and
// state.json are local and git-ignored, so they are not checked.
func (r *run) projectCheck(s *project.Snapshot) error {
	if len(s.Events) == 0 {
		r.warn("%v", project.ErrNoEvents)
		r.out(map[string]any{"ok": true, "invalid": []project.Invalid{}}, func(w io.Writer) { fmt.Fprintln(w, "ok: no events") })
		return nil
	}
	invalid, err := s.CheckProjections()
	if err != nil {
		return errf(CodeInternal, "projections: %v", err)
	}
	ok := len(invalid) == 0
	r.out(struct {
		OK      bool              `json:"ok"`
		Invalid []project.Invalid `json:"invalid"`
	}{ok, invalid}, func(w io.Writer) {
		for _, i := range invalid {
			fmt.Fprintf(w, "invalid %s: %s\n", i.Path, i.Error)
		}
		if ok {
			fmt.Fprintf(w, "ok: %d decision file(s) match the events\n", len(s.DecisionIDs()))
		}
	})
	if !ok {
		return &Error{Code: CodeValidation, Silent: true}
	}
	return nil
}
