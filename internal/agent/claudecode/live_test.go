//go:build claudelive

package claudecode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/agent"
)

// The live probes drive the installed claude. They are opt-in and never
// run in CI.
//
// TestLiveForkReadOnly needs no trusted folder (-p runs anywhere):
//
//	EKUMEN_CLAUDE_LIVE=1 go test -tags claudelive -run TestLiveForkReadOnly -v ./internal/agent/claudecode
//
// TestLive needs a folder already trusted in Claude Code, since background
// sessions refuse untrusted folders and the probe never accepts trust:
//
//	EKUMEN_CLAUDE_LIVE=1 EKUMEN_CLAUDE_PROBE_DIR=<trusted folder> \
//	  go test -tags claudelive -run 'TestLive$' -v ./internal/agent/claudecode
//
// It starts background sessions named ekumen-probe-*, records sanitized
// listing entries and hook records into testdata/live-<version>/, and
// stops and removes every ekumen-probe-* session in the folder afterwards,
// including one whose Start returned an error.

func liveAdapter(t *testing.T) (*Adapter, string) {
	t.Helper()
	if os.Getenv("EKUMEN_CLAUDE_LIVE") != "1" {
		t.Skip("set EKUMEN_CLAUDE_LIVE=1")
	}
	a := New(Options{StateDir: t.TempDir(), Timeout: 3 * time.Minute})
	if err := a.Available(context.Background()); err != nil {
		t.Fatal(err)
	}
	res, _ := a.exec(context.Background(), "", "--version")
	f := strings.Fields(string(res.Stdout))
	if len(f) == 0 {
		t.Fatal("no version")
	}
	return a, f[0]
}

// TestLiveForkReadOnly is the #32 item 2 probe: a fork asked to write a
// file, with ReadOnly left false, must not write it.
func TestLiveForkReadOnly(t *testing.T) {
	a, ver := liveAdapter(t)
	ctx := context.Background()
	dir := t.TempDir()
	defer func() {
		res, err := a.exec(ctx, dir, "purge", "-y", dir)
		t.Logf("claude purge -y <tmp>: exit %d %v %s", res.Code, err, firstLine(res.Stdout, res.Stderr))
	}()
	src := uuidV5("live-src:" + dir)
	res, err := a.exec(ctx, dir, "-p", "--session-id", src, "--output-format", "json", "--", "Reply with exactly OK and nothing else.")
	if err != nil || res.Code != 0 {
		t.Fatalf("source session: exit %d %v %s", res.Code, err, firstLine(res.Stderr))
	}
	const target = "ekumen-fork-probe.txt"
	prompt := "Create a file named " + target + " in the current folder containing the word hi. Use your file-writing tool, or the shell if that fails. Then reply with exactly WROTE if the file now exists, or DENIED if you could not write it."
	f, err := a.Fork(ctx, agent.SessionRef{Adapter: Name, ID: src, Cwd: dir}, agent.ForkReq{Key: "live-fork-write", Prompt: prompt})
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	answer := ""
	if ch, err := a.Events(ctx, f, ""); err == nil {
		for ev := range ch {
			answer = ev.Text
		}
	}
	t.Logf("claude %s fork answer: %q", ver, answer)
	if _, err := os.Stat(filepath.Join(dir, target)); !os.IsNotExist(err) {
		t.Fatalf("the fork wrote %s: forks are not read-only", target)
	}
	t.Logf("claude %s: the fork could not write %s (ReadOnly=false, plan mode, prompts denied)", ver, target)
}

func TestLive(t *testing.T) {
	a, ver := liveAdapter(t)
	dir := os.Getenv("EKUMEN_CLAUDE_PROBE_DIR")
	if dir == "" {
		t.Skip("set EKUMEN_CLAUDE_PROBE_DIR to a folder trusted in Claude Code")
	}
	ctx := context.Background()
	out := filepath.Join("testdata", "live-"+ver)
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	leaks := leakMarkers(dir)
	save := func(name string, v any) {
		t.Helper()
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(out, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		for _, m := range leaks {
			if bytes.Contains(b, []byte(m)) {
				os.Remove(p)
				t.Fatalf("fixture %s would leak %q; removed", name, m)
			}
		}
	}

	defer cleanup(t, a, dir)

	probe := func(key, prompt string, until func(agent.Status) bool) agent.SessionRef {
		ref, err := a.Start(ctx, agent.StartReq{Key: key, Cwd: dir, Prompt: prompt, Task: "probe"})
		if err != nil {
			t.Fatalf("Start %s: %v", key, err) // cleanup still removes it by name
		}
		var st agent.Status
		for i := 0; i < 90; i++ {
			if st, _ = a.Status(ctx, ref); until(st) {
				break
			}
			time.Sleep(2 * time.Second)
		}
		t.Logf("%s: %+v", key, st)
		es, _ := a.listing(ctx)
		if e := find(es, ref.ID); e != nil {
			save("agents-"+key+".json", sanitizeEntry(*e, "ekumen-probe-"+key))
		}
		rec, _ := a.state.session(ref.ID)
		hs, _ := os.ReadDir(rec.HooksDir)
		for i, h := range hs {
			b, err := os.ReadFile(filepath.Join(rec.HooksDir, h.Name()))
			if err != nil {
				continue
			}
			if m, ok := sanitizeHook(b); ok {
				save(fmtHookName(key, i, m), m)
			}
		}
		return ref
	}
	isIdle := func(s agent.Status) bool { return s.State == agent.StateIdle }
	needsInput := func(s agent.Status) bool { return s.State == agent.StateNeedsInput }

	ok := probe("ok", "Reply with exactly OK and nothing else.", isIdle)
	d, err := a.Send(ctx, ok, agent.Message{Key: "probe-send", Text: "Reply with exactly OK2.", Mode: agent.ModeNextTurn})
	t.Logf("Send to idle background session: %+v %v", d, err)
	f, err := a.Fork(ctx, ok, agent.ForkReq{Key: "probe-fork", Prompt: "What did you reply? One word."})
	t.Logf("Fork: %+v %v", f, err)

	perm := probe("permission", "Run the shell command `touch ekumen-probe.txt` in the current folder.", needsInput)
	d, err = a.Send(ctx, perm, agent.Message{Key: "probe-send-blocked", Text: "Reply with exactly OK3.", Mode: agent.ModeNextTurn})
	t.Logf("Send to a session waiting on a permission prompt: %+v %v", d, err)
	probe("question", "Use the AskUserQuestion tool to ask me whether I prefer red or blue, then wait.", needsInput)
}

// cleanup stops and removes every ekumen-probe-* session in dir, found by
// name in the listing, so a session whose Start reported an error is not
// leaked; it never slices ids.
func cleanup(t *testing.T, a *Adapter, dir string) {
	ctx := context.Background()
	es, err := a.listing(ctx)
	if err != nil {
		t.Logf("cleanup: listing failed (%v); check `claude agents --all` for ekumen-probe-* sessions", err)
		return
	}
	for _, e := range es {
		if !strings.HasPrefix(e.Name, "ekumen-probe-") || !sameDir(e.Cwd, dir) {
			continue
		}
		id := e.ID
		if id == "" {
			id = e.SessionID
		}
		if checkID(id) != nil {
			continue
		}
		a.exec(ctx, dir, "stop", id)
		a.exec(ctx, dir, "rm", id)
	}
	t.Logf("sessions stopped and removed; to delete their transcripts run: claude purge %s", dir)
}

func fmtHookName(key string, i int, m map[string]any) string {
	name := m["hook_event_name"].(string)
	if n, ok := m["notification_type"].(string); ok {
		name += "-" + n
	}
	return fmt.Sprintf("hook-%s-%02d-%s.json", key, i, name)
}
