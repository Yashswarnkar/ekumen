package cli

import (
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

func skillCommands(text string) []string {
	var out []string
	for _, b := range fencedBlocks(text, "") {
		for _, l := range strings.Split(b, "\n") {
			if strings.HasPrefix(l, "ekumen ") {
				out = append(out, l)
			}
		}
	}
	return out
}

func shellWords(t *testing.T, line string) []string {
	t.Helper()
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
			t.Fatalf("%q: keep skill command lines to plain arguments (found %q)", line, c)
		default:
			cur.WriteRune(c)
			started = true
		}
	}
	if started {
		words = append(words, cur.String())
	}
	return words
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

func TestSkillCommandsParse(t *testing.T) {
	h := newHarness(t)
	for name, text := range skillTexts(t) {
		cmds := skillCommands(text)
		if len(cmds) == 0 {
			t.Errorf("%s: no ekumen commands in fenced blocks", name)
		}
		for _, line := range cmds {
			words := shellWords(t, placeholder.ReplaceAllString(line, "x"))
			if !slices.Contains(words, "--json") {
				t.Errorf("%s: %q has no --json", name, line)
			}
			want := "Usage: ekumen " + words[1]
			if len(words) > 2 && !strings.HasPrefix(words[2], "-") {
				want += " " + words[2]
			}
			code, out, errOut := h.run(append(words[1:], "-h")...)
			if code != ExitUsage || !strings.HasPrefix(strings.TrimSpace(out+errOut), want) {
				t.Errorf("%s: %q: want %q, got exit %d\n%s%s", name, line, want, code, out, errOut)
			}
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

func TestSkillFlow(t *testing.T) {
	texts := skillTexts(t)
	tmpls := fencedBlocks(texts["blocked"], "yaml")
	if len(tmpls) != 1 {
		t.Fatalf("blocked: want one yaml template, got %d", len(tmpls))
	}
	record := placeholder.ReplaceAllStringFunc(tmpls[0], func(p string) string {
		v, ok := templateValues[p[1:len(p)-1]]
		if !ok {
			t.Fatalf("template placeholder %s has no test value", p)
		}
		return v
	})

	h := newHarness(t)
	h.write(".ekumen/briefs/T-demo/brief.md", "# T-demo\n")
	h.write("t.yaml", "id: T-demo\ntitle: Demo\nkind: build\nsession: s-demo\ntier_grants: [push_own_branch]\n")
	h.must("--as", "orchestrator:main", "claim", "orchestrator")
	h.must("--as", "orchestrator:main", "task", "draft", "--file", "t.yaml")
	h.must("--as", "lead:yash", "task", "approve", "T-demo")

	jsonOf := func(stdin string, args ...string) map[string]any {
		t.Helper()
		code, out, errOut := h.runIn(stdin, append(args, "--json")...)
		if code != 0 {
			t.Fatalf("ekumen %s: exit %d\n%s%s", strings.Join(args, " "), code, out, errOut)
		}
		h.tick()
		return decodeJSON(t, out)
	}
	kinds := func(m map[string]any) []string {
		var ks []string
		for _, it := range m["items"].([]any) {
			ks = append(ks, it.(map[string]any)["kind"].(string))
		}
		return ks
	}

	jsonOf(record, "--as", "session:s-demo", "decision", "raise", "--file", "-")
	if ks := kinds(jsonOf("", "inbox", "--for", "lead")); !slices.Contains(ks, "decision") {
		t.Fatalf("lead inbox kinds %v, want a decision", ks)
	}

	jsonOf("", "--as", "lead:yash", "question", "ask", "D-demo-choice", "--text", "Why not SQLite?")
	qs := jsonOf("", "question", "list", "--for", "s-demo", "--open")["questions"].([]any)
	if len(qs) != 1 {
		t.Fatalf("open questions for s-demo: %v", qs)
	}
	if ks := kinds(jsonOf("", "inbox", "--for", "s-demo")); !slices.Contains(ks, "question") {
		t.Fatalf("session inbox kinds %v, want a question", ks)
	}
	jsonOf("", "--as", "session:s-demo", "question", "answer", qs[0].(map[string]any)["id"].(string), "--text", "Not needed yet.")

	d := jsonOf("", "decision", "show", "D-demo-choice")["decision"].(map[string]any)
	if d["status"] != "pending" || d["applied_on"] != nil {
		t.Fatalf("before answer: status %v applied_on %v", d["status"], d["applied_on"])
	}
	if a := d["questions"].([]any)[0].(map[string]any)["answer"]; a != "Not needed yet." {
		t.Fatalf("questions[0].answer = %v", a)
	}

	jsonOf("", "--as", "lead:yash", "decision", "answer", "D-demo-choice", "files", "--note", "Keep it simple.")
	jsonOf("", "--as", "loop:main", "decision", "apply", "D-demo-choice")
	d = jsonOf("", "decision", "show", "D-demo-choice")["decision"].(map[string]any)
	if d["status"] != "decided" || d["applied_on"] == nil {
		t.Fatalf("after apply: status %v applied_on %v", d["status"], d["applied_on"])
	}
	if u := d["applied"].(map[string]any)["unblocked_tasks"].([]any); !slices.Contains(u, any("T-demo")) {
		t.Fatalf("applied.unblocked_tasks = %v, want T-demo", u)
	}
	if am := jsonOf("", "task", "show", "T-demo")["brief"].(map[string]any)["amendments"].([]any); len(am) != 1 {
		t.Fatalf("brief.amendments = %v, want one", am)
	}
}
