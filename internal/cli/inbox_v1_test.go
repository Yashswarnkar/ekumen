package cli

import (
	"strings"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/observe"
)

// inboxV1Fixture is a small project shaped like the problems T-inbox-v1
// fixes (GitHub #37, #38, #39, #63):
//
//   - D-wide touches C-api (contract_impact) and is named only in
//     T-wait's blocked_by. T-done waits on C-api but its PR merged, so
//     D-wide really holds up T-wait and, through it, T-next.
//   - T-stack's PR is on build/t-stack-2 (stack rule); T-titled has no
//     branch and a PR titled "T-titled: …" (title rule); a PR titled
//     "T-titled brief: …" links to nothing.
//   - D-on-branch is raised only on build/t-wait, in open PR #12.
func inboxV1Fixture(h *harness) {
	t := h.t
	t.Helper()
	h.write(".ekumen/contracts/C-api.md", "---\nid: C-api\n---\n# API\n")
	for _, id := range []string{"T-done", "T-wait", "T-next", "T-stack", "T-titled"} {
		h.write(".ekumen/briefs/"+id+"/brief.md", "# "+id+"\n")
	}
	h.write("t-done.yaml", "id: T-done\ntitle: Already merged\nkind: build\nbranch: build/t-done\nworktree: .worktrees/t-done\nblocked_by: [C-api]\n")
	h.write("t-wait.yaml", "id: T-wait\ntitle: Waits on the decision\nkind: build\nbranch: build/t-wait\nworktree: .worktrees/t-wait\nblocked_by: [D-wide]\n")
	h.write("t-next.yaml", "id: T-next\ntitle: Comes after T-wait\nkind: build\nblocked_by: [T-wait]\n")
	h.write("t-stack.yaml", "id: T-stack\ntitle: Stacked branches\nkind: build\nbranch: build/t-stack\nworktree: .worktrees/t-stack\n")
	h.write("t-titled.yaml", "id: T-titled\ntitle: No branch recorded\nkind: build\n")
	h.write("d-wide.yaml", `id: D-wide
title: How writes stay all-or-nothing
tier: 2
question: Which protection do we adopt?
options:
  - {id: lock, tradeoff: small}
  - {id: journal, tradeoff: big}
recommendation: lock
blocks: []
contract_impact: [C-api]
context:
  summary: Touches the API contract.
`)
	h.raw(event.ContractDraftedType, event.RoleOrchestrator, "main", event.KindContract, "C-api",
		event.ContractDrafted{Title: "API", Path: "contracts/C-api.md"})
	h.must("--as", "orchestrator:main", "claim", "orchestrator")
	h.must("--as", "lead:yash", "contract", "freeze", "C-api")
	for _, f := range []string{"t-done.yaml", "t-wait.yaml", "t-next.yaml", "t-stack.yaml", "t-titled.yaml"} {
		h.must("--as", "orchestrator:main", "task", "draft", "--file", f)
	}
	for _, id := range []string{"T-done", "T-stack", "T-titled"} {
		h.must("--as", "lead:yash", "task", "approve", id)
	}
	h.must("--as", "lead:yash", "decision", "raise", "--file", "d-wide.yaml")

	branchRaise, err := event.New("01JD8Z3K6Q4M2V7X9T1B5N0ZZZ", event.DecisionRaisedType, h.now,
		event.Actor{Role: event.RoleSession, ID: "t-wait"}, event.Subject{Kind: event.KindDecision, ID: "D-on-branch"},
		event.DecisionRaised{ID: "D-on-branch", Title: "Which status source the adapter trusts", Tier: 2,
			RaisedBy: event.RaisedBy{Session: "t-wait", On: "2026-11-20"}, Question: "Which source?",
			Options: []event.Option{{ID: "listing", Tradeoff: "a"}, {ID: "hooks", Tradeoff: "b"}},
			Blocks:  []string{"T-next"}, ContractImpact: []string{}, Context: event.DecisionContext{Summary: "s"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := event.Encode(branchRaise)
	file := observe.BranchFile{Path: ".ekumen/events/decision/D-on-branch/01JD8Z3K6Q4M2V7X9T1B5N0ZZZ.DecisionRaised.json", Body: body}
	h.prs = observe.StaticBranches{
		PRs: []observe.PR{
			{Number: 5, Branch: "build/t-done", State: observe.StateMerged, Checks: observe.ChecksSuccess, Title: "T-done: merged"},
			{Number: 9, Branch: "build/t-stack-2", State: observe.StateMerged, Checks: observe.ChecksSuccess, Title: "core: second part"},
			{Number: 10, Branch: "feature/x", State: observe.StateMerged, Checks: observe.ChecksSuccess, Title: "T-titled: the work"},
			{Number: 11, Branch: "orch/brief", State: observe.StateOpen, Checks: observe.ChecksNone, Title: "T-titled brief: wording"},
			{Number: 12, Branch: "build/t-wait", State: observe.StateDraft, Checks: observe.ChecksPending, Title: "T-wait: start"},
			{Number: 13, Branch: "build/t-wait-2", State: observe.StateDraft, Checks: observe.ChecksPending, Title: "T-wait 2/2"},
		},
		Files: map[string][]observe.BranchFile{"build/t-wait": {file}, "build/t-wait-2": {file}},
	}
}

func TestInboxV1Golden(t *testing.T) {
	h := newHarness(t)
	inboxV1Fixture(h)
	cases := []struct {
		name string
		args []string
	}{
		{"inbox-v1-lead.txt", []string{"inbox", "--for", "lead"}},
		{"inbox-v1-lead.json", []string{"inbox", "--for", "lead", "--json"}},
		{"why-v1-decision.txt", []string{"why", "D-wide"}},
		{"why-v1-decision.json", []string{"why", "D-wide", "--json"}},
		{"why-v1-task.txt", []string{"why", "T-next"}},
		{"why-v1-contract.txt", []string{"why", "C-api"}},
		{"why-v1-branch.txt", []string{"why", "D-on-branch"}},
		{"task-list-v1-linked.json", []string{"task", "list", "--json"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, errOut := h.run(c.args...)
			if code != 0 {
				t.Fatalf("exit %d: %s %s", code, out, errOut)
			}
			golden(t, c.name, out)
		})
	}
}

// #37: no done task appears as held up; D-wide ranks by the two
// unfinished tasks only, not by what C-api reaches.
func TestInboxV1HonestImpact_Issue37(t *testing.T) {
	h := newHarness(t)
	inboxV1Fixture(h)
	_, out, _ := h.run("inbox", "--for", "lead", "--json")
	items := decodeJSON(t, out)["items"].([]any)
	var wide map[string]any
	for _, it := range items {
		m := it.(map[string]any)
		if m["id"] == "D-wide" {
			wide = m
		}
		if strings.Contains(strings.Join(toStrings(m["unblocks"]), ","), "T-done") {
			t.Errorf("%s lists done T-done as unblocked", m["id"])
		}
	}
	if wide == nil {
		t.Fatal("D-wide missing from the inbox")
	}
	if got := toStrings(wide["unblocks"]); strings.Join(got, ",") != "T-next,T-wait" {
		t.Errorf("unblocks %v", got)
	}
	if got := toStrings(wide["direct"]); strings.Join(got, ",") != "T-wait" {
		t.Errorf("direct %v", got)
	}
	if got := toStrings(wide["contract_context"]); strings.Join(got, ",") != "C-api" {
		t.Errorf("contract_context %v", got)
	}
	if wide["priority"].(float64) != 2 {
		t.Errorf("priority %v, want 2 (two unfinished tasks × urgency 1)", wide["priority"])
	}
}

// #39: PRs link by recorded branch, then stack suffix, then title prefix,
// and task show says which rule linked it.
func TestTaskLinkRules_Issue39(t *testing.T) {
	h := newHarness(t)
	inboxV1Fixture(h)
	for id, want := range map[string]string{"T-done": "branch", "T-stack": "stack", "T-titled": "title", "T-wait": "branch"} {
		_, out, _ := h.run("task", "show", id, "--json")
		pr := decodeJSON(t, out)["task"].(map[string]any)["observed"].(map[string]any)["pr"]
		if pr == nil {
			t.Errorf("%s: no PR linked", id)
			continue
		}
		if got := pr.(map[string]any)["linked_by"]; got != want {
			t.Errorf("%s linked by %v, want %s", id, got, want)
		}
	}
	for id, want := range map[string]string{"T-stack": "done", "T-titled": "done", "T-done": "done", "T-wait": "draft"} {
		if got := taskStatus(t, h, id); got != want {
			t.Errorf("%s is %s, want %s", id, got, want)
		}
	}
	_, out, _ := h.run("task", "show", "T-stack")
	if !strings.Contains(out, "linked by stack") {
		t.Errorf("task show does not say which rule linked the PR:\n%s", out)
	}
}

// #38: a decision raised only on an open branch is in the lead's inbox,
// marked with its branch and PR, and answering it is refused until
// D-branch-decision-answer is made.
func TestBranchDecisions_Issue38(t *testing.T) {
	h := newHarness(t)
	inboxV1Fixture(h)
	_, out, _ := h.run("inbox", "--for", "lead", "--json")
	var found map[string]any
	for _, it := range decodeJSON(t, out)["items"].([]any) {
		if m := it.(map[string]any); m["id"] == "D-on-branch" {
			found = m
		}
	}
	if found == nil {
		t.Fatalf("branch decision missing:\n%s", out)
	}
	if found["on_branch"] != "build/t-wait" || found["pr"].(float64) != 12 || found["answerable"] != false ||
		found["blocked_by"] != "D-branch-decision-answer" {
		t.Errorf("branch decision item %v", found)
	}
	before := countEvents(t, h)
	code, _, errOut := h.run("--as", "lead:yash", "decision", "answer", "D-on-branch", "listing")
	if code != ExitRefused || !strings.Contains(errOut, "D-branch-decision-answer") || !strings.Contains(errOut, "PR #12") {
		t.Errorf("answer: exit %d, %s", code, errOut)
	}
	if countEvents(t, h) != before {
		t.Error("a refused answer wrote events")
	}
	code, out, errOut = h.run("decision", "show", "D-on-branch", "--json")
	if code != 0 || decodeJSON(t, out)["on_branch"] != "build/t-wait" || !strings.Contains(errOut, "not on main") {
		t.Errorf("decision show: exit %d, %s %s", code, out, errOut)
	}
}
