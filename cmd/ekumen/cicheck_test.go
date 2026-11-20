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

	cases := []struct {
		name    string
		setup   func(t *testing.T, root string)
		fail    bool
		wantOut string
	}{
		{"no events folder", func(t *testing.T, root string) {}, false, "skipped"},
		{"empty events folder", func(t *testing.T, root string) {
			os.MkdirAll(filepath.Join(root, ".ekumen/events"), 0o755)
		}, true, "holds no events"},
		{"clean log", claim, false, `"ok": true`},
		{"lane violation", func(t *testing.T, root string) {
			claim(t, root)
			// A session approving a task: the lead's lane (C-event-log §3),
			// as it could arrive through a git merge.
			appendEvent(t, root, event.TaskApprovedType, event.Actor{Role: event.RoleSession, ID: "t-x"},
				event.Subject{Kind: event.KindTask, ID: "T-x"}, event.TaskApproved{TierGrants: []string{}, TextSHA256: strings.Repeat("0", 64)})
		}, true, `"rule": "lane"`},
		{"stale decision file", func(t *testing.T, root string) {
			claim(t, root)
			appendEvent(t, root, event.DecisionRaisedType, event.Actor{Role: event.RoleOrchestrator, ID: "main"},
				event.Subject{Kind: event.KindDecision, ID: "D-x"}, event.DecisionRaised{ID: "D-x", Title: "x", Tier: 2,
					RaisedBy: event.RaisedBy{Session: "main", On: "2026-11-20"}, Question: "?",
					Options: []event.Option{{ID: "a", Tradeoff: "a"}, {ID: "b", Tradeoff: "b"}},
					Blocks:  []string{}, ContractImpact: []string{}, Context: event.DecisionContext{Summary: "x"}})
			os.MkdirAll(filepath.Join(root, ".ekumen/decisions"), 0o755)
			os.WriteFile(filepath.Join(root, ".ekumen/decisions/D-x.yaml"), []byte("id: D-x\n"), 0o644)
		}, true, "does not match the events"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".ekumen"), 0o755); err != nil {
				t.Fatal(err)
			}
			c.setup(t, root)
			cmd := exec.Command("bash", script, root)
			cmd.Env = append(os.Environ(), "EKUMEN_BIN="+bin)
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
