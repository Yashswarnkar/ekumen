package core

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// The fixture log and the expected result. Tests embed them so this package
// and its tests never import os (brief: no os, io/fs, net, os/exec).
//
// To refresh state.golden.json after an intended change, run
// `go test ./internal/core -run TestGolden -v` and copy the "got" output.

//go:embed testdata/log.jsonl
var fixtureLog []byte

//go:embed testdata/state.golden.json
var fixtureGolden []byte

func loadFixture(t *testing.T) []event.Event {
	t.Helper()
	var out []event.Event
	for line := range bytes.SplitSeq(bytes.TrimSpace(fixtureLog), []byte("\n")) {
		e, err := event.Decode(line)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

// goldenView is everything the core derives from the fixture.
type goldenView struct {
	State     State                 `json:"state"`
	Statuses  map[string]TaskStatus `json:"statuses"`
	LeadInbox []InboxItem           `json:"lead_inbox"`
	OrchInbox []InboxItem           `json:"orchestrator_inbox"`
	Routes    []RoutedToOwner       `json:"routes_to_write"`
	Conflicts []Conflict            `json:"conflicts"`
}

func view(events []event.Event) goldenView {
	s := Reduce(events)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	obs := Observed{Tasks: map[string]TaskObservation{"T-core": {Session: SessionRunning}}}
	return goldenView{
		State:     s,
		Statuses:  TaskStatuses(&s, obs),
		LeadInbox: Inbox(&s, "lead", now),
		OrchInbox: Inbox(&s, "orchestrator", now),
		Routes:    Route(&s, events),
		Conflicts: Conflicts(&s, events, ConflictInputs{TextSHA256: map[string]string{".ekumen/contracts/C-event-log.md": "aaa2"}}),
	}
}

func TestGolden_FixtureLog(t *testing.T) {
	got, err := json.MarshalIndent(view(loadFixture(t)), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(fixtureGolden)) {
		t.Errorf("state differs from testdata/state.golden.json; got:\n%s", got)
	}
}

// Brief job 4: reducing the same events in any input order gives the same
// state.
func TestReduceOrderIndependence(t *testing.T) {
	events := loadFixture(t)
	want := view(events)
	r := rand.New(rand.NewPCG(1, 2))
	for i := range 50 {
		shuffled := append([]event.Event(nil), events...)
		r.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		if i%2 == 1 { // duplicates (e.g. the same file seen twice) change nothing
			shuffled = append(shuffled, shuffled[:5]...)
		}
		if got := view(shuffled); !reflect.DeepEqual(got, want) {
			t.Fatalf("shuffle %d gave a different result", i)
		}
	}
}
