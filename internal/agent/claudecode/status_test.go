package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/agent"
)

var t0 = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func loadAgents(t *testing.T, name string) []entry {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "2.1.329", name))
	if err != nil {
		t.Fatal(err)
	}
	es, err := parseAgents(b)
	if err != nil {
		t.Fatal(err)
	}
	return es
}

func byName(t *testing.T, es []entry, name string) *entry {
	t.Helper()
	e := findByName(es, name)
	if e == nil {
		t.Fatalf("fixture has no %q", name)
	}
	return e
}

func hook(t *testing.T, name string, at time.Duration) hookRecord {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "2.1.329", "hooks", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := parseHook(b)
	if err != nil {
		t.Fatal(err)
	}
	h.At = t0.Add(at)
	return h
}

type want struct{ state, waitingFor, detail, source string }

func check(t *testing.T, got agent.Status, w want) {
	t.Helper()
	if got.State != w.state || got.WaitingFor != w.waitingFor || got.Source != w.source || !strings.Contains(got.Detail, w.detail) {
		t.Errorf("got %+v, want %+v", got, w)
	}
	if got.ObservedAt.IsZero() {
		t.Errorf("ObservedAt is zero: %+v", got)
	}
}

func TestMapStatus_ListingRows(t *testing.T) {
	es := append(loadAgents(t, "agents-documented.json"), loadAgents(t, "agents-observed.json")...)
	now := t0.Add(time.Hour)
	cases := []struct {
		name string
		want want
	}{
		{"working-busy", want{agent.StateRunning, "", "", SourceListing}},
		{"working", want{agent.StateRunning, "", "", SourceListing}},
		{"term-busy", want{agent.StateRunning, "", "", SourceListing}},
		{"wait-permission", want{agent.StateNeedsInput, agent.WaitPermission, Unverified, SourceListing}},
		{"term-wait", want{agent.StateNeedsInput, agent.WaitPermission, Unverified, SourceListing}},
		{"wait-input", want{agent.StateNeedsInput, agent.WaitQuestion, Unverified, SourceListing}},
		{"wait-sandbox", want{agent.StateNeedsInput, agent.WaitOther, "sandbox request", SourceListing}},
		{"wait-worker", want{agent.StateNeedsInput, agent.WaitOther, "worker request", SourceListing}},
		{"wait-dialog", want{agent.StateNeedsInput, agent.WaitOther, "dialog open", SourceListing}},
		{"unknown-waiting", want{agent.StateNeedsInput, agent.WaitOther, "coffee", SourceListing}},
		{"term-idle", want{agent.StateIdle, "", "", SourceListing}},
		{"done-idle", want{agent.StateIdle, "", "", SourceListing}},
		{"done", want{agent.StateIdle, "", "", SourceListing}},
		{"bg-blocked", want{agent.StateNeedsInput, agent.WaitOther, Unverified + "; blocked", SourceListing}},
		{"bg-failed", want{agent.StateDone, "", "failed", SourceListing}},
		{"stopped", want{agent.StateDone, "", "stopped", SourceListing}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			check(t, mapStatus(byName(t, es, c.name), nil, now), c.want)
		})
	}
}

func TestMapStatus_UnknownListingValues_CAdapter1UnknownNeverGuess(t *testing.T) {
	es := loadAgents(t, "agents-documented.json")
	for _, name := range []string{"unknown-kind", "unknown-status", "unknown-state"} {
		got := mapStatus(byName(t, es, name), nil, t0)
		check(t, got, want{agent.StateUnknown, "", "unrecognised listing values", SourceListing})
	}
	got := mapStatus(&entry{Kind: "interactive"}, nil, t0)
	check(t, got, want{agent.StateUnknown, "", "", SourceListing})
}

func TestMapStatus_NoData_CAdapter1UnknownNeverGuess(t *testing.T) {
	check(t, mapStatus(nil, nil, t0), want{agent.StateUnknown, "", "not listed", SourceNone})
}

func TestMapStatus_HookRows(t *testing.T) {
	live := &entry{Kind: "background", State: "working", Status: "busy"}
	cases := []struct {
		hook string
		want want
	}{
		{"PermissionRequest", want{agent.StateNeedsInput, agent.WaitPermission, "Bash", SourceHooks}},
		{"Notification-permission_prompt", want{agent.StateNeedsInput, agent.WaitPermission, "", SourceHooks}},
		{"PreToolUse-AskUserQuestion", want{agent.StateNeedsInput, agent.WaitQuestion, "", SourceHooks}},
		{"Notification-agent_needs_input", want{agent.StateNeedsInput, agent.WaitQuestion, "", SourceHooks}},
		{"Notification-elicitation_dialog", want{agent.StateNeedsInput, agent.WaitOther, "elicitation_dialog", SourceHooks}},
	}
	for _, c := range cases {
		t.Run(c.hook, func(t *testing.T) {
			check(t, mapStatus(live, []hookRecord{hook(t, c.hook, 0)}, t0.Add(10*time.Second)), c.want)
		})
	}
	for name, w := range map[string]string{"UserPromptSubmit": agent.StateRunning, "Stop": agent.StateIdle, "StopFailure": agent.StateDone, "SessionEnd": agent.StateDone} {
		s, ok := hookSignal(hook(t, name, 0))
		if !ok || s.state != w {
			t.Errorf("hook %s: %+v %v, want %s", name, s, ok, w)
		}
	}
	for _, name := range []string{"SessionStart", "Notification-idle_prompt"} {
		if _, ok := hookSignal(hook(t, name, 0)); ok {
			t.Errorf("hook %s should say nothing about status", name)
		}
	}
}

func TestMapStatus_FreshHookNeedsInputBeatsBusyListing_FailTowardAttention(t *testing.T) {
	busy := &entry{Kind: "interactive", Status: "busy"}
	hs := []hookRecord{hook(t, "UserPromptSubmit", 0), hook(t, "PermissionRequest", time.Second)}
	check(t, mapStatus(busy, hs, t0.Add(30*time.Second)), want{agent.StateNeedsInput, agent.WaitPermission, "Bash", SourceHooks})
}

// A permission prompt answered in `claude attach` fires no hook, so the
// needs_input record stays the newest hook while the session runs on.
func TestMapStatus_ListingLivenessBeatsStaleHookNeedsInput_Issue32Item4(t *testing.T) {
	hs := []hookRecord{hook(t, "UserPromptSubmit", 0), hook(t, "PermissionRequest", time.Second)}
	at := t0.Add(time.Second + hookGrace)
	check(t, mapStatus(&entry{Kind: "interactive", Status: "busy"}, hs, at), want{agent.StateRunning, "", "", SourceListing})
	check(t, mapStatus(&entry{Kind: "background", State: "working"}, hs, at), want{agent.StateRunning, "", "", SourceListing})
	check(t, mapStatus(&entry{Kind: "background", State: "done", Status: "idle"}, hs, at), want{agent.StateIdle, "", "", SourceListing})
	// a waiting listing still takes the hook's reason, however old
	check(t, mapStatus(&entry{Kind: "background", State: "blocked"}, hs, t0.Add(time.Hour)), want{agent.StateNeedsInput, agent.WaitPermission, "Bash", SourceHooks})
}

func TestMapStatus_ListingLivenessBeatsStaleHook(t *testing.T) {
	hs := []hookRecord{hook(t, "PermissionRequest", 0), hook(t, "Stop", time.Second)}
	check(t, mapStatus(&entry{Kind: "interactive", Status: "busy"}, hs, t0.Add(time.Hour)), want{agent.StateRunning, "", "", SourceListing})
	hs = []hookRecord{hook(t, "UserPromptSubmit", 0)}
	check(t, mapStatus(&entry{Kind: "interactive", Status: "idle"}, hs, t0.Add(time.Hour)), want{agent.StateIdle, "", "", SourceListing})
	hs = []hookRecord{hook(t, "PermissionRequest", 0)}
	check(t, mapStatus(&entry{Kind: "background", State: "stopped"}, hs, t0.Add(time.Hour)), want{agent.StateDone, "", "stopped", SourceListing})
}

func TestMapStatus_HookReasonRefinesBlockedListing(t *testing.T) {
	blocked := &entry{Kind: "background", State: "blocked"}
	hs := []hookRecord{hook(t, "PreToolUse-AskUserQuestion", 0)}
	check(t, mapStatus(blocked, hs, t0.Add(time.Minute)), want{agent.StateNeedsInput, agent.WaitQuestion, "", SourceHooks})
}

func TestMapStatus_PermissionTwinsDeduped_R2Lesson2(t *testing.T) {
	live := &entry{Kind: "interactive", Status: "busy"}
	for _, order := range [][]string{{"PermissionRequest", "Notification-permission_prompt"}, {"Notification-permission_prompt", "PermissionRequest"}} {
		hs := []hookRecord{hook(t, order[0], 0), hook(t, order[1], time.Millisecond)}
		check(t, mapStatus(live, hs, t0.Add(10*time.Second)), want{agent.StateNeedsInput, agent.WaitPermission, "Bash", SourceHooks})
	}
}

func TestMapStatus_NotListed(t *testing.T) {
	check(t, mapStatus(nil, []hookRecord{hook(t, "SessionEnd", 0)}, t0.Add(time.Minute)), want{agent.StateDone, "", "SessionEnd", SourceHooks})
	check(t, mapStatus(nil, []hookRecord{hook(t, "UserPromptSubmit", 0)}, t0.Add(time.Minute)), want{agent.StateUnknown, "", "last hook said running", SourceHooks})
}

func TestReadHooks_BadRecordsSkipped_Issue32Item8(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string, at time.Time) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write("r.1", `{"hook_event_name":"UserPromptSubmit","session_id":"x"}`, t0)
	write("r.2", `{"hook_event_name":"Stop"`, t0.Add(time.Second))
	write("r.3", `{"session_id":"x"}`, t0.Add(2*time.Second))
	write("r.4", `{"hook_event_name":"Stop","session_id":7}`, t0.Add(3*time.Second))
	write("r.5", `{"hook_event_name":"Stop","session_id":"x","message":"`+strings.Repeat("a", maxHookBytes)+`"}`, t0.Add(4*time.Second))
	hs, err := readHooks(dir, "x")
	if err != nil || len(hs) != 1 || hs[0].Event != "UserPromptSubmit" {
		t.Errorf("records %+v, %v; want only the good one", hs, err)
	}
	if hs, err := readHooks(filepath.Join(dir, "missing"), "x"); err != nil || hs != nil {
		t.Errorf("missing dir: %v %v", hs, err)
	}
}

func TestReadHooks_PrunesOldest(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < maxHookRecords+5; i++ {
		p := filepath.Join(dir, fmt.Sprintf("r.%04d", i))
		if err := os.WriteFile(p, []byte(`{"hook_event_name":"Stop","session_id":"x"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		at := t0.Add(time.Duration(i) * time.Second)
		os.Chtimes(p, at, at)
	}
	hs, err := readHooks(dir, "x")
	if err != nil || len(hs) != maxHookRecords || hs[0].file != "r.0005" {
		t.Fatalf("%d records, first %+v, %v", len(hs), hs, err)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != maxHookRecords {
		t.Errorf("%d files left, want %d", len(ents), maxHookRecords)
	}
}

func TestParseAgents_OneBadEntryOnlyBlanksItself_Issue32Item6(t *testing.T) {
	es, err := parseAgents([]byte(`[
		{"kind":"interactive","sessionId":"a1","status":"busy","cwd":"/w","startedAt":1795170000000},
		{"kind":"interactive","sessionId":"a2","status":7,"cwd":"/w"},
		{"kind":"background","id":"a3a3a3a3","state":"working","startedAt":1795170000}
	]`))
	if err != nil || len(es) != 3 {
		t.Fatalf("%d entries, %v", len(es), err)
	}
	check(t, mapStatus(find(es, "a1"), nil, t0), want{agent.StateRunning, "", "", SourceListing})
	check(t, mapStatus(find(es, "a2"), nil, t0), want{agent.StateUnknown, "", "unreadable listing entry", SourceListing})
	check(t, mapStatus(find(es, "a3a3a3a3"), nil, t0), want{agent.StateRunning, "", "", SourceListing})
	if ms, s := es[0].started(), es[2].started(); !ms.Equal(s) || ms.Year() != 2026 {
		t.Errorf("startedAt ms %v and s %v should agree", ms, s)
	}
	if _, err := parseAgents([]byte(`{"not":"an array"}`)); err == nil {
		t.Error("a listing that is not an array parsed")
	}
}

func TestEntryStarted_Bounds(t *testing.T) {
	for raw, ok := range map[string]bool{`1795170000000`: true, `1795170000`: true, `17951`: false, `-5`: false, `179517000000000000`: false, `"2026-11-20T12:00:00Z"`: true, `"soon"`: false} {
		if got := (entry{StartedAt: json.RawMessage(raw)}).started(); got.IsZero() == ok {
			t.Errorf("startedAt %s → %v", raw, got)
		}
	}
}

func TestReadHooks_OrderAndSessionFilter(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string, at time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	write("r.b", `{"hook_event_name":"Stop","session_id":"s1"}`, t0.Add(2*time.Second))
	write("r.a", `{"hook_event_name":"UserPromptSubmit","session_id":"s1"}`, t0.Add(time.Second))
	write("r.c", `{"hook_event_name":"Stop","session_id":"other"}`, t0.Add(3*time.Second))
	write("settings.json", `not a record`, t0)
	hs, err := readHooks(dir, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(hs) != 2 || hs[0].Event != "UserPromptSubmit" || hs[1].Event != "Stop" {
		t.Errorf("records: %+v", hs)
	}
}
