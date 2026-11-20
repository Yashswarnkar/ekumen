// Package event defines Ekumen's event envelope, the typed payload of every
// v1 event type, ids, and JSON encoding (contract C-event-log §1, §2, §6).
//
// The package is pure: no file, network or process access.
package event

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// SchemaVersion is the envelope version this package writes (C-event-log §2).
const SchemaVersion = 1

// Type is an event type (C-event-log §6).
type Type string

// Event types (C-event-log v4 §6).
const (
	TaskDraftedType         Type = "TaskDrafted"
	TaskApprovedType        Type = "TaskApproved"
	TaskAmendedType         Type = "TaskAmended"
	TaskBlockersChangedType Type = "TaskBlockersChanged"
	TaskSessionAttachedType Type = "TaskSessionAttached"
	TaskHandoffType         Type = "TaskHandoff"
	TaskDeferredType        Type = "TaskDeferred"

	DecisionRaisedType     Type = "DecisionRaised"
	DecisionDecidedType    Type = "DecisionDecided"
	DecisionDefaultedType  Type = "DecisionDefaulted"
	DecisionSupersededType Type = "DecisionSuperseded"
	DecisionAppliedType    Type = "DecisionApplied"

	QuestionAskedType    Type = "QuestionAsked"
	QuestionAnsweredType Type = "QuestionAnswered"

	ContractDraftedType         Type = "ContractDrafted"
	ContractBlockersChangedType Type = "ContractBlockersChanged" // C-event-log v4
	ContractFrozenType          Type = "ContractFrozen"
	ContractVersionedType       Type = "ContractVersioned"

	ExternalAddedType    Type = "ExternalAdded"
	ExternalUpdatedType  Type = "ExternalUpdated"
	ExternalResolvedType Type = "ExternalResolved"

	RoutedToOwnerType    Type = "RoutedToOwner"
	RouteResolvedType    Type = "RouteResolved"
	RoleClaimedType      Type = "RoleClaimed"
	RoleReleasedType     Type = "RoleReleased"
	ConflictDetectedType Type = "ConflictDetected"
)

// Types lists every event type in the order of C-event-log v4 §6.
var Types = []Type{
	TaskDraftedType, TaskApprovedType, TaskAmendedType, TaskBlockersChangedType,
	TaskSessionAttachedType, TaskHandoffType, TaskDeferredType,
	DecisionRaisedType, DecisionDecidedType, DecisionDefaultedType,
	DecisionSupersededType, DecisionAppliedType,
	QuestionAskedType, QuestionAnsweredType,
	ContractDraftedType, ContractBlockersChangedType, ContractFrozenType, ContractVersionedType,
	ExternalAddedType, ExternalUpdatedType, ExternalResolvedType,
	RoutedToOwnerType, RouteResolvedType, RoleClaimedType, RoleReleasedType,
	ConflictDetectedType,
}

// Known reports whether t is a v1 event type. Readers ignore unknown types
// (C-event-log §9); writers refuse them.
func (t Type) Known() bool { return slices.Contains(Types, t) }

// Role is an actor role (C-event-log §2).
type Role string

// Actor roles (C-event-log §2).
const (
	RoleLead         Role = "lead"
	RoleOrchestrator Role = "orchestrator"
	RoleSession      Role = "session"
	RoleLoop         Role = "loop"
	RoleIntegrator   Role = "integrator"
)

// Roles lists every actor role.
var Roles = []Role{RoleLead, RoleOrchestrator, RoleSession, RoleLoop, RoleIntegrator}

// Known reports whether r is a role from C-event-log §2.
func (r Role) Known() bool { return slices.Contains(Roles, r) }

// SubjectKind is the kind of thing an event is about (C-event-log §2).
type SubjectKind string

// Subject kinds (C-event-log §2).
const (
	KindTask     SubjectKind = "task"
	KindDecision SubjectKind = "decision"
	KindContract SubjectKind = "contract"
	KindExternal SubjectKind = "external"
	KindProject  SubjectKind = "project"
)

// SubjectKinds lists every subject kind.
var SubjectKinds = []SubjectKind{KindTask, KindDecision, KindContract, KindExternal, KindProject}

// Known reports whether k is a subject kind from C-event-log §2.
func (k SubjectKind) Known() bool { return slices.Contains(SubjectKinds, k) }

// ProjectSubjectID is subject.id for project-wide events (C-event-log §2).
const ProjectSubjectID = "project"

// Actor is who wrote an event.
type Actor struct {
	Role Role   `json:"role"`
	ID   string `json:"id"`
}

// Subject is what an event is about.
type Subject struct {
	Kind SubjectKind `json:"kind"`
	ID   string      `json:"id"`
}

// Event is one envelope (C-event-log §2). Data holds the type-specific
// payload exactly as read, so unknown payload fields survive a decode and
// re-encode; Payload decodes it into the typed struct for Type. Unknown
// envelope fields are kept in Extra.
type Event struct {
	Schema  int             `json:"schema"`
	ID      string          `json:"id"`
	Type    Type            `json:"type"`
	At      time.Time       `json:"at"`
	Actor   Actor           `json:"actor"`
	Subject Subject         `json:"subject"`
	Data    json.RawMessage `json:"data"`
	// Extra holds envelope fields this version does not know (C-event-log
	// §9). They are written back unchanged by MarshalJSON.
	Extra map[string]json.RawMessage `json:"-"`
}

var envelopeKeys = []string{"schema", "id", "type", "at", "actor", "subject", "data"}

// New builds an event with schema 1, the time in UTC, and data encoded as
// JSON. data should be one of the payload types in this package.
func New(id string, typ Type, at time.Time, actor Actor, subject Subject, data any) (Event, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return Event{}, fmt.Errorf("event %s: encode data: %w", typ, err)
	}
	return Event{
		Schema:  SchemaVersion,
		ID:      id,
		Type:    typ,
		At:      at.UTC(),
		Actor:   actor,
		Subject: subject,
		Data:    raw,
	}, nil
}

// MarshalJSON writes the envelope fields in contract order, then any
// unknown envelope fields in key order.
func (e Event) MarshalJSON() ([]byte, error) {
	type plain Event
	b, err := json.Marshal(plain(e))
	if err != nil {
		return nil, err
	}
	return appendExtra(b, e.Extra)
}

// UnmarshalJSON reads an envelope and keeps unknown fields in Extra.
func (e *Event) UnmarshalJSON(b []byte) error {
	type plain Event
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return err
	}
	for _, k := range envelopeKeys {
		delete(all, k)
	}
	if len(all) == 0 {
		all = nil
	}
	p.Extra = all
	*e = Event(p)
	return nil
}

// Decode parses one JSON event.
func Decode(b []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(b, &e); err != nil {
		return Event{}, fmt.Errorf("decode event: %w", err)
	}
	return e, nil
}

// Encode writes one event as JSON.
func Encode(e Event) ([]byte, error) { return json.Marshal(e) }

// appendExtra splices extra fields, sorted by key, before the closing brace
// of a JSON object.
func appendExtra(obj []byte, extra map[string]json.RawMessage) ([]byte, error) {
	if len(extra) == 0 {
		return obj, nil
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var buf bytes.Buffer
	buf.Write(obj[:len(obj)-1])
	empty := bytes.Equal(bytes.TrimSpace(obj), []byte("{}"))
	for i, k := range keys {
		if !empty || i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(extra[k])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// ErrUnknownType is returned by Payload for an event type this version does
// not know. Readers should skip such events (C-event-log §9).
var ErrUnknownType = errors.New("unknown event type")

// Payload decodes Data into the typed payload for Type, returned as a
// pointer (for example *TaskDrafted). Unknown fields in Data are ignored
// here but remain in e.Data.
func (e Event) Payload() (any, error) {
	p := NewPayload(e.Type)
	if p == nil {
		return nil, fmt.Errorf("event %s: %w %q", e.ID, ErrUnknownType, e.Type)
	}
	data := e.Data
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		data = []byte("{}")
	}
	if err := json.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("event %s (%s): decode data: %w", e.ID, e.Type, err)
	}
	return p, nil
}

// UnknownDataFields returns the keys in Data that the typed payload for Type
// does not define, sorted. It is useful for diagnostics; the fields
// themselves are kept in Data.
func (e Event) UnknownDataFields() ([]string, error) {
	p := NewPayload(e.Type)
	if p == nil {
		return nil, fmt.Errorf("event %s: %w %q", e.ID, ErrUnknownType, e.Type)
	}
	var all map[string]json.RawMessage
	if len(bytes.TrimSpace(e.Data)) > 0 {
		if err := json.Unmarshal(e.Data, &all); err != nil {
			return nil, err
		}
	}
	known := jsonKeys(p)
	var out []string
	for k := range all {
		if !slices.Contains(known, k) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out, nil
}

// Validate checks an envelope against C-event-log §2 (and the id rules of
// §1): schema 1, a canonical ULID, a known type, `at` in UTC, a known actor
// role with an id, a known subject kind, and a subject id that is a valid
// id of that kind (or "project" for project events). It also checks that
// the payload decodes. Writers call it before appending; readers may accept
// events it refuses (e.g. newer types) and skip them.
func (e Event) Validate() error {
	var errs []error
	if e.Schema != SchemaVersion {
		errs = append(errs, fmt.Errorf("schema: want %d, got %d", SchemaVersion, e.Schema))
	}
	if err := ValidateULID(e.ID); err != nil {
		errs = append(errs, fmt.Errorf("id: %w", err))
	}
	if !e.Type.Known() {
		errs = append(errs, fmt.Errorf("type: unknown %q", e.Type))
	}
	if e.At.IsZero() {
		errs = append(errs, errors.New("at: missing"))
	} else if _, off := e.At.Zone(); off != 0 {
		errs = append(errs, errors.New("at: not UTC"))
	}
	if !e.Actor.Role.Known() {
		errs = append(errs, fmt.Errorf("actor.role: unknown %q", e.Actor.Role))
	}
	if e.Actor.ID == "" {
		errs = append(errs, errors.New("actor.id: missing"))
	}
	switch {
	case !e.Subject.Kind.Known():
		errs = append(errs, fmt.Errorf("subject.kind: unknown %q", e.Subject.Kind))
	case e.Subject.Kind == KindProject:
		if e.Subject.ID != ProjectSubjectID {
			errs = append(errs, fmt.Errorf("subject.id: want %q for a project event, got %q", ProjectSubjectID, e.Subject.ID))
		}
	default:
		if err := ValidateIDOfKind(e.Subject.ID, e.Subject.Kind); err != nil {
			errs = append(errs, fmt.Errorf("subject.id: %w", err))
		}
	}
	if e.Type.Known() {
		if _, err := e.Payload(); err != nil {
			errs = append(errs, fmt.Errorf("data: %w", err))
		}
	}
	return errors.Join(errs...)
}
