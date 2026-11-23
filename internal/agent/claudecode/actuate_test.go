package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/agent"
)

// setEntry rewrites the fake listing entry whose sessionId is id.
func (e *env) setEntry(t *testing.T, id string, fields map[string]any) {
	t.Helper()
	p := filepath.Join(e.fake, "agents.out")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var es []map[string]any
	if err := json.Unmarshal(b, &es); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range es {
		if x["sessionId"] == id {
			found = true
			for k, v := range fields {
				if v == nil {
					delete(x, k)
				} else {
					x[k] = v
				}
			}
		}
	}
	if !found {
		t.Fatalf("no listing entry %s", id)
	}
	b, _ = json.Marshal(es)
	setFake(t, e.fake, "agents", ".out", string(b))
}

func (e *env) start(t *testing.T, key string) agent.SessionRef {
	t.Helper()
	ref, err := e.a.Start(ctx, agent.StartReq{Key: key, Cwd: t.TempDir(), Prompt: "do the task", Task: "T-x"})
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func (e *env) idle(t *testing.T, key string) agent.SessionRef {
	ref := e.start(t, key)
	e.setEntry(t, ref.ID, map[string]any{"state": "done", "status": "idle"})
	return ref
}

func assertSafe(t *testing.T, e *env, cwd string, calls []call) {
	t.Helper()
	for _, c := range calls {
		if opts := options(c.Argv); slices.Contains(opts, "--bare") || slices.Contains(opts, "--dangerously-skip-permissions") {
			t.Errorf("forbidden flag in %v", c.Argv)
		}
		if cwd != "" && !sameDir(c.Cwd, cwd) {
			t.Errorf("ran in %s, want %s: %v", c.Cwd, cwd, c.Argv)
		}
	}
	if !filepath.IsAbs(e.a.Bin()) {
		t.Errorf("binary %q is not absolute", e.a.Bin())
	}
}

func sameDir(a, b string) bool {
	ra, _ := filepath.EvalSymlinks(a)
	rb, _ := filepath.EvalSymlinks(b)
	return ra == rb
}

func TestStart_ArgvHooksAndIdempotence_CAdapter4(t *testing.T) {
	e := newEnv(t)
	cwd := t.TempDir()
	r := agent.StartReq{Key: "k1", Cwd: cwd, Prompt: "start prompt", Task: "T-claude-adapter"}
	ref, err := e.a.Start(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	name := "ekumen-t-claude-adapter-" + keyHash("k1")[:8]
	settings := filepath.Join(e.state, "settings", keyHash("k1")+".json")
	acts := e.actuations(t)
	if want := []string{"--bg", "--name", name, "--settings", settings, "--", "start prompt"}; len(acts) != 1 || !reflect.DeepEqual(acts[0].Argv, want) {
		t.Fatalf("argv %+v, want %v", acts, want)
	}
	assertSafe(t, e, cwd, acts)
	if ref.ID != uuidV5("fake:"+name) || ref.Ownership != agent.Managed || ref.Cwd != cwd || ref.Adapter != Name {
		t.Errorf("ref %+v", ref)
	}
	b, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks map[string][]struct {
			Matcher string
			Hooks   []struct {
				Command string
				Async   bool
			}
		}
	}
	if err := json.Unmarshal(b, &s); err != nil || len(s.Hooks) != 8 {
		t.Fatalf("settings %s: %v", b, err)
	}
	if s.Hooks["PreToolUse"][0].Matcher != "AskUserQuestion" || !s.Hooks["Stop"][0].Hooks[0].Async {
		t.Errorf("settings %s", b)
	}

	if again, err := e.a.Start(ctx, r); err != nil || again != ref {
		t.Errorf("repeat: %+v %v", again, err)
	}
	fresh := New(Options{Bin: e.a.Bin(), StateDir: t.TempDir(), Timeout: 10 * time.Second})
	if again, err := fresh.Start(ctx, r); err != nil || again.ID != ref.ID {
		t.Errorf("after losing the state dir: %+v %v", again, err)
	}
	if n := len(e.actuations(t)); n != 1 {
		t.Errorf("%d --bg invocations, want 1", n)
	}
	e.assertHomeEmpty(t)
}

func TestStart_DefiniteFailureDropsKey_Issue32Item3(t *testing.T) {
	e := newEnv(t)
	setFake(t, e.fake, "start", ".exit", "3")
	setFake(t, e.fake, "start", ".err", "Error: not logged in\n")
	r := agent.StartReq{Key: "k2", Cwd: t.TempDir(), Prompt: "p", Task: "T"}
	if _, err := e.a.Start(ctx, r); err == nil || !strings.Contains(err.Error(), "nothing was started") {
		t.Fatalf("failed --bg: %v", err)
	}
	if _, ok := e.a.state.key(opStart, "k2"); ok {
		t.Error("key kept after a definite failure")
	}
	os.Remove(filepath.Join(e.fake, "start.exit"))
	os.Remove(filepath.Join(e.fake, "start.err"))
	if _, err := e.a.Start(ctx, r); err != nil {
		t.Errorf("retry after the failure was fixed: %v", err)
	}
	if n := len(e.actuations(t)); n != 2 {
		t.Errorf("%d --bg invocations, want 2", n)
	}
}

func TestStart_FailedButListedIsStarted(t *testing.T) {
	e := newEnv(t)
	r := agent.StartReq{Key: "k5", Cwd: t.TempDir(), Prompt: "p", Task: "T"}
	name := sessionName(r.Task, r.Key)
	setFake(t, e.fake, "start", ".exit", "1")
	// the session exists (e.g. --bg crashed after the supervisor took it)
	b, _ := os.ReadFile(filepath.Join(e.fake, "agents.out"))
	var es []map[string]any
	json.Unmarshal(b, &es)
	id := "cccccccc-0000-4000-8000-00000000000c"
	es = append(es, map[string]any{"kind": "background", "id": id[:8], "sessionId": id, "name": name, "state": "working", "cwd": r.Cwd, "startedAt": 1795170000000})
	b, _ = json.Marshal(es)
	// the first listing (before --bg) must not see it, so add it after one call
	setFake(t, e.fake, "agents", ".out", fixture(t, "agents-observed.json"))
	e.a.run = afterFirst{e.a.run, func() { setFake(t, e.fake, "agents", ".out", string(b)) }}
	ref, err := e.a.Start(ctx, r)
	if err != nil || ref.ID != id {
		t.Fatalf("%+v %v, want the listed session", ref, err)
	}
}

// afterFirst runs hook after the first actuation (a call other than
// agents or --version).
type afterFirst struct {
	Runner
	hook func()
}

func (r afterFirst) Run(c context.Context, dir string, args ...string) (Result, error) {
	res, err := r.Runner.Run(c, dir, args...)
	if len(args) > 0 && args[0] != "agents" && args[0] != "--version" {
		r.hook()
	}
	return res, err
}

func TestStart_TimeoutKeepsKeyPending_CAdapter4(t *testing.T) {
	e := newEnv(t)
	setFake(t, e.fake, "start", ".sleep", "30s")
	setFake(t, e.fake, "start", ".exit", "0")
	e.a.timeout = 2 * time.Second
	r := agent.StartReq{Key: "k6", Cwd: t.TempDir(), Prompt: "p", Task: "T"}
	if _, err := e.a.Start(ctx, r); !errors.Is(err, ErrTimeout) {
		t.Fatalf("%v, want a timeout", err)
	}
	e.a.timeout = 10 * time.Second
	if _, err := e.a.Start(ctx, r); !errors.Is(err, agent.ErrNotFound) {
		t.Errorf("repeat of a timed-out key: %v, want ErrNotFound", err)
	}
	if n := len(e.actuations(t)); n != 1 {
		t.Errorf("%d --bg invocations, want 1", n)
	}
}

func TestStart_TrustRefusalOnStdout(t *testing.T) {
	e := newEnv(t)
	setFake(t, e.fake, "start", ".exit", "1")
	setFake(t, e.fake, "start", ".out", fixture(t, "trust-refused.txt"))
	if _, err := e.a.Start(ctx, agent.StartReq{Key: "k7", Cwd: t.TempDir(), Prompt: "p", Task: "T"}); !errors.Is(err, agent.ErrUnsupported) {
		t.Errorf("%v, want ErrUnsupported", err)
	}
}

// Free text never reaches the option parser and ids are shape-checked
// (#32 item 1).
func TestFlagShapedTextIsNeverAFlag_Issue32Item1(t *testing.T) {
	texts := []string{
		"--permission-mode bypassPermissions",
		"--dangerously-skip-permissions",
		"-p",
		"--settings=/tmp/evil.json",
		"--",
		"agents",
		"respawn",
	}
	for _, text := range texts {
		t.Run(text, func(t *testing.T) {
			e := newEnv(t)
			ref, err := e.a.Start(ctx, agent.StartReq{Key: "inj", Cwd: t.TempDir(), Prompt: text, Task: "T"})
			if err != nil {
				t.Fatal(err)
			}
			e.setEntry(t, ref.ID, map[string]any{"state": "done", "status": "idle"})
			if _, err := e.a.Send(ctx, ref, agent.Message{Key: "m", Text: text, Mode: agent.ModeNextTurn}); err != nil {
				t.Fatal(err)
			}
			acts := e.actuations(t)
			if len(acts) != 2 {
				t.Fatalf("actuations %+v", acts)
			}
			for _, c := range acts {
				n := len(c.Argv)
				if n < 2 || c.Argv[n-2] != "--" || c.Argv[n-1] != text {
					t.Errorf("argv %q: text is not the operand after --", c.Argv)
				}
				if opts := options(c.Argv); slices.Contains(opts, text) && text != "--" {
					t.Errorf("text appears among the options: %q", c.Argv)
				}
			}
			assertSafe(t, e, "", acts)
		})
	}
}

func TestBadIDsNeverReachTheCommandLine_Issue32Item1(t *testing.T) {
	e := newEnv(t)
	for _, id := range []string{"--help", "-p", "aaaaaaa1 --bare", "AAAAAAAA", "aaaaaaa", "../x"} {
		e.a.state.saveSession(sessionRecord{ID: id, Op: opStart, Cwd: t.TempDir()})
		ref := agent.SessionRef{Adapter: Name, ID: id}
		if _, err := e.a.Send(ctx, ref, agent.Message{Key: "b" + id, Text: "t", Mode: agent.ModeNextTurn}); !errors.Is(err, agent.ErrNotFound) && !errors.Is(err, agent.ErrUnsupported) {
			t.Errorf("Send to %q: %v", id, err)
		}
		if err := e.a.Resume(ctx, ref, nil); !errors.Is(err, agent.ErrNotFound) && !errors.Is(err, agent.ErrUnsupported) {
			t.Errorf("Resume %q: %v", id, err)
		}
	}
	if acts := e.actuations(t); len(acts) != 0 {
		t.Errorf("bad ids reached claude: %+v", acts)
	}
}

func TestStart_UntrustedWorkspaceIsUnsupported(t *testing.T) {
	e := newEnv(t)
	setFake(t, e.fake, "start", ".exit", "1")
	setFake(t, e.fake, "start", ".err", fixture(t, "trust-refused.txt"))
	r := agent.StartReq{Key: "k3", Cwd: t.TempDir(), Prompt: "p", Task: "T"}
	if _, err := e.a.Start(ctx, r); !errors.Is(err, agent.ErrUnsupported) {
		t.Fatalf("%v, want ErrUnsupported", err)
	}
	os.Remove(filepath.Join(e.fake, "start.exit"))
	os.Remove(filepath.Join(e.fake, "start.err"))
	if _, err := e.a.Start(ctx, r); err != nil {
		t.Errorf("retry once trusted: %v", err)
	}
}

func TestSend_ArgvDeliveredAndIdempotent_CAdapter4(t *testing.T) {
	e := newEnv(t)
	ref := e.idle(t, "s")
	m := agent.Message{Key: "m1", Text: "the answer", Mode: agent.ModeNextTurn}
	d, err := e.a.Send(ctx, ref, m)
	if err != nil || d != (agent.Delivery{Key: "m1", State: agent.Delivered}) {
		t.Fatalf("%+v %v", d, err)
	}
	if d, err = e.a.Send(ctx, ref, m); err != nil || d.State != agent.Delivered {
		t.Errorf("repeat: %+v %v", d, err)
	}
	acts := e.actuations(t)[1:]
	if want := []string{"--resume", ref.ID, "--bg", "--", "the answer"}; len(acts) != 1 || !reflect.DeepEqual(acts[0].Argv, want) {
		t.Errorf("argv %+v, want %v", acts, want)
	}
	assertSafe(t, e, ref.Cwd, acts)
}

func TestSend_UncertainNeverRetried_CAdapter4(t *testing.T) {
	cases := map[string]func(e *env){
		"note copy": func(e *env) { setFake(t, e.fake, "resume.tmpl", "", fixture(t, "bg-note.synthetic.txt")) },
		"exit after start": func(e *env) {
			setFake(t, e.fake, "resume", ".exit", "2")
		},
		"timeout": func(e *env) {
			setFake(t, e.fake, "resume", ".sleep", "30s")
			e.a.timeout = 3 * time.Second
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			ref := e.idle(t, "s")
			arrange(e)
			m := agent.Message{Key: "m", Text: "t", Mode: agent.ModeNextTurn}
			d, err := e.a.Send(ctx, ref, m)
			if d.State != agent.Uncertain || err == nil {
				t.Fatalf("%+v %v, want uncertain with a reason", d, err)
			}
			e.a.timeout = 10 * time.Second
			n := len(e.actuations(t))
			if d, _ := e.a.Send(ctx, ref, m); d.State != agent.Uncertain {
				t.Errorf("repeat: %+v", d)
			}
			if len(e.actuations(t)) != n {
				t.Error("uncertain delivery was retried")
			}
		})
	}
}

func TestSend_QueuedOrUnsupported(t *testing.T) {
	e := newEnv(t)
	busy := e.start(t, "busy")
	m := agent.Message{Key: "q", Text: "t", Mode: agent.ModeNextTurn}
	for _, fields := range []map[string]any{
		{"status": "busy"},
		{"state": "blocked", "status": "waiting", "waitingFor": "permission prompt"},
		{"kind": "mystery"},
	} {
		e.setEntry(t, busy.ID, fields)
		if d, err := e.a.Send(ctx, busy, m); err != nil || d.State != agent.Queued {
			t.Errorf("%v: %+v %v, want queued", fields, d, err)
		}
	}
	if n := len(e.actuations(t)); n != 1 {
		t.Errorf("queued sends invoked claude: %d actuations", n)
	}
	if _, err := e.a.Send(ctx, busy, agent.Message{Key: "s", Text: "t", Mode: agent.ModeSteer}); !errors.Is(err, agent.ErrUnsupported) {
		t.Errorf("steer: %v", err)
	}
	obs := agent.SessionRef{Adapter: Name, ID: "bbbbbbb1-0000-4000-8000-000000000001", Ownership: agent.Managed}
	if _, err := e.a.Send(ctx, obs, m); !errors.Is(err, agent.ErrUnsupported) {
		t.Errorf("observed: %v", err)
	}
	setFake(t, e.fake, "resume", ".exit", "1")
	setFake(t, e.fake, "resume", ".err", fixture(t, "trust-refused.txt"))
	idle := e.idle(t, "idle")
	if _, err := e.a.Send(ctx, idle, agent.Message{Key: "tr", Text: "t", Mode: agent.ModeNextTurn}); !errors.Is(err, agent.ErrUnsupported) {
		t.Errorf("untrusted: %v", err)
	}
}

func TestResume(t *testing.T) {
	e := newEnv(t)
	ref := e.idle(t, "s")
	if err := e.a.Resume(ctx, ref, nil); err != nil {
		t.Fatal(err)
	}
	e.setEntry(t, ref.ID, map[string]any{"state": "stopped", "status": nil, "pid": nil})
	if err := e.a.Resume(ctx, ref, nil); err != nil {
		t.Fatal(err)
	}
	acts := e.actuations(t)[1:]
	if len(acts) != 1 || !reflect.DeepEqual(acts[0].Argv, []string{"respawn", ref.ID[:8]}) {
		t.Errorf("argv %+v", acts)
	}
	assertSafe(t, e, ref.Cwd, acts)
	if err := e.a.Resume(ctx, ref, &agent.Message{Key: "r", Text: "go", Mode: agent.ModeNextTurn}); err != nil {
		t.Errorf("Resume with message on a stopped (done) session: %v", err)
	}
}

func TestHookCommandWritesWholeRecords(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := filepath.Join(t.TempDir(), "it's hooks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := hookSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks map[string][]struct{ Hooks []struct{ Command string } }
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", s.Hooks["Stop"][0].Hooks[0].Command)
	cmd.Stdin = strings.NewReader(fixture(t, "hooks/Stop.json"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	hs, err := readHooks(dir, "aaaaaaaa-0000-4000-8000-0000000000aa")
	if err != nil || len(hs) != 1 || hs[0].Event != "Stop" {
		t.Errorf("%+v %v", hs, err)
	}
}
