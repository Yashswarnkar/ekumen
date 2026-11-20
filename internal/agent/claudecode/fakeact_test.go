package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func flag(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func has(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func template(dir, name string, vars map[string]string) string {
	b, _ := os.ReadFile(filepath.Join(dir, name))
	s := string(b)
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

// fakeActuate plays --bg, --resume --bg, -p --fork-session and respawn.
// A <op>.exit file makes it replay <op>.* instead of succeeding.
func fakeActuate(dir string, args []string) int {
	switch {
	case has(args, "--bg") && !has(args, "--resume"):
		if exists(dir, "start.exit") || exists(dir, "start.out") {
			return replay(dir, "start")
		}
		name := flag(args, "--name")
		id := uuidV5("fake:" + name)
		var es []map[string]any
		b, _ := os.ReadFile(filepath.Join(dir, "agents.out"))
		json.Unmarshal(b, &es)
		wd, _ := os.Getwd()
		es = append(es, map[string]any{"cwd": wd, "id": id[:8], "kind": "background", "name": name, "sessionId": id, "startedAt": 1795170000000, "state": "working", "pid": 4242, "status": "busy"})
		b, _ = json.Marshal(es)
		os.WriteFile(filepath.Join(dir, "agents.out"), b, 0o600)
		fmt.Print(template(dir, "bg-start", map[string]string{"short": id[:8], "name": name}))
		return 0
	case has(args, "--bg"):
		if exists(dir, "resume.exit") || exists(dir, "resume.sleep") {
			return replay(dir, "resume")
		}
		id := flag(args, "--resume")
		fmt.Print(template(dir, "resume.tmpl", map[string]string{"short": id[:8], "copy": "c0c0c0c0"}))
		return 0
	case has(args, "--fork-session"):
		if exists(dir, "fork.exit") {
			return replay(dir, "fork")
		}
		b, _ := os.ReadFile(filepath.Join(dir, "fork-result"))
		fmt.Print(strings.Replace(string(b), "11111111-1111-4111-8111-000000000002", flag(args, "--session-id"), 1))
		return 0
	case len(args) > 0 && args[0] == "respawn":
		return replay(dir, "respawn")
	}
	return 64
}
