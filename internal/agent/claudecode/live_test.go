//go:build claudelive

package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/agent"
)

// TestLive drives the installed claude. It is opt-in and never runs in CI:
//
//	EKUMEN_CLAUDE_LIVE=1 EKUMEN_CLAUDE_PROBE_DIR=<a folder you trust in Claude Code> \
//	  go test -tags claudelive -run TestLive -v ./internal/agent/claudecode
//
// It starts three background sessions named ekumen-probe-*, records
// sanitized listing entries and hook records into testdata/live-<version>/,
// and stops and removes the sessions. The probe folder must already be
// trusted: background sessions refuse untrusted folders, and the probe
// never accepts trust itself.
func TestLive(t *testing.T) {
	if os.Getenv("EKUMEN_CLAUDE_LIVE") != "1" {
		t.Skip("set EKUMEN_CLAUDE_LIVE=1")
	}
	dir := os.Getenv("EKUMEN_CLAUDE_PROBE_DIR")
	if dir == "" {
		t.Skip("set EKUMEN_CLAUDE_PROBE_DIR to a folder trusted in Claude Code")
	}
	ctx := context.Background()
	a := New(Options{StateDir: t.TempDir(), Timeout: 2 * time.Minute})
	if err := a.Available(ctx); err != nil {
		t.Fatal(err)
	}
	res, _ := a.exec(ctx, "", "--version")
	ver := strings.Fields(string(res.Stdout))[0]
	out := filepath.Join("testdata", "live-"+ver)
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	sanitize := func(b []byte) []byte {
		s := strings.ReplaceAll(string(b), dir, "/work/probe")
		if home != "" {
			s = strings.ReplaceAll(s, home, "/home/user")
		}
		return []byte(s)
	}
	save := func(name string, b []byte) {
		if err := os.WriteFile(filepath.Join(out, name), sanitize(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var started []agent.SessionRef
	defer func() {
		for _, s := range started {
			short := s.ID[:8]
			a.exec(ctx, dir, "stop", short)
			a.exec(ctx, dir, "rm", short)
		}
		t.Logf("sessions stopped and removed; to delete their transcripts run: claude purge %s", dir)
	}()

	probe := func(key, prompt string, until func(agent.Status) bool) agent.SessionRef {
		ref, err := a.Start(ctx, agent.StartReq{Key: key, Cwd: dir, Prompt: prompt, Task: "probe"})
		if err != nil {
			t.Fatalf("Start %s: %v", key, err)
		}
		started = append(started, ref)
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
			e.Name = "ekumen-probe-" + key
			b, _ := json.MarshalIndent(e, "", "  ")
			save("agents-"+key+".json", b)
		}
		rec, _ := a.state.session(ref.ID)
		hs, _ := os.ReadDir(rec.HooksDir)
		for _, h := range hs {
			b, _ := os.ReadFile(filepath.Join(rec.HooksDir, h.Name()))
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				for _, k := range []string{"prompt", "tool_input", "last_assistant_message"} {
					if _, ok := m[k]; ok {
						m[k] = "<redacted>"
					}
				}
				b, _ = json.Marshal(m)
			}
			save("hook-"+key+"-"+h.Name()+".json", b)
		}
		return ref
	}
	isIdle := func(s agent.Status) bool { return s.State == agent.StateIdle }
	needsInput := func(s agent.Status) bool { return s.State == agent.StateNeedsInput }

	ok := probe("ok", "Reply with exactly OK and nothing else.", isIdle)
	d, err := a.Send(ctx, ok, agent.Message{Key: "probe-send", Text: "Reply with exactly OK2.", Mode: agent.ModeNextTurn})
	t.Logf("Send to idle background session: %+v %v", d, err)
	f, err := a.Fork(ctx, ok, agent.ForkReq{Key: "probe-fork", Prompt: "What did you reply? One word.", ReadOnly: true})
	t.Logf("Fork: %+v %v", f, err)

	perm := probe("permission", "Run the shell command `touch ekumen-probe.txt` in the current folder.", needsInput)
	d, err = a.Send(ctx, perm, agent.Message{Key: "probe-send-blocked", Text: "Reply with exactly OK3.", Mode: agent.ModeNextTurn})
	t.Logf("Send to a session waiting on a permission prompt: %+v %v", d, err)
	probe("question", "Use the AskUserQuestion tool to ask me whether I prefer red or blue, then wait.", needsInput)
}
