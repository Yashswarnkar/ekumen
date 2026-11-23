package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/project"
)

// gitEnv isolates git from the user's configuration.
func gitEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "Test")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "test@example.com")
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitCmd(dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// startRepo makes h.root a git repository whose origin (a bare repository
// next to it) has a main branch, and drafts tasks:
//   - T-go: approved, unblocked, all location fields set (startable)
//   - T-wait: approved, blocked by T-go
//   - T-new: drafted only, not approved
//   - T-bare: approved, no branch, worktree or session
func startRepo(t *testing.T) *harness {
	t.Helper()
	gitEnv(t)
	h := newHarness(t)
	origin := filepath.Join(filepath.Dir(h.root), "origin.git")
	git(t, filepath.Dir(h.root), "init", "--quiet", "--bare", "-b", "main", origin)
	git(t, h.root, "init", "--quiet", "-b", "main")
	h.write(".gitignore", "bin/\n.worktrees/\n.ekumen/registry.yaml\n.ekumen/state.json\n")
	h.write("README.md", "demo\n")
	git(t, h.root, "add", ".gitignore", "README.md")
	git(t, h.root, "commit", "--quiet", "-m", "init")
	git(t, h.root, "remote", "add", "origin", origin)
	git(t, h.root, "push", "--quiet", "origin", "main")

	h.write(".ekumen/config.yaml", "project: demo\nlead: yash\nagents:\n  default: tool\n  tool: {command: \"/opt/agent --flag\"}\n  other: {command: /opt/other}\nsetup: [\"mkdir -p bin && echo built > bin/built\"]\n")
	brief := "# T-go\n\n**Goal:** go.\n\n**Start prompt:**\n\n    export EKUMEN_ACTOR=session:t-go\n    Read the brief.\n\n      Indented more.\n\n**After:** not part of it.\n"
	for _, id := range []string{"T-go", "T-wait", "T-new", "T-bare"} {
		h.write(".ekumen/briefs/"+id+"/brief.md", strings.ReplaceAll(brief, "t-go", strings.ToLower(id)))
	}
	h.write("t-go.yaml", "id: T-go\ntitle: Go\nkind: build\nbase_branch: main\nbranch: build/t-go\nworktree: .worktrees/t-go\nsession: t-go\nblocked_by: []\n")
	h.write("t-wait.yaml", "id: T-wait\ntitle: Wait\nkind: build\nbase_branch: main\nbranch: build/t-wait\nworktree: .worktrees/t-wait\nsession: t-wait\nblocked_by: [T-go]\n")
	h.write("t-new.yaml", "id: T-new\ntitle: New\nkind: build\nbase_branch: main\nbranch: build/t-new\nworktree: .worktrees/t-new\nsession: t-new\nblocked_by: []\n")
	h.write("t-bare.yaml", "id: T-bare\ntitle: Bare\nkind: build\nblocked_by: []\n")
	h.must("--as", "orchestrator:main", "claim", "orchestrator")
	for _, f := range []string{"t-go", "t-wait", "t-new", "t-bare"} {
		h.must("--as", "orchestrator:main", "task", "draft", "--file", f+".yaml")
	}
	for _, id := range []string{"T-go", "T-wait", "T-bare"} {
		h.must("--as", "lead:yash", "task", "approve", id)
	}
	return h
}

func TestTaskStartCreatesThenNoop(t *testing.T) {
	h := startRepo(t)
	orch := []string{"--as", "orchestrator:main", "--json", "task", "start", "T-go"}

	out := h.must(orch...)
	res := decodeJSON(t, out)
	wt := filepath.Join(h.root, ".worktrees", "t-go")
	if !samePath(res["worktree"].(string), wt) {
		t.Fatalf("worktree = %v, want %s", res["worktree"], wt)
	}
	if got := strings.Join(toStrings(res["done"]), ","); got != "branch,worktree,attach,setup,prompt" {
		t.Errorf("done = %s", got)
	}
	if len(toStrings(res["events"])) != 1 {
		t.Errorf("events = %v, want one TaskSessionAttached", res["events"])
	}
	if br := git(t, wt, "branch", "--show-current"); br != "build/t-go" {
		t.Errorf("branch = %q", br)
	}
	if up, err := gitCmd(wt, "rev-parse", "--abbrev-ref", "@{upstream}"); err == nil {
		t.Errorf("the new branch tracks %q; it must not track the base branch", up)
	}
	if b, err := os.ReadFile(filepath.Join(wt, "bin", "built")); err != nil || string(b) != "built\n" {
		t.Errorf("setup did not run in the worktree: %q %v", b, err)
	}
	prompt, err := os.ReadFile(filepath.Join(h.root, ".worktrees", "T-go.prompt.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"EKUMEN_ACTOR=session:t-go",
		"mkdir -p bin && echo built > bin/built",
		"Start prompt:\n\nexport EKUMEN_ACTOR=session:t-go\nRead the brief.\n\n  Indented more.\n",
	} {
		if !strings.Contains(string(prompt), want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(string(prompt), "not part of it") {
		t.Errorf("prompt has text after the Start prompt block:\n%s", prompt)
	}
	cmd := res["command"].(string)
	for _, want := range []string{"cd '" + res["worktree"].(string) + "'", "EKUMEN_ACTOR=session:t-go /opt/agent --flag \"$(cat '", "T-go.prompt.txt')\""} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command %q lacks %q", cmd, want)
		}
	}
	show := decodeJSON(t, h.must("--json", "task", "show", "T-go"))
	if s := show["task"].(map[string]any)["session"]; s != "t-go" {
		t.Errorf("session = %v", s)
	}

	// A second run changes nothing and prints the same command.
	again := decodeJSON(t, h.must(orch...))
	if len(toStrings(again["done"])) != 0 || len(toStrings(again["events"])) != 0 || len(toStrings(again["paths"])) != 0 {
		t.Errorf("second run changed something: %v", again)
	}
	if again["command"] != cmd {
		t.Errorf("second command %q, want %q", again["command"], cmd)
	}

	// A dirty worktree is left alone, but the command is still printed.
	os.WriteFile(filepath.Join(wt, "work.txt"), []byte("x"), 0o644)
	code, out, errOut := h.run(append(orch, "--setup")...)
	if code != 0 || decodeJSON(t, out)["command"] != cmd || !strings.Contains(errOut, "uncommitted changes") {
		t.Errorf("dirty: exit %d\n%s%s", code, out, errOut)
	}
	if b, _ := os.ReadFile(filepath.Join(wt, "work.txt")); string(b) != "x" {
		t.Error("the dirty worktree was touched")
	}
}

func TestTaskStartReusesExistingBranch(t *testing.T) {
	h := startRepo(t)
	git(t, h.root, "branch", "build/t-go", "main")
	res := decodeJSON(t, h.must("--as", "orchestrator:main", "--json", "task", "start", "T-go", "--agent", "other"))
	if got := strings.Join(toStrings(res["done"]), ","); got != "worktree,attach,setup,prompt" {
		t.Errorf("done = %s", got)
	}
	if !strings.Contains(res["command"].(string), " /opt/other \"$(cat ") || res["agent"] != "other" {
		t.Errorf("--agent other: %v", res)
	}
}

func TestTaskStartWrongBranchRefused(t *testing.T) {
	h := startRepo(t)
	git(t, h.root, "worktree", "add", "--quiet", "-b", "elsewhere", filepath.Join(h.root, ".worktrees", "t-go"), "main")
	code, out, _ := h.run("--as", "orchestrator:main", "--json", "task", "start", "T-go")
	if code != ExitRefused || !strings.Contains(out, "not the task's") {
		t.Errorf("exit %d: %s", code, out)
	}
}

func TestTaskStartRefusals(t *testing.T) {
	h := startRepo(t)
	cases := []struct {
		args []string
		code int
		want []string
	}{
		{[]string{"T-new"}, ExitRefused, []string{"not approved", "ekumen task approve T-new"}},
		{[]string{"T-wait"}, ExitRefused, []string{"blocked by T-go"}},
		{[]string{"T-bare"}, ExitRefused, []string{"base_branch, branch, worktree, session are empty", "ekumen task attach T-bare --session"}},
		{[]string{"T-go", "--agent", "nope"}, ExitRefused, []string{"--agent nope is not configured", "other, tool"}},
		{[]string{"T-nope"}, ExitNotFound, []string{"no task"}},
		{[]string{}, ExitUsage, []string{"give a task id or --list"}},
	}
	for _, c := range cases {
		code, out, errOut := h.run(append([]string{"--as", "orchestrator:main", "task", "start"}, c.args...)...)
		if code != c.code {
			t.Errorf("%v: exit %d, want %d\n%s%s", c.args, code, c.code, out, errOut)
		}
		for _, w := range c.want {
			if !strings.Contains(errOut, w) {
				t.Errorf("%v: stderr lacks %q:\n%s", c.args, w, errOut)
			}
		}
		h.tick()
	}
	if _, err := os.Stat(filepath.Join(h.root, ".worktrees")); err == nil {
		t.Error("a refused start created .worktrees/")
	}
	// Attaching needs the orchestrator's lane and claim, checked before
	// anything is created.
	code, _, _ := h.run("--as", "session:t-go", "task", "start", "T-go")
	if code != ExitRefused {
		t.Errorf("as a session: exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(h.root, ".worktrees", "t-go")); err == nil {
		t.Error("a refused attach still created the worktree")
	}
}

func TestTaskStartNoAgentConfigured(t *testing.T) {
	h := startRepo(t)
	h.write(".ekumen/config.yaml", "project: demo\n")
	code, _, errOut := h.run("--as", "orchestrator:main", "task", "start", "T-go")
	if code != ExitRefused || !strings.Contains(errOut, "no agent is configured") {
		t.Errorf("exit %d: %s", code, errOut)
	}
}

func TestTaskStartList(t *testing.T) {
	h := startRepo(t)
	list := func() []any {
		return decodeJSON(t, h.must("--json", "task", "start", "--list"))["tasks"].([]any)
	}
	ids := func(items []any) string {
		var out []string
		for _, it := range items {
			m := it.(map[string]any)
			out = append(out, m["id"].(string)+":"+strings.Join(toStrings(m["missing"]), "+"))
		}
		return strings.Join(out, " ")
	}
	if got := ids(list()); got != "T-bare:base_branch+branch+worktree+session T-go:" {
		t.Errorf("before: %s", got)
	}
	h.must("--as", "orchestrator:main", "task", "start", "T-go")
	if got := ids(list()); got != "T-bare:base_branch+branch+worktree+session" {
		t.Errorf("after starting T-go: %s", got)
	}
}

func TestTaskStartOpen(t *testing.T) {
	h := startRepo(t)
	var opened string
	var out, errb bytes.Buffer
	env := h.env("", &out, &errb)
	env.OpenTerminal = func(c string) error { opened = c; return nil }
	if code := Run([]string{"--as", "orchestrator:main", "--json", "task", "start", "T-go", "--open"}, env); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	res := decodeJSON(t, out.String())
	if res["opened"] != true || opened != res["command"] {
		t.Errorf("opened %q, result %v", opened, res)
	}
	// Where it isn't supported, the command is printed with a note.
	h.tick()
	code, o, e := h.run("--as", "orchestrator:main", "--json", "task", "start", "T-go", "--open")
	if code != 0 || decodeJSON(t, o)["opened"] != false || !strings.Contains(e, "--open isn't supported on this platform") {
		t.Errorf("unsupported: exit %d\n%s%s", code, o, e)
	}
}

func TestExtractStartPrompt(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"**Start prompt:**\n\n    a\n    b\n\nnext\n", "a\nb", true},
		{"## Start prompt\n\n```\nx\n  y\n```\n", "x\n  y", true},
		{"**Start prompt:**\n\n```\nunclosed\n", "", false},
		{"**Start prompt:**\n\nnot indented\n", "", false},
		{"no heading\n    a\n", "", false},
	}
	for _, c := range cases {
		got, ok := extractStartPrompt(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("%q: got %q %v, want %q %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestInitThenCheckClean(t *testing.T) {
	gitEnv(t)
	dir := filepath.Join(t.TempDir(), "fresh")
	os.MkdirAll(dir, 0o755)
	git(t, dir, "init", "--quiet")
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules/\n.worktrees/"), 0o644)
	h := &harness{t: t, root: dir, now: newHarness(t).now}

	res := decodeJSON(t, h.must("--json", "init", "--lead", "yash"))
	if res["project"] != "fresh" {
		t.Errorf("project = %v", res["project"])
	}
	for _, p := range []string{".ekumen/briefs/.gitkeep", ".ekumen/contracts/.gitkeep",
		".ekumen/decisions/.gitkeep", ".ekumen/research/.gitkeep", ".ekumen/spine.md", ".ekumen/config.yaml", ".gitignore"} {
		if !strings.Contains(strings.Join(toStrings(res["paths"]), " "), p) {
			t.Errorf("paths lack %s: %v", p, res["paths"])
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, ".ekumen", "events")); err != nil || !fi.IsDir() {
		t.Error("no .ekumen/events/")
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(gi) != "node_modules/\n.worktrees/\n# Ekumen: local projections and task worktrees\n.ekumen/registry.yaml\n.ekumen/state.json\n" {
		t.Errorf(".gitignore:\n%s", gi)
	}
	spine, _ := os.ReadFile(filepath.Join(dir, ".ekumen", "spine.md"))
	if !strings.HasPrefix(string(spine), "# fresh: spine") || !strings.Contains(string(spine), "## 5. Invariants") {
		t.Errorf("spine:\n%s", spine)
	}
	cfg, err := project.ReadConfig(filepath.Join(dir, ".ekumen"))
	if err != nil || cfg.Project != "fresh" || cfg.Lead != "yash" || cfg.DefaultAgent != project.DefaultAgentName ||
		cfg.Agents[project.DefaultAgentName].Command != project.DefaultAgentCommand || cfg.Setup != nil && len(cfg.Setup) != 0 {
		t.Errorf("config = %+v, %v", cfg, err)
	}

	code, out, errOut := h.run("--json", "check")
	if code != 0 || decodeJSON(t, out)["ok"] != true {
		t.Errorf("check after init: exit %d\n%s%s", code, out, errOut)
	}
	code, _, errOut = h.run("init")
	if code != ExitRefused || !strings.Contains(errOut, "already exists") {
		t.Errorf("second init: exit %d %s", code, errOut)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(b) != string(gi) {
		t.Error("second init changed .gitignore")
	}
	if st := git(t, dir, "log", "--oneline", "--all"); st != "" {
		t.Errorf("init committed: %s", st)
	}
}

func TestInitOutsideGitRefused(t *testing.T) {
	gitEnv(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	h := &harness{t: t, root: dir, now: newHarness(t).now}
	code, _, errOut := h.run("init")
	if code != ExitRefused || !strings.Contains(errOut, "not inside a git repository") {
		t.Errorf("exit %d %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, ".ekumen")); err == nil {
		t.Error("created .ekumen outside a repository")
	}
}

// TestAgentDefaultsAreConfigDefaults documents the only agent tool name in
// internal/: the default `ekumen init` writes into config.yaml (spine §5
// rule 12). `task start` reads agents from the config file alone.
func TestAgentDefaultsAreConfigDefaults(t *testing.T) {
	if project.DefaultAgentName != "claude-code" || project.DefaultAgentCommand != "claude" {
		t.Errorf("defaults changed: %s %s", project.DefaultAgentName, project.DefaultAgentCommand)
	}
	cfg := string(project.ConfigYAML("p", ""))
	if !strings.Contains(cfg, "default: claude-code") || !strings.Contains(cfg, "claude-code: {command: claude}") {
		t.Errorf("config template:\n%s", cfg)
	}
}
