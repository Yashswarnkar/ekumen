// Package cli implements the `ekumen` commands of C-cli-json: argument
// parsing (standard library flag), the project root, the acting role, JSON
// output, exit codes and the error shape. It is glue: domain rules are in
// internal/core, file access in internal/store/files, views and
// projections in internal/project.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/mapview"
	"github.com/Yashswarnkar/ekumen/internal/observe"
	"github.com/Yashswarnkar/ekumen/internal/project"
)

// Exit codes (C-cli-json §1).
const (
	ExitOK       = 0
	ExitUsage    = 1
	ExitRefused  = 2
	ExitNotFound = 3
	ExitConflict = 4
	ExitInternal = 5
)

// Error codes (C-cli-json §1).
const (
	CodeLane       = "lane"
	CodeValidation = "validation"
	CodeNotFound   = "not_found"
	CodeConflict   = "conflict"
	CodeUsage      = "usage"
	CodeInternal   = "internal"
)

// Error is a command failure with its C-cli-json code.
type Error struct {
	Code    string
	Message string
	Field   string
	// Silent: the command already printed its result; only the exit
	// status carries the failure (check with conflicts).
	Silent bool
}

func (e *Error) Error() string { return e.Message }

// Exit maps the code to an exit status.
func (e *Error) Exit() int {
	switch e.Code {
	case CodeUsage:
		return ExitUsage
	case CodeLane, CodeValidation:
		return ExitRefused
	case CodeNotFound:
		return ExitNotFound
	case CodeConflict:
		return ExitConflict
	}
	return ExitInternal
}

func errf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func fieldErr(code, field, format string, args ...any) *Error {
	return &Error{Code: code, Field: field, Message: fmt.Sprintf(format, args...)}
}

// Env is everything a command touches outside its arguments. Tests replace
// the clock, ids, observer and streams.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
	Getenv func(string) string
	// Dir is the working directory used to find the project root.
	Dir string
	Now func() time.Time
	// NewID returns a new event ULID for a time.
	NewID func(time.Time) (string, error)
	// Observer returns the observer for a project root.
	Observer func(root string) observe.Observer
	// Commit returns the short commit of the project root, or "".
	Commit func(root string) string
	// Git runs `git -C root <args>` and returns its trimmed output, or ""
	// on any failure. The CLI reads the origin remote (once per run, for the
	// project name and map links), and `ekumen map` also reads origin/HEAD
	// and the root's prefix in the repository.
	Git func(root string, args ...string) string

	// beforeAction, when set, runs before each reconcile action (by index)
	// and stops the pass on error. Tests use it to interrupt a pass.
	beforeAction func(action int) error
}

func gitOut(root string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// DefaultEnv is the environment of the real binary. Observation uses `gh`
// unless EKUMEN_OBSERVE=off.
func DefaultEnv() Env {
	wd, _ := os.Getwd()
	return Env{
		Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin, Getenv: os.Getenv, Dir: wd,
		Now: time.Now, NewID: event.NewULID,
		Observer: func(root string) observe.Observer {
			if os.Getenv("EKUMEN_OBSERVE") == "off" {
				return observe.None{}
			}
			return observe.GH{Dir: root}
		},
		Commit: func(root string) string { return gitOut(root, "rev-parse", "--short", "HEAD") },
		Git:    gitOut,
	}
}

// git runs a git command at root through Env.Git ("" when unavailable).
func (r *run) git(root string, args ...string) string {
	if r.env.Git == nil {
		return ""
	}
	return r.env.Git(root, args...)
}

// remote is the origin remote's URL, read once per run.
func (r *run) remote(root string) string {
	if r.remoteURL == nil {
		u := r.git(root, "remote", "get-url", "origin")
		r.remoteURL = &u
	}
	return *r.remoteURL
}

// repoName is the origin remote's repository name when root is the top of
// a git repository (it holds .git), else "" (ProjectName then falls back
// to the folder name, which suits a project in a monorepo subfolder).
func (r *run) repoName(root string) string {
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return ""
	}
	return mapview.RepoName(r.remote(root))
}

// globals are the flags every command accepts (C-cli-json §1).
type globals struct {
	root string
	as   string
	json bool
}

func (g *globals) register(fs *flag.FlagSet) {
	fs.StringVar(&g.root, "root", g.root, "project root (default: nearest parent with .ekumen/)")
	fs.StringVar(&g.as, "as", g.as, "acting role, <role>:<id> (default: $EKUMEN_ACTOR)")
	fs.BoolVar(&g.json, "json", g.json, "print one JSON object on stdout")
}

// run is one invocation in progress.
type run struct {
	env    Env
	g      globals
	ctx    context.Context
	warned map[string]bool

	remoteURL *string
}

// Run executes one ekumen command and returns its exit status.
func Run(args []string, env Env) int {
	r := &run{env: env, ctx: context.Background()}
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "--json" || a == "-json" || a == "--json=true" {
			r.g.json = true // so early usage errors are JSON too
		}
	}
	err := r.dispatch(args)
	if err == nil {
		return ExitOK
	}
	var ce *Error
	if !errors.As(err, &ce) {
		ce = &Error{Code: CodeInternal, Message: err.Error()}
	}
	if ce.Silent || (ce.Code == CodeUsage && ce.Message == "") {
		return ce.Exit()
	}
	if r.g.json {
		body := map[string]any{"code": ce.Code, "message": ce.Message}
		if ce.Field != "" {
			body["field"] = ce.Field
		}
		r.printJSON(map[string]any{"error": body})
	} else {
		fmt.Fprintf(env.Stderr, "ekumen: %s\n", ce.Message)
		if ce.Code == CodeUsage {
			fmt.Fprintf(env.Stderr, "Run 'ekumen help' for usage.\n")
		}
	}
	return ce.Exit()
}

func (r *run) warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if r.warned == nil {
		r.warned = map[string]bool{}
	}
	if r.warned[msg] {
		return
	}
	r.warned[msg] = true
	fmt.Fprintf(r.env.Stderr, "ekumen: warning: %s\n", msg)
}

func (r *run) printJSON(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(r.env.Stderr, "ekumen: encode: %v\n", err)
		return
	}
	r.env.Stdout.Write(append(b, '\n'))
}

// out prints v as JSON with --json, otherwise calls human.
func (r *run) out(v any, human func(w io.Writer)) {
	if r.g.json {
		r.printJSON(v)
		return
	}
	human(r.env.Stdout)
}

func (r *run) helpOut() io.Writer {
	if r.g.json {
		return r.env.Stderr
	}
	return r.env.Stdout
}

func (r *run) newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	r.g.register(fs)
	return fs
}

// parse parses flags and positionals in any order ("--" ends flags).
func (r *run) parse(fs *flag.FlagSet, args []string, minPos, maxPos int, usage string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fmt.Fprintf(r.helpOut(), "Usage: ekumen %s\n", usage)
				return nil, &Error{Code: CodeUsage, Silent: true}
			}
			return nil, errf(CodeUsage, "%s: %v (usage: ekumen %s)", fs.Name(), err, usage)
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		if args[0] == "--" {
			pos = append(pos, args[1:]...)
			break
		}
		pos, args = append(pos, args[0]), args[1:]
	}
	if len(pos) < minPos || (maxPos >= 0 && len(pos) > maxPos) {
		return nil, errf(CodeUsage, "%s: wrong number of arguments (usage: ekumen %s)", fs.Name(), usage)
	}
	return pos, nil
}

func (r *run) now() time.Time { return r.env.Now().UTC() }

func (r *run) rootDir() (string, error) {
	if r.g.root != "" {
		root := r.g.root
		if fi, err := os.Stat(root + "/" + project.Dir); err != nil || !fi.IsDir() {
			return "", errf(CodeNotFound, "--root %s has no %s/ folder", root, project.Dir)
		}
		return root, nil
	}
	root, err := project.FindRoot(r.env.Dir)
	if err != nil {
		return "", errf(CodeNotFound, "%v (use --root)", err)
	}
	return root, nil
}

func (r *run) load() (*project.Snapshot, error) {
	root, err := r.rootDir()
	if err != nil {
		return nil, err
	}
	var obs observe.Observer = observe.None{}
	if r.env.Observer != nil {
		obs = r.env.Observer(root)
	}
	s, err := project.Load(r.ctx, project.Options{Root: root, Now: r.now(), Observer: obs,
		Name: r.repoName(root), Warn: func(m string) { r.warn("%s", m) }})
	if err != nil {
		return nil, errf(CodeInternal, "%v", err)
	}
	if len(s.Events) == 0 {
		r.warn("the event log in %s/events is empty; hand-written records are not read until they are imported (ekumen-import)", project.Dir)
	}
	return s, nil
}

// actor returns the acting role from --as or EKUMEN_ACTOR. required makes
// a missing actor a usage error.
func (r *run) actor(required bool) (event.Actor, bool, error) {
	v := r.g.as
	if v == "" && r.env.Getenv != nil {
		v = r.env.Getenv("EKUMEN_ACTOR")
	}
	if v == "" {
		if required {
			return event.Actor{}, false, errf(CodeUsage, "who is acting? pass --as <role>:<id> or set EKUMEN_ACTOR (e.g. session:t-core)")
		}
		return event.Actor{}, false, nil
	}
	role, id, ok := strings.Cut(v, ":")
	if !ok || id == "" || !event.Role(role).Known() {
		return event.Actor{}, false, errf(CodeUsage, "actor %q: want <role>:<id> with role one of lead, orchestrator, session, loop, integrator", v)
	}
	return event.Actor{Role: event.Role(role), ID: id}, true, nil
}

const usageText = `Usage: ekumen <command> [flags]

Read (every read prints one JSON object with --json):
  status                                  counts and the top of your inbox
  inbox [--for lead|orchestrator|<session>]
  task list [--status s] | task show <id>
  decision list [--status s] | decision show <id>
  question list [--for x] [--open]
  graph                                   task graph nodes and edges
  map --out <file.html> [--fragment] [--link-base url]
                                          self-contained build map
  check                                   validate files and run conflict rules
  project --check                         fail if committed decisions/*.yaml are stale

Write (need --as <role>:<id> or EKUMEN_ACTOR):
  decision raise --file <d.yaml>
  decision answer <id> <option|other> [--note t]
  decision supersede <id> --by <id> [--note t]
  decision apply <id> [--amend-text-file f]   (as loop:<orchestrator session>)
  question ask <subject id> --text t [--to x]
  question answer <question id> --text t   (question and route ids take a unique prefix)
  task draft --file <t.yaml>
  task approve <id> [--grants a,b]
  task amend <id> --text-file f [--note t]
  task handoff <id> --text-file f
  task block <id> [--add x] [--remove y] --reason t
  task defer <id> --reason t
  task attach <id> --session s [--adapter a]
  contract draft --file <c.yaml> [--text-file body.md]
  contract block <id> [--add x] [--remove y] --reason t
  contract freeze <id>
  contract version <id> --by <decision>   (orchestrator; then the lead freezes it)
  spine approve                           (lead; hashes .ekumen/spine.md)
  external add --file f | external update <id> --file f | external resolve <id> --note t
  route resolve <route id> --note t
  claim <role> | release <role>
  project                                 regenerate projections
  reconcile [--dry-run]                   one pass of the control loop (as loop:<session>; --dry-run needs no actor)

Skills (no actor needed; nothing written under .ekumen/):
  skills list                             skills embedded in this binary
  skills install --dir <path> [--force]   write each skill to <path>/<name>/

Other:
  version | serve [--addr a] | help

Global flags: --root <dir>, --as <role>:<id>, --json
Exit codes: 0 ok, 1 usage, 2 refused, 3 not found, 4 conflict, 5 internal.
See docs/cli.md.
`

func (r *run) dispatch(args []string) error {
	// Global flags may come before the command.
	fs := r.newFlags("ekumen")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(r.helpOut(), usageText)
			return nil
		}
		return errf(CodeUsage, "%v", err)
	}
	args = fs.Args()
	if len(args) == 0 {
		fmt.Fprint(r.env.Stderr, usageText)
		if r.g.json {
			return errf(CodeUsage, "no command given")
		}
		return &Error{Code: CodeUsage, Silent: true}
	}
	cmd, rest := args[0], args[1:]
	sub := func() (string, []string) {
		if len(rest) == 0 {
			return "", nil
		}
		return rest[0], rest[1:]
	}
	switch cmd {
	case "help":
		fmt.Fprint(r.helpOut(), usageText)
		return nil
	case "status":
		return r.status(rest)
	case "inbox":
		return r.inbox(rest)
	case "graph":
		return r.graph(rest)
	case "map":
		return r.mapCmd(rest)
	case "check":
		return r.check(rest)
	case "project":
		return r.projectCmd(rest)
	case "reconcile":
		return r.reconcile(rest)
	case "claim":
		return r.claim(rest, true)
	case "release":
		return r.claim(rest, false)
	case "task":
		s, a := sub()
		switch s {
		case "list":
			return r.taskList(a)
		case "show":
			return r.taskShow(a)
		case "draft":
			return r.taskDraft(a)
		case "approve":
			return r.taskApprove(a)
		case "amend":
			return r.taskEntry(a, true)
		case "handoff":
			return r.taskEntry(a, false)
		case "block":
			return r.taskBlock(a)
		case "defer":
			return r.taskDefer(a)
		case "attach":
			return r.taskAttach(a)
		}
	case "decision":
		s, a := sub()
		switch s {
		case "list":
			return r.decisionList(a)
		case "show":
			return r.decisionShow(a)
		case "raise":
			return r.decisionRaise(a)
		case "answer":
			return r.decisionAnswer(a)
		case "supersede":
			return r.decisionSupersede(a)
		case "apply":
			return r.decisionApply(a)
		}
	case "question":
		s, a := sub()
		switch s {
		case "list":
			return r.questionList(a)
		case "ask":
			return r.questionAsk(a)
		case "answer":
			return r.questionAnswer(a)
		}
	case "contract":
		s, a := sub()
		switch s {
		case "freeze":
			return r.contractFreeze(a)
		case "draft":
			return r.contractDraft(a)
		case "block":
			return r.contractBlock(a)
		case "version":
			return r.contractVersion(a)
		}
	case "spine":
		s, a := sub()
		if s == "approve" {
			return r.spineApprove(a)
		}
	case "external":
		s, a := sub()
		switch s {
		case "add":
			return r.externalAdd(a)
		case "update":
			return r.externalUpdate(a)
		case "resolve":
			return r.externalResolve(a)
		}
	case "route":
		s, a := sub()
		if s == "resolve" {
			return r.routeResolve(a)
		}
	case "skills":
		s, a := sub()
		return r.skillsCmd(s, a)
	default:
		return errf(CodeUsage, "unknown command %q", cmd)
	}
	s, _ := sub()
	return errf(CodeUsage, "unknown command %q", strings.TrimSpace(cmd+" "+s))
}
