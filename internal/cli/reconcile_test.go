package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/core"
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

// Issue #12 and review: an action that fails is rolled back and listed in
// skipped; the other actions still go ahead, the pass exits 5 for an
// internal failure, and the next pass finishes the work with no duplicate
// effects.
func TestReconcileFailedActionDoesNotBlockOthers_Issue12(t *testing.T) {
	ref := reconcileFixture(t)
	ref.must("--as", "loop:main", "reconcile")
	want := loopEvents(t, ref)
	if len(want) < 6 {
		t.Fatalf("fixture too small: %v", want)
	}
	for k := 0; k < 5; k++ {
		h := reconcileFixture(t)
		n := 0
		h.beforeAction = func(core.LoopAction) error {
			n++
			if n == k+1 {
				return errors.New("injected failure")
			}
			return nil
		}
		code, out, _ := h.run("--as", "loop:main", "reconcile", "--json")
		m := decodeJSON(t, out)
		sk := m["skipped"].([]any)
		if code != ExitInternal || m["ok"] != false || len(sk) != 1 || sk[0].(map[string]any)["code"] != CodeInternal ||
			!strings.Contains(sk[0].(map[string]any)["reason"].(string), "injected failure") {
			t.Fatalf("k=%d: exit %d %s", k, code, out)
		}
		h.beforeAction = nil
		h.tick()
		h.must("--as", "loop:main", "reconcile")
		if got := stripUnblocked(loopEvents(t, h)); !reflect.DeepEqual(got, stripUnblocked(want)) {
			t.Errorf("k=%d: after the second pass\n got %v\nwant %v", k, got, want)
		}
		if n := unblockedRecords(loopEvents(t, h), "T-gated"); n != 1 {
			t.Errorf("k=%d: %d applies record T-gated unblocked, want 1", k, n)
		}
		if code, out, _ := h.run("check", "--json"); code != 0 {
			t.Errorf("k=%d: check %s", k, out)
		}
	}
}

var unblockedRe = regexp.MustCompile(`"unblocked_tasks":\[[^]]*\]`)

// stripUnblocked hides unblocked_tasks: which apply removes a task's last
// gate depends on the order the applies were written in.
func stripUnblocked(evs []string) []string {
	out := []string{}
	for _, e := range evs {
		out = append(out, unblockedRe.ReplaceAllString(e, ""))
	}
	slices.Sort(out)
	return out
}

func unblockedRecords(evs []string, task string) int {
	n := 0
	for _, e := range evs {
		if strings.Contains(unblockedRe.FindString(e), `"`+task+`"`) {
			n++
		}
	}
	return n
}

// Review: a deterministic refusal of one action keeps its exit 2, does not
// block the other actions, and is reported again by the next pass.
func TestReconcileDeterministicRefusalIsReportedEachPass(t *testing.T) {
	h := reconcileFixture(t)
	h.beforeAction = func(a core.LoopAction) error {
		if a.Subject.ID == "D-gate" {
			return errf(CodeValidation, "refused for the test")
		}
		return nil
	}
	for pass := 0; pass < 2; pass++ {
		code, out, _ := h.run("--as", "loop:main", "reconcile", "--json")
		m := decodeJSON(t, out)
		sk := m["skipped"].([]any)
		if code != ExitRefused || len(sk) != 1 || sk[0].(map[string]any)["subject"].(map[string]any)["id"] != "D-gate" ||
			sk[0].(map[string]any)["code"] != CodeValidation {
			t.Fatalf("pass %d: exit %d %s", pass, code, out)
		}
		if pass == 0 && len(m["actions"].([]any)) < 4 {
			t.Errorf("the refusal blocked other actions: %v", m["actions"])
		}
		h.tick()
	}
	if d := decodeJSON(t, h.must("decision", "show", "D-gate-two", "--json"))["decision"].(map[string]any); d["applied_on"] == nil {
		t.Error("D-gate-two not applied")
	}
}

// Review (concurrency): when another writer applies a decision between
// this pass's load and its write, the pass reads the log again, drops the
// apply as held, and no second §B amendment is written.
func TestReconcileRechecksBeforeApply_Issue12(t *testing.T) {
	h := reconcileFixture(t)
	h.beforeAction = func(a core.LoopAction) error {
		if a.Kind == core.LoopApply && a.Subject.ID == "D-gate" {
			if code, out, errOut := h.run("--as", "loop:other", "decision", "apply", "D-gate"); code != 0 {
				t.Fatalf("concurrent apply: %s %s", out, errOut)
			}
		}
		return nil
	}
	m := decodeJSON(t, h.must("--as", "loop:main", "reconcile", "--json"))
	held := false
	for _, x := range m["held"].([]any) {
		x := x.(map[string]any)
		if x["subject"].(map[string]any)["id"] == "D-gate" && x["reason"] == heldByOther {
			held = true
		}
	}
	if !held {
		t.Errorf("held %v", m["held"])
	}
	n := 0
	for _, e := range loopEvents(t, h) {
		if strings.HasPrefix(e, "TaskAmended T-gated ") && strings.Contains(e, `"by_decision":"D-gate"`) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d amendments for D-gate, want 1", n)
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

	for _, obs := range []observe.Observer{&countingObserver{err: observe.ErrTruncated}, observe.None{}} {
		h2 := reconcileFixture(t)
		h2.prs = obs
		code, out, _ := h2.run("--as", "loop:main", "reconcile", "--json")
		m = decodeJSON(t, out)
		var kinds []string
		for _, a := range m["actions"].([]any) {
			kinds = append(kinds, a.(map[string]any)["kind"].(string))
		}
		if code != 0 || m["observed"].(map[string]any)["prs"] == "ok" || m["ready"] != nil || m["not_startable"] != nil {
			t.Errorf("%T: exit %d %v ready %v", obs, code, m["observed"], m["ready"])
		}
		if slices.Contains(kinds, "apply") || !slices.Contains(kinds, "default") || !slices.Contains(kinds, "route") || len(m["held"].([]any)) != 3 {
			t.Errorf("%T: actions %v held %v", obs, kinds, m["held"])
		}
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
	if held := m["held"].([]any); len(held) != 1 || held[0].(map[string]any)["subject"].(map[string]any)["id"] != "D-gate" {
		t.Errorf("contested D-gate not held: %v", held)
	}
	inbox := h.must("inbox", "--for", "lead", "--json")
	if !strings.Contains(inbox, `"id": "`+id+`"`) {
		t.Errorf("%s not in the lead's inbox: %s", id, inbox)
	}
}

func readyIDs(m map[string]any) []string {
	out := []string{}
	for _, r := range m["ready"].([]any) {
		out = append(out, r.(map[string]any)["id"].(string))
	}
	return out
}

func heldReasons(m map[string]any) map[string]string {
	out := map[string]string{}
	for _, x := range m["held"].([]any) {
		x := x.(map[string]any)
		out[x["subject"].(map[string]any)["id"].(string)] = x["reason"].(string)
	}
	return out
}

// Review: a pass reports what it did, not what it planned. Ready comes
// from the log as written and unblocked from the DecisionApplied events
// written, so a failed or held apply never shows its task as ready or
// unblocked.
func TestReconcileReportsTheLogAsWritten(t *testing.T) {
	cases := []struct {
		name   string
		hook   func(h *harness) func(core.LoopAction) error
		code   int
		held   map[string]string
		gated  string
		unblkd int
	}{
		{"every apply fails", func(*harness) func(core.LoopAction) error {
			return func(a core.LoopAction) error {
				if a.Kind == core.LoopApply {
					return errors.New("injected failure")
				}
				return nil
			}
		}, ExitInternal, map[string]string{}, "blocked", 0},
		{"default fails, its apply is held", func(*harness) func(core.LoopAction) error {
			return func(a core.LoopAction) error {
				if a.Kind == core.LoopDefault {
					return errors.New("injected failure")
				}
				return nil
			}
		}, ExitInternal, map[string]string{"D-late-default": heldNotDecided}, "blocked", 0},
		{"contested before its apply", func(h *harness) func(core.LoopAction) error {
			return func(a core.LoopAction) error {
				if a.Kind == core.LoopApply && a.Subject.ID == "D-gate" {
					h.raw(event.DecisionDecidedType, event.RoleLead, "yash", event.KindDecision, "D-gate", event.DecisionDecided{Answer: "stop"})
				}
				return nil
			}
		}, ExitOK, map[string]string{"D-gate": core.HeldContested}, "blocked", 0},
		{"applied by another writer", func(h *harness) func(core.LoopAction) error {
			return func(a core.LoopAction) error {
				if a.Kind == core.LoopApply && a.Subject.ID == "D-gate" {
					h.run("--as", "loop:other", "decision", "apply", "D-gate")
				}
				return nil
			}
		}, ExitOK, map[string]string{"D-gate": heldByOther}, "ready", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := reconcileFixture(t)
			h.beforeAction = c.hook(h)
			code, out, _ := h.run("--as", "loop:main", "reconcile", "--json")
			h.beforeAction = nil
			h.tick()
			m := decodeJSON(t, out)
			if code != c.code {
				t.Fatalf("exit %d, want %d: %s", code, c.code, out)
			}
			if got := heldReasons(m); !reflect.DeepEqual(got, c.held) {
				t.Errorf("held %v, want %v", got, c.held)
			}
			st := taskStatus(t, h, "T-gated")
			if st != c.gated {
				t.Errorf("T-gated is %s, want %s", st, c.gated)
			}
			if slices.Contains(readyIDs(m), "T-gated") != (st == "ready") {
				t.Errorf("ready %v but T-gated is %s", readyIDs(m), st)
			}
			if u := m["unblocked"].([]any); len(u) != c.unblkd {
				t.Errorf("unblocked %v", u)
			}
		})
	}
}

// C-cli-json §1 (review): a write whose events were recorded but whose
// projections could not be written exits 5 with error.details {events,
// paths}.
func TestReconcileProjectionFailureDetails_CLIJSON1(t *testing.T) {
	h := reconcileFixture(t)
	reg := filepath.Join(h.root, ".ekumen", "registry.yaml")
	if err := os.Remove(reg); err != nil {
		t.Fatal(err)
	}
	h.write(".ekumen/registry.yaml/keep", "x")
	code, out, _ := h.run("--as", "loop:main", "reconcile", "--json")
	if code != ExitInternal {
		t.Fatalf("exit %d: %s", code, out)
	}
	e := decodeJSON(t, out)["error"].(map[string]any)
	d, ok := e["details"].(map[string]any)
	if !ok || e["code"] != CodeInternal || len(d["events"].([]any)) == 0 || len(d["paths"].([]any)) == 0 {
		t.Fatalf("error %v", e)
	}
	for _, id := range d["events"].([]any) {
		if !strings.Contains(e["message"].(string), id.(string)) {
			t.Errorf("message does not name %s", id)
		}
	}
}

// Brief T-reconcile job 4: the orchestrator's brief action names the
// brief's path, and goes away once the brief exists.
func TestOrchestratorBriefActionDisappears_InboxGaps(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	brief := func() string {
		for _, it := range decodeJSON(t, h.must("inbox", "--for", "orchestrator", "--json"))["items"].([]any) {
			if it := it.(map[string]any); it["action"] == "brief" && it["id"] == "T-skills" {
				return it["command"].(string)
			}
		}
		return ""
	}
	if c := brief(); c != "ekumen brief new T-skills --existing" {
		t.Fatalf("command %q", c)
	}
	h.write(".ekumen/briefs/T-skills/brief.md", "# T-skills\n")
	if c := brief(); c != "" {
		t.Errorf("brief action after the brief exists: %q", c)
	}
}
