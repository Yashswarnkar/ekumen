package claudecode

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/agent"
)

var ctx = context.Background()

func TestAvailable(t *testing.T) {
	e := newEnv(t)
	if err := e.a.Available(ctx); err != nil {
		t.Fatalf("2.1.329: %v", err)
	}
	setFake(t, e.fake, "version", ".out", "2.1.258 (Claude Code)\n")
	if err := e.a.Available(ctx); !errors.Is(err, agent.ErrUnavailable) {
		t.Errorf("2.1.258: %v, want ErrUnavailable", err)
	}
	setFake(t, e.fake, "version", ".out", "garbage\n")
	if err := e.a.Available(ctx); !errors.Is(err, agent.ErrUnavailable) {
		t.Errorf("garbage version: %v", err)
	}
	gone := New(Options{Bin: filepath.Join(t.TempDir(), "claude"), StateDir: e.state})
	if err := gone.Available(ctx); !errors.Is(err, agent.ErrUnavailable) {
		t.Errorf("missing binary: %v", err)
	}
	for _, c := range e.calls(t) {
		if !filepath.IsAbs(e.a.Bin()) || len(c.Argv) != 1 || c.Argv[0] != "--version" {
			t.Errorf("call %+v (bin %s)", c, e.a.Bin())
		}
	}
	e.assertHomeEmpty(t)
}

func TestResolveBin_EnvThenPath(t *testing.T) {
	t.Setenv("EKUMEN_CLAUDE_BIN", "relative/claude")
	got, err := resolveBin("")
	if err != nil || !filepath.IsAbs(got) || !strings.HasSuffix(got, "relative/claude") {
		t.Errorf("EKUMEN_CLAUDE_BIN: %q %v", got, err)
	}
	t.Setenv("EKUMEN_CLAUDE_BIN", "")
	t.Setenv("PATH", t.TempDir())
	if _, err := resolveBin(""); !errors.Is(err, agent.ErrUnavailable) {
		t.Errorf("no claude on PATH: %v", err)
	}
}

func TestCaps_PerOwnership(t *testing.T) {
	e := newEnv(t)
	managed := agent.SessionRef{Adapter: Name, ID: "m1", Ownership: agent.Managed}
	if err := e.a.state.saveSession(sessionRecord{ID: "m1", Op: "start"}); err != nil {
		t.Fatal(err)
	}
	lying := agent.SessionRef{Adapter: Name, ID: "o1", Ownership: agent.Managed}
	want := map[string]map[string]string{
		"new":      {"list": "supported", "status": "supported", "start": "unsupported", "resume": "unsupported", "send": "unsupported", "fork": "unsupported", "events": "unsupported"},
		"managed":  {"list": "supported", "status": "supported", "start": "unsupported", "resume": "unsupported", "send": "unsupported", "fork": "unsupported", "events": "unsupported"},
		"observed": {"list": "supported", "status": "partial", "start": "unsupported", "resume": "unsupported", "send": "unsupported", "fork": "unsupported", "events": "unsupported"},
	}
	for name, ref := range map[string]*agent.SessionRef{"new": nil, "managed": &managed, "observed": &lying} {
		c, err := e.a.Caps(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		if c.AttachObserved {
			t.Error("AttachObserved must be false")
		}
		for op, w := range want[name] {
			if c.Ops[op] != w {
				t.Errorf("%s %s: %q, want %q", name, op, c.Ops[op], w)
			}
		}
	}
}

func TestList_OwnershipAndFilter(t *testing.T) {
	e := newEnv(t)
	if err := e.a.state.saveSession(sessionRecord{ID: "bbbbbbb1-0000-4000-8000-000000000001", Op: "start"}); err != nil {
		t.Fatal(err)
	}
	all, err := e.a.List(ctx, agent.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("listed %d, want 5", len(all))
	}
	for _, s := range all {
		wantOwn := agent.Observed
		if s.Ref.ID == "bbbbbbb1-0000-4000-8000-000000000001" {
			wantOwn = agent.Managed
		}
		if s.Ref.Ownership != wantOwn || s.Ref.Adapter != Name || s.StartedAt.IsZero() || s.Status.State == "" {
			t.Errorf("%+v", s)
		}
	}
	got, err := e.a.List(ctx, agent.ListFilter{Cwd: "/work/repo-b", Ownership: agent.Observed})
	if err != nil || len(got) != 1 || got[0].Name != "term-busy" {
		t.Errorf("filtered: %+v %v", got, err)
	}
	if got, _ := e.a.List(ctx, agent.ListFilter{Cwd: "/work/repo"}); len(got) != 0 {
		t.Errorf("cwd filter matched a sibling prefix: %+v", got)
	}
	setFake(t, e.fake, "agents", ".out", "{garbled")
	if _, err := e.a.List(ctx, agent.ListFilter{}); err == nil {
		t.Error("garbled listing listed without error")
	}
	for _, c := range e.calls(t) {
		if strings.Join(c.Argv, " ") != "agents --json --all" {
			t.Errorf("argv %v", c.Argv)
		}
	}
	e.assertHomeEmpty(t)
}

func TestStatus_FailuresAreUnknown_CAdapter3(t *testing.T) {
	e := newEnv(t)
	ref := agent.SessionRef{Adapter: Name, ID: "bbbbbbb2-0000-4000-8000-000000000002", Ownership: agent.Observed}
	st, err := e.a.Status(ctx, ref)
	if err != nil || st.State != agent.StateRunning || st.Source != SourceListing {
		t.Fatalf("busy: %+v %v", st, err)
	}
	st, _ = e.a.Status(ctx, agent.SessionRef{Adapter: Name, ID: "aaaaaaa1"})
	if st.State != agent.StateNeedsInput {
		t.Errorf("short id lookup: %+v", st)
	}
	st, err = e.a.Status(ctx, agent.SessionRef{Adapter: Name, ID: "ffffffff-0000-4000-8000-000000000000"})
	if err != nil || st.State != agent.StateUnknown {
		t.Errorf("missing: %+v %v", st, err)
	}
	setFake(t, e.fake, "agents", ".out", "[{")
	if st, err = e.a.Status(ctx, ref); err != nil || st.State != agent.StateUnknown {
		t.Errorf("garbled: %+v %v", st, err)
	}
	setFake(t, e.fake, "agents", ".exit", "1")
	if st, err = e.a.Status(ctx, ref); err != nil || st.State != agent.StateUnknown || st.Source != SourceNone {
		t.Errorf("failing: %+v %v", st, err)
	}
}

func TestStatus_ManagedReadsHookLog(t *testing.T) {
	e := newEnv(t)
	id := "bbbbbbb2-0000-4000-8000-000000000002"
	dir := filepath.Join(e.state, "hooks", "k")
	if err := e.a.state.saveSession(sessionRecord{ID: id, Op: "start", HooksDir: dir}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := strings.ReplaceAll(fixture(t, "hooks/PermissionRequest.json"), "aaaaaaaa-0000-4000-8000-0000000000aa", id)
	if err := os.WriteFile(filepath.Join(dir, "r.1"), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := e.a.Status(ctx, agent.SessionRef{Adapter: Name, ID: id})
	if err != nil || st.State != agent.StateNeedsInput || st.WaitingFor != agent.WaitPermission || st.Source != SourceHooks || st.Detail != "Bash" {
		t.Errorf("%+v %v", st, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "r.2"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st2, _ := e.a.Status(ctx, agent.SessionRef{Adapter: Name, ID: id}); st2 != st {
		t.Errorf("a garbled hook record changed the status: %+v, was %+v", st2, st)
	}
}

func TestStatus_CancelledIsAnError(t *testing.T) {
	e := newEnv(t)
	c, cancel := context.WithCancel(ctx)
	cancel()
	st, err := e.a.Status(c, agent.SessionRef{Adapter: Name, ID: "bbbbbbb2-0000-4000-8000-000000000002"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled Status: %+v %v, want context.Canceled", st, err)
	}
}

func TestStateDir_AbsoluteAndNotACache_Issue32Items7And9(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("EKUMEN_STATE_DIR", "")
	a := New(Options{StateDir: "rel/state"})
	if !filepath.IsAbs(a.state.dir) || !strings.HasSuffix(a.state.dir, "rel/state") {
		t.Errorf("relative state dir kept as %q", a.state.dir)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	if got, _ := stateDir(""); got != filepath.Join(home, ".local", "state", "ekumen", "claude-code") {
		t.Errorf("default %q", got)
	}
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "xdg"))
	if got, _ := stateDir(""); got != filepath.Join(home, "xdg", "ekumen", "claude-code") {
		t.Errorf("XDG default %q", got)
	}
	t.Setenv("EKUMEN_STATE_DIR", "env/state")
	if got, _ := stateDir(""); !filepath.IsAbs(got) {
		t.Errorf("EKUMEN_STATE_DIR kept relative: %q", got)
	}
}

func TestStore_CreateIsExclusiveAndWritesLeaveNoTemps_Issue32Item7(t *testing.T) {
	s := &store{dir: t.TempDir()}
	r := deliveryRecord{Key: "k", State: agent.Uncertain}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.claimDelivery(r); err == nil {
				wins.Add(1)
			} else if !errors.Is(err, fs.ErrExist) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Errorf("%d claims of one key succeeded, want 1", wins.Load())
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.saveDelivery(deliveryRecord{Key: "k", State: agent.Delivered}) }()
	}
	wg.Wait()
	if d, ok := s.delivery("k"); !ok || d.State != agent.Delivered {
		t.Errorf("after concurrent writes: %+v %v", d, ok)
	}
	ents, _ := os.ReadDir(s.path("deliveries"))
	if len(ents) != 1 {
		t.Errorf("files left: %v", ents)
	}
}
