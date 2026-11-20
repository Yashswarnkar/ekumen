// Package conformance is the shared adapter test suite of C-adapter §7. An
// adapter test builds a Harness around the adapter and a scripted fake of
// its tool, and calls Run.
package conformance

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/agent"
)

// Harness is one fresh adapter wired to a scripted fake of its tool.
type Harness struct {
	Adapter agent.Adapter
	Cwd     string // a directory Start may use

	Idle     agent.SessionRef // managed and idle: a next_turn Send can be delivered
	Busy     agent.SessionRef // managed and running or needs_input: Send must queue
	Observed agent.SessionRef // zero when the tool has no observed sessions
	Missing  agent.SessionRef // unknown to the tool

	Invocations   func() int // side-effecting tool invocations so far
	MakeUncertain func()     // the next delivery attempt is ambiguous
	Garble        func()     // status data becomes unreadable
	BreakStatus   func()     // the status source fails
}

type Factory func(t testing.TB) *Harness

type check struct {
	name string
	fn   func(ctx context.Context, h *Harness) error
}

var checks = []check{
	{"Available", checkAvailable},
	{"CapsDeclareEveryOp", checkCaps},
	{"List", checkList},
	{"StatusKnown", checkStatusKnown},
	{"StatusMissing_CAdapter1UnknownNeverGuess", checkStatusMissing},
	{"StatusGarbled_CAdapter1UnknownNeverGuess", checkStatusGarbled},
	{"StatusFailing_CAdapter3FallbackUnknown", checkStatusFailing},
	{"UnsupportedOps_CAdapter3Fallbacks", checkUnsupported},
	{"StartIdempotent_CAdapter4", checkStartIdempotent},
	{"SendIdempotent_CAdapter4", checkSendIdempotent},
	{"SendBusyQueues", checkSendBusy},
	{"UncertainNeverRetried_CAdapter4", checkUncertain},
	{"ForkIdempotent_CAdapter4", checkForkIdempotent},
	{"Events", checkEvents},
	{"Resume", checkResume},
}

// Run runs every check against a fresh harness from f.
func Run(t *testing.T, f Factory) {
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if err := run(t, f, c); err != nil {
				t.Error(err)
			}
		})
	}
}

// Failures runs every check and returns the names of those that fail, so a
// test can prove the suite catches a broken adapter.
func Failures(t testing.TB, f Factory) []string {
	var out []string
	for _, c := range checks {
		if err := run(t, f, c); err != nil {
			out = append(out, c.name)
		}
	}
	return out
}

func run(t testing.TB, f Factory, c check) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return c.fn(ctx, f(t))
}

func checkAvailable(ctx context.Context, h *Harness) error {
	return h.Adapter.Available(ctx)
}

func checkCaps(ctx context.Context, h *Harness) error {
	for _, ref := range refs(h) {
		c, err := h.Adapter.Caps(ctx, ref)
		if err != nil {
			return fmt.Errorf("Caps(%v): %w", ref, err)
		}
		for _, op := range agent.Ops {
			switch v := c.Ops[op]; v {
			case agent.Supported, agent.Partial, agent.Unsupported:
			default:
				return fmt.Errorf("Caps(%v)[%s] = %q", ref, op, v)
			}
		}
	}
	return nil
}

func checkList(ctx context.Context, h *Harness) error {
	all, err := h.Adapter.List(ctx, agent.ListFilter{})
	if err != nil {
		return err
	}
	want := []agent.SessionRef{h.Idle}
	if h.Observed.ID != "" {
		want = append(want, h.Observed)
	}
	for _, w := range want {
		if !listed(all, w) {
			return fmt.Errorf("List missing %s session %s", w.Ownership, w.ID)
		}
	}
	managed, err := h.Adapter.List(ctx, agent.ListFilter{Ownership: agent.Managed})
	if err != nil {
		return err
	}
	for _, s := range managed {
		if s.Ref.Ownership != agent.Managed {
			return fmt.Errorf("List(managed) returned %s session %s", s.Ref.Ownership, s.Ref.ID)
		}
	}
	if !listed(managed, h.Idle) {
		return fmt.Errorf("List(managed) missing %s", h.Idle.ID)
	}
	return nil
}

func listed(ss []agent.SessionInfo, r agent.SessionRef) bool {
	for _, s := range ss {
		if s.Ref.ID == r.ID && s.Ref.Ownership == r.Ownership {
			return true
		}
	}
	return false
}

func checkStatusKnown(ctx context.Context, h *Harness) error {
	st, err := h.Adapter.Status(ctx, h.Idle)
	if err != nil {
		return err
	}
	if st.State != agent.StateIdle {
		return fmt.Errorf("Status(idle session) = %+v", st)
	}
	st, err = h.Adapter.Status(ctx, h.Busy)
	if err != nil {
		return err
	}
	if st.State != agent.StateRunning && st.State != agent.StateNeedsInput {
		return fmt.Errorf("Status(busy session) = %+v", st)
	}
	return nil
}

func checkStatusMissing(ctx context.Context, h *Harness) error {
	st, err := h.Adapter.Status(ctx, h.Missing)
	if errors.Is(err, agent.ErrNotFound) {
		return nil
	}
	return wantUnknown("missing session", st, err)
}

func checkStatusGarbled(ctx context.Context, h *Harness) error {
	h.Garble()
	st, err := h.Adapter.Status(ctx, h.Idle)
	return wantUnknown("garbled data", st, err)
}

func checkStatusFailing(ctx context.Context, h *Harness) error {
	h.BreakStatus()
	st, err := h.Adapter.Status(ctx, h.Idle)
	return wantUnknown("failing status source", st, err)
}

func wantUnknown(what string, st agent.Status, err error) error {
	if err != nil {
		return fmt.Errorf("%s: Status error %v, want state unknown", what, err)
	}
	if st.State != agent.StateUnknown {
		return fmt.Errorf("%s: Status = %+v, want unknown", what, st)
	}
	return nil
}

func checkUnsupported(ctx context.Context, h *Harness) error {
	c, err := h.Adapter.Caps(ctx, nil)
	if err != nil {
		return err
	}
	if c.Support(agent.OpStart) == agent.Unsupported {
		_, err := h.Adapter.Start(ctx, agent.StartReq{Key: "conf-unsupported-start", Cwd: h.Cwd, Prompt: "p"})
		if !errors.Is(err, agent.ErrUnsupported) {
			return fmt.Errorf("Start with start unsupported: %v", err)
		}
	}
	for _, ref := range []agent.SessionRef{h.Idle, h.Observed} {
		if ref.ID == "" {
			continue
		}
		c, err := h.Adapter.Caps(ctx, &ref)
		if err != nil {
			return err
		}
		calls := map[string]func() error{
			agent.OpResume: func() error { return h.Adapter.Resume(ctx, ref, nil) },
			agent.OpSend: func() error {
				_, err := h.Adapter.Send(ctx, ref, agent.Message{Key: "conf-unsupported-send", Text: "t", Mode: agent.ModeNextTurn})
				return err
			},
			agent.OpFork: func() error {
				_, err := h.Adapter.Fork(ctx, ref, agent.ForkReq{Key: "conf-unsupported-fork", Prompt: "p", ReadOnly: true})
				return err
			},
			agent.OpEvents: func() error { _, err := h.Adapter.Events(ctx, ref, ""); return err },
		}
		for op, call := range calls {
			if c.Support(op) != agent.Unsupported {
				continue
			}
			if err := call(); !errors.Is(err, agent.ErrUnsupported) {
				return fmt.Errorf("%s on %s session with %s unsupported: %v, want ErrUnsupported", op, ref.Ownership, op, err)
			}
		}
	}
	return nil
}

func checkStartIdempotent(ctx context.Context, h *Harness) error {
	c, err := h.Adapter.Caps(ctx, nil)
	if err != nil || c.Support(agent.OpStart) == agent.Unsupported {
		return err
	}
	r := agent.StartReq{Key: "conf-start", Cwd: h.Cwd, Prompt: "p", Task: "T-conf"}
	a, err := h.Adapter.Start(ctx, r)
	if err != nil {
		return fmt.Errorf("Start: %w", err)
	}
	if a.ID == "" || a.Ownership != agent.Managed {
		return fmt.Errorf("Start returned %+v", a)
	}
	n := h.Invocations()
	b, err := h.Adapter.Start(ctx, r)
	if err != nil {
		return fmt.Errorf("repeated Start: %w", err)
	}
	if b.ID != a.ID {
		return fmt.Errorf("repeated Start key made session %s, first was %s", b.ID, a.ID)
	}
	if h.Invocations() != n {
		return fmt.Errorf("repeated Start key invoked the tool again")
	}
	return nil
}

func sendSupported(ctx context.Context, h *Harness) (bool, error) {
	c, err := h.Adapter.Caps(ctx, &h.Idle)
	if err != nil {
		return false, err
	}
	return c.Support(agent.OpSend) != agent.Unsupported, nil
}

func checkSendIdempotent(ctx context.Context, h *Harness) error {
	if ok, err := sendSupported(ctx, h); !ok {
		return err
	}
	m := agent.Message{Key: "conf-send", Text: "hello", Mode: agent.ModeNextTurn}
	d, err := h.Adapter.Send(ctx, h.Idle, m)
	if err != nil || d.State != agent.Delivered || d.Key != m.Key {
		return fmt.Errorf("Send to idle session: %+v, %v; want delivered", d, err)
	}
	n := h.Invocations()
	d, err = h.Adapter.Send(ctx, h.Idle, m)
	if err != nil || d.State != agent.Delivered {
		return fmt.Errorf("repeated Send: %+v, %v", d, err)
	}
	if h.Invocations() != n {
		return fmt.Errorf("repeated Send key invoked the tool again")
	}
	return nil
}

func checkSendBusy(ctx context.Context, h *Harness) error {
	if ok, err := sendSupported(ctx, h); !ok {
		return err
	}
	n := h.Invocations()
	d, err := h.Adapter.Send(ctx, h.Busy, agent.Message{Key: "conf-busy", Text: "t", Mode: agent.ModeNextTurn})
	if err != nil || d.State != agent.Queued {
		return fmt.Errorf("Send to busy session: %+v, %v; want queued", d, err)
	}
	if h.Invocations() != n {
		return fmt.Errorf("queued Send invoked the tool")
	}
	return nil
}

func checkUncertain(ctx context.Context, h *Harness) error {
	if ok, err := sendSupported(ctx, h); !ok {
		return err
	}
	h.MakeUncertain()
	m := agent.Message{Key: "conf-uncertain", Text: "t", Mode: agent.ModeNextTurn}
	d, _ := h.Adapter.Send(ctx, h.Idle, m)
	if d.State != agent.Uncertain {
		return fmt.Errorf("ambiguous delivery: %+v, want uncertain", d)
	}
	n := h.Invocations()
	d, _ = h.Adapter.Send(ctx, h.Idle, m)
	if d.State != agent.Uncertain {
		return fmt.Errorf("repeated uncertain key: %+v, want uncertain", d)
	}
	if h.Invocations() != n {
		return fmt.Errorf("uncertain delivery was retried")
	}
	return nil
}

func checkForkIdempotent(ctx context.Context, h *Harness) error {
	c, err := h.Adapter.Caps(ctx, &h.Idle)
	if err != nil || c.Support(agent.OpFork) == agent.Unsupported {
		return err
	}
	r := agent.ForkReq{Key: "conf-fork", Prompt: "why?", ReadOnly: true}
	a, err := h.Adapter.Fork(ctx, h.Idle, r)
	if err != nil {
		return fmt.Errorf("Fork: %w", err)
	}
	if a.ID == "" || a.ID == h.Idle.ID {
		return fmt.Errorf("Fork returned %+v", a)
	}
	n := h.Invocations()
	b, err := h.Adapter.Fork(ctx, h.Idle, r)
	if err != nil || b.ID != a.ID {
		return fmt.Errorf("repeated Fork key: %+v, %v; first was %s", b, err, a.ID)
	}
	if h.Invocations() != n {
		return fmt.Errorf("repeated Fork key invoked the tool again")
	}
	return nil
}

func checkEvents(ctx context.Context, h *Harness) error {
	c, err := h.Adapter.Caps(ctx, &h.Idle)
	if err != nil || c.Support(agent.OpEvents) == agent.Unsupported {
		return err
	}
	ch, err := h.Adapter.Events(ctx, h.Idle, "")
	if err != nil {
		return fmt.Errorf("Events: %w", err)
	}
	drain, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return nil
			}
		case <-drain.Done():
			return nil
		}
	}
}

func checkResume(ctx context.Context, h *Harness) error {
	c, err := h.Adapter.Caps(ctx, &h.Idle)
	if err != nil || c.Support(agent.OpResume) == agent.Unsupported {
		return err
	}
	return h.Adapter.Resume(ctx, h.Idle, nil)
}

func refs(h *Harness) []*agent.SessionRef {
	out := []*agent.SessionRef{nil, &h.Idle}
	if h.Observed.ID != "" {
		out = append(out, &h.Observed)
	}
	return out
}
