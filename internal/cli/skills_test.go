package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const skillsDir = "../../skills"

var (
	skillName   = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	placeholder = regexp.MustCompile(`<[^<>]+>`)
	codeSpan    = regexp.MustCompile("`(ekumen [^`]+)`")
	skillKeys   = []string{"name", "description", "license", "compatibility", "metadata", "allowed-tools"}
	forbidden   = []string{".ekumen/events", "registry.yaml", "state.json", "decisions/",
		"claude", "codex", "gemini", "opencode", "cursor", "copilot", "anthropic", "openai"}
)

func skillFrontMatter(text string) (map[string]any, error) {
	rest, ok := strings.CutPrefix(text, "---\n")
	if !ok {
		return nil, fmt.Errorf("no front matter")
	}
	head, _, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return nil, fmt.Errorf("unterminated front matter")
	}
	var m map[string]any
	err := yaml.Unmarshal([]byte(head), &m)
	return m, err
}

func skillTexts(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(skillsDir, "*", "SKILL.md"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no skills found under %s: %v", skillsDir, err)
	}
	out := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[filepath.Base(filepath.Dir(p))] = string(b)
	}
	return out
}

func fencedBlocks(text, lang string) []string {
	var out, cur []string
	in, match := false, false
	for _, l := range strings.Split(text, "\n") {
		switch {
		case !in && strings.HasPrefix(l, "```"):
			in, match, cur = true, strings.TrimPrefix(l, "```") == lang, nil
		case in && strings.HasPrefix(l, "```"):
			if match {
				out = append(out, strings.Join(cur, "\n"))
			}
			in = false
		case in:
			cur = append(cur, l)
		}
	}
	return out
}

func skillBody(text string) string {
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if _, after, ok := strings.Cut(rest, "\n---\n"); ok {
			return after
		}
	}
	return text
}

// skillCommands returns every ekumen command line a skill names: lines in
// fenced blocks, then inline code spans (except the generic "ekumen …").
func skillCommands(text string) []string {
	var out []string
	var prose []string
	in := false
	for _, l := range strings.Split(skillBody(text), "\n") {
		switch {
		case strings.HasPrefix(l, "```"):
			in = !in
		case in && strings.HasPrefix(l, "ekumen "):
			out = append(out, l)
		case !in:
			prose = append(prose, l)
		}
	}
	for _, m := range codeSpan.FindAllStringSubmatch(strings.Join(prose, "\n"), -1) {
		if !strings.HasPrefix(m[1], "ekumen …") {
			out = append(out, m[1])
		}
	}
	return out
}

func shellWords(line string) ([]string, error) {
	var words []string
	var cur strings.Builder
	var quote rune
	started := false
	for _, c := range line {
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(c)
		case c == '"' || c == '\'':
			quote, started = c, true
		case c == ' ' || c == '\t':
			if started {
				words = append(words, cur.String())
				cur.Reset()
				started = false
			}
		case strings.ContainsRune("|;&<>$`", c):
			return nil, fmt.Errorf("%q: keep skill command lines to plain arguments (found %q)", line, c)
		default:
			cur.WriteRune(c)
			started = true
		}
	}
	if started {
		words = append(words, cur.String())
	}
	return words, nil
}

func runAs(h *harness, actor, stdin string, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	env := h.env(stdin, &out, &errb)
	env.Getenv = func(k string) string {
		if k == "EKUMEN_ACTOR" {
			return actor
		}
		return ""
	}
	code := Run(args, env)
	return code, out.String(), errb.String()
}

// checkSkillLine runs a skill's command line with its placeholders filled
// in, for real on a fixture, so the command, its flags, required flags and
// positional arity all go through the CLI's own parser. Only a usage error
// fails; not found, refusals and successes all mean the line is well formed.
func checkSkillLine(h *harness, line string) error {
	words, err := shellWords(placeholder.ReplaceAllString(line, "x"))
	if err != nil {
		return err
	}
	if len(words) < 2 || words[0] != "ekumen" {
		return fmt.Errorf("%q: not an ekumen command", line)
	}
	if !slices.Contains(words, "--json") {
		return fmt.Errorf("%q has no --json", line)
	}
	code, out, errOut := runAs(h, "session:t-cli", "", words[1:]...)
	h.tick()
	if code == ExitUsage {
		return fmt.Errorf("%q: usage error\n%s%s", line, out, errOut)
	}
	want := "Usage: ekumen " + words[1]
	if len(words) > 2 && !strings.HasPrefix(words[2], "-") {
		want += " " + words[2]
	}
	code, out, errOut = h.run(append(words[1:], "-h")...)
	if code != ExitUsage || !strings.HasPrefix(strings.TrimSpace(out+errOut), want) {
		return fmt.Errorf("%q: -h: want %q, got exit %d\n%s%s", line, want, code, out, errOut)
	}
	return nil
}

func TestSkillCommandsParse(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	for name, text := range skillTexts(t) {
		cmds := skillCommands(text)
		if len(cmds) == 0 {
			t.Errorf("%s: no ekumen commands", name)
		}
		for _, line := range cmds {
			if err := checkSkillLine(h, line); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	}
}

func TestSkillLineCheckerCatchesBrokenLines(t *testing.T) {
	h := newHarness(t)
	buildFixture(h)
	if err := checkSkillLine(h, "ekumen decision answer <id> <option> --note … --json"); err != nil {
		t.Fatalf("well-formed line refused: %v", err)
	}
	for _, line := range []string{
		"ekumen decision answer <id> --note … --json",
		"ekumen decision anwser <id> <option> --json",
		"ekumen question answer <qid> --txt … --json",
		"ekumen question ask <id> --json",
		"ekumen task show --json",
		"ekumen inbox --for lead",
		"ekumen decision raise --file - --json | cat",
	} {
		if err := checkSkillLine(h, line); err == nil {
			t.Errorf("%q passed the checker", line)
		}
	}
}

func TestSkillFrontMatter(t *testing.T) {
	for name, text := range skillTexts(t) {
		m, err := skillFrontMatter(text)
		if err != nil {
			t.Errorf("%s: front matter: %v", name, err)
			continue
		}
		for k := range m {
			if !slices.Contains(skillKeys, k) {
				t.Errorf("%s: front matter key %q is not in the Agent Skills set %v", name, k, skillKeys)
			}
		}
		if got, _ := m["name"].(string); got != name || !skillName.MatchString(got) || len(got) > 64 {
			t.Errorf("%s: name %q must equal the folder, match %s and be at most 64 characters", name, got, skillName)
		}
		if d, _ := m["description"].(string); strings.TrimSpace(d) == "" || len(d) > 1024 {
			t.Errorf("%s: description must be non-empty and at most 1024 characters (has %d)", name, len(d))
		}
		if n := strings.Count(text, "\n"); n > 160 {
			t.Errorf("%s: %d lines; keep SKILL.md to about 150 and move detail to a reference file", name, n)
		}
	}
}

func TestSkillForbiddenContent(t *testing.T) {
	for name, text := range skillTexts(t) {
		low := strings.ToLower(text)
		for _, f := range forbidden {
			if strings.Contains(low, f) {
				t.Errorf("%s: mentions %q (skills read state through ekumen --json and name no agent tool or vendor)", name, f)
			}
		}
	}
}

var templateValues = map[string]string{
	"slug": "demo-choice",
	"the choice, as a choice, at most 100 characters": "Which store the demo uses",
	"answerable without opening this session":         "Files or SQLite for the demo?",
	"option-a":                        "files",
	"option-b":                        "sqlite",
	"what you gain and what it costs": "a tradeoff",
	"YYYY-MM-DD":                      "2026-11-27",
	"your task":                       "T-demo",
	"150 words or fewer":              "Came up while building the demo.",
	"repo path or URL":                ".ekumen/briefs/T-demo/brief.md",
}

func fillTemplate(t *testing.T, tmpl string, vals map[string]string) string {
	t.Helper()
	return placeholder.ReplaceAllStringFunc(tmpl, func(p string) string {
		v, ok := vals[p[1:len(p)-1]]
		if !ok {
			t.Fatalf("placeholder %s has no test value", p)
		}
		return v
	})
}

// skillArgs takes the first command line in a skill that starts with
// prefix, fills its placeholders (and "…" values) and returns its argv.
func skillArgs(t *testing.T, text, prefix string, vals map[string]string) []string {
	t.Helper()
	for _, line := range skillCommands(text) {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		words, err := shellWords(fillTemplate(t, line, vals))
		if err != nil {
			t.Fatal(err)
		}
		for i, w := range words {
			if w == "…" {
				words[i] = vals["…"]
			}
		}
		return words[1:]
	}
	t.Fatalf("no command starting %q", prefix)
	return nil
}

func replaceOnce(t *testing.T, s, old, new string) string {
	t.Helper()
	if !strings.Contains(s, old) {
		t.Fatalf("template has no %q", old)
	}
	return strings.Replace(s, old, new, 1)
}

func dig(t *testing.T, v any, path ...string) any {
	t.Helper()
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("%v: not an object at %q", path, k)
		}
		if v, ok = m[k]; !ok {
			t.Fatalf("missing key %q (path %v)", k, path)
		}
	}
	return v
}

func TestSkillFlow(t *testing.T) {
	texts := skillTexts(t)
	blocked, decide := texts["blocked"], texts["decide"]
	tmpls := fencedBlocks(blocked, "yaml")
	if len(tmpls) != 1 {
		t.Fatalf("blocked: want one yaml template, got %d", len(tmpls))
	}
	tierOne := fillTemplate(t, tmpls[0], templateValues)
	tierTwo := replaceOnce(t, tierOne, "id: D-demo-choice", "id: D-demo-tier-two")
	tierTwo = replaceOnce(t, tierTwo, "tier: 1", "tier: 2")
	tierTwo = replaceOnce(t, tierTwo, "default_if_no_answer: files", "default_if_no_answer: null")

	h := newHarness(t)
	h.write(".ekumen/briefs/T-demo/brief.md", "# T-demo\n")
	h.write("t.yaml", "id: T-demo\ntitle: Demo\nkind: build\nsession: s-demo\ntier_grants: [push_own_branch]\n")
	h.must("--as", "orchestrator:main", "claim", "orchestrator")
	h.must("--as", "orchestrator:main", "task", "draft", "--file", "t.yaml")
	h.must("--as", "lead:yash", "task", "approve", "T-demo")

	call := func(actor, stdin string, args []string) (int, map[string]any) {
		t.Helper()
		code, out, errOut := runAs(h, actor, stdin, args...)
		h.tick()
		if code != 0 && code != ExitRefused {
			t.Fatalf("ekumen %s: exit %d\n%s%s", strings.Join(args, " "), code, out, errOut)
		}
		return code, decodeJSON(t, out)
	}
	ok := func(actor, stdin string, args []string) map[string]any {
		t.Helper()
		code, m := call(actor, stdin, args)
		if code != 0 {
			t.Fatalf("ekumen %s: exit %d: %v", strings.Join(args, " "), code, m)
		}
		return m
	}
	const session, lead = "session:s-demo", "lead:yash"
	v := map[string]string{"task": "T-demo", "session": "s-demo", "handle": "yash", "this session": "desk"}
	with := func(kv ...string) map[string]string {
		m := map[string]string{}
		for k, x := range v {
			m[k] = x
		}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}

	task := ok(session, "", skillArgs(t, blocked, "ekumen task show", v))
	if g := dig(t, task, "task", "tier_grants").([]any); !slices.Contains(g, any("push_own_branch")) {
		t.Fatalf("task.tier_grants = %v", g)
	}
	raise := skillArgs(t, blocked, "ekumen decision raise", v)
	ok(session, tierOne, raise)
	ok(session, tierTwo, raise)

	var ids []string
	for _, it := range ok("", "", skillArgs(t, decide, "ekumen inbox", v))["items"].([]any) {
		if dig(t, it, "kind") == "decision" {
			ids = append(ids, dig(t, it, "id").(string))
		}
	}
	if !slices.Contains(ids, "D-demo-choice") || !slices.Contains(ids, "D-demo-tier-two") {
		t.Fatalf("lead inbox decisions %v", ids)
	}

	show := func(id string) any {
		return dig(t, ok("", "", skillArgs(t, decide, "ekumen decision show", with("id", id))), "decision")
	}
	d2 := show("D-demo-tier-two")
	if dig(t, d2, "tier") != 2.0 || dig(t, d2, "default_if_no_answer") != nil || dig(t, d2, "status") != "pending" {
		t.Fatalf("tier-2 variant: %v", d2)
	}
	d := show("D-demo-choice")
	for _, k := range []string{"holds_up", "priority", "answer", "answer_note", "superseded_by", "applied_on", "deadline", "default_if_no_answer"} {
		dig(t, d, k)
	}
	if dig(t, d, "raised_by", "session") != "s-demo" || dig(t, d, "status") != "pending" {
		t.Fatalf("raised decision: %v", d)
	}
	if hu := dig(t, d, "holds_up").([]any); !slices.Contains(hu, any("T-demo")) {
		t.Fatalf("holds_up = %v", hu)
	}

	ok(lead, "", skillArgs(t, decide, "ekumen question ask", with("id", "D-demo-choice", "…", "Why not SQLite?")))
	qs := ok(session, "", skillArgs(t, blocked, "ekumen question list", v))["questions"].([]any)
	if len(qs) != 1 {
		t.Fatalf("open questions for s-demo: %v", qs)
	}
	var kinds []string
	for _, it := range ok("", "", []string{"inbox", "--for", "s-demo", "--json"})["items"].([]any) {
		kinds = append(kinds, dig(t, it, "kind").(string))
	}
	if !slices.Contains(kinds, "question") {
		t.Fatalf("session inbox kinds %v, want a question", kinds)
	}
	ok(session, "", skillArgs(t, blocked, "ekumen question answer", with("question id", dig(t, qs[0], "id").(string), "…", "Not needed yet.")))

	d = dig(t, ok(session, "", skillArgs(t, blocked, "ekumen decision show", with("decision id", "D-demo-choice"))), "decision")
	q := dig(t, d, "questions").([]any)[0]
	if dig(t, q, "answer") != "Not needed yet." || dig(t, q, "by") != lead || dig(t, q, "to") != session || dig(t, q, "answered_by") != session {
		t.Fatalf("questions[0] = %v", q)
	}

	h.must("--as", lead, "decision", "supersede", "D-demo-tier-two", "--by", "D-demo-choice", "--note", "One decision is enough.")
	if s := dig(t, show("D-demo-tier-two"), "superseded_by"); s != "D-demo-choice" {
		t.Fatalf("superseded_by = %v", s)
	}
	if code, m := call("", "", skillArgs(t, decide, "ekumen decision apply", with("id", "D-demo-tier-two"))); code != ExitRefused {
		t.Fatalf("applying a superseded decision: exit %d %v, want refused", code, m)
	}

	ok("", "", skillArgs(t, decide, "ekumen decision answer", with("id", "D-demo-choice", "option", "files", "…", "Keep it simple.")))
	ok("", "", skillArgs(t, decide, "ekumen decision apply", with("id", "D-demo-choice")))
	d = show("D-demo-choice")
	if dig(t, d, "status") != "decided" || dig(t, d, "answer") != "files" || dig(t, d, "answer_note") != "Keep it simple." || dig(t, d, "applied_on") == nil {
		t.Fatalf("after apply: %v", d)
	}
	if u := dig(t, d, "applied", "unblocked_tasks").([]any); !slices.Contains(u, any("T-demo")) {
		t.Fatalf("applied.unblocked_tasks = %v, want T-demo", u)
	}
	if am := dig(t, ok(session, "", skillArgs(t, blocked, "ekumen task show", v)), "brief", "amendments").([]any); len(am) != 1 {
		t.Fatalf("brief.amendments = %v, want one", am)
	}
}
