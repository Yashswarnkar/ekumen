package claudecode

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/agent"
	"github.com/Yashswarnkar/ekumen/internal/agent/conformance"
)

func TestFork_ArgvReadOnlyAndIdempotent_CAdapter4(t *testing.T) {
	e := newEnv(t)
	src := e.idle(t, "s")
	r := agent.ForkReq{Key: "f1", Prompt: "why?", ReadOnly: true}
	f, err := e.a.Fork(ctx, src, r)
	if err != nil {
		t.Fatal(err)
	}
	id := uuidV5("fork:f1")
	if f.ID != id || f.Ownership != agent.Managed || f.Cwd != src.Cwd {
		t.Errorf("fork %+v", f)
	}
	if g, err := e.a.Fork(ctx, src, r); err != nil || g != f {
		t.Errorf("repeat: %+v %v", g, err)
	}
	acts := e.actuations(t)[1:]
	want := []string{"-p", "--resume", src.ID, "--fork-session", "--session-id", id, "--output-format", "json", "--permission-mode", "plan", "--permission-prompts", "none", "why?"}
	if len(acts) != 1 || !reflect.DeepEqual(acts[0].Argv, want) {
		t.Errorf("argv %+v, want %v", acts, want)
	}
	assertSafe(t, e, src.Cwd, acts)
	ch, err := e.a.Events(ctx, f, "")
	if err != nil {
		t.Fatal(err)
	}
	if ev := <-ch; ev.Kind != agent.EventMessage || ev.Text != "OK" {
		t.Errorf("fork answer event %+v", ev)
	}
}

func TestObserved_ReadOnly_DClaudeAuth(t *testing.T) {
	e := newEnv(t)
	cwd := t.TempDir()
	id := "bbbbbbb1-0000-4000-8000-000000000001"
	e.setEntry(t, id, map[string]any{"cwd": cwd})
	obs := agent.SessionRef{Adapter: Name, ID: id, Ownership: agent.Observed}
	if err := e.a.Resume(ctx, obs, nil); !errors.Is(err, agent.ErrUnsupported) {
		t.Errorf("Resume: %v", err)
	}
	if _, err := e.a.Events(ctx, obs, ""); !errors.Is(err, agent.ErrUnsupported) {
		t.Errorf("Events: %v", err)
	}
	f, err := e.a.Fork(ctx, obs, agent.ForkReq{Key: "fo", Prompt: "q", ReadOnly: true})
	if err != nil || f.ID == id {
		t.Fatalf("Fork: %+v %v", f, err)
	}
	acts := e.actuations(t)
	if len(acts) != 1 || !slices.Contains(acts[0].Argv, "plan") {
		t.Errorf("argv %+v", acts)
	}
	assertSafe(t, e, cwd, acts)
	for _, d := range []string{e.home, cwd} {
		if _, err := os.Stat(filepath.Join(d, ".claude")); !os.IsNotExist(err) {
			t.Errorf("%s/.claude exists", d)
		}
	}
	e.assertHomeEmpty(t)
}

func TestEvents_FromHooks(t *testing.T) {
	e := newEnv(t)
	ref := e.start(t, "s")
	rec, _ := e.a.state.session(ref.ID)
	for i, name := range []string{"UserPromptSubmit", "PermissionRequest", "Stop"} {
		body := strings.ReplaceAll(fixture(t, "hooks/"+name+".json"), "aaaaaaaa-0000-4000-8000-0000000000aa", ref.ID)
		p := filepath.Join(rec.HooksDir, "r."+name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		at := t0.Add(time.Duration(i) * time.Second)
		os.Chtimes(p, at, at)
	}
	ch, err := e.a.Events(ctx, ref, t0.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for ev := range ch {
		kinds = append(kinds, ev.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{agent.EventNeedsInput, agent.EventTurnEnd}) {
		t.Errorf("events %v", kinds)
	}
}

func TestConformance(t *testing.T) {
	conformance.Run(t, func(tb testing.TB) *conformance.Harness {
		t := tb.(*testing.T)
		e := newEnv(t)
		obs := agent.SessionRef{Adapter: Name, ID: "bbbbbbb1-0000-4000-8000-000000000001", Cwd: "/work/repo-b", Ownership: agent.Observed}
		return &conformance.Harness{
			Adapter:       e.a,
			Cwd:           t.TempDir(),
			Idle:          e.idle(t, "seed-idle"),
			Busy:          e.start(t, "seed-busy"),
			Observed:      obs,
			Missing:       agent.SessionRef{Adapter: Name, ID: "ffffffff-0000-4000-8000-000000000000", Ownership: agent.Managed},
			Invocations:   func() int { return len(e.actuations(t)) },
			MakeUncertain: func() { setFake(t, e.fake, "resume.tmpl", "", fixture(t, "bg-note.synthetic.txt")) },
			Garble:        func() { setFake(t, e.fake, "agents", ".out", "[{") },
			BreakStatus:   func() { setFake(t, e.fake, "agents", ".exit", "1") },
		}
	})
}
