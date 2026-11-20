package core

import (
	"slices"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

func TestSupersedeStampedBeforeBackdatedDecideAndApply(t *testing.T) {
	b := newLog(t)
	b.raise(record("D-x", "T-a", "s1", 2), "s1")
	decided := b.decide("D-x", "a")
	applied := b.apply("D-x")
	sup := b.add(event.DecisionSupersededType, event.RoleLead, "lead", event.KindDecision, "D-x", event.DecisionSuperseded{By: "D-y"})
	b.events[1] = restamp(t, decided, t0.Add(10*time.Minute))
	b.events[2] = restamp(t, applied, t0.Add(10*time.Minute+time.Second))
	b.events[3] = restamp(t, sup, t0.Add(5*time.Minute))
	s := b.state()
	d := s.Decisions["D-x"]
	if d.Status != StatusSuperseded || d.DecidedEvent == "" || d.AppliedOn == "" {
		t.Fatalf("status=%s decided_event=%q applied_on=%q", d.Status, d.DecidedEvent, d.AppliedOn)
	}
	if c := OrphanStepConflicts(&s, b.events); len(c) != 0 {
		t.Fatalf("orphan-step: %v", c)
	}
	var types []event.Type
	for _, e := range SortEvents(b.events) {
		types = append(types, e.Type)
	}
	want := []event.Type{event.DecisionRaisedType, event.DecisionDecidedType, event.DecisionAppliedType, event.DecisionSupersededType}
	if !slices.Equal(types, want) {
		t.Fatalf("order %v, want %v", types, want)
	}
}
