package claudecode

import (
	"encoding/json"
	"os"
	"os/user"
	"strings"
	"testing"
)

// leakMarkers are strings that must never appear in a fixture: home and
// user paths, the probe folder and the user name (#32 item 10).
func leakMarkers(dir string) []string {
	ms := []string{"/Users/", "/home/", dir}
	if home, _ := os.UserHomeDir(); home != "" {
		ms = append(ms, home)
	}
	if u, err := user.Current(); err == nil && len(u.Username) > 2 {
		ms = append(ms, u.Username)
	}
	return ms
}

// sanitizeEntry keeps only the listing fields the adapter reads, with the
// folder and name replaced.
func sanitizeEntry(e entry, name string) map[string]any {
	m := map[string]any{"cwd": "/work/probe", "name": name, "kind": e.Kind}
	for k, v := range map[string]string{"id": e.ID, "sessionId": e.SessionID, "state": e.State, "status": e.Status, "waitingFor": e.WaitingFor} {
		if v != "" {
			m[k] = v
		}
	}
	if len(e.StartedAt) > 0 {
		m["startedAt"] = json.RawMessage(e.StartedAt)
	}
	return m
}

// hookEnums are the hook fields kept as recorded; every value must be one
// of the listed ones (an empty list allows any id-shaped value). Every
// other field, free text included (message, error, prompt, tool_input,
// transcript_path, cwd, last_assistant_message), is dropped.
var hookEnums = map[string][]string{
	"hook_event_name":   {"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "Notification", "Stop", "StopFailure", "SessionEnd"},
	"notification_type": {"permission_prompt", "idle_prompt", "elicitation_dialog", "agent_needs_input", "agent_completed"},
	"source":            {"startup", "resume", "clear", "compact", "fork"},
	"permission_mode":   {"default", "plan", "acceptEdits", "auto", "dontAsk", "bypassPermissions"},
	"tool_name":         {"Bash", "Write", "Edit", "Read", "AskUserQuestion", "NotebookEdit", "WebFetch", "WebSearch", "Glob", "Grep"},
	"session_id":        nil,
}

func sanitizeHook(b []byte) (map[string]any, bool) {
	var in map[string]any
	if json.Unmarshal(b, &in) != nil {
		return nil, false
	}
	out := map[string]any{}
	for k, allowed := range hookEnums {
		v, ok := in[k].(string)
		if !ok {
			continue
		}
		switch {
		case allowed == nil && checkID(v) == nil:
			out[k] = v
		case contains(allowed, v):
			out[k] = v
		}
	}
	var dropped []string
	for k := range in {
		if _, kept := out[k]; !kept {
			dropped = append(dropped, k)
		}
	}
	if len(dropped) > 0 {
		out["_dropped"] = dropped
	}
	return out, out["hook_event_name"] != nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// TestSanitizeHook_Allowlist_Issue32Item10 feeds the sanitizer a record
// full of private text and checks none of it survives.
func TestSanitizeHook_Allowlist_Issue32Item10(t *testing.T) {
	in := `{"hook_event_name":"Notification","notification_type":"permission_prompt",
		"session_id":"aaaaaaaa-0000-4000-8000-0000000000aa",
		"transcript_path":"/Users/someone/.claude/projects/-Users-someone-work-secret/aaaaaaaa.jsonl",
		"cwd":"/Users/someone/work/secret","message":"Claude needs your permission in /home/someone",
		"error":"EACCES /Users/someone","source":"/Users/someone/free text","tool_name":"mcp__private__x",
		"tool_input":{"command":"cat ~/.ssh/id_rsa"},"prompt":"my secret prompt"}`
	out, ok := sanitizeHook([]byte(in))
	if !ok {
		t.Fatal("record dropped")
	}
	b, _ := json.Marshal(out)
	for _, leak := range []string{"someone", "secret", "/Users/", "/home/", "id_rsa", "mcp__private"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("sanitized record leaks %q: %s", leak, b)
		}
	}
	if out["notification_type"] != "permission_prompt" || out["session_id"] == nil {
		t.Errorf("kept fields lost: %s", b)
	}
	e := sanitizeEntry(entry{Kind: "background", Cwd: "/Users/someone/w", Name: "someone-task", State: "blocked", StartedAt: json.RawMessage("1795170000000")}, "ekumen-probe-x")
	if b, _ := json.Marshal(e); strings.Contains(string(b), "someone") {
		t.Errorf("sanitized entry leaks: %s", b)
	}
}
