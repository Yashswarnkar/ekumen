package claudecode

import (
	"encoding/json"
	"fmt"
	"time"
)

// entry is one element of `claude agents --json`
// (https://code.claude.com/docs/en/agent-view). startedAt was observed as
// epoch milliseconds on 2.1.329; an RFC 3339 string is accepted too.
type entry struct {
	ID         string          `json:"id"`
	SessionID  string          `json:"sessionId"`
	Name       string          `json:"name"`
	Cwd        string          `json:"cwd"`
	Kind       string          `json:"kind"`
	State      string          `json:"state"`
	Status     string          `json:"status"`
	WaitingFor string          `json:"waitingFor"`
	PID        int             `json:"pid"`
	StartedAt  json.RawMessage `json:"startedAt"`
}

func parseAgents(b []byte) ([]entry, error) {
	var es []entry
	if err := json.Unmarshal(b, &es); err != nil {
		return nil, fmt.Errorf("agents --json: %w", err)
	}
	return es, nil
}

// ref is the ID a SessionRef carries: the full session UUID when listed,
// else the background short id.
func (e entry) ref() string {
	if e.SessionID != "" {
		return e.SessionID
	}
	return e.ID
}

func (e entry) started() time.Time {
	var ms int64
	if json.Unmarshal(e.StartedAt, &ms) == nil {
		return time.UnixMilli(ms).UTC()
	}
	var s string
	if json.Unmarshal(e.StartedAt, &s) == nil {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func find(es []entry, id string) *entry {
	if id == "" {
		return nil
	}
	for i := range es {
		if es[i].SessionID == id || es[i].ID == id {
			return &es[i]
		}
	}
	return nil
}

func findByName(es []entry, name string) *entry {
	for i := range es {
		if es[i].Name == name {
			return &es[i]
		}
	}
	return nil
}
