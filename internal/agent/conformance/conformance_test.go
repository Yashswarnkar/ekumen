package conformance

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/agent"
)

type fakeSession struct {
	ref   agent.SessionRef
	state string
}

// fake is a tiny in-memory adapter. Its "tool" is the sessions map; calls
// counts side-effecting tool invocations.
type fake struct {
	mu        sync.Mutex
	caps      map[string]map[string]string // ownership ("" for new) → op → level
	sessions  map[string]*fakeSession
	starts    map[string]string
	forks     map[string]string
	sent      map[string]agent.Delivery
	calls     int
	ambiguous bool
	garbled   bool
	broken    bool
	bug       string // a deliberate defect, for the mutation tests
}

// Deliberate defects the suite must catch.
const (
	bugResend       = "resend"        // Send resends on a repeated key
	bugRestart      = "restart"       // Start starts again on a repeated key
	bugRefork       = "refork"        // Fork forks again on a repeated key
	bugConstantID   = "constant-id"   // every Start returns the same session
	bugNoTool       = "no-tool"       // Start answers without invoking the tool
	bugObservedSend = "observed-send" // Send delivers to observed sessions
	bugOverclaim    = "overclaim"     // Caps says fork supported, Fork says unsupported
)

func newFake(limited bool) *fake {
	full := map[string]map[string]string{
		"":             {agent.OpList: agent.Supported, agent.OpStatus: agent.Supported, agent.OpStart: agent.Supported},
		agent.Managed:  {agent.OpList: agent.Supported, agent.OpStatus: agent.Supported, agent.OpResume: agent.Supported, agent.OpSend: agent.Supported, agent.OpFork: agent.Supported, agent.OpEvents: agent.Supported},
		agent.Observed: {agent.OpList: agent.Supported, agent.OpStatus: agent.Supported, agent.OpFork: agent.Supported},
	}
	if limited {
		full[""][agent.OpStart] = agent.Unsupported
		full[agent.Managed] = map[string]string{agent.OpList: agent.Supported, agent.OpStatus: agent.Supported}
		full[agent.Observed] = map[string]string{agent.OpList: agent.Supported, agent.OpStatus: agent.Partial}
	}
	f := &fake{caps: full, sessions: map[string]*fakeSession{}, starts: map[string]string{}, forks: map[string]string{}, sent: map[string]agent.Delivery{}}
	f.add("idle", agent.Managed, agent.StateIdle)
	f.add("busy", agent.Managed, agent.StateRunning)
	f.add("obs", agent.Observed, agent.StateIdle)
	return f
}

func (f *fake) add(id, own, state string) agent.SessionRef {
	r := agent.SessionRef{Adapter: "fake", ID: id, Cwd: "/w", Ownership: own}
	f.sessions[id] = &fakeSession{ref: r, state: state}
	return r
}

func (f *fake) Name() string                        { return "fake" }
func (f *fake) Available(ctx context.Context) error { return nil }

func (f *fake) Caps(ctx context.Context, s *agent.SessionRef) (agent.Caps, error) {
	own := ""
	if s != nil {
		own = s.Ownership
	}
	if f.bug == bugObservedSend && own == agent.Observed {
		own = agent.Managed
	}
	ops := map[string]string{}
	for _, op := range agent.Ops {
		ops[op] = agent.Unsupported
		if v, ok := f.caps[own][op]; ok {
			ops[op] = v
		}
	}
	return agent.Caps{Ops: ops}, nil
}

func (f *fake) supports(s agent.SessionRef, op string) bool {
	c, _ := f.Caps(context.Background(), &s)
	return c.Support(op) != agent.Unsupported
}

func (f *fake) List(ctx context.Context, lf agent.ListFilter) ([]agent.SessionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []agent.SessionInfo
	for _, s := range f.sessions {
		if lf.Ownership != "" && s.ref.Ownership != lf.Ownership {
			continue
		}
		out = append(out, agent.SessionInfo{Ref: s.ref, Status: agent.Status{State: s.state}})
	}
	return out, nil
}

func (f *fake) Status(ctx context.Context, s agent.SessionRef) (agent.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fs, ok := f.sessions[s.ID]
	if !ok || f.garbled || f.broken {
		return agent.Status{State: agent.StateUnknown, Source: "none", ObservedAt: time.Now()}, nil
	}
	return agent.Status{State: fs.state, Source: "fake", ObservedAt: time.Now()}, nil
}

func (f *fake) Events(ctx context.Context, s agent.SessionRef, since string) (<-chan agent.Event, error) {
	if !f.supports(s, agent.OpEvents) {
		return nil, agent.ErrUnsupported
	}
	ch := make(chan agent.Event)
	close(ch)
	return ch, nil
}

func (f *fake) Start(ctx context.Context, r agent.StartReq) (agent.SessionRef, error) {
	if f.caps[""][agent.OpStart] == agent.Unsupported {
		return agent.SessionRef{}, agent.ErrUnsupported
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.starts[r.Key]; ok && f.bug != bugRestart {
		return f.sessions[id].ref, nil
	}
	if f.bug != bugNoTool {
		f.calls++
	}
	id := fmt.Sprintf("s%d", len(f.sessions))
	if f.bug == bugConstantID {
		id = "s"
	}
	f.starts[r.Key] = id
	return f.add(id, agent.Managed, agent.StateRunning), nil
}

func (f *fake) Resume(ctx context.Context, s agent.SessionRef, m *agent.Message) error {
	if !f.supports(s, agent.OpResume) {
		return agent.ErrUnsupported
	}
	return nil
}

func (f *fake) Send(ctx context.Context, s agent.SessionRef, m agent.Message) (agent.Delivery, error) {
	if !f.supports(s, agent.OpSend) {
		return agent.Delivery{}, agent.ErrUnsupported
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if d, ok := f.sent[m.Key]; ok && f.bug != bugResend {
		return d, nil
	}
	if f.sessions[s.ID].state != agent.StateIdle {
		return agent.Delivery{Key: m.Key, State: agent.Queued}, nil
	}
	f.calls++
	d := agent.Delivery{Key: m.Key, State: agent.Delivered}
	if f.ambiguous {
		f.ambiguous = false
		d.State = agent.Uncertain
	}
	f.sent[m.Key] = d
	return d, nil
}

func (f *fake) Fork(ctx context.Context, s agent.SessionRef, r agent.ForkReq) (agent.SessionRef, error) {
	if !f.supports(s, agent.OpFork) || f.bug == bugOverclaim {
		return agent.SessionRef{}, agent.ErrUnsupported
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.forks[r.Key]; ok && f.bug != bugRefork {
		return f.sessions[id].ref, nil
	}
	f.calls++
	id := fmt.Sprintf("f%d", f.calls)
	f.forks[r.Key] = id
	return f.add(id, agent.Managed, agent.StateIdle), nil
}

func factory(limited bool, bug string) Factory {
	return func(t testing.TB) *Harness {
		f := newFake(limited)
		f.bug = bug
		return &Harness{
			Adapter:       f,
			Cwd:           t.TempDir(),
			Idle:          f.sessions["idle"].ref,
			Busy:          f.sessions["busy"].ref,
			Observed:      f.sessions["obs"].ref,
			Missing:       agent.SessionRef{Adapter: "fake", ID: "nope", Ownership: agent.Managed},
			Invocations:   func() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls },
			MakeUncertain: func() { f.mu.Lock(); f.ambiguous = true; f.mu.Unlock() },
			Garble:        func() { f.mu.Lock(); f.garbled = true; f.mu.Unlock() },
			BreakStatus:   func() { f.mu.Lock(); f.broken = true; f.mu.Unlock() },
		}
	}
}

func TestFakePassesConformance(t *testing.T) {
	Run(t, factory(false, ""))
}

func TestLimitedFakePassesConformance(t *testing.T) {
	Run(t, factory(true, ""))
}

// TestBrokenFakesFailConformance is the suite's mutation test: each
// deliberate defect must fail the named checks.
func TestBrokenFakesFailConformance(t *testing.T) {
	for bug, want := range map[string][]string{
		bugResend:       {"SendIdempotent_CAdapter4", "UncertainNeverRetried_CAdapter4"},
		bugRestart:      {"StartIdempotent_CAdapter4"},
		bugRefork:       {"ForkIdempotent_CAdapter4"},
		bugConstantID:   {"StartDistinctKeysDistinctSessions_CAdapter4"},
		bugNoTool:       {"StartIdempotent_CAdapter4", "StartDistinctKeysDistinctSessions_CAdapter4"},
		bugObservedSend: {"ObservedSendQueuesOrUnsupported_CAdapter3"},
		bugOverclaim:    {"CapsHonest_CAdapter3", "ForkIdempotent_CAdapter4"},
	} {
		t.Run(bug, func(t *testing.T) {
			got := Failures(t, factory(false, bug))
			for _, w := range want {
				if !slices.Contains(got, w) {
					t.Errorf("fake with bug %q passed %s; failures: %v", bug, w, got)
				}
			}
		})
	}
}
