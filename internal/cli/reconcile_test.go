package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/observe"
)

// countingObserver counts PullRequests calls.
type countingObserver struct {
	n   int
	prs observe.Static
	err error
}

func (c *countingObserver) PullRequests(ctx context.Context) ([]observe.PR, error) {
	c.n++
	prs, _ := c.prs.PullRequests(ctx)
	return prs, c.err
}

// reconcileFixture is gapsFixture (two decided, unapplied decisions gating
// T-gated, a handoff with follow-ups, an open question) plus a tier-1
// decision past its deadline that gates T-gated too.
func reconcileFixture(t *testing.T) *harness {
	h := gapsFixture(t)
	h.write("d-late.yaml", strings.NewReplacer("D-yaml-library", "D-late-default", "deadline: 2026-11-21", "deadline: 2026-11-19",
		"blocks: [T-cli]", "blocks: [T-gated]").Replace(tierOneDecision))
	h.must("--as", "orchestrator:main", "decision", "raise", "--file", "d-late.yaml")
	return h
}

// tree reads every file under .ekumen/.
func tree(t *testing.T, h *harness) map[string]string {
	t.Helper()
	out := map[string]string{}
	root := filepath.Join(h.root, ".ekumen")
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		out[strings.TrimPrefix(p, root)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// loopEvents lists the log's events written by the loop, as type, subject
// and data, without ids or file names.
func loopEvents(t *testing.T, h *harness) []string {
	t.Helper()
	var out []string
	for p, body := range tree(t, h) {
		if !strings.HasPrefix(p, "/events/") {
			continue
		}
		e, err := event.Decode([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if e.Actor.Role != event.RoleLoop {
			continue
		}
		var data map[string]any
		json.Unmarshal(e.Data, &data)
		delete(data, "file")
		b, _ := json.Marshal(data)
		out = append(out, string(e.Type)+" "+e.Subject.ID+" "+string(b))
	}
	slices.Sort(out)
	return out
}

// D-control-loop: golden --json of a dry run and of the pass, on a log
// with a tier-1 decision past its deadline, two decided decisions, a
// handoff with follow-ups and an open question.
func TestReconcileGolden_DControlLoop(t *testing.T) {
	h := reconcileFixture(t)
	golden(t, "reconcile-dry-run.json", h.must("reconcile", "--dry-run", "--json"))
	out := h.must("--as", "loop:main", "reconcile", "--json")
	golden(t, "reconcile.json", out)
	m := decodeJSON(t, out)
	if st := taskStatus(t, h, "T-gated"); st != "ready" {
		t.Errorf("T-gated %s after the pass", st)
	}
	if u := m["unblocked"].([]any); len(u) != 1 || u[0] != "T-gated" {
		t.Errorf("unblocked %v", u)
	}
	if code, out, _ := h.run("check", "--json"); code != 0 {
		t.Errorf("check after the pass: %s", out)
	}
}

// D-control-loop: --dry-run writes nothing (the .ekumen/ tree is byte for
// byte the same) and needs no actor.
func TestReconcileDryRunWritesNothing_DControlLoop(t *testing.T) {
	h := reconcileFixture(t)
	before := tree(t, h)
	m := decodeJSON(t, h.must("reconcile", "--dry-run", "--json"))
	if m["dry_run"] != true || len(m["actions"].([]any)) == 0 || len(m["planned"].([]any)) != len(m["events"].([]any)) {
		t.Fatalf("dry run %v", m)
	}
	if after := tree(t, h); !reflect.DeepEqual(before, after) {
		t.Error("--dry-run changed .ekumen/")
	}
}

// D-control-loop: a pass is closed; a second pass right after records
// nothing and a dry run plans nothing.
func TestReconcileSecondPassRecordsNothing_DControlLoop(t *testing.T) {
	h := reconcileFixture(t)
	h.must("--as", "loop:main", "reconcile")
	before := tree(t, h)
	m := decodeJSON(t, h.must("--as", "loop:main", "reconcile", "--json"))
	if len(m["actions"].([]any)) != 0 || len(m["events"].([]any)) != 0 || len(m["paths"].([]any)) != 0 {
		t.Errorf("second pass %v", m)
	}
	if after := tree(t, h); !reflect.DeepEqual(before, after) {
		t.Error("the second pass changed .ekumen/")
	}
	if m := decodeJSON(t, h.must("reconcile", "--dry-run", "--json")); len(m["actions"].([]any)) != 0 {
		t.Errorf("dry run after a pass plans %v", m["actions"])
	}
}

// Issue #12: a pass interrupted after k actions exits 5 naming what it
// recorded; the next pass finishes the work with no duplicate effects.
func TestReconcileInterruptedPassIsCompleted_Issue12(t *testing.T) {
	ref := reconcileFixture(t)
	ref.must("--as", "loop:main", "reconcile")
	want := loopEvents(t, ref)
	if len(want) < 6 {
		t.Fatalf("fixture too small: %v", want)
	}
	for k := 0; k < 5; k++ {
		h := reconcileFixture(t)
		h.beforeAction = func(i int) error {
			if i == k {
				return errors.New("injected failure")
			}
			return nil
		}
		code, out, _ := h.run("--as", "loop:main", "reconcile", "--json")
		if code != ExitInternal || !strings.Contains(out, "injected failure") || !strings.Contains(out, "events recorded before it") {
			t.Fatalf("k=%d: exit %d %s", k, code, out)
		}
		h.beforeAction = nil
		h.tick()
		h.must("--as", "loop:main", "reconcile")
		if got := loopEvents(t, h); !reflect.DeepEqual(got, want) {
			t.Errorf("k=%d: after the second pass\n got %v\nwant %v", k, got, want)
		}
		if code, out, _ := h.run("check", "--json"); code != 0 {
			t.Errorf("k=%d: check %s", k, out)
		}
	}
}

// C-event-log §3: reconcile writes in the loop's lane only; any other role
// is refused and nothing is written. A real pass needs an actor.
func TestReconcileRefusesNonLoop_EventLog3(t *testing.T) {
	h := reconcileFixture(t)
	before := tree(t, h)
	for _, as := range []string{"orchestrator:main", "lead:yash", "session:t-gated"} {
		code, out, _ := h.run("--as", as, "reconcile", "--json")
		if code != ExitRefused || !strings.Contains(out, `"lane"`) {
			t.Errorf("%s: exit %d %s", as, code, out)
		}
		if code, _, _ := h.run("--as", as, "reconcile", "--dry-run"); code != ExitRefused {
			t.Errorf("%s --dry-run: exit %d", as, code)
		}
	}
	if code, _, _ := h.run("reconcile"); code != ExitUsage {
		t.Errorf("no actor: exit %d", code)
	}
	if after := tree(t, h); !reflect.DeepEqual(before, after) {
		t.Error("a refused reconcile changed .ekumen/")
	}
}

// Issue #15: a pass observes pull requests once, however many actions it
// writes, and a truncated observation is reported with ready unknown.
func TestReconcileObservesOnce_Issue15(t *testing.T) {
	h := reconcileFixture(t)
	c := &countingObserver{}
	h.prs = c
	m := decodeJSON(t, h.must("--as", "loop:main", "reconcile", "--json"))
	if c.n != 1 || len(m["actions"].([]any)) < 3 {
		t.Errorf("gh called %d times for %d actions", c.n, len(m["actions"].([]any)))
	}
	if m["observed"].(map[string]any)["prs"] != "ok" || m["ready"] == nil {
		t.Errorf("observed %v ready %v", m["observed"], m["ready"])
	}

	h2 := reconcileFixture(t)
	h2.prs = &countingObserver{err: observe.ErrTruncated}
	code, out, errOut := h2.run("--as", "loop:main", "reconcile", "--json")
	m = decodeJSON(t, out)
	if code != 0 || m["observed"].(map[string]any)["prs"] != "truncated" || m["ready"] != nil || !strings.Contains(errOut, "may be incomplete") {
		t.Errorf("truncated: exit %d %v ready %v stderr %q", code, m["observed"], m["ready"], errOut)
	}
	if len(m["actions"].([]any)) == 0 {
		t.Error("a truncated observation stopped the pass")
	}
}

// D-apply-before-loop: the loop's applies record what `decision apply`
// records (same events, same amendment text), so the two share one
// implementation.
func TestReconcileApplyMatchesDecisionApply_DApplyBeforeLoop(t *testing.T) {
	cli := gapsFixture(t)
	cli.must("--as", "loop:main", "decision", "apply", "D-gate")
	cli.must("--as", "loop:main", "decision", "apply", "D-gate-two")
	loop := gapsFixture(t)
	loop.must("--as", "loop:main", "reconcile")
	applies := func(h *harness) []string {
		var out []string
		for _, e := range loopEvents(t, h) {
			if strings.HasPrefix(e, "TaskAmended ") || strings.HasPrefix(e, "DecisionApplied ") {
				out = append(out, e)
			}
		}
		return out
	}
	if a, b := applies(cli), applies(loop); len(a) != 5 || !reflect.DeepEqual(a, b) {
		t.Errorf("decision apply wrote\n%v\nreconcile wrote\n%v", a, b)
	}
	amendments := func(h *harness) []string {
		var out []string
		for p, body := range tree(t, h) {
			if strings.Contains(p, "/amendments/") {
				out = append(out, body)
			}
		}
		slices.Sort(out)
		return out
	}
	if a, b := amendments(cli), amendments(loop); !reflect.DeepEqual(a, b) {
		t.Errorf("amendment files differ\n%v\n%v", a, b)
	}
}

// C-event-log §5: a conflict found by the pass becomes a tier-2 decision in
// the lead's inbox plus ConflictDetected; the pass still exits 0 and
// `check` no longer reports it.
func TestReconcileRecordsConflict_EventLog5(t *testing.T) {
	h := reconcileFixture(t)
	h.raw(event.DecisionDecidedType, event.RoleLead, "yash", event.KindDecision, "D-gate", event.DecisionDecided{Answer: "stop"})
	if code, _, _ := h.run("check"); code != ExitConflict {
		t.Fatalf("check before the pass: exit %d", code)
	}
	m := decodeJSON(t, h.must("--as", "loop:main", "reconcile", "--json"))
	var id string
	for _, a := range m["actions"].([]any) {
		a := a.(map[string]any)
		if a["kind"] == "conflict" {
			id = a["subject"].(map[string]any)["id"].(string)
			if n := len(a["events"].([]any)); n != 2 {
				t.Errorf("conflict action wrote %d events", n)
			}
		}
	}
	if !strings.HasPrefix(id, "D-conflict-decided-twice-") {
		t.Fatalf("no conflict action: %v", m["actions"])
	}
	if code, out, _ := h.run("check", "--json"); code != 0 {
		t.Errorf("check after the pass: %s", out)
	}
	inbox := h.must("inbox", "--for", "lead", "--json")
	if !strings.Contains(inbox, `"id": "`+id+`"`) {
		t.Errorf("%s not in the lead's inbox: %s", id, inbox)
	}
}
