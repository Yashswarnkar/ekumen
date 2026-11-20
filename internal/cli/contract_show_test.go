package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestContractShowGolden(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	show := func(name string, args ...string) {
		t.Helper()
		code, out, errOut := h.run(append([]string{"contract", "show", "--json"}, args...)...)
		if code != ExitOK {
			t.Fatalf("contract show %v: exit %d\n%s%s", args, code, out, errOut)
		}
		golden(t, "contract-show-"+name+".json", out)
		_, out, _ = h.run(append([]string{"contract", "show"}, args...)...)
		golden(t, "contract-show-"+name+".txt", out)
	}
	show("draft", "C-cli-json")
	show("frozen", "C1")

	h.must("--as", "lead:yash", "decision", "answer", "D-claim-lane", "claiming-session")
	h.write(".ekumen/contracts/C-event-log.md", "---\nid: C-event-log\nversion: 4\nconsumers: [T-core, T-cli]\nproducers: [T-core]\n---\n# Event log\n\nRoleClaimed is the claiming session's.\n")
	h.must("--as", "orchestrator:main", "contract", "version", "C1", "--by", "D-claim-lane")
	show("versioned", "C-event-log")
	h.must("--as", "lead:yash", "contract", "freeze", "C1")
	show("versioned-frozen", "C1")
}

func TestShowKindHint(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	cases := []struct{ cmd, id, want string }{
		{"decision", "C-cli-json", "use `ekumen contract show C-cli-json`"},
		{"decision", "C1", "use `ekumen contract show C1`"},
		{"task", "D-claim-lane", "use `ekumen decision show D-claim-lane`"},
		{"contract", "T-core", "use `ekumen task show T-core`"},
		{"contract", "D-http-api", "use `ekumen decision show D-http-api`"},
	}
	for _, c := range cases {
		code, out, _ := h.run(c.cmd, "show", c.id, "--json")
		if code != ExitNotFound || !strings.Contains(out, c.want) {
			t.Errorf("%s show %s: exit %d, %s", c.cmd, c.id, code, out)
		}
	}
	code, out, _ := h.run("contract", "show", "C-nope", "--json")
	if code != ExitNotFound || strings.Contains(out, "use `") {
		t.Errorf("same-kind miss: exit %d, %s", code, out)
	}
}

type graphNodes struct {
	Nodes []struct {
		ID        string `json:"id"`
		Kind      string `json:"kind"`
		Status    string `json:"status"`
		WaitingOn string `json:"waiting_on"`
	} `json:"nodes"`
}

// decisionWaits returns status/waiting_on per decision node, and checks
// that the graph's lead gates are exactly the decisions and apply actions
// in the lead's inbox.
func decisionWaits(t *testing.T, h *harness) (map[string]string, string) {
	t.Helper()
	_, out, _ := h.run("graph", "--json")
	var g graphNodes
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	var leadGates []string
	for _, n := range g.Nodes {
		if n.Kind == "decision" {
			got[n.ID] = n.Status + "/" + n.WaitingOn
		}
		if n.WaitingOn == "lead" {
			leadGates = append(leadGates, n.ID)
		}
	}
	_, inboxOut, _ := h.run("inbox", "--for", "lead", "--json")
	var inbox struct {
		Items []struct{ Kind, ID, Action string } `json:"items"`
	}
	if err := json.Unmarshal([]byte(inboxOut), &inbox); err != nil {
		t.Fatal(err)
	}
	var waiting []string
	for _, i := range inbox.Items {
		if i.Kind == "decision" || i.Action == "apply" {
			waiting = append(waiting, i.ID)
		}
	}
	slices.Sort(leadGates)
	slices.Sort(waiting)
	if !slices.Equal(leadGates, waiting) {
		t.Errorf("graph lead gates %v, lead inbox %v", leadGates, waiting)
	}
	return got, out
}

func TestGraphWaitingOn(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	h.must("--as", "lead:yash", "decision", "answer", "D-claim-lane", "claiming-session")
	got, out := decisionWaits(t, h)
	golden(t, "graph-waiting-on.json", out)
	want := map[string]string{"D-claim-lane": "decided/loop", "D-new-choice": "pending/lead"}
	if !maps.Equal(got, want) {
		t.Errorf("decision nodes %v, want %v (D-yaml-library is applied, so not a gate)", got, want)
	}

	h.now = h.now.Add(25 * time.Hour)
	got, _ = decisionWaits(t, h)
	want["D-claim-lane"] = "decided/lead"
	if !maps.Equal(got, want) {
		t.Errorf("a day after deciding: %v, want %v", got, want)
	}
}

var mapNodeRe = regexp.MustCompile(`<g class="n ([^"]*)"><title>[^<]*</title>`)
var mapNidRe = regexp.MustCompile(`<text class="nid"[^>]*>([^<]*)</text>`)
var mapEdgeRe = regexp.MustCompile(`<path class="e ?([^"]*)"`)

func mapDecisions(t *testing.T, h *harness, node string) string {
	t.Helper()
	page := filepath.Join(t.TempDir(), "map.html")
	h.must("map", "--out", page)
	b, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{"Decision waiting on you (gate)", "Decided, apply pending"} {
		if !strings.Contains(html, want) {
			t.Errorf("legend lacks %q", want)
		}
	}
	var srcs []string
	for _, m := range scriptRe.FindAllStringSubmatch(html, -1) {
		srcs = append(srcs, m[1])
	}
	arg, _ := json.Marshal(srcs)
	js := `const vm = require("vm"); const m = {};
const r = {querySelector: q => (m[q] ??= {innerHTML: ""})};
const ctx = {console}; ctx.window = ctx; ctx.self = ctx; vm.createContext(ctx);
ctx.document = {currentScript: {closest: () => r}};
for (const s of JSON.parse(require("fs").readFileSync(0, "utf8"))) vm.runInContext(s, ctx);
process.stdout.write(m['[data-ek="graph"]'].innerHTML);`
	cmd := exec.Command(node, "-e", js)
	cmd.Stdin = strings.NewReader(string(arg))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("render graph: %v", err)
	}
	svg := string(out)
	var lines []string
	nids := mapNidRe.FindAllStringSubmatch(svg, -1)
	for i, n := range mapNodeRe.FindAllStringSubmatch(svg, -1) {
		if strings.HasPrefix(n[1], "decision") && i < len(nids) {
			lines = append(lines, n[1]+" | "+nids[i][1])
		}
	}
	gates := 0
	for _, e := range mapEdgeRe.FindAllStringSubmatch(svg, -1) {
		if e[1] == "fromgate" {
			gates++
		}
	}
	slices.Sort(lines)
	return fmt.Sprintf("%s\ngate edges: %d\n", strings.Join(lines, "\n"), gates)
}

func TestMapGateStyles(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	h := newHarness(t)
	buildFixture(h)
	h.must("--as", "lead:yash", "decision", "answer", "D-claim-lane", "claiming-session")
	golden(t, "map-gate-styles.txt", mapDecisions(t, h, node))
	h.now = h.now.Add(25 * time.Hour)
	golden(t, "map-gate-styles-apply-due.txt", mapDecisions(t, h, node))
}

func TestContractShowMissingFile(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	if err := os.Remove(filepath.Join(h.root, ".ekumen/contracts/C-cli-json.md")); err != nil {
		t.Fatal(err)
	}
	code, out, _ := h.run("contract", "show", "C-cli-json", "--json")
	if code != ExitNotFound || !strings.Contains(out, `"field": "path"`) {
		t.Errorf("exit %d: %s", code, out)
	}
}

var scriptRe = regexp.MustCompile(`(?s)<script>(.*?)</script>`)
