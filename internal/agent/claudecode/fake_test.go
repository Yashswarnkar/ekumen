package claudecode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as a fake `claude`: with fakeEnv set it logs its
// argv and working directory to <dir>/argv.jsonl and replays files from
// <dir>, so no test needs the real binary or the network.
const fakeEnv = "EKUMEN_FAKE_CLAUDE_DIR"

func TestMain(m *testing.M) {
	if dir := os.Getenv(fakeEnv); dir != "" {
		os.Exit(fakeClaude(dir, os.Args[1:]))
	}
	os.Exit(m.Run())
}

type call struct {
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd"`
}

func fakeClaude(dir string, args []string) int {
	wd, _ := os.Getwd()
	b, _ := json.Marshal(call{Argv: args, Cwd: wd})
	f, err := os.OpenFile(filepath.Join(dir, "argv.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 70
	}
	fmt.Fprintf(f, "%s\n", b)
	f.Close()
	if len(args) == 0 {
		return 64
	}
	switch {
	case args[0] == "--version":
		return replay(dir, "version")
	case args[0] == "agents":
		return replay(dir, "agents")
	}
	return fakeActuate(dir, args)
}

// replay prints <dir>/<name>.out and <name>.err, sleeps <name>.sleep and
// exits with <name>.exit (default 0).
func replay(dir, name string) int {
	read := func(ext string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name+ext))
		return string(b)
	}
	if d, err := time.ParseDuration(strings.TrimSpace(read(".sleep"))); err == nil {
		time.Sleep(d)
	}
	fmt.Fprint(os.Stdout, read(".out"))
	fmt.Fprint(os.Stderr, read(".err"))
	code, _ := strconv.Atoi(strings.TrimSpace(read(".exit")))
	return code
}

func setFake(t *testing.T, dir, name, ext, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+ext), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "2.1.329", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type env struct {
	a     *Adapter
	fake  string
	home  string
	state string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{fake: t.TempDir(), home: t.TempDir(), state: t.TempDir()}
	t.Setenv(fakeEnv, e.fake)
	t.Setenv("HOME", e.home)
	setFake(t, e.fake, "version", ".out", fixture(t, "version.txt"))
	setFake(t, e.fake, "agents", ".out", fixture(t, "agents-observed.json"))
	setFake(t, e.fake, "bg-start", "", fixture(t, "bg-start.synthetic.txt"))
	setFake(t, e.fake, "resume.tmpl", "", fixture(t, "bg-resume.synthetic.txt"))
	setFake(t, e.fake, "fork-result", "", fixture(t, "fork-result.json"))
	e.a = New(Options{Bin: os.Args[0], StateDir: e.state, Timeout: 10 * time.Second, Now: func() time.Time { return t0 }})
	return e
}

func (e *env) calls(t *testing.T) []call {
	t.Helper()
	f, err := os.Open(filepath.Join(e.fake, "argv.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []call
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var c call
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

// actuations are the calls other than --version and agents.
func (e *env) actuations(t *testing.T) []call {
	var out []call
	for _, c := range e.calls(t) {
		if len(c.Argv) > 0 && c.Argv[0] != "--version" && c.Argv[0] != "agents" {
			out = append(out, c)
		}
	}
	return out
}

func (e *env) assertHomeEmpty(t *testing.T) {
	t.Helper()
	ents, err := os.ReadDir(e.home)
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range ents {
		t.Errorf("HOME was written: %s", de.Name())
	}
}
