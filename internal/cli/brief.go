package cli

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/project"
)

// builtinBriefTemplate is the brief scaffold used when the project has no
// .ekumen/templates/brief.md. Placeholders are {{name}}; see renderBrief.
//
//go:embed brief_template.md
var builtinBriefTemplate string

// briefTemplatePath is a project's own brief template, under .ekumen/.
const briefTemplatePath = "templates/brief.md"

const goalHint = core.BriefHint + ": what this task delivers and why it matters now; name the spine section and the GitHub issue."

// errGHMissing is Env.GH's error when the gh CLI is not installed.
var errGHMissing = errors.New("the gh CLI is not installed")

// ghRun is the real Env.GH: `gh <args>` in root.
func ghRun(root string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errGHMissing
	}
	cmd := exec.Command("gh", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
	}
	return out, err
}

func (r *run) briefCmd(sub string, args []string) error {
	switch sub {
	case "new":
		return r.briefNew(args)
	case "check":
		return r.briefCheck(args)
	}
	return errf(CodeUsage, "unknown command %q", strings.TrimSpace("brief "+sub))
}

// briefTemplate is the project's brief template, else the built-in one.
func briefTemplate(s *project.Snapshot) (string, error) {
	b, err := os.ReadFile(filepath.Join(s.EkumenDir(), filepath.FromSlash(briefTemplatePath)))
	switch {
	case err == nil:
		return string(b), nil
	case errors.Is(err, os.ErrNotExist):
		return builtinBriefTemplate, nil
	}
	return "", err
}

// briefValues are the placeholders a brief template is rendered with.
type briefValues struct {
	ID, Title, Kind, Session, Goal string
	BlockedBy, TierGrants          []string
}

func yamlScalar(s string) string {
	b, err := yaml.Marshal(s)
	if err != nil {
		return strconv.Quote(s)
	}
	return strings.TrimSuffix(string(b), "\n")
}

// renderBrief fills a template: {{id}}, {{title}}, {{title_yaml}} (the
// title as a YAML scalar), {{kind}}, {{session}}, {{blocked_by}} and
// {{tier_grants}} (comma-separated) and {{goal}}.
func renderBrief(tmpl string, v briefValues) string {
	goal := v.Goal
	if goal == "" {
		goal = goalHint
	}
	return strings.NewReplacer(
		"{{id}}", v.ID, "{{title_yaml}}", yamlScalar(v.Title), "{{title}}", v.Title, "{{kind}}", v.Kind,
		"{{session}}", v.Session, "{{blocked_by}}", strings.Join(v.BlockedBy, ", "),
		"{{tier_grants}}", strings.Join(v.TierGrants, ", "), "{{goal}}", goal,
	).Replace(tmpl)
}

// splitFrontMatter splits "---\n<yaml>\n---\n<body>". ok is false when the
// text has no front matter.
func splitFrontMatter(text string) (front, body string, ok bool, err error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	rest, found := strings.CutPrefix(text, "---\n")
	if !found {
		return "", text, false, nil
	}
	head, after, found := strings.Cut(rest, "\n---\n")
	if !found {
		if h, isEnd := strings.CutSuffix(rest, "\n---"); isEnd {
			return h, "", true, nil
		}
		return "", text, false, errors.New("front matter has no closing ---")
	}
	return head, after, true, nil
}

// issueGoal is the Goal for --from-issue: the issue's number, title and
// link, then its body quoted.
func issueGoal(n int, title, url, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "GitHub **#%d**: [%s](%s).", n, title, url)
	if body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n")); body != "" {
		b.WriteString("\n\n")
		for i, l := range strings.Split(body, "\n") {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(strings.TrimRight("> "+l, " "))
		}
	}
	return b.String()
}

type ghIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
}

// fetchIssue reads a GitHub issue through gh. ok is false (with a
// warning) when gh is missing or fails: the brief is written without it.
func (r *run) fetchIssue(root string, n int) (ghIssue, bool) {
	gh := r.env.GH
	if gh == nil {
		gh = func(string, ...string) ([]byte, error) { return nil, errGHMissing }
	}
	out, err := gh(root, "issue", "view", strconv.Itoa(n), "--json", "number,title,body,url")
	if err != nil {
		r.warn("--from-issue %d: %v; writing the brief without the issue (fill the Goal by hand)", n, err)
		return ghIssue{}, false
	}
	var is ghIssue
	if err := json.Unmarshal(out, &is); err != nil || is.Number == 0 {
		r.warn("--from-issue %d: gh printed something unexpected; writing the brief without the issue", n)
		return ghIssue{}, false
	}
	return is, true
}

// writeNewFile creates path (and its folder) only if it does not exist.
// created is the folder when this call made it, for cleanup.
func writeNewFile(path string, body []byte) (created string, err error) {
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		created = dir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if created != "" {
			os.Remove(created)
		}
		return "", err
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	return created, f.Close()
}

const briefNewUsage = "brief new <id> --title t [--kind build|research|contract] [--blocked-by a,b] [--grants a,b] [--from-issue N] [--session s] [--branch b] [--worktree w] [--base-branch b] | brief new <id> --existing"

func (r *run) briefNew(args []string) error {
	fs := r.newFlags("brief new")
	title := fs.String("title", "", "task title (default with --from-issue: the issue's title)")
	kind := fs.String("kind", event.TaskKindBuild, "build, research or contract")
	var blocked, grants strList
	fs.Var(&blocked, "blocked-by", "ids this task waits on, comma-separated")
	fs.Var(&grants, "grants", "tier grants, comma-separated")
	issue := fs.Int("from-issue", 0, "GitHub issue whose title, body and link fill the Goal (needs gh)")
	session := fs.String("session", "", "session name (default: the id in lower case)")
	branch := fs.String("branch", "", "branch (default: build/<id lower>)")
	worktree := fs.String("worktree", "", "worktree (default: .worktrees/<id lower>)")
	base := fs.String("base-branch", "", "base branch (default: origin's default branch, else main)")
	existing := fs.Bool("existing", false, "scaffold brief.md for a task already drafted without one; records nothing")
	pos, err := r.parse(fs, args, 1, 1, briefNewUsage)
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
	tmpl, err := briefTemplate(s)
	if err != nil {
		return errf(CodeInternal, "read %s: %v", briefTemplatePath, err)
	}
	if *existing {
		return r.briefScaffoldExisting(s, a, pos[0], tmpl, *issue)
	}
	id := pos[0]
	low := strings.ToLower(id)
	t := taskFile{ID: id, Title: *title, Kind: *kind, BlockedBy: []string(blocked), TierGrants: []string(grants),
		Repo: s.ProjectName(), BaseBranch: *base, Branch: *branch, Worktree: *worktree, Session: *session}
	if t.Session == "" {
		t.Session = low
	}
	if t.Branch == "" {
		t.Branch = "build/" + low
	}
	if t.Worktree == "" {
		t.Worktree = ".worktrees/" + low
	}
	if t.BaseBranch == "" {
		t.BaseBranch = strings.TrimPrefix(r.git(s.Root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"), "origin/")
		if t.BaseBranch == "" {
			t.BaseBranch = "main"
		}
	}
	// Check the lane, id and kind before asking GitHub anything; the title
	// may still come from the issue.
	probe := t
	if probe.Title == "" {
		probe.Title = "-"
	}
	probe.BlockedBy = append([]string(nil), t.BlockedBy...)
	if _, err := r.taskDraftEvent(s, a, probe); err != nil {
		return err
	}
	var goal string
	if *issue > 0 {
		if is, ok := r.fetchIssue(s.Root, *issue); ok {
			goal = issueGoal(is.Number, is.Title, is.URL, is.Body)
			if t.Title == "" {
				t.Title = is.Title
			}
		}
	}
	if strings.TrimSpace(t.Title) == "" {
		return fieldErr(CodeValidation, "title", "required: pass --title (or --from-issue with gh available)")
	}
	r.warned = nil // the probe's warnings are printed once more by the real draft
	e, err := r.taskDraftEvent(s, a, t)
	if err != nil {
		return err
	}
	var d taskDrafted
	if err := json.Unmarshal(e.Data, &d); err != nil {
		return errf(CodeInternal, "%v", err)
	}
	path, err := s.ResolvePath(d.Brief)
	if err != nil {
		return errf(CodeInternal, "%v", err)
	}
	text := renderBrief(tmpl, briefValues{ID: id, Title: t.Title, Kind: t.Kind, Session: t.Session, Goal: goal,
		BlockedBy: d.BlockedBy, TierGrants: d.TierGrants})
	created, err := writeNewFile(path, []byte(text))
	if errors.Is(err, os.ErrExist) {
		return fieldErr(CodeValidation, "id", "%s already exists; pick another id or remove the file", s.Rel(path))
	} else if err != nil {
		return errf(CodeInternal, "write %s: %v", s.Rel(path), err)
	}
	err = r.commit(s, []event.Event{e}, []string{path},
		fmt.Sprintf("drafted %s with a brief to fill in; then run `ekumen brief check %s`", id, id))
	if err != nil && created != "" {
		if _, statErr := os.Stat(path); statErr != nil {
			os.Remove(created)
		}
	}
	return err
}

// briefScaffoldExisting writes brief.md for a task drafted without one,
// from its record. It records no event: the task is already drafted.
func (r *run) briefScaffoldExisting(s *project.Snapshot, a event.Actor, ref, tmpl string, issue int) error {
	id, err := r.findTask(s, ref)
	if err != nil {
		return err
	}
	if err := r.probe(s, a, event.TaskDraftedType, event.Subject{Kind: event.KindTask, ID: id}); err != nil {
		return err
	}
	t := s.State.Tasks[id]
	if t.Approved {
		return errf(CodeValidation, "%s is approved; its brief is frozen", id)
	}
	path, err := s.BriefPath(id)
	if err != nil {
		return errf(CodeValidation, "%s: the recorded brief path is refused (%v)", id, err)
	}
	var goal string
	if issue > 0 {
		if is, ok := r.fetchIssue(s.Root, issue); ok {
			goal = issueGoal(is.Number, is.Title, is.URL, is.Body)
		}
	}
	session := t.Session
	if session == "" {
		session = strings.ToLower(id)
	}
	text := renderBrief(tmpl, briefValues{ID: id, Title: t.Title, Kind: t.Kind, Session: session, Goal: goal,
		BlockedBy: t.BlockedBy, TierGrants: t.TierGrants})
	if _, err := writeNewFile(path, []byte(text)); errors.Is(err, os.ErrExist) {
		return errf(CodeValidation, "%s already has a brief (%s)", id, s.Rel(path))
	} else if err != nil {
		return errf(CodeInternal, "write %s: %v", s.Rel(path), err)
	}
	res := writeResult{OK: true, Events: []string{}, Paths: []string{s.Rel(path)}}
	r.out(res, func(w io.Writer) {
		fmt.Fprintf(w, "brief scaffolded for %s; fill it in, then run `ekumen brief check %s`\n  %s\n", id, id, s.Rel(path))
	})
	return nil
}

// briefReport is one brief's check result.
type briefReport struct {
	Task     string              `json:"task"`
	Path     string              `json:"path"`
	OK       bool                `json:"ok"`
	Problems []core.BriefProblem `json:"problems"`
}

// checkTaskBrief runs core.CheckBrief on a task's brief.md. found is
// false when the brief file does not exist.
func checkTaskBrief(s *project.Snapshot, id string) (rep briefReport, found bool, err error) {
	path, err := s.BriefPath(id)
	if err != nil {
		return rep, false, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return rep, false, nil
	} else if err != nil {
		return rep, false, err
	}
	tmpl, err := briefTemplate(s)
	if err != nil {
		return rep, true, err
	}
	in := core.BriefInput{TaskID: id, Exists: func(x string) bool { return s.State.Has(s.State.Resolve(x)) }}
	front, body, ok, ferr := splitFrontMatter(string(b))
	in.Body = body
	switch {
	case ferr != nil:
		in.FrontErr = ferr.Error()
	case ok:
		var f core.BriefFront
		if err := yaml.Unmarshal([]byte(front), &f); err != nil {
			in.FrontErr = "not valid YAML: " + err.Error()
		} else {
			in.Front = &f
		}
	}
	t := s.State.Tasks[id]
	session := t.Session
	if session == "" {
		session = strings.ToLower(id)
	}
	rendered := renderBrief(tmpl, briefValues{ID: id, Title: t.Title, Kind: t.Kind, Session: session,
		BlockedBy: t.BlockedBy, TierGrants: t.TierGrants})
	if _, tb, ok, _ := splitFrontMatter(rendered); ok {
		rendered = tb
	}
	in.Template = rendered
	probs := core.CheckBrief(in)
	if probs == nil {
		probs = []core.BriefProblem{}
	}
	return briefReport{Task: id, Path: s.Rel(path), OK: len(probs) == 0, Problems: probs}, true, nil
}

func (r *run) briefCheck(args []string) error {
	fs := r.newFlags("brief check")
	pos, err := r.parse(fs, args, 1, 1, "brief check <id>")
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
	rep, found, err := checkTaskBrief(s, id)
	if err != nil {
		return errf(CodeInternal, "%v", err)
	}
	if !found {
		p, _ := s.BriefPath(id)
		return fieldErr(CodeNotFound, "id", "%s has no brief at %s; run `ekumen brief new %s --existing`", id, s.Rel(p), id)
	}
	r.out(rep, func(w io.Writer) {
		for _, p := range rep.Problems {
			fmt.Fprintf(w, "%s: %s\n", p.Section, p.Message)
		}
		switch {
		case rep.OK && s.State.Tasks[id].Approved:
			fmt.Fprintf(w, "ok: %s has every section filled in\n", rep.Path)
		case rep.OK:
			fmt.Fprintf(w, "ok: %s has every section filled in; the lead can approve it (`ekumen task approve %s`)\n", rep.Path, id)
		default:
			fmt.Fprintf(w, "%s: %d problem%s\n", rep.Path, len(rep.Problems), plural(len(rep.Problems)))
		}
	})
	if !rep.OK {
		return &Error{Code: CodeValidation, Silent: true}
	}
	return nil
}

// draftBriefReports checks the briefs of drafted tasks that are not
// approved, deferred or done, for `ekumen check`. Tasks without a brief
// are skipped (they are the orchestrator's "brief" inbox action).
func draftBriefReports(s *project.Snapshot) []briefReport {
	out := []briefReport{}
	for _, id := range s.AllTaskIDs() {
		t := s.State.Tasks[id]
		if !t.Drafted() || t.Approved || t.Deferred {
			continue
		}
		rep, found, err := checkTaskBrief(s, id)
		if err != nil {
			p, _ := s.BriefPath(id)
			rep = briefReport{Task: id, Path: s.Rel(p), Problems: []core.BriefProblem{{Section: "file", Message: err.Error()}}}
		} else if !found || rep.OK {
			continue
		}
		out = append(out, rep)
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
