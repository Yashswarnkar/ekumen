package claudecode

import (
	"encoding/json"
	"fmt"
	"time"
)

// entry is one element of `claude agents --json`
// (https://code.claude.com/docs/en/agent-view). startedAt was observed as
// epoch milliseconds on 2.1.329; epoch seconds and an RFC 3339 string are
// accepted too. bad is set when the element did not decode; its status is
// unknown.
type entry struct {
	ID         string          `json:"id"`
	SessionID  string          `json:"sessionId"`
	Name       string          `json:"name"`
	Cwd        string          `json:"cwd"`
	Kind       string          `json:"kind"`
	State      string          `json:"state"`
	Status     string          `json:"status"`
	WaitingFor string          `json:"waitingFor"`
	StartedAt  json.RawMessage `json:"startedAt"`
	bad        string
}

// parseAgents decodes each element on its own, so one mistyped entry makes
// only that session unknown (#32 item 6). A listing that is not a JSON
// array is an error.
func parseAgents(b []byte) ([]entry, error) {
	var raws []json.RawMessage
	if err := json.Unmarshal(b, &raws); err != nil {
		return nil, fmt.Errorf("agents --json: %w", err)
	}
	es := make([]entry, 0, len(raws))
	for _, r := range raws {
		var e entry
		if err := json.Unmarshal(r, &e); err != nil {
			e.bad = err.Error()
		}
		es = append(es, e)
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

// started reads startedAt. A number is epoch milliseconds or seconds,
// told apart by size; one outside 2001..5138 in either unit is no time.
func (e entry) started() time.Time {
	var n int64
	if json.Unmarshal(e.StartedAt, &n) == nil {
		switch {
		case n >= 1e12 && n < 1e14:
			return time.UnixMilli(n).UTC()
		case n >= 1e9 && n < 1e11:
			return time.Unix(n, 0).UTC()
		}
		return time.Time{}
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
