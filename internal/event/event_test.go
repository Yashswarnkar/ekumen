package event

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// C-event-log §1: slug ids.
func TestValidateID_EventLog1(t *testing.T) {
	tests := []struct {
		id string
		ok bool
	}{
		{"T-core", true},
		{"D-control-loop", true},
		{"C-event-log", true},
		{"X-sandbox-creds", true},
		{"T-a-b-c-d-e", true},                   // 5 words
		{"T-a-b-c-d-e-f", false},                // 6 words
		{"R-core", false},                       // unknown kind letter
		{"t-core", false},                       // lowercase kind letter
		{"T-Core", false},                       // uppercase slug
		{"T-core_x", false},                     // underscore
		{"T--core", false},                      // empty word
		{"T-core-", false},                      // trailing hyphen
		{"T-", false},                           // empty slug
		{"T0", false},                           // an alias, not an id
		{"D-012", true},                         // matches the regex (digits are a slug)
		{"T-" + strings.Repeat("a", 38), true},  // 40 in total
		{"T-" + strings.Repeat("a", 39), false}, // 41 in total
	}
	for _, tt := range tests {
		err := ValidateID(tt.id)
		if (err == nil) != tt.ok {
			t.Errorf("ValidateID(%q) = %v, want ok=%v", tt.id, err, tt.ok)
		}
	}
}

func TestKindOf_EventLog1(t *testing.T) {
	for id, want := range map[string]SubjectKind{
		"T-core": KindTask, "D-x": KindDecision, "C-x": KindContract, "X-x": KindExternal, "R1": "", "Q-x": "",
	} {
		if got := KindOf(id); got != want {
			t.Errorf("KindOf(%q) = %q, want %q", id, got, want)
		}
	}
	if err := ValidateIDOfKind("T-core", KindDecision); err == nil {
		t.Error("ValidateIDOfKind accepted a task id as a decision")
	}
}

// C-event-log §1, "Aliases".
func TestValidateAlias_EventLog1Aliases(t *testing.T) {
	for alias, ok := range map[string]bool{
		"R1": true, "T0": true, "C1": true, "D-012": false /* is a valid slug id */, "": false,
		"1R": false, "T 0": false, "a--b": false,
	} {
		if err := ValidateAlias(alias); (err == nil) != ok {
			t.Errorf("ValidateAlias(%q) = %v, want ok=%v", alias, err, ok)
		}
	}
}

// C-event-log §1, "Events use a ULID". Vector from the ULID spec.
func TestULIDEncoding_EventLog1(t *testing.T) {
	id, err := ULIDFromParts(1469918176385, [10]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if want := "01ARYZ6S410000000000000000"; id != want {
		t.Errorf("got %s, want %s", id, want)
	}
	var max [10]byte
	for i := range max {
		max[i] = 0xff
	}
	id, _ = ULIDFromParts(maxULIDTime, max)
	if id != "7ZZZZZZZZZZZZZZZZZZZZZZZZZ" {
		t.Errorf("max ulid = %s", id)
	}
	if _, err := ULIDFromParts(maxULIDTime+1, max); err == nil {
		t.Error("accepted an out-of-range timestamp")
	}
	ts, err := ULIDTime("01ARYZ6S410000000000000000")
	if err != nil || ts.UnixMilli() != 1469918176385 {
		t.Errorf("ULIDTime = %v, %v", ts, err)
	}
}

func TestValidateULID_EventLog1(t *testing.T) {
	for s, ok := range map[string]bool{
		"01JD8Z3K6Q4M2V7X9T1B5N0RCE":  true,
		"01JD8Z3K6Q4M2V7X9T1B5N0RC":   false, // short
		"01JD8Z3K6Q4M2V7X9T1B5N0RCEX": false, // long
		"01JD8Z3K6Q4M2V7X9T1B5N0RCI":  false, // I is not Crockford
		"01jd8z3k6q4m2v7x9t1b5n0rce":  false, // not canonical
		"81JD8Z3K6Q4M2V7X9T1B5N0RCE":  false, // overflow
	} {
		if err := ValidateULID(s); (err == nil) != ok {
			t.Errorf("ValidateULID(%q) = %v, want ok=%v", s, err, ok)
		}
	}
}

func TestULIDGenerator_SortsByTime(t *testing.T) {
	n := byte(0)
	g := NewULIDGenerator(func(b []byte) error {
		for i := range b {
			b[i] = 0
		}
		b[len(b)-1] = n
		n += 200
		return nil
	})
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	var prev string
	for i := range 5 {
		ts := at
		if i >= 3 {
			ts = at.Add(time.Millisecond)
		}
		id, err := g.New(ts)
		if err != nil {
			t.Fatal(err)
		}
		if ValidateULID(id) != nil || id <= prev {
			t.Fatalf("id %d %s not after %s", i, id, prev)
		}
		prev = id
	}
	if _, err := NewULID(at); err != nil {
		t.Fatal(err)
	}
	failing := NewULIDGenerator(func([]byte) error { return errors.New("no entropy") })
	if _, err := failing.New(at); err == nil {
		t.Error("expected entropy error")
	}
}

// C-event-log §2 (envelope) and §9 (unknown fields are kept).
func TestEnvelopeRoundTripKeepsUnknownFields_EventLog2(t *testing.T) {
	in := `{"schema":1,"id":"01JD8Z3K6Q4M2V7X9T1B5N0RCE","type":"TaskDeferred","at":"2026-11-20T09:14:03Z",` +
		`"actor":{"role":"orchestrator","id":"orch"},"subject":{"kind":"task","id":"T-core"},` +
		`"data":{"reason":"later","future_field":[1,2]},"trace":"abc"}`
	e, err := Decode([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if string(e.Extra["trace"]) != `"abc"` {
		t.Errorf("envelope extra lost: %v", e.Extra)
	}
	p, err := e.Payload()
	if err != nil || p.(*TaskDeferred).Reason != "later" {
		t.Fatalf("Payload = %#v, %v", p, err)
	}
	unk, _ := e.UnknownDataFields()
	if len(unk) != 1 || unk[0] != "future_field" {
		t.Errorf("UnknownDataFields = %v", unk)
	}
	out, err := Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	_ = json.Unmarshal([]byte(in), &a)
	_ = json.Unmarshal(out, &b)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Errorf("round trip changed the event:\n in %s\nout %s", ja, jb)
	}
}

func TestNewAndValidate_EventLog2(t *testing.T) {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.FixedZone("IST", 19800))
	e, err := New("01JD8Z3K6Q4M2V7X9T1B5N0RCE", TaskDeferredType, at,
		Actor{RoleOrchestrator, "orch"}, Subject{KindTask, "T-core"}, TaskDeferred{Reason: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Validate(); err != nil {
		t.Errorf("New gave an invalid event: %v", err)
	}
	if _, off := e.At.Zone(); off != 0 {
		t.Error("New did not convert to UTC")
	}

	bad := []struct {
		name  string
		edit  func(*Event)
		field string
	}{
		{"schema", func(e *Event) { e.Schema = 2 }, "schema"},
		{"id", func(e *Event) { e.ID = "nope" }, "id"},
		{"type", func(e *Event) { e.Type = "Nope" }, "type"},
		{"at missing", func(e *Event) { e.At = time.Time{} }, "at"},
		{"at not utc", func(e *Event) { e.At = at }, "at"},
		{"role", func(e *Event) { e.Actor.Role = "boss" }, "actor.role"},
		{"actor id", func(e *Event) { e.Actor.ID = "" }, "actor.id"},
		{"subject kind", func(e *Event) { e.Subject.Kind = "thing" }, "subject.kind"},
		{"subject id kind", func(e *Event) { e.Subject.ID = "D-core" }, "subject.id"},
		{"project id", func(e *Event) { e.Subject = Subject{KindProject, "x"} }, "subject.id"},
		{"data", func(e *Event) { e.Data = json.RawMessage(`{"reason":5}`) }, "data"},
	}
	for _, tt := range bad {
		c := e
		tt.edit(&c)
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), tt.field+":") {
			t.Errorf("%s: Validate = %v, want an error naming %s", tt.name, err, tt.field)
		}
	}
}

// C-event-log §6: every v1 type has a typed payload.
func TestEveryTypeHasPayload_EventLog6(t *testing.T) {
	if len(Types) != 25 {
		t.Errorf("C-event-log §6 lists 25 types, Types has %d", len(Types))
	}
	for _, typ := range Types {
		if !typ.Known() || NewPayload(typ) == nil {
			t.Errorf("%s: no payload", typ)
		}
		e := Event{ID: "01JD8Z3K6Q4M2V7X9T1B5N0RCE", Type: typ, Data: json.RawMessage(`{}`)}
		if _, err := e.Payload(); err != nil {
			t.Errorf("%s: %v", typ, err)
		}
	}
	e := Event{Type: "FutureThing"}
	if _, err := e.Payload(); !errors.Is(err, ErrUnknownType) {
		t.Errorf("unknown type: %v", err)
	}
}

func TestDecisionRaisedDistinguishesMissingFromEmpty(t *testing.T) {
	var d DecisionRaised
	_ = json.Unmarshal([]byte(`{"blocks":[]}`), &d)
	if d.Blocks == nil || d.ContractImpact != nil {
		t.Errorf("blocks=%#v contract_impact=%#v", d.Blocks, d.ContractImpact)
	}
	keys, err := RawDataKeys(Event{Data: json.RawMessage(`{"a":1,"b":2}`)})
	if err != nil || len(keys) != 2 {
		t.Errorf("RawDataKeys = %v, %v", keys, err)
	}
}
