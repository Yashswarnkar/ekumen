package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/agent"
)

type printResult struct {
	SessionID string `json:"session_id"`
	Result    string `json:"result"`
	IsError   bool   `json:"is_error"`
	Subtype   string `json:"subtype"`
}

// forkReadOnly is on every fork, whatever ForkReq.ReadOnly says (#32 item
// 2): plan mode, and any permission prompt is denied rather than asked.
// A side question never needs to change the worktree, and the zero value
// of ReadOnly must not be the unsafe one. The live probe asks a fork to
// write a file and checks it was denied (README "Fork").
var forkReadOnly = []string{"--permission-mode", "plan", "--permission-prompts", "none"}

// Fork asks a side question in a copy of the session (spine rule 10). The
// fork's ID is a UUIDv5 of the key; its answer is kept in the state
// directory and returned by Events on the fork.
func (a *Adapter) Fork(ctx context.Context, s agent.SessionRef, r agent.ForkReq) (agent.SessionRef, error) {
	if r.Key == "" || r.Prompt == "" {
		return agent.SessionRef{}, errors.New("claudecode: Fork needs Key and Prompt")
	}
	if rec, ok := a.state.key(opFork, r.Key); ok {
		return forkRecorded(rec)
	}
	if err := checkID(s.ID); err != nil {
		return agent.SessionRef{}, err
	}
	cwd := s.Cwd
	if rec, ok := a.state.session(s.ID); ok {
		cwd = rec.Cwd
	}
	if cwd == "" {
		es, err := a.listing(ctx)
		if err != nil {
			return agent.SessionRef{}, err
		}
		e := find(es, s.ID)
		if e == nil {
			return agent.SessionRef{}, fmt.Errorf("%w: %s", agent.ErrNotFound, s.ID)
		}
		cwd = e.Cwd
	}
	rec := sessionRecord{ID: uuidV5("fork:" + r.Key), Key: r.Key, Op: opFork, Cwd: cwd, From: s.ID, Pending: true, At: a.now()}
	if err := a.state.claimKey(rec); err != nil {
		if errors.Is(err, fs.ErrExist) {
			if rec, ok := a.state.key(opFork, r.Key); ok {
				return forkRecorded(rec)
			}
		}
		return agent.SessionRef{}, err
	}
	args := append([]string{"-p", "--resume", s.ID, "--fork-session", "--session-id", rec.ID, "--output-format", "json"}, forkReadOnly...)
	res, err := a.exec(ctx, cwd, promptArgs(args, r.Prompt)...)
	if errors.Is(err, ErrNotStarted) {
		a.state.dropKey(opFork, r.Key)
		return agent.SessionRef{}, notStarted(err)
	}
	if err != nil {
		return agent.SessionRef{}, fmt.Errorf("claude -p --fork-session: %w; key %q stays pending", err, r.Key)
	}
	var out printResult
	if res.Code != 0 || json.Unmarshal(res.Stdout, &out) != nil {
		// the process ran and ended without an answer: drop the key so a
		// repeat can ask again (#32 item 3)
		a.state.dropKey(opFork, r.Key)
		return agent.SessionRef{}, fmt.Errorf("claude -p --fork-session exited %d: %s", res.Code, firstLine(res.Stderr, res.Stdout))
	}
	if out.SessionID != "" {
		rec.ID = out.SessionID
	}
	rec.Result = out.Result
	if out.IsError {
		rec.Result = "error (" + out.Subtype + "): " + out.Result
	}
	if len(rec.Result) > maxForkAnswer {
		rec.Result = rec.Result[:maxForkAnswer] + " [truncated]"
	}
	return a.finish(rec, rec.ID)
}

// maxForkAnswer bounds the answer kept in the state directory.
const maxForkAnswer = 64 << 10

func forkRecorded(rec sessionRecord) (agent.SessionRef, error) {
	if rec.Pending {
		return agent.SessionRef{}, fmt.Errorf("%w: Fork %q timed out or was interrupted and its outcome is unknown; it is not asked again", agent.ErrNotFound, rec.Key)
	}
	return agent.SessionRef{Adapter: Name, ID: rec.ID, Cwd: rec.Cwd, Ownership: agent.Managed}, nil
}

// Events replays what the adapter holds for a managed session: a fork's
// answer, then its hook records. since is an RFC 3339 time; only later
// events are sent. The channel is closed after the snapshot.
func (a *Adapter) Events(ctx context.Context, s agent.SessionRef, since string) (<-chan agent.Event, error) {
	rec, managed := a.state.session(s.ID)
	if !managed {
		return nil, fmt.Errorf("%w: observed session", agent.ErrUnsupported)
	}
	var after time.Time
	if since != "" {
		t, err := time.Parse(time.RFC3339Nano, since)
		if err != nil {
			return nil, fmt.Errorf("claudecode: since %q: %w", since, err)
		}
		after = t
	}
	var evs []agent.Event
	if rec.Op == opFork && rec.Result != "" {
		evs = append(evs, agent.Event{Kind: agent.EventMessage, Text: rec.Result, At: rec.At})
	}
	if rec.HooksDir != "" {
		hs, err := readHooks(rec.HooksDir, s.ID)
		if err != nil {
			return nil, err
		}
		for _, h := range hs {
			if ev, ok := hookEvent(h); ok {
				evs = append(evs, ev)
			}
		}
	}
	ch := make(chan agent.Event, len(evs))
	for _, ev := range evs {
		if ev.At.After(after) {
			ch <- ev
		}
	}
	close(ch)
	return ch, nil
}

func hookEvent(h hookRecord) (agent.Event, bool) {
	ev := agent.Event{At: h.At}
	switch s, ok := hookSignal(h); {
	case ok && s.state == agent.StateNeedsInput:
		ev.Kind, ev.Text = agent.EventNeedsInput, strings.TrimSpace(s.waitingFor+" "+s.detail+" "+h.Message)
	case h.Event == "Stop" || h.Event == "SessionEnd":
		ev.Kind, ev.Text = agent.EventTurnEnd, h.Event
	case h.Event == "StopFailure":
		ev.Kind, ev.Text = agent.EventError, h.Event
	default:
		return ev, false
	}
	return ev, true
}
