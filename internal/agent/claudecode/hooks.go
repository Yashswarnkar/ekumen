package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// hookRecord is one hook input (https://code.claude.com/docs/en/hooks), as
// the per-session hook command wrote it: one file per record, timed by the
// file's modification time.
type hookRecord struct {
	Event            string `json:"hook_event_name"`
	SessionID        string `json:"session_id"`
	NotificationType string `json:"notification_type"`
	ToolName         string `json:"tool_name"`
	Message          string `json:"message"`
	Source           string `json:"source"`
	Reason           string `json:"reason"`
	At               time.Time
	file             string
}

func parseHook(b []byte) (hookRecord, error) {
	var h hookRecord
	if err := json.Unmarshal(b, &h); err != nil {
		return h, err
	}
	if h.Event == "" {
		return h, fmt.Errorf("no hook_event_name")
	}
	return h, nil
}

const (
	maxHookBytes   = 256 << 10 // larger records are skipped, not read
	maxHookRecords = 256       // older records are pruned
)

// readHooks returns the records for session id in dir, oldest first. A
// missing dir is no data. One bad record never poisons the rest (#32 item
// 8): records that are too large, unreadable, not JSON, for another
// session, or without an event name are skipped. Beyond the newest
// maxHookRecords, older records are deleted.
func readHooks(dir, id string) ([]hookRecord, error) {
	ents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	type file struct {
		name string
		at   time.Time
		size int64
	}
	var fs []file
	for _, de := range ents {
		if de.IsDir() || !strings.HasPrefix(de.Name(), "r.") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		fs = append(fs, file{de.Name(), info.ModTime(), info.Size()})
	}
	sort.Slice(fs, func(i, j int) bool {
		if !fs[i].at.Equal(fs[j].at) {
			return fs[i].at.Before(fs[j].at)
		}
		return fs[i].name < fs[j].name
	})
	if n := len(fs) - maxHookRecords; n > 0 {
		for _, f := range fs[:n] {
			os.Remove(filepath.Join(dir, f.name))
		}
		fs = fs[n:]
	}
	var out []hookRecord
	for _, f := range fs {
		if f.size > maxHookBytes {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil {
			continue
		}
		var sid struct {
			SessionID string `json:"session_id"`
		}
		if json.Unmarshal(b, &sid) != nil || (sid.SessionID != "" && sid.SessionID != id) {
			continue
		}
		h, err := parseHook(b)
		if err != nil {
			continue
		}
		h.At, h.file = f.at, f.name
		out = append(out, h)
	}
	return out, nil
}
