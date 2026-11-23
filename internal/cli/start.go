package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/project"
)

// gitCmd runs `git -C dir args…` and returns its trimmed stdout, or an
// error carrying git's stderr. Unlike Env.Git (best-effort reads), the
// commands that change a repository need to know why they failed.
func gitCmd(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// startResult is what `task start` prints with --json.
type startResult struct {
	writeResult
	Task     string `json:"task"`
	Session  string `json:"session"`
	Agent    string `json:"agent"`
	Branch   string `json:"branch"`
	Worktree string `json:"worktree"` // absolute
	Prompt   string `json:"prompt"`   // absolute
	// Done lists what this run changed: "branch", "worktree", "attach",
	// "setup", "prompt". Empty on a repeat run.
	Done []string `json:"done"`
	// Command is the one line the lead pastes into a new terminal.
	Command string `json:"command"`
	// Opened is true when --open started a terminal window.
	Opened bool `json:"opened"`
}

// startable is one task of `task start --list`.
type startable struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Branch   string   `json:"branch"`
	Worktree string   `json:"worktree"`
	Session  string   `json:"session"`
	Missing  []string `json:"missing"`
	Command  string   `json:"command"`
}

// taskStart is `ekumen task start <id> [--open] [--agent name] [--setup]`
// and `ekumen task start --list`.
func (r *run) taskStart(args []string) error {
	fs := r.newFlags("task start")
	list := fs.Bool("list", false, "list the approved, unblocked tasks with no worktree yet")
	open := fs.Bool("open", false, "open a new terminal window running the command (macOS Terminal)")
	agentName := fs.String("agent", "", "which agents: entry of .ekumen/config.yaml to use")
	setup := fs.Bool("setup", false, "run the setup commands again in an existing, clean worktree")
	usage := "task start <id> [--open] [--agent name] [--setup] | task start --list"
	pos, err := r.parse(fs, args, 0, 1, usage)
	if err != nil {
		return err
	}
	if *list {
		if len(pos) != 0 {
			return errf(CodeUsage, "task start --list takes no task id (usage: ekumen %s)", usage)
		}
		return r.taskStartList()
	}
	if len(pos) != 1 {
		return errf(CodeUsage, "task start: give a task id or --list (usage: ekumen %s)", usage)
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
	t := s.State.Tasks[id]
	cfg, err := project.ReadConfig(s.EkumenDir())
	if err != nil {
		return errf(CodeValidation, "%v", err)
	}

	// 1. Check, before touching anything.
	var why []string
	why = append(why, startBlockers(s, id)...)
	startPrompt, err := r.startPrompt(s, id)
	if err != nil {
		why = append(why, err.Error())
	}
	agent, command, agentErr := r.pickAgent(cfg, *agentName, t.Adapter)
	if agentErr != nil {
		why = append(why, agentErr.Error())
	}
	if len(why) > 0 {
		e := errf(CodeValidation, "%s can't start yet:\n  - %s", id, strings.Join(why, "\n  - "))
		e.Details = map[string]any{"reasons": why}
		return e
	}
	base, err := repoBase(s.Root)
	if err != nil {
		return errf(CodeValidation, "%v", err)
	}
	wt, err := worktreePath(base, t.Worktree)
	if err != nil {
		return fieldErr(CodeValidation, "worktree", "%s: %v", id, err)
	}
	needAttach := !attached(s, id, t.Session)
	if needAttach {
		if err := r.probe(s, a, event.TaskSessionAttachedType, event.Subject{Kind: event.KindTask, ID: id}); err != nil {
			return err
		}
	}

	res := startResult{writeResult: writeResult{OK: true, Events: []string{}, Paths: []string{}},
		Task: id, Session: t.Session, Agent: agent, Branch: t.Branch, Worktree: wt, Done: []string{}}

	// 2. Worktree and branch: create, or reuse a clean one.
	created, dirty, err := r.ensureWorktree(base, wt, t)
	if err != nil {
		return err
	}
	res.Done = append(res.Done, created...)

	// 3. Attach the recorded session, once.
	if needAttach {
		e, err := r.mkEvent(a, event.TaskSessionAttachedType, event.Subject{Kind: event.KindTask, ID: id},
			event.TaskSessionAttached{Session: t.Session, Adapter: agent})
		if err != nil {
			return err
		}
		appended, err := r.appendEvents(s, []event.Event{e}, nil)
		if err != nil {
			return err
		}
		res.Events = append(res.Events, e.ID)
		for _, p := range appended {
			res.Paths = append(res.Paths, s.Rel(p))
		}
		fresh, err := r.load()
		if err == nil {
			var ps []string
			ps, err = fresh.WriteProjections()
			res.Paths = append(res.Paths, ps...)
		}
		if err != nil {
			r.warn("%v", err)
			return projectionsErr(res.Events, res.Paths)
		}
		res.Done = append(res.Done, "attach")
	}

	// 4. Setup, in a new worktree (or a clean one with --setup).
	newWorktree := len(created) > 0
	switch {
	case dirty:
		r.warn("%s has uncommitted changes; leaving it alone (no setup run)", wt)
	case len(cfg.Setup) > 0 && (newWorktree || *setup):
		if err := r.runSetup(wt, cfg.Setup); err != nil {
			return err
		}
		res.Done = append(res.Done, "setup")
	}

	// 5. The prompt file, next to the worktree.
	res.Prompt = filepath.Join(filepath.Dir(wt), id+".prompt.txt")
	text := promptText(s.ProjectName(), id, t, s.Rel(mustBrief(s, id)), cfg.Setup, startPrompt)
	if old, err := os.ReadFile(res.Prompt); err != nil || string(old) != text {
		if err := os.WriteFile(res.Prompt, []byte(text), 0o644); err != nil {
			return errf(CodeInternal, "write prompt: %v", err)
		}
		res.Done = append(res.Done, "prompt")
		res.Paths = append(res.Paths, s.Rel(res.Prompt))
	}

	// 6. The command.
	res.Command = fmt.Sprintf("cd %s && EKUMEN_ACTOR=session:%s %s \"$(cat %s)\"",
		shellQuote(wt), t.Session, command, shellQuote(res.Prompt))
	if *open {
		if r.env.OpenTerminal == nil {
			r.warn("--open isn't supported on this platform (macOS Terminal only); paste the command into a new terminal")
		} else if err := r.env.OpenTerminal(res.Command); err != nil {
			r.warn("--open failed (%v); paste the command into a new terminal", err)
		} else {
			res.Opened = true
		}
	}
	r.out(res, func(w io.Writer) {
		if len(res.Done) == 0 {
			fmt.Fprintf(w, "%s is already set up in %s (nothing changed).\n", id, wt)
		} else {
			fmt.Fprintf(w, "%s set up in %s (%s).\n", id, wt, strings.Join(res.Done, ", "))
		}
		if res.Opened {
			fmt.Fprintln(w, "Opened a new terminal window running:")
		} else {
			fmt.Fprintln(w, "Paste into a new terminal:")
		}
		fmt.Fprintf(w, "\n  %s\n", res.Command)
	})
	return nil
}

// startBlockers says why a task can't start: not approved, deferred, done,
// open blockers, or empty location fields. Empty when it can.
func startBlockers(s *project.Snapshot, id string) []string {
	t := s.State.Tasks[id]
	var why []string
	switch st := s.TaskStatus(id); {
	case t.Deferred:
		why = append(why, fmt.Sprintf("it is deferred (%s)", t.DeferredReason))
	case st == core.TaskDone:
		why = append(why, "it is done (its pull request is merged)")
	case !t.Approved:
		why = append(why, fmt.Sprintf("it is not approved (the lead runs `ekumen task approve %s`)", id))
	}
	if open := core.OpenBlockers(&s.State, s.Observed, id); len(open) > 0 {
		why = append(why, fmt.Sprintf("it is blocked by %s", strings.Join(open, ", ")))
	}
	if m := missingFields(t); len(m) > 0 {
		var fix []string
		for _, f := range m {
			if f == "session" {
				fix = append(fix, fmt.Sprintf("`ekumen task attach %s --session <name>`", id))
			} else {
				fix = append(fix, fmt.Sprintf("`ekumen task update %s --%s …`", id, strings.ReplaceAll(f, "_", "-")))
			}
		}
		why = append(why, fmt.Sprintf("its %s %s empty (the orchestrator sets them: %s)",
			strings.Join(m, ", "), map[bool]string{true: "is", false: "are"}[len(m) == 1], strings.Join(fix, ", ")))
	}
	return why
}

func missingFields(t *core.Task) []string {
	var m []string
	for _, f := range []struct{ name, v string }{
		{"base_branch", t.BaseBranch}, {"branch", t.Branch}, {"worktree", t.Worktree}, {"session", t.Session},
	} {
		if strings.TrimSpace(f.v) == "" {
			m = append(m, f.name)
		}
	}
	return m
}

// attached reports whether a TaskSessionAttached recorded this session.
// A session named only in TaskDrafted is not attached yet.
func attached(s *project.Snapshot, id, session string) bool {
	for _, e := range s.Events {
		if e.Type != event.TaskSessionAttachedType || s.State.Resolve(e.Subject.ID) != id {
			continue
		}
		var d event.TaskSessionAttached
		if json.Unmarshal(e.Data, &d) == nil && project.NormalizeOwner(d.Session) == session {
			return true
		}
	}
	return false
}

// pickAgent chooses the agents: entry: --agent, else the task's attached
// adapter when it is configured, else agents.default, else the only one.
// It returns the name and the command, resolved to an absolute path when
// its first word is a bare program name (C-adapter §6: never an alias).
func (r *run) pickAgent(cfg project.Config, flagName, adapter string) (string, string, error) {
	configured := strings.Join(cfg.AgentNames(), ", ")
	name := flagName
	switch {
	case name != "":
		if _, ok := cfg.Agents[name]; !ok {
			if configured == "" {
				return "", "", fmt.Errorf("no agents are configured in %s/%s, so --agent %s is unknown", project.Dir, project.ConfigFile, name)
			}
			return "", "", fmt.Errorf("--agent %s is not configured in %s/%s (configured: %s)", name, project.Dir, project.ConfigFile, configured)
		}
	case adapter != "" && cfg.Agents[adapter].Command != "":
		name = adapter
	case cfg.DefaultAgent != "":
		name = cfg.DefaultAgent
		if _, ok := cfg.Agents[name]; !ok {
			return "", "", fmt.Errorf("agents.default is %s, which has no agents.%s: {command: …} in %s/%s", name, name, project.Dir, project.ConfigFile)
		}
	case len(cfg.Agents) == 1:
		name = cfg.AgentNames()[0]
	case len(cfg.Agents) == 0:
		return "", "", fmt.Errorf("no agent is configured: add `agents: {default: <name>, <name>: {command: \"<path>\"}}` to %s/%s", project.Dir, project.ConfigFile)
	default:
		return "", "", fmt.Errorf("several agents are configured (%s) and none is agents.default; pass --agent", configured)
	}
	cmd := strings.TrimSpace(cfg.Agents[name].Command)
	first, rest, _ := strings.Cut(cmd, " ")
	if !strings.ContainsAny(first, "/~$") {
		if p, err := exec.LookPath(first); err == nil {
			if abs, err := filepath.Abs(p); err == nil {
				p = abs
			}
			cmd = strings.TrimSpace(shellQuoteIfNeeded(p) + " " + rest)
		} else {
			r.warn("agents.%s.command: %q is not on PATH; the command may not run", name, first)
		}
	}
	return name, cmd, nil
}

// startPromptRe finds the brief's Start prompt heading: `**Start prompt:**`
// (the de facto brief format) or a `## Start prompt` heading.
var startPromptRe = regexp.MustCompile(`(?i)^\s*(\*\*start prompt:?\*\*:?|#+\s*start prompt:?)\s*$`)

// startPrompt extracts the brief's Start prompt block: the indented or
// fenced code block after the heading, without its indentation or fences.
func (r *run) startPrompt(s *project.Snapshot, id string) (string, error) {
	p := mustBrief(s, id)
	b, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("its brief %s can't be read", s.Rel(p))
	}
	block, ok := extractStartPrompt(string(b))
	if !ok {
		return "", fmt.Errorf("its brief %s has no **Start prompt:** block", s.Rel(p))
	}
	return block, nil
}

func mustBrief(s *project.Snapshot, id string) string {
	p, err := s.BriefPath(id)
	if err != nil || p == "" {
		return filepath.Join(s.EkumenDir(), "briefs", id, "brief.md")
	}
	return p
}

func extractStartPrompt(text string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	start := -1
	for i, l := range lines {
		if startPromptRe.MatchString(l) {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", false
	}
	i := start
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i == len(lines) {
		return "", false
	}
	var out []string
	if fence := strings.TrimSpace(lines[i]); strings.HasPrefix(fence, "```") || strings.HasPrefix(fence, "~~~") {
		mark := fence[:3]
		indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " "))]
		closed := false
		for i++; i < len(lines); i++ {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), mark) {
				closed = true
				break
			}
			out = append(out, strings.TrimPrefix(lines[i], indent))
		}
		if !closed {
			return "", false
		}
	} else {
		for ; i < len(lines); i++ {
			l := lines[i]
			switch {
			case strings.TrimSpace(l) == "":
				out = append(out, "")
			case strings.HasPrefix(l, "    "):
				out = append(out, l[4:])
			case strings.HasPrefix(l, "\t"):
				out = append(out, l[1:])
			default:
				i = len(lines)
				continue
			}
		}
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return "", false
	}
	return strings.Join(out, "\n"), true
}

// repoBase is where task worktree paths are rooted: the project's folder
// in the repository's main working tree. From a task's own worktree, that
// is still the main checkout, so worktrees never nest.
func repoBase(root string) (string, error) {
	common, err := gitCmd(root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("the project is not in a git repository: %v", err)
	}
	if filepath.Base(common) != ".git" {
		return "", fmt.Errorf("the repository at %s has no main working tree", common)
	}
	prefix, err := gitCmd(root, "rev-parse", "--show-prefix")
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(common), filepath.FromSlash(prefix)), nil
}

func worktreePath(base, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("worktree %q is absolute; it is relative to the project root", rel)
	}
	p := filepath.Join(base, filepath.FromSlash(rel))
	if r, err := filepath.Rel(base, p); err != nil || r == "." || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("worktree %q leads outside the project", rel)
	}
	return p, nil
}

// ensureWorktree creates the branch (from origin/<base_branch>) and the
// worktree, or checks that an existing one is the task's. It never
// changes an existing worktree; dirty reports uncommitted changes in it.
func (r *run) ensureWorktree(base, wt string, t *core.Task) (created []string, dirty bool, err error) {
	if _, statErr := os.Stat(wt); statErr == nil {
		top, err := gitCmd(wt, "rev-parse", "--show-toplevel")
		if err != nil || !samePath(top, wt) {
			return nil, false, fieldErr(CodeValidation, "worktree", "%s exists but is not a git worktree; move it away or fix the task's worktree", wt)
		}
		if cur, _ := gitCmd(wt, "branch", "--show-current"); cur != t.Branch {
			return nil, false, fieldErr(CodeValidation, "branch", "%s is on branch %q, not the task's %q; leaving it alone", wt, cur, t.Branch)
		}
		st, err := gitCmd(wt, "status", "--porcelain")
		if err != nil {
			return nil, false, errf(CodeInternal, "%v", err)
		}
		return nil, st != "", nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, false, errf(CodeInternal, "%v", statErr)
	}
	if err := os.MkdirAll(filepath.Dir(wt), 0o755); err != nil {
		return nil, false, errf(CodeInternal, "%v", err)
	}
	if _, err := gitCmd(base, "rev-parse", "--verify", "--quiet", "refs/heads/"+t.Branch); err == nil {
		if _, err := gitCmd(base, "worktree", "add", wt, t.Branch); err != nil {
			return nil, false, errf(CodeValidation, "%v", err)
		}
		return []string{"worktree"}, false, nil
	}
	if _, err := gitCmd(base, "fetch", "--quiet", "origin", t.BaseBranch); err != nil {
		r.warn("could not fetch origin %s (%v); using the local origin/%s", t.BaseBranch, err, t.BaseBranch)
	}
	from := "origin/" + t.BaseBranch
	if _, err := gitCmd(base, "rev-parse", "--verify", "--quiet", "refs/remotes/"+from); err != nil {
		return nil, false, fieldErr(CodeValidation, "base_branch", "%s does not exist; fetch it, or fix the task's base_branch", from)
	}
	// --no-track: the new branch must not push to the base branch.
	if _, err := gitCmd(base, "worktree", "add", "--no-track", "-b", t.Branch, wt, from); err != nil {
		return nil, false, errf(CodeValidation, "%v", err)
	}
	return []string{"branch", "worktree"}, false, nil
}

func samePath(a, b string) bool {
	ea, err1 := filepath.EvalSymlinks(a)
	eb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ea == eb
}

// runSetup runs each setup command with `sh -c` in the worktree. Their
// output goes to stderr, so --json output stays one object.
func (r *run) runSetup(wt string, cmds []string) error {
	for _, c := range cmds {
		fmt.Fprintf(r.env.Stderr, "ekumen: setup: %s\n", c)
		cmd := exec.Command("sh", "-c", c)
		cmd.Dir = wt
		cmd.Stdout, cmd.Stderr = r.env.Stderr, r.env.Stderr
		if err := cmd.Run(); err != nil {
			return errf(CodeInternal, "setup command %q failed in %s: %v (fix it, then run `ekumen task start --setup`)", c, wt, err)
		}
	}
	return nil
}

// promptText is the prompt file: who the session is, its actor, the setup
// reminder and the brief's Start prompt, verbatim.
func promptText(projectName, id string, t *core.Task, brief string, setup []string, startPrompt string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are session %s, working on %s task %s in this worktree (branch %s).\n", t.Session, projectName, id, t.Branch)
	fmt.Fprintf(&b, "EKUMEN_ACTOR=session:%s is set in this terminal; keep it for every ekumen command.\n", t.Session)
	if len(setup) > 0 {
		b.WriteString("Setup ran when the worktree was created. Run it again after you rebase or pull:\n")
		for _, c := range setup {
			fmt.Fprintf(&b, "  %s\n", c)
		}
	}
	fmt.Fprintf(&b, "Your brief is %s: run `ekumen task show %s` and follow its Start prompt and any amendments exactly.\n", brief, id)
	b.WriteString("\nStart prompt:\n\n")
	b.WriteString(startPrompt)
	b.WriteString("\n")
	return b.String()
}

// shellQuote single-quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./+-]+$`)

func shellQuoteIfNeeded(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return shellQuote(s)
}

// taskStartList is `task start --list`: every approved, unblocked task
// whose worktree does not exist yet, with what is missing to start it.
func (r *run) taskStartList() error {
	s, err := r.load()
	if err != nil {
		return err
	}
	base, baseErr := repoBase(s.Root)
	out := []startable{}
	for _, id := range s.TaskIDs() {
		st := s.TaskStatus(id)
		if st != core.TaskReady && st != core.TaskApproved {
			continue
		}
		t := s.State.Tasks[id]
		if t.Worktree != "" && baseErr == nil {
			if p, err := worktreePath(base, t.Worktree); err == nil {
				if _, err := os.Stat(p); err == nil {
					continue
				}
			}
		}
		item := startable{ID: id, Title: t.Title, Status: string(st), Branch: t.Branch, Worktree: t.Worktree,
			Session: t.Session, Missing: missingFields(t)}
		if len(item.Missing) == 0 {
			item.Missing, item.Command = []string{}, "ekumen task start "+id
		}
		out = append(out, item)
	}
	if baseErr != nil {
		r.warn("%v; worktrees were not checked", baseErr)
	}
	r.out(map[string]any{"tasks": out}, func(w io.Writer) {
		if len(out) == 0 {
			fmt.Fprintln(w, "no task is ready to start (approved, unblocked, no worktree yet)")
			return
		}
		for _, it := range out {
			fmt.Fprintf(w, "%-22s %-9s %s\n", it.ID, it.Status, it.Title)
			if it.Command != "" {
				fmt.Fprintf(w, "    %s\n", it.Command)
			} else {
				fmt.Fprintf(w, "    missing: %s\n", strings.Join(it.Missing, ", "))
			}
		}
	})
	return nil
}
