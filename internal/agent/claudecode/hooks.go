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

// readHooks returns the records for session id in dir, oldest first. A
// missing dir is no data; an unreadable record is an error, which the
// caller turns into unknown.
func readHooks(dir, id string) ([]hookRecord, error) {
	ents, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []hookRecord
	for _, de := range ents {
		if de.IsDir() || !strings.HasPrefix(de.Name(), "r.") {
			continue
		}
		p := filepath.Join(dir, de.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		info, err := de.Info()
		if err != nil {
			return nil, err
		}
		h, err := parseHook(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", de.Name(), err)
		}
		if h.SessionID != "" && h.SessionID != id {
			continue
		}
		h.At, h.file = info.ModTime(), de.Name()
		out = append(out, h)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].file < out[j].file
	})
	return out, nil
}
