package claudecode

import (
	"fmt"
	"strings"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/agent"
)

const (
	SourceListing = "listing"
	SourceHooks   = "hooks"
	SourceNone    = "none"
)

// Unverified is put in Detail for needs_input read from the listing alone:
// no permission prompt or question has been observed in `claude agents
// --json` hands-on yet (C-adapter "Open before freeze"). Move a value into
// verifiedListing once a live probe records it.
const Unverified = "unverified signal"

var verifiedListing = map[string]bool{}

type signal struct {
	state, waitingFor, detail string
	at                        time.Time
}

var waitingFor = map[string]string{
	"permission prompt": agent.WaitPermission,
	"input needed":      agent.WaitQuestion,
	"sandbox request":   agent.WaitOther,
	"worker request":    agent.WaitOther,
	"dialog open":       agent.WaitOther,
}

// listingSignal maps one listing entry. ok is false when a kind, status or
// state value is not one the adapter knows.
func listingSignal(e entry) (signal, bool) {
	switch e.Kind {
	case "interactive", "background":
	default:
		return signal{}, false
	}
	switch e.State {
	case "", "working", "blocked", "done", "failed", "stopped":
	default:
		return signal{}, false
	}
	switch e.Status {
	case "busy":
		return signal{state: agent.StateRunning}, true
	case "idle":
		return signal{state: agent.StateIdle}, true
	case "waiting":
		w, known := waitingFor[e.WaitingFor]
		if !known {
			w = agent.WaitOther
		}
		s := signal{state: agent.StateNeedsInput, waitingFor: w}
		if w == agent.WaitOther {
			s.detail = "waiting: " + e.WaitingFor
		}
		return markUnverified(s, "status=waiting/"+e.WaitingFor), true
	case "":
	default:
		return signal{}, false
	}
	switch e.State {
	case "working":
		return signal{state: agent.StateRunning}, true
	case "blocked":
		return markUnverified(signal{state: agent.StateNeedsInput, waitingFor: agent.WaitOther, detail: "blocked"}, "state=blocked"), true
	case "done":
		return signal{state: agent.StateIdle}, true
	case "failed", "stopped":
		return signal{state: agent.StateDone, detail: e.State}, true
	}
	return signal{}, false
}

func markUnverified(s signal, key string) signal {
	if verifiedListing[key] {
		return s
	}
	if s.detail == "" {
		s.detail = Unverified
	} else {
		s.detail = Unverified + "; " + s.detail
	}
	return s
}

// hookSignal maps one hook record; ok is false for records that say
// nothing about status (SessionStart, idle_prompt, unknown events).
func hookSignal(h hookRecord) (signal, bool) {
	s := signal{at: h.At}
	switch h.Event {
	case "PermissionRequest":
		s.state, s.waitingFor, s.detail = agent.StateNeedsInput, agent.WaitPermission, h.ToolName
	case "PreToolUse":
		if h.ToolName != "AskUserQuestion" {
			return s, false
		}
		s.state, s.waitingFor = agent.StateNeedsInput, agent.WaitQuestion
	case "Notification":
		switch h.NotificationType {
		case "permission_prompt":
			s.state, s.waitingFor = agent.StateNeedsInput, agent.WaitPermission
		case "agent_needs_input":
			s.state, s.waitingFor = agent.StateNeedsInput, agent.WaitQuestion
		case "elicitation_dialog":
			s.state, s.waitingFor = agent.StateNeedsInput, agent.WaitOther
			s.detail = "elicitation_dialog"
		default:
			return s, false
		}
	case "UserPromptSubmit":
		s.state = agent.StateRunning
	case "Stop":
		s.state = agent.StateIdle
	case "StopFailure", "SessionEnd":
		s.state, s.detail = agent.StateDone, h.Event
	default:
		return s, false
	}
	return s, true
}

// latestHook is the newest status-bearing hook signal. A PermissionRequest
// and its twin permission_prompt Notification collapse into one signal that
// keeps the tool name (R2 §3 lesson 2).
func latestHook(hs []hookRecord) (signal, bool) {
	var cur signal
	have := false
	for _, h := range hs {
		s, ok := hookSignal(h)
		if !ok {
			continue
		}
		if have && s.waitingFor == agent.WaitPermission && cur.waitingFor == agent.WaitPermission && s.detail == "" {
			s.detail = cur.detail
		}
		cur, have = s, true
	}
	return cur, have
}

// hookGrace is how long a hook needs_input outranks a listing that says
// running or idle. The listing is read now, so it is the newest signal; the
// grace only covers a listing that has not caught up with a prompt that
// just appeared. After it, listing liveness wins (#32 item 4,
// D-claude-hooks-policy-blocked: listing primary, hooks optional): no hook
// fires when a permission prompt is answered, so an older needs_input hook
// says nothing about now.
const hookGrace = time.Minute

// mapStatus is the adapter's whole status rule (README "Status mapping").
func mapStatus(e *entry, hooks []hookRecord, now time.Time) agent.Status {
	h, haveHook := latestHook(hooks)
	if e == nil {
		if haveHook && h.state == agent.StateDone {
			return fromSignal(h, SourceHooks)
		}
		if haveHook {
			return unknown(now, SourceHooks, "not listed; last hook said "+h.state)
		}
		return unknown(now, SourceNone, "not listed and no hook data")
	}
	if e.bad != "" {
		return unknown(now, SourceListing, "unreadable listing entry: "+e.bad)
	}
	l, ok := listingSignal(*e)
	if !ok {
		return unknown(now, SourceListing, fmt.Sprintf("unrecognised listing values kind=%q status=%q state=%q", e.Kind, e.Status, e.State))
	}
	l.at = now
	if haveHook && h.state == agent.StateNeedsInput {
		switch {
		case l.state == agent.StateNeedsInput:
			return fromSignal(h, SourceHooks) // the hook names what is awaited
		case l.state != agent.StateDone && now.Sub(h.at) < hookGrace:
			return fromSignal(h, SourceHooks)
		}
	}
	return fromSignal(l, SourceListing)
}

func fromSignal(s signal, source string) agent.Status {
	return agent.Status{State: s.state, WaitingFor: s.waitingFor, Detail: strings.TrimSpace(s.detail), Source: source, ObservedAt: s.at}
}

func unknown(now time.Time, source, detail string) agent.Status {
	return agent.Status{State: agent.StateUnknown, Detail: detail, Source: source, ObservedAt: now}
}
