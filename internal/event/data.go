package event

import (
	"encoding/json"
	"reflect"
	"strings"
)

// Payloads: one struct per event type (C-event-log v4 §6). Field names
// follow the contract. Optional fields are omitted when empty; readers ignore
// what they don't know (C-event-log §9).

// TaskDrafted creates a task.
type TaskDrafted struct {
	Title      string   `json:"title"`
	Kind       string   `json:"kind"` // research | contract | build
	Brief      string   `json:"brief"`
	Repo       string   `json:"repo"`
	BaseBranch string   `json:"base_branch"`
	Branch     string   `json:"branch"`
	Worktree   string   `json:"worktree"`
	BlockedBy  []string `json:"blocked_by"`
	TierGrants []string `json:"tier_grants"`
	// Aliases are older ids for this task (C-event-log v3 §1, D-alias-field).
	Aliases []string `json:"aliases,omitempty"`
	// Session is the session that owns the task, so it can write in its
	// lane before any TaskSessionAttached (C-event-log v3 §3, D-lane-gaps).
	Session string `json:"session,omitempty"`
}

// Task kinds (spine §3).
const (
	TaskKindResearch = "research"
	TaskKindContract = "contract"
	TaskKindBuild    = "build"
)

// TaskApproved records the lead's approval of a task's §A.
type TaskApproved struct {
	TierGrants []string `json:"tier_grants"`
	TextSHA256 string   `json:"text_sha256"`
}

// TaskAmended records one §B entry.
type TaskAmended struct {
	File       string `json:"file"`
	TextSHA256 string `json:"text_sha256"`
	Note       string `json:"note"`
	// ByDecision is set when the amendment is made by applying that
	// decision; such amendments are in the loop's lane (C-event-log v5 §3,
	// D-apply-amend-lane).
	ByDecision string `json:"by_decision,omitempty"`
}

// TaskBlockersChanged adds and removes ids from a task's blocked_by.
type TaskBlockersChanged struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Reason  string   `json:"reason"`
}

// TaskSessionAttached binds a session to a task.
type TaskSessionAttached struct {
	Session string `json:"session"`
	Adapter string `json:"adapter"`
}

// TaskHandoff records one §C entry.
type TaskHandoff struct {
	File          string `json:"file"`
	TextSHA256    string `json:"text_sha256"`
	StatusLine    string `json:"status_line"`
	HasFollowups  bool   `json:"has_followups"`
	HasDecisions  bool   `json:"has_decisions"`
	HasDeviations bool   `json:"has_deviations"`
}

// TaskDeferred parks a task.
type TaskDeferred struct {
	Reason string `json:"reason"`
}

// RaisedBy is a decision record's raised_by (C-decision-record §1).
type RaisedBy struct {
	Task    *string `json:"task"` // null for project decisions
	Session string  `json:"session"`
	On      string  `json:"on"` // date, YYYY-MM-DD
}

// Option is one choice in a decision record.
type Option struct {
	ID       string `json:"id"`
	Tradeoff string `json:"tradeoff"`
}

// DecisionContext is a decision record's context.
type DecisionContext struct {
	Summary        string   `json:"summary"`
	Evidence       []string `json:"evidence,omitempty"`
	PriorDecisions []string `json:"prior_decisions,omitempty"`
}

// DecisionRaised is the decision record of C-decision-record §1 without its
// lifecycle fields. Nil slices for Blocks and ContractImpact mean the field
// was missing or null, which C-decision-record §1 forbids; use an empty
// slice for "none".
type DecisionRaised struct {
	ID                string          `json:"id"`
	Aliases           []string        `json:"aliases,omitempty"`
	Title             string          `json:"title"`
	Tier              int             `json:"tier"`
	RaisedBy          RaisedBy        `json:"raised_by"`
	Question          string          `json:"question"`
	Options           []Option        `json:"options"`
	Recommendation    *string         `json:"recommendation"`
	DefaultIfNoAnswer *string         `json:"default_if_no_answer"`
	Deadline          *string         `json:"deadline"` // date, YYYY-MM-DD
	Blocks            []string        `json:"blocks"`
	ContractImpact    []string        `json:"contract_impact"`
	Context           DecisionContext `json:"context"`
}

// DecisionLifecycleFields are the record fields set only by events
// (C-decision-record §1, "Lifecycle fields"). A DecisionRaised payload must
// not carry them.
var DecisionLifecycleFields = []string{
	"status", "questions", "answer", "answer_note", "decided_by", "decided_on",
	"superseded_by", "applied_on", "applied",
}

// AnswerOther is the answer for a choice outside the listed options; the
// choice goes in the note (C-decision-record §1).
const AnswerOther = "other"

// DecisionDecided is the lead's answer.
type DecisionDecided struct {
	Answer string `json:"answer"`
	Note   string `json:"note"`
}

// DecisionDefaulted applies a tier-1 default after its deadline.
type DecisionDefaulted struct {
	Answer   string `json:"answer"`
	Deadline string `json:"deadline"`
}

// DecisionSuperseded replaces a decision with another.
type DecisionSuperseded struct {
	By   string `json:"by"`
	Note string `json:"note"`
}

// DecisionApplied records the loop applying a decision.
type DecisionApplied struct {
	AmendedTasks     []string `json:"amended_tasks"`
	UnblockedTasks   []string `json:"unblocked_tasks"`
	NotifiedSessions []string `json:"notified_sessions"`
}

// QuestionAsked asks for information on a task, decision or contract.
type QuestionAsked struct {
	QuestionID string `json:"question_id"`
	Text       string `json:"text"`
	To         string `json:"to"` // role or session; may be empty (C-event-log §4)
}

// QuestionAnswered answers a question.
type QuestionAnswered struct {
	QuestionID string `json:"question_id"`
	Text       string `json:"text"`
}

// ContractDrafted creates a contract.
type ContractDrafted struct {
	Title string `json:"title"`
	Path  string `json:"path"`
	// Aliases are older ids (C-event-log v3 §1, D-alias-field).
	Aliases []string `json:"aliases,omitempty"`
	// BlockedBy are ids the contract waits on (C-event-log v4 §6,
	// D-contract-blockers).
	BlockedBy []string `json:"blocked_by,omitempty"`
}

// ContractBlockersChanged adds and removes ids from a contract's
// blocked_by (C-event-log v4 §6, D-contract-blockers).
type ContractBlockersChanged struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
	Reason  string   `json:"reason"`
}

// ContractFrozen freezes a contract version.
type ContractFrozen struct {
	Version    int    `json:"version"`
	TextSHA256 string `json:"text_sha256"`
}

// ContractVersioned records a new frozen version made by a decision.
type ContractVersioned struct {
	Version    int    `json:"version"`
	ByDecision string `json:"by_decision"`
	TextSHA256 string `json:"text_sha256"`
}

// ExternalAdded creates an external dependency.
type ExternalAdded struct {
	What      string   `json:"what"`
	OwnerTeam string   `json:"owner_team"`
	AskedOn   string   `json:"asked_on"`
	NeededBy  string   `json:"needed_by"`
	Blocks    []string `json:"blocks"`
	// Aliases are older ids (C-event-log v3 §1, D-alias-field).
	Aliases []string `json:"aliases,omitempty"`
}

// ExternalUpdated changes fields of an external dependency. Fields maps an
// ExternalAdded field name to its new value.
type ExternalUpdated struct {
	Fields map[string]json.RawMessage `json:"fields"`
}

// ExternalResolved closes an external dependency.
type ExternalResolved struct {
	Note string `json:"note"`
}

// RoutedToOwner puts an item in an owner's inbox (C-event-log §4).
type RoutedToOwner struct {
	RouteID   string `json:"route_id"`
	To        string `json:"to"`
	FromEvent string `json:"from_event"`
	Reason    string `json:"reason"`
}

// RouteResolved closes a routed item.
type RouteResolved struct {
	RouteID string `json:"route_id"`
	Note    string `json:"note"`
}

// RoleClaimed takes a role for a session.
type RoleClaimed struct {
	Role    Role   `json:"role"`
	Session string `json:"session"`
}

// RoleReleased gives a role up.
type RoleReleased struct {
	Role    Role   `json:"role"`
	Session string `json:"session"`
}

// ConflictDetected records a contradiction found by a rule of C-event-log §5.
type ConflictDetected struct {
	Rule   string   `json:"rule"`
	Events []string `json:"events"`
}

// SpineApproved records the lead's approval of the spine's current text
// (C-event-log v5 §6, D-post-switch-hand-edits). Subject: project.
type SpineApproved struct {
	TextSHA256 string `json:"text_sha256"`
}

// NewPayload returns a pointer to a zero payload for t, or nil for an
// unknown type.
func NewPayload(t Type) any {
	switch t {
	case TaskDraftedType:
		return new(TaskDrafted)
	case TaskApprovedType:
		return new(TaskApproved)
	case TaskAmendedType:
		return new(TaskAmended)
	case TaskBlockersChangedType:
		return new(TaskBlockersChanged)
	case TaskSessionAttachedType:
		return new(TaskSessionAttached)
	case TaskHandoffType:
		return new(TaskHandoff)
	case TaskDeferredType:
		return new(TaskDeferred)
	case DecisionRaisedType:
		return new(DecisionRaised)
	case DecisionDecidedType:
		return new(DecisionDecided)
	case DecisionDefaultedType:
		return new(DecisionDefaulted)
	case DecisionSupersededType:
		return new(DecisionSuperseded)
	case DecisionAppliedType:
		return new(DecisionApplied)
	case QuestionAskedType:
		return new(QuestionAsked)
	case QuestionAnsweredType:
		return new(QuestionAnswered)
	case ContractDraftedType:
		return new(ContractDrafted)
	case ContractBlockersChangedType:
		return new(ContractBlockersChanged)
	case ContractFrozenType:
		return new(ContractFrozen)
	case ContractVersionedType:
		return new(ContractVersioned)
	case ExternalAddedType:
		return new(ExternalAdded)
	case ExternalUpdatedType:
		return new(ExternalUpdated)
	case ExternalResolvedType:
		return new(ExternalResolved)
	case RoutedToOwnerType:
		return new(RoutedToOwner)
	case RouteResolvedType:
		return new(RouteResolved)
	case RoleClaimedType:
		return new(RoleClaimed)
	case RoleReleasedType:
		return new(RoleReleased)
	case ConflictDetectedType:
		return new(ConflictDetected)
	case SpineApprovedType:
		return new(SpineApproved)
	}
	return nil
}

// jsonKeys returns the top-level JSON keys of the struct p points to.
func jsonKeys(p any) []string {
	t := reflect.TypeOf(p)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	var keys []string
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		keys = append(keys, name)
	}
	return keys
}

// RawDataKeys returns the top-level keys present in an event's Data. It
// lets validators tell a missing field from an empty one and spot fields
// that must not be there (such as decision lifecycle fields).
func RawDataKeys(e Event) ([]string, error) {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(e.Data, &all); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	return keys, nil
}
