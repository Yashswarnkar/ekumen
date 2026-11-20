package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/store/files"
)

// TestCICheckScript runs scripts/ci-check.sh (the CI `ekumen check` step,
// docs/development.md) against fixtures with a freshly built binary.
func TestCICheckScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the CI step is a bash script")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	if testing.Short() {
		t.Skip("builds the binary")
	}
	script, err := filepath.Abs("../../scripts/ci-check.sh")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "ekumen")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	n := 0
	appendEvent := func(t *testing.T, root string, typ event.Type, actor event.Actor, sub event.Subject, data any) {
		t.Helper()
		n++
		id, err := event.ULIDFromParts(uint64(at.UnixMilli())+uint64(n), [10]byte{9: byte(n)})
		if err != nil {
			t.Fatal(err)
		}
		e, err := event.New(id, typ, at, actor, sub, data)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := event.Encode(e)
		if _, err := files.New(filepath.Join(root, ".ekumen")).Append(b); err != nil {
			t.Fatal(err)
		}
	}
	project := event.Subject{Kind: event.KindProject, ID: event.ProjectSubjectID}
	claim := func(t *testing.T, root string) {
		appendEvent(t, root, event.RoleClaimedType, event.Actor{Role: event.RoleOrchestrator, ID: "main"}, project,
			event.RoleClaimed{Role: event.RoleOrchestrator, Session: "main"})
	}

	gitVars := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com"}
	git := func(t *testing.T, root string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), gitVars...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	firstEvent := func(t *testing.T, root string) string {
		t.Helper()
		fs, _ := filepath.Glob(filepath.Join(root, ".ekumen/events/*/*/*.json"))
		if len(fs) == 0 {
			t.Fatal("no events")
		}
		return fs[0]
	}

	cases := []struct {
		name  string
		setup func(t *testing.T, root string)
		// after, when set, runs once setup is committed as the base.
		after   func(t *testing.T, root string)
		fail    bool
		wantOut string
	}{
		{"no events folder", func(t *testing.T, root string) {}, nil, false, "skipped"},
		{"empty events folder", func(t *testing.T, root string) {
			os.MkdirAll(filepath.Join(root, ".ekumen/events"), 0o755)
		}, nil, true, "holds no events"},
		{"clean log", claim, nil, false, `"ok": true`},
		{"no events folder on the base or now", func(t *testing.T, root string) {
			os.WriteFile(filepath.Join(root, "README"), []byte("x\n"), 0o644)
		}, func(t *testing.T, root string) {}, false, "skipped"},
		{"events folder removed since the base", claim, func(t *testing.T, root string) {
			os.RemoveAll(filepath.Join(root, ".ekumen/events"))
		}, true, "exists on the base"},
		{"event edited since the base", claim, func(t *testing.T, root string) {
			p := firstEvent(t, root)
			b, _ := os.ReadFile(p)
			os.WriteFile(p, append(b, '\n'), 0o644)
		}, true, "append-only"},
		{"event deleted since the base", func(t *testing.T, root string) {
			claim(t, root)
			appendEvent(t, root, event.RoleReleasedType, event.Actor{Role: event.RoleOrchestrator, ID: "main"}, project,
				event.RoleReleased{Role: event.RoleOrchestrator, Session: "main"})
		}, func(t *testing.T, root string) {
			os.Remove(firstEvent(t, root))
		}, true, "append-only"},
		{"event appended since the base", claim, func(t *testing.T, root string) {
			appendEvent(t, root, event.RoleReleasedType, event.Actor{Role: event.RoleOrchestrator, ID: "main"}, project,
				event.RoleReleased{Role: event.RoleOrchestrator, Session: "main"})
		}, false, `"ok": true`},
		{"lane violation", func(t *testing.T, root string) {
			claim(t, root)
			// A session approving a task: the lead's lane (C-event-log §3),
			// as it could arrive through a git merge.
			appendEvent(t, root, event.TaskApprovedType, event.Actor{Role: event.RoleSession, ID: "t-x"},
				event.Subject{Kind: event.KindTask, ID: "T-x"}, event.TaskApproved{TierGrants: []string{}, TextSHA256: strings.Repeat("0", 64)})
		}, nil, true, `"rule": "lane"`},
		{"stale decision file", func(t *testing.T, root string) {
			claim(t, root)
			appendEvent(t, root, event.DecisionRaisedType, event.Actor{Role: event.RoleOrchestrator, ID: "main"},
				event.Subject{Kind: event.KindDecision, ID: "D-x"}, event.DecisionRaised{ID: "D-x", Title: "x", Tier: 2,
					RaisedBy: event.RaisedBy{Session: "main", On: "2026-11-20"}, Question: "?",
					Options: []event.Option{{ID: "a", Tradeoff: "a"}, {ID: "b", Tradeoff: "b"}},
					Blocks:  []string{}, ContractImpact: []string{}, Context: event.DecisionContext{Summary: "x"}})
			os.MkdirAll(filepath.Join(root, ".ekumen/decisions"), 0o755)
			os.WriteFile(filepath.Join(root, ".ekumen/decisions/D-x.yaml"), []byte("id: D-x\n"), 0o644)
		}, nil, true, "does not match the events"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".ekumen"), 0o755); err != nil {
				t.Fatal(err)
			}
			c.setup(t, root)
			env := append(os.Environ(), "EKUMEN_BIN="+bin, "EKUMEN_CI_BASE=", "GITHUB_BASE_REF=", "GITHUB_EVENT_NAME=")
			if c.after != nil {
				git(t, root, "init", "-q")
				git(t, root, "add", "-A")
				git(t, root, "commit", "-q", "-m", "base")
				c.after(t, root)
				env = append(env, "EKUMEN_CI_BASE=HEAD")
			}
			cmd := exec.Command("bash", script, root)
			cmd.Env = append(env, gitVars...)
			out, err := cmd.CombinedOutput()
			if failed := err != nil; failed != c.fail {
				t.Fatalf("failed %v, want %v: %v\n%s", failed, c.fail, err, out)
			}
			if !strings.Contains(string(out), c.wantOut) {
				t.Errorf("output lacks %q:\n%s", c.wantOut, out)
			}
		})
	}
}
