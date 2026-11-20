package core

import (
	"bytes"
	_ "embed"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// The repository's own event log at 1ac0c45, the base of T-reconcile.
//
//go:embed testdata/reallog-1ac0c45.jsonl
var realLog []byte

// The first pass over the real log: 20 decided decisions applied (none
// blocks a task), 13 handoffs routed, no defaults, no conflicts; a second
// pass plans nothing.
func TestPlanRealLog_DControlLoop(t *testing.T) {
	var events []event.Event
	for line := range bytes.SplitSeq(bytes.TrimSpace(realLog), []byte("\n")) {
		e, err := event.Decode(line)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, e)
	}
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	p := planAt(t, events, now)
	count := map[string]int{}
	for _, a := range p.Actions {
		count[a.Kind]++
		if a.Kind == LoopApply && len(a.Events) != 1 {
			t.Errorf("%s writes %d events, want DecisionApplied only", a.Subject.ID, len(a.Events))
		}
	}
	if count[LoopApply] != 20 || count[LoopRoute] != 13 || count[LoopDefault] != 0 || count[LoopConflict] != 0 ||
		len(p.Held) != 0 || len(p.Skipped) != 0 || len(p.Unblocked) != 0 {
		t.Errorf("plan %v held %v skipped %v unblocked %v", count, p.Held, p.Skipped, p.Unblocked)
	}
	closed(t, events, p, now)
}
