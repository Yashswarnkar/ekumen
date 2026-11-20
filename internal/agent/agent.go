// Package agent is the tool-agnostic adapter interface of C-adapter v1
// (.ekumen/contracts/C-adapter.md). The core, the control loop, the CLI and
// the UI see coding-agent sessions only through these types; every
// tool-specific choice lives in an adapter package below this one.
//
// The §2 shapes are copied verbatim. ListFilter and SessionInfo are named by
// §2 but not spelled out there; they are defined here.
package agent

import (
	"context"
	"errors"
	"time"
)

type Adapter interface {
	Name() string
	Available(ctx context.Context) error
	Caps(ctx context.Context, s *SessionRef) (Caps, error)

	List(ctx context.Context, f ListFilter) ([]SessionInfo, error)
	Status(ctx context.Context, s SessionRef) (Status, error)
	Events(ctx context.Context, s SessionRef, since string) (<-chan Event, error)

	Start(ctx context.Context, r StartReq) (SessionRef, error)
	Resume(ctx context.Context, s SessionRef, m *Message) error
	Send(ctx context.Context, s SessionRef, m Message) (Delivery, error)
	Fork(ctx context.Context, s SessionRef, r ForkReq) (SessionRef, error)
}

type SessionRef struct {
	Adapter, ID, Cwd string
	Ownership        string
}

type Status struct {
	State, WaitingFor, Detail, Source string
	ObservedAt                        time.Time
}

type StartReq struct{ Key, Cwd, Prompt, Task string }

type ForkReq struct {
	Key, Prompt string
	ReadOnly    bool
}

type Message struct {
	Key, Text string
	Mode      string
}

type Delivery struct{ Key, State string }

type Caps struct {
	Ops            map[string]string
	AttachObserved bool
}

type Event struct {
	Kind, Text string
	At         time.Time
}

// ListFilter narrows List. Empty fields match everything.
type ListFilter struct {
	Cwd       string
	Ownership string
}

// SessionInfo is one listed session with its status at listing time.
type SessionInfo struct {
	Ref       SessionRef
	Name      string
	Status    Status
	StartedAt time.Time
}

var (
	ErrUnsupported = errors.New("agent: operation unsupported for this session")
	ErrUnavailable = errors.New("agent: tool unavailable")
	ErrNotFound    = errors.New("agent: session not found")
)

const (
	StateRunning    = "running"
	StateIdle       = "idle"
	StateNeedsInput = "needs_input"
	StateDone       = "done"
	StateUnknown    = "unknown"
)

const (
	WaitPermission = "permission"
	WaitQuestion   = "question"
	WaitOther      = "other"
)

const (
	Managed  = "managed"
	Observed = "observed"
)

const (
	OpList   = "list"
	OpStatus = "status"
	OpStart  = "start"
	OpResume = "resume"
	OpSend   = "send"
	OpFork   = "fork"
	OpEvents = "events"
)

// Ops lists every operation a Caps map may name.
var Ops = []string{OpList, OpStatus, OpStart, OpResume, OpSend, OpFork, OpEvents}

const (
	Supported   = "supported"
	Partial     = "partial"
	Unsupported = "unsupported"
)

const (
	Delivered = "delivered"
	Queued    = "queued"
	Uncertain = "uncertain"
)

const (
	ModeNextTurn = "next_turn"
	ModeSteer    = "steer"
)

const (
	EventMessage    = "message"
	EventToolUse    = "tool_use"
	EventNeedsInput = "needs_input"
	EventTurnEnd    = "turn_end"
	EventError      = "error"
)

// Support returns the support level of op, treating a missing entry as
// unsupported.
func (c Caps) Support(op string) string {
	if v, ok := c.Ops[op]; ok {
		return v
	}
	return Unsupported
}
