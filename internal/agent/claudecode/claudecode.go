package claudecode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/agent"
)

const Name = "claude-code"

// MinVersion is the oldest Claude Code the adapter drives: `--permission-prompts`
// (Fork) needs 2.1.259, and `--resume <id> --bg` keeping the same ID needs 2.1.257.
var MinVersion = [3]int{2, 1, 259}

type Options struct {
	Bin      string // absolute path; default EKUMEN_CLAUDE_BIN, else claude on PATH
	StateDir string // default EKUMEN_STATE_DIR, else $XDG_STATE_HOME or ~/.local/state, + /ekumen/claude-code
	Timeout  time.Duration
	Now      func() time.Time
	Runner   Runner // default: exec in the session's Cwd
}

type Adapter struct {
	bin      string
	binErr   error
	state    *store
	stateErr error
	run      Runner
	now      func() time.Time
	timeout  time.Duration
}

var _ agent.Adapter = (*Adapter)(nil)

func New(o Options) *Adapter {
	a := &Adapter{now: o.Now, timeout: o.Timeout, run: o.Runner}
	if a.now == nil {
		a.now = time.Now
	}
	if a.timeout == 0 {
		a.timeout = time.Minute
	}
	a.bin, a.binErr = resolveBin(o.Bin)
	dir, err := stateDir(o.StateDir)
	a.state, a.stateErr = &store{dir: dir}, err
	if err != nil {
		a.state.dir = ""
	}
	if a.run == nil {
		a.run = execRunner{bin: a.bin}
	}
	return a
}

// stateDir resolves the state directory to an absolute path (#32 item 9:
// a relative one would put hook records in the session's worktree and
// point --settings at a missing file). It is a state directory, not a
// cache, since key and delivery records must survive cache purges (#32
// item 7).
func stateDir(dir string) (string, error) {
	if dir == "" {
		dir = os.Getenv("EKUMEN_STATE_DIR")
	}
	if dir == "" {
		base := os.Getenv("XDG_STATE_HOME")
		if !filepath.IsAbs(base) {
			home, err := os.UserHomeDir()
			if err != nil || home == "" {
				return "", fmt.Errorf("no state directory: set EKUMEN_STATE_DIR (%v)", err)
			}
			base = filepath.Join(home, ".local", "state")
		}
		dir = filepath.Join(base, "ekumen", "claude-code")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("state directory %q: %w", dir, err)
	}
	return abs, nil
}

func resolveBin(bin string) (string, error) {
	if bin == "" {
		bin = os.Getenv("EKUMEN_CLAUDE_BIN")
	}
	if bin == "" {
		p, err := exec.LookPath("claude")
		if err != nil {
			return "", fmt.Errorf("%w: claude not on PATH: %v", agent.ErrUnavailable, err)
		}
		bin = p
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		return "", fmt.Errorf("%w: %v", agent.ErrUnavailable, err)
	}
	return abs, nil
}

func (a *Adapter) Name() string { return Name }

// Bin is the absolute path of the binary every invocation uses.
func (a *Adapter) Bin() string { return a.bin }

var versionRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)

func parseVersion(out []byte) ([3]int, error) {
	m := versionRe.FindSubmatch(out)
	if m == nil {
		return [3]int{}, fmt.Errorf("unrecognised version output %q", out)
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(string(m[i+1]))
	}
	return v, nil
}

func versionLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func (a *Adapter) Available(ctx context.Context) error {
	if a.binErr != nil {
		return a.binErr
	}
	if a.stateErr != nil {
		return fmt.Errorf("%w: %v", agent.ErrUnavailable, a.stateErr)
	}
	res, err := a.exec(ctx, "", "--version")
	if err != nil {
		return fmt.Errorf("%w: %v", agent.ErrUnavailable, err)
	}
	if res.Code != 0 {
		return fmt.Errorf("%w: --version exited %d", agent.ErrUnavailable, res.Code)
	}
	v, err := parseVersion(res.Stdout)
	if err != nil {
		return fmt.Errorf("%w: %v", agent.ErrUnavailable, err)
	}
	if versionLess(v, MinVersion) {
		return fmt.Errorf("%w: version %d.%d.%d is older than %d.%d.%d", agent.ErrUnavailable, v[0], v[1], v[2], MinVersion[0], MinVersion[1], MinVersion[2])
	}
	return nil
}

// caps declares only what is implemented (README "Capabilities").
var caps = map[string]map[string]string{
	"": {
		agent.OpList: agent.Supported, agent.OpStatus: agent.Supported, agent.OpStart: agent.Supported,
	},
	agent.Managed: {
		agent.OpList: agent.Supported, agent.OpStatus: agent.Supported, agent.OpResume: agent.Supported,
		agent.OpSend: agent.Partial,
	},
	agent.Observed: {
		agent.OpList: agent.Supported, agent.OpStatus: agent.Partial,
	},
}

func (a *Adapter) Caps(ctx context.Context, s *agent.SessionRef) (agent.Caps, error) {
	own := ""
	if s != nil {
		own = a.ownership(*s)
	}
	ops := make(map[string]string, len(agent.Ops))
	for _, op := range agent.Ops {
		ops[op] = agent.Unsupported
		if v, ok := caps[own][op]; ok {
			ops[op] = v
		}
	}
	return agent.Caps{Ops: ops, AttachObserved: false}, nil
}

// ownership trusts the adapter's own records over the caller's label: a
// session is managed only if this adapter started or forked it.
func (a *Adapter) ownership(s agent.SessionRef) string {
	if _, ok := a.state.session(s.ID); ok {
		return agent.Managed
	}
	return agent.Observed
}

func (a *Adapter) exec(ctx context.Context, dir string, args ...string) (Result, error) {
	if a.binErr != nil {
		return Result{}, a.binErr
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	return a.run.Run(ctx, dir, args...)
}

func (a *Adapter) listing(ctx context.Context) ([]entry, error) {
	res, err := a.exec(ctx, "", "agents", "--json", "--all")
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("agents --json exited %d: %s", res.Code, firstLine(res.Stderr))
	}
	return parseAgents(res.Stdout)
}

func (a *Adapter) List(ctx context.Context, f agent.ListFilter) ([]agent.SessionInfo, error) {
	es, err := a.listing(ctx)
	if err != nil {
		return nil, err
	}
	now := a.now()
	var out []agent.SessionInfo
	for _, e := range es {
		id := e.ref()
		if id == "" || !under(e.Cwd, f.Cwd) {
			continue
		}
		ref := agent.SessionRef{Adapter: Name, ID: id, Cwd: e.Cwd}
		ref.Ownership = a.ownership(ref)
		if f.Ownership != "" && ref.Ownership != f.Ownership {
			continue
		}
		hooks, herr := a.hooks(id)
		st := mapStatus(&e, hooks, now)
		if herr != nil {
			st = unknown(now, SourceHooks, "hook log unreadable: "+herr.Error())
		}
		out = append(out, agent.SessionInfo{Ref: ref, Name: e.Name, Status: st, StartedAt: e.started()})
	}
	return out, nil
}

func (a *Adapter) Status(ctx context.Context, s agent.SessionRef) (agent.Status, error) {
	now := a.now()
	es, err := a.listing(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return agent.Status{}, ctx.Err() // the caller gave up; not a status
		}
		return unknown(now, SourceNone, "listing failed: "+err.Error()), nil
	}
	hooks, err := a.hooks(s.ID)
	if err != nil {
		return unknown(now, SourceHooks, "hook log unreadable: "+err.Error()), nil
	}
	return mapStatus(find(es, s.ID), hooks, now), nil
}

func (a *Adapter) hooks(id string) ([]hookRecord, error) {
	rec, ok := a.state.session(id)
	if !ok || rec.HooksDir == "" {
		return nil, nil
	}
	return readHooks(rec.HooksDir, id)
}

func under(path, root string) bool {
	if root == "" {
		return true
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// firstLine is the first line of the first non-empty output.
func firstLine(outs ...[]byte) string {
	for _, b := range outs {
		if s := strings.TrimSpace(string(b)); s != "" {
			line, _, _ := strings.Cut(s, "\n")
			return line
		}
	}
	return ""
}
