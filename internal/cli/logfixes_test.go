package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Tests for T-log-fixes, one per decision (or audit finding) it implements.

func mustExit(t *testing.T, h *harness, want int, args ...string) string {
	t.Helper()
	code, out, errOut := h.run(append(args, "--json")...)
	if code != want {
		t.Fatalf("ekumen %s: exit %d, want %d\nstdout: %s\nstderr: %s", strings.Join(args, " "), code, want, out, errOut)
	}
	h.tick()
	return out
}

func checkJSON(t *testing.T, h *harness) (int, map[string]any) {
	t.Helper()
	code, out, _ := h.run("check", "--json")
	return code, decodeJSON(t, out)
}

func rulesOf(m map[string]any) []string {
	var out []string
	for _, c := range m["conflicts"].([]any) {
		out = append(out, c.(map[string]any)["rule"].(string))
	}
	return out
}

// D-imported-event-time-order: on a log whose decide is stamped later than
// "now" (as the import's noon stamps were), `decision apply` takes effect
// and check stays clean.
func TestApplyBeforeDecideStamp_DImportedEventTimeOrder(t *testing.T) {
	h := gapsFixture(t)
	h.write(".ekumen/briefs/T-late/brief.md", "# T-late\n")
	h.write("t-late.yaml", "id: T-late\ntitle: Late\nkind: build\nbranch: build/t-late\nworktree: .worktrees/t-late\n")
	h.write("d-late.yaml", strings.NewReplacer("D-gate", "D-late", "T-gated", "T-late").Replace(gateDecision))
	h.must("--as", "orchestrator:main", "task", "draft", "--file", "t-late.yaml")
	h.must("--as", "lead:yash", "task", "approve", "T-late")
	h.must("--as", "orchestrator:main", "decision", "raise", "--file", "d-late.yaml")
	// The lead's answer, imported with a noon stamp three hours ahead.
	now := h.now
	h.now = now.Add(3 * time.Hour)
	h.raw(event.DecisionDecidedType, event.RoleLead, "yash", event.KindDecision, "D-late", event.DecisionDecided{Answer: "go"})
	h.now = now
	if st := taskStatus(t, h, "T-late"); st != "blocked" {
		t.Fatalf("T-late %s, want blocked", st)
	}
	h.must("--as", "loop:main", "decision", "apply", "D-late")
	_, out, _ := h.run("decision", "show", "D-late", "--json")
	if d := decodeJSON(t, out)["decision"].(map[string]any); d["applied_on"] == nil {
		t.Fatalf("apply did not take effect: %v", d)
	}
	if st := taskStatus(t, h, "T-late"); st != "ready" {
		t.Errorf("T-late %s after apply, want ready", st)
	}
	if code, m := checkJSON(t, h); code != 0 {
		t.Errorf("check: %v", m)
	}
}

// D-imported-event-time-order: an apply with no decide is reported as
// orphan-step by check (exit 4), not silently ignored.
func TestOrphanStepInCheck_DImportedEventTimeOrder(t *testing.T) {
	h := gapsFixture(t)
	h.raw(event.DecisionAppliedType, event.RoleLoop, "loop", event.KindDecision, "D-claim-lane", event.DecisionApplied{})
	code, m := checkJSON(t, h)
	if code != ExitConflict || !strings.Contains(strings.Join(rulesOf(m), ","), "orphan-step") {
		t.Errorf("check exit %d, conflicts %v; want orphan-step", code, rulesOf(m))
	}
}

// D-apply-amend-lane (loop-lane): `decision apply` as the loop needs no
// orchestrator claim and writes the amendments as the loop, with
// by_decision.
func TestDecisionApplyAsLoop_DApplyAmendLane(t *testing.T) {
	h := gapsFixture(t)
	h.must("--as", "orchestrator:main", "release", "orchestrator")
	h.must("--as", "loop:loop", "decision", "apply", "D-gate")
	files, _ := filepath.Glob(filepath.Join(h.root, ".ekumen/events/task/T-gated/*.TaskAmended.json"))
	if len(files) != 1 {
		t.Fatalf("%d TaskAmended events", len(files))
	}
	b, _ := os.ReadFile(files[0])
	var e struct {
		Actor event.Actor       `json:"actor"`
		Data  event.TaskAmended `json:"data"`
	}
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	if e.Actor.Role != event.RoleLoop || e.Data.ByDecision != "D-gate" {
		t.Errorf("amendment by %v with by_decision %q", e.Actor, e.Data.ByDecision)
	}
	if code, m := checkJSON(t, h); code != 0 {
		t.Errorf("check: %v", m)
	}
	// The orchestrator still writes other amendments; the loop does not.
	h.write("plain.md", "A plain amendment.\n")
	out := mustExit(t, h, ExitRefused, "--as", "loop:loop", "task", "amend", "T-gated", "--text-file", "plain.md")
	if !strings.Contains(out, `"lane"`) {
		t.Errorf("loop amend: %s", out)
	}
}

// D-projections-in-git (per-item-only): `project --check` passes on fresh
// projections, fails on a stale, missing or stray decision file, and every
// command works without registry.yaml and state.json (a fresh clone).
func TestProjectCheck_DProjectionsInGit(t *testing.T) {
	h := gapsFixture(t)
	mustExit(t, h, 0, "project", "--check")
	ek := filepath.Join(h.root, ".ekumen")
	for _, f := range []string{"registry.yaml", "state.json"} {
		if err := os.Remove(filepath.Join(ek, f)); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"status"}, {"inbox", "--for", "lead"}, {"task", "list"}, {"graph"}, {"check"}, {"project", "--check"}} {
		if code, out, errOut := h.run(append(args, "--json")...); code != 0 {
			t.Errorf("%v on a fresh clone: exit %d %s %s", args, code, out, errOut)
		}
	}
	if _, out, _ := h.run("status", "--json"); decodeJSON(t, out)["project"] != "demo" {
		t.Errorf("project name: %s", out)
	}
	h.must("--as", "loop:main", "decision", "apply", "D-gate") // a write regenerates them
	for _, f := range []string{"registry.yaml", "state.json"} {
		if _, err := os.Stat(filepath.Join(ek, f)); err != nil {
			t.Errorf("%s not regenerated: %v", f, err)
		}
	}
	// Stale, missing and stray per-item files.
	h.write(".ekumen/decisions/D-gate.yaml", "id: D-gate\n")
	os.Remove(filepath.Join(ek, "decisions/D-gate-two.yaml"))
	h.write(".ekumen/decisions/D-stray.yaml", "id: D-stray\n")
	out := mustExit(t, h, ExitRefused, "project", "--check")
	for _, want := range []string{"decisions/D-gate.yaml", "decisions/D-gate-two.yaml", "decisions/D-stray.yaml"} {
		if !strings.Contains(out, want) {
			t.Errorf("project --check does not report %s:\n%s", want, out)
		}
	}
	h.must("project")
	os.Remove(filepath.Join(ek, "decisions/D-stray.yaml"))
	mustExit(t, h, 0, "project", "--check")
	// The project name comes from the git remote when there is one.
	h.git = GitInfo{Remote: "git@github.com:o/ekumen.git"}
	if _, out, _ := h.run("status", "--json"); decodeJSON(t, out)["project"] != "ekumen" {
		t.Errorf("project name from the remote: %s", out)
	}
}

// D-contract-version-recording (versioned-command): `contract version`
// records a decided change to a frozen contract; `contract freeze` then
// freezes it; frozen-text-changed compares with the latest frozen version.
func TestContractVersion_DContractVersionRecording(t *testing.T) {
	h := gapsFixture(t)
	h.write("d-log.yaml", strings.NewReplacer("D-gate", "D-log-change", "blocks: [T-gated]", "blocks: []",
		"contract_impact: []", "contract_impact: [C-event-log]").Replace(gateDecision))
	h.must("--as", "orchestrator:main", "decision", "raise", "--file", "d-log.yaml")
	h.must("--as", "lead:yash", "decision", "answer", "D-log-change", "go")
	v4 := "---\nid: C-event-log\nversion: 4\n---\n# Event log\n\nNew rules.\n"
	h.write(".ekumen/contracts/C-event-log.md", v4)
	if code, m := checkJSON(t, h); code != ExitConflict || rulesOf(m)[0] != "frozen-text-changed" {
		t.Fatalf("edited frozen contract: exit %d %v", code, m)
	}
	refusals := []struct {
		name string
		code int
		args []string
	}{
		{"decision without the contract", ExitRefused, []string{"--as", "orchestrator:main", "contract", "version", "C1", "--by", "D-gate"}},
		{"pending decision", ExitRefused, []string{"--as", "orchestrator:main", "contract", "version", "C1", "--by", "D-claim-lane"}},
		{"session", ExitRefused, []string{"--as", "session:t-cli", "contract", "version", "C1", "--by", "D-log-change"}},
		{"no --by", ExitUsage, []string{"--as", "orchestrator:main", "contract", "version", "C1"}},
		{"draft contract", ExitRefused, []string{"--as", "orchestrator:main", "contract", "version", "C-cli-json", "--by", "D-log-change"}},
		{"freeze before version", ExitRefused, []string{"--as", "lead:yash", "contract", "freeze", "C-event-log"}},
	}
	for _, r := range refusals {
		t.Run(r.name, func(t *testing.T) { mustExit(t, h, r.code, r.args...) })
	}
	out := mustExit(t, h, 0, "--as", "orchestrator:main", "contract", "version", "C1", "--by", "D-log-change")
	if len(decodeJSON(t, out)["events"].([]any)) != 1 {
		t.Fatalf("version: %s", out)
	}
	if out := mustExit(t, h, 0, "--as", "orchestrator:main", "contract", "version", "C1", "--by", "D-log-change"); len(decodeJSON(t, out)["events"].([]any)) != 0 {
		t.Errorf("repeat recorded again: %s", out)
	}
	if code, m := checkJSON(t, h); code != 0 {
		t.Errorf("check with a recorded, unfrozen version: %v", m)
	}
	_, out, _ = h.run("inbox", "--for", "lead", "--json")
	if !strings.Contains(out, `"command": "ekumen contract freeze C-event-log"`) {
		t.Errorf("lead inbox lacks the freeze action:\n%s", out)
	}
	h.must("--as", "lead:yash", "contract", "freeze", "C-event-log")
	_, out, _ = h.run("status", "--json")
	if code, m := checkJSON(t, h); code != 0 {
		t.Errorf("check after freeze: %v", m)
	}
	// A version must be higher than the frozen one.
	h.write(".ekumen/contracts/C-event-log.md", v4+"More.\n")
	mustExit(t, h, ExitRefused, "--as", "orchestrator:main", "contract", "version", "C1", "--by", "D-log-change")
}

// D-post-switch-hand-edits (prose-carve-out): `spine approve` records
// SpineApproved; check reports spine-unapproved when spine.md changes.
func TestSpineApprove_DPostSwitchHandEdits(t *testing.T) {
	h := gapsFixture(t)
	h.write(".ekumen/spine.md", "# Spine\n")
	if code, m := checkJSON(t, h); code != 0 {
		t.Fatalf("check before any approval: %v", m)
	}
	mustExit(t, h, ExitRefused, "--as", "orchestrator:main", "spine", "approve")
	mustExit(t, h, ExitRefused, "--as", "session:t-cli", "spine", "approve")
	mustExit(t, h, 0, "--as", "lead:yash", "spine", "approve")
	if out := mustExit(t, h, 0, "--as", "lead:yash", "spine", "approve"); len(decodeJSON(t, out)["events"].([]any)) != 0 {
		t.Errorf("approving the same text again recorded: %s", out)
	}
	if code, m := checkJSON(t, h); code != 0 {
		t.Errorf("check after approval: %v", m)
	}
	h.write(".ekumen/spine.md", "# Spine\n\nAn unapproved edit.\n")
	if code, m := checkJSON(t, h); code != ExitConflict || !strings.Contains(strings.Join(rulesOf(m), ","), "spine-unapproved") {
		t.Errorf("unapproved edit: exit %d %v", code, rulesOf(m))
	}
	mustExit(t, h, 0, "--as", "lead:yash", "spine", "approve")
	if code, m := checkJSON(t, h); code != 0 {
		t.Errorf("check after re-approval: %v", m)
	}
}

// T-audit F-2026-11-20-3: check reports an amendment or handoff file that
// no event records (orphan-entry).
func TestOrphanEntry_AuditF3(t *testing.T) {
	h := gapsFixture(t)
	if code, m := checkJSON(t, h); code != 0 {
		t.Fatalf("check: %v", m)
	}
	h.write(".ekumen/briefs/T-cli/handoffs/20261120T071348Z-t-audit.md", "---\ntask: T-cli\n---\n\n## STATUS\n\nBy hand.\n")
	code, m := checkJSON(t, h)
	inv := m["invalid"].([]any)
	if code != ExitRefused || len(inv) != 1 || !strings.HasPrefix(inv[0].(map[string]any)["error"].(string), "orphan-entry") ||
		inv[0].(map[string]any)["path"] != ".ekumen/briefs/T-cli/handoffs/20261120T071348Z-t-audit.md" {
		t.Errorf("check exit %d, invalid %v", code, inv)
	}
}

// T-audit F-2026-11-20-2: `inbox --for lead` lists kind "action" items
// (approve, freeze, apply) after decisions.
func TestInboxLeadActions_AuditF2(t *testing.T) {
	h := gapsFixture(t)
	h.write(".ekumen/briefs/T-next/brief.md", "# T-next\n")
	h.write("t-next.yaml", "id: T-next\ntitle: Next\nkind: build\n")
	h.must("--as", "orchestrator:main", "task", "draft", "--file", "t-next.yaml")
	h.now = h.now.Add(25 * time.Hour) // D-gate and D-gate-two are a day old
	_, out, _ := h.run("inbox", "--for", "lead", "--json")
	golden(t, "inbox-lead-actions.json", out)
	items := decodeJSON(t, out)["items"].([]any)
	seenAction := false
	for _, it := range items {
		kind := it.(map[string]any)["kind"]
		if kind == "action" {
			seenAction = true
		} else if kind == "decision" && seenAction {
			t.Error("a decision ranks after an action")
		}
	}
}

var dataRe = regexp.MustCompile(`const DATA = (\{.*\});`)

// mapData returns the DATA object embedded in a map page.
func mapData(t *testing.T, page string) map[string]any {
	t.Helper()
	m := dataRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no DATA in the page")
	}
	return decodeJSON(t, strings.ReplaceAll(m[1], `<\/`, `</`))
}

// T-log-fixes job 7: `map --fragment` has no document wrappers.
func TestMapFragment_TLogFixes7(t *testing.T) {
	h := gapsFixture(t)
	h.must("map", "--out", "frag.html", "--fragment")
	b, err := os.ReadFile(filepath.Join(h.root, "frag.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	for _, bad := range []string{"<!doctype", "<html", "<head>", "</head>", "<body", "</body>", "</html>"} {
		if strings.Contains(strings.ToLower(page), bad) {
			t.Errorf("fragment contains %s", bad)
		}
	}
	for _, want := range []string{"<style>", `<div class="ekumen-map">`, `id="graph"`, "Chris Pettitt"} {
		if !strings.Contains(page, want) {
			t.Errorf("fragment lacks %s", want)
		}
	}
	mapData(t, page)
}

// T-log-fixes job 8 (amendment): map links to source files, from the
// origin remote or --link-base; plain text with neither.
func TestMapLinks_TLogFixes8(t *testing.T) {
	h := gapsFixture(t)
	h.write("h2.md", "## STATUS\n\nStarted.\n")
	h.must("--as", "session:t-gated", "task", "handoff", "T-gated", "--text-file", "h2.md")
	links := func(args ...string) string {
		t.Helper()
		h.must(append([]string{"map", "--out", "map.html"}, args...)...)
		b, err := os.ReadFile(filepath.Join(h.root, "map.html"))
		if err != nil {
			t.Fatal(err)
		}
		out, _ := json.MarshalIndent(mapData(t, string(b))["links"], "", "  ")
		return string(out) + "\n"
	}
	golden(t, "map-links-no-remote.json", links())
	h.git = GitInfo{Remote: "git@github.com:o/r.git", DefaultBranch: "trunk"}
	golden(t, "map-links-github.json", links())
	h.git = GitInfo{Remote: "https://git.example.com/o/r.git"}
	if got := links(); got != "{}\n" {
		t.Errorf("unknown host gave links: %s", got)
	}
	got := links("--link-base", "https://git.example.com/o/r/src/branch/main/")
	if !strings.Contains(got, `"https://git.example.com/o/r/src/branch/main/.ekumen/decisions/D-gate.yaml"`) {
		t.Errorf("--link-base not used: %s", got)
	}
}
