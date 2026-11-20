package legacyimport

import (
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Import actors. The import replays history in the lanes of C-event-log §3:
// the lead approves, decides and freezes; the orchestrator drafts and
// amends; sessions hand off; the loop applies, defaults and attaches.
const (
	orchestratorID = "orchestrator"
	loopID         = "loop"
)

// rank orders events that share a millisecond, which is common: most
// records only carry a date, so their events sit at 12:00:00Z on it. It is
// the first entropy byte of the event's ULID, so sorting by id (the log
// order, C-event-log §2) follows a record's lifecycle.
var rank = map[event.Type]byte{
	event.ContractDraftedType:     0,
	event.ContractFrozenType:      1,
	event.TaskDraftedType:         2,
	event.TaskSessionAttachedType: 3,
	event.TaskApprovedType:        4,
	event.DecisionRaisedType:      5,
	event.TaskAmendedType:         6,
	event.TaskHandoffType:         7,
	event.DecisionDecidedType:     8,
	event.DecisionDefaultedType:   8,
	event.DecisionAppliedType:     9,
}

// Payloads with the fields C-event-log v3 adds or that the import keeps
// beyond the typed structs. Unknown data fields are kept by readers
// (C-event-log §2, §9).

type taskDrafted struct {
	event.TaskDrafted
	Session string    `json:"session,omitempty"` // C-event-log v3; not yet in event.TaskDrafted
	Legacy  yamlExtra `json:"legacy,omitempty"`
}

type contractDrafted struct {
	event.ContractDrafted
	Legacy yamlExtra `json:"legacy,omitempty"`
}

type decisionRaised struct {
	event.DecisionRaised
	Legacy yamlExtra `json:"legacy,omitempty"`
}

type decisionDecided struct {
	event.DecisionDecided
	Legacy yamlExtra `json:"legacy,omitempty"`
}

type decisionDefaulted struct {
	event.DecisionDefaulted
	Legacy yamlExtra `json:"legacy,omitempty"`
}

type decisionApplied struct {
	event.DecisionApplied
	Legacy yamlExtra `json:"legacy,omitempty"`
}

// planned is an event before its id is fixed.
type planned struct {
	typ     event.Type
	at      time.Time
	actor   event.Actor
	subject event.Subject
	data    any
	source  string // the hand-written file it came from
}

// Note is something the import noticed about a record: a legacy shape it
// had to fill in, a malformed field it recovered, or a fact worth reporting.
type Note struct {
	Record string `json:"record"`
	Kind   string `json:"kind"` // malformed | legacy-shape | info
	Text   string `json:"text"`
}

type builder struct {
	in      *Inputs
	ids     aliases
	lead    event.Actor
	events  []planned
	notes   []Note
	briefs  map[string]*taskBrief // by task id
	epoch   time.Time             // date for records that carry none
	unknown []string              // old ids missing from the mapping
}

func (b *builder) note(record, kind, format string, args ...any) {
	b.notes = append(b.notes, Note{Record: record, Kind: kind, Text: fmt.Sprintf(format, args...)})
}

func (b *builder) add(p planned) { b.events = append(b.events, p) }

// ref maps an id in a record to its slug id, noting any that is neither a
// slug id nor in the mapping.
func (b *builder) ref(record, id string) string {
	n, ok := b.ids.newID(id)
	if !ok {
		b.unknown = append(b.unknown, fmt.Sprintf("%s: %q", record, id))
	}
	return n
}

func (b *builder) refs(record string, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, b.ref(record, id))
	}
	return out
}

// noon returns 12:00:00Z on a date (the brief's rule when only a date is
// known), or the epoch date when the date is missing or bad.
func (b *builder) noon(record, field, date string) time.Time {
	d, err := time.Parse(time.DateOnly, strings.TrimSpace(date))
	if err != nil {
		if date != "" {
			b.note(record, "malformed", "%s %q is not a date; used %s", field, date, b.epoch.Format(time.DateOnly))
		}
		return b.epoch
	}
	return d.Add(12 * time.Hour).UTC()
}

// build turns the inputs into planned events.
func (b *builder) build() error {
	b.ids = newAliases()
	for _, c := range b.in.Contracts {
		for _, a := range c.Aliases {
			b.ids[a] = c.ID
		}
	}
	for _, d := range b.in.Decisions {
		for _, a := range d.Aliases {
			b.ids[a] = d.ID
		}
	}
	b.epoch = b.earliestDate()
	b.lead = event.Actor{Role: event.RoleLead, ID: cmp.Or(b.in.Lead, "lead")}

	if v, ok := b.in.Externals.(map[string]any); b.in.Externals != nil && (!ok || len(v) > 0) {
		return errorf("registry.yaml: external dependencies are not supported by this import (found %v)", b.in.Externals)
	}
	for _, c := range b.in.Contracts {
		b.contract(c)
	}
	b.briefs = map[string]*taskBrief{}
	for _, t := range b.in.Tasks {
		if err := b.task(t); err != nil {
			return err
		}
	}
	for _, d := range b.in.Decisions {
		b.decision(d)
	}
	if len(b.unknown) > 0 {
		slices.Sort(b.unknown)
		return errorf("ids missing from the id mapping (escalate): %s", strings.Join(b.unknown, "; "))
	}
	return nil
}

// earliestDate is the earliest date found in any record. Records with no
// date (tasks never approved, planned contracts) are placed on it. It only
// moves if a record dated earlier than all others appears.
func (b *builder) earliestDate() time.Time {
	var dates []string
	for _, d := range b.in.Decisions {
		for _, kv := range mappingPairs(&d.RaisedBy) {
			dates = append(dates, kv[1].Value)
		}
		dates = append(dates, d.DecidedOn, d.AppliedOn)
	}
	for _, t := range b.in.Tasks {
		if t.Approved != nil {
			dates = append(dates, *t.Approved)
		}
	}
	for _, c := range b.in.Contracts {
		dates = append(dates, c.FrozenOn)
	}
	var best time.Time
	for _, s := range dates {
		d, err := time.Parse(time.DateOnly, strings.TrimSpace(s))
		if err == nil && (best.IsZero() || d.Before(best)) {
			best = d
		}
	}
	if best.IsZero() {
		best = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return best.Add(12 * time.Hour).UTC()
}

func (b *builder) contract(c *legacyContract) {
	rec := "contract " + c.ID
	if c.File == "" {
		rec += " (registry.yaml)"
	} else {
		rec += " (" + c.File + ")"
	}
	sub := event.Subject{Kind: event.KindContract, ID: c.ID}
	legacy := yamlExtra{}
	if len(c.BlockedBy) > 0 {
		legacy["blocked_by"] = b.refs(rec, c.BlockedBy)
		b.note(rec, "info", "blocked_by %v kept as legacy data: C-event-log has no contract blockers", c.BlockedBy)
	}
	at := b.noon(rec, "frozen_on", c.FrozenOn)
	switch c.Status {
	case "frozen":
	case "draft":
	default:
		legacy["status"] = c.Status
		b.note(rec, "info", "status %q is not a C-event-log contract status; imported as draft, status kept as legacy data", c.Status)
	}
	if c.FrozenOn == "" {
		legacy["date_unknown"] = true
	}
	if len(legacy) == 0 {
		legacy = nil
	}
	b.add(planned{typ: event.ContractDraftedType, at: at, subject: sub, source: cmp.Or(c.File, "registry.yaml"),
		actor: event.Actor{Role: event.RoleOrchestrator, ID: orchestratorID},
		data:  contractDrafted{event.ContractDrafted{Title: c.Title, Path: c.File, Aliases: c.Aliases}, legacy}})
	if c.Status == "frozen" {
		if c.File == "" {
			b.note(rec, "malformed", "frozen but has no file; not frozen")
			return
		}
		b.add(planned{typ: event.ContractFrozenType, at: at, subject: sub, actor: b.lead, source: c.File,
			data: event.ContractFrozen{Version: max(c.Version, 1), TextSHA256: c.TextSHA256}})
	}
}

var approvedRe = strings.ToLower("approved by lead")

func (b *builder) task(t *legacyTask) error {
	rec := "task " + t.OldID + " (registry.yaml)"
	if t.ID != t.OldID {
		rec = fmt.Sprintf("task %s → %s (registry.yaml)", t.OldID, t.ID)
	}
	if err := event.ValidateIDOfKind(t.ID, event.KindTask); err != nil {
		b.unknown = append(b.unknown, fmt.Sprintf("registry task %q", t.OldID))
		return nil
	}
	sub := event.Subject{Kind: event.KindTask, ID: t.ID}
	br, err := readBrief(b.in.fsys, t, t.Session)
	if err != nil {
		return err
	}
	if br != nil {
		b.briefs[t.ID] = br
	}

	// When: the first amendment saying "Approved by lead" (a full UTC
	// timestamp), else 12:00Z on the registry's approved date, else the
	// epoch date.
	var at time.Time
	var approvalEntry string
	if br != nil {
		for _, e := range br.Amendments {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(e.body())), approvedRe) {
				at, approvalEntry = e.At, e.Rel
				break
			}
		}
	}
	if at.IsZero() && t.Approved != nil {
		at = b.noon(rec, "approved", *t.Approved)
	}
	legacy := yamlExtra{}
	if at.IsZero() {
		at = b.epoch
		legacy["date_unknown"] = true
	}
	for k, v := range t.Extra {
		legacy[k] = v
	}
	if len(legacy) == 0 {
		legacy = nil
	}
	if t.Approved != nil && approvalEntry != "" && at.Format(time.DateOnly) != *t.Approved {
		b.note(rec, "info", "approved %s in the registry; the approval entry %s is %s UTC, which the import uses",
			*t.Approved, approvalEntry, at.Format(time.RFC3339))
	}

	brief := ""
	if br != nil {
		brief = br.Rel
	}
	var aliasesOut []string
	if t.ID != t.OldID {
		aliasesOut = []string{t.OldID}
	}
	td := taskDrafted{TaskDrafted: event.TaskDrafted{
		Title: t.Title, Kind: t.Kind, Brief: brief, Repo: t.Repo, BaseBranch: t.BaseBranch,
		Branch: t.Branch, Worktree: t.Worktree, BlockedBy: b.refs(rec, orEmpty(t.BlockedBy)),
		TierGrants: orEmpty(t.TierGrants), Aliases: aliasesOut,
	}, Session: t.Session, Legacy: legacy}
	orch := event.Actor{Role: event.RoleOrchestrator, ID: orchestratorID}
	b.add(planned{typ: event.TaskDraftedType, at: at, subject: sub, actor: orch, data: td, source: "registry.yaml"})
	if t.Session != "" {
		// Today's core reads a session's own task only from
		// TaskSessionAttached (loop lane), so the attachment is also
		// written as its own event.
		b.add(planned{typ: event.TaskSessionAttachedType, at: at, subject: sub, source: "registry.yaml",
			actor: event.Actor{Role: event.RoleLoop, ID: loopID},
			data:  event.TaskSessionAttached{Session: t.Session, Adapter: ""}})
	}
	if t.Approved != nil {
		if br == nil {
			b.note(rec, "malformed", "approved but has no brief; TaskApproved has no text to hash")
		}
		sha := ""
		if br != nil {
			sha = sha256Hex(br.Text)
		}
		b.add(planned{typ: event.TaskApprovedType, at: at, subject: sub, actor: b.lead, source: "registry.yaml",
			data: event.TaskApproved{TierGrants: orEmpty(t.TierGrants), TextSHA256: sha}})
	}
	if br == nil {
		return nil
	}
	for _, e := range br.Amendments {
		b.add(planned{typ: event.TaskAmendedType, at: e.At, subject: sub, actor: orch, source: e.Rel,
			data: event.TaskAmended{File: e.Rel, TextSHA256: sha256Hex(e.Text), Note: firstLine(e.body(), 200)}})
	}
	for _, e := range br.Handoffs {
		secs := handoffSections(e.body())
		if _, ok := secs["STATUS"]; !ok {
			b.note(e.Rel, "malformed", "handoff has no STATUS section")
		}
		b.add(planned{typ: event.TaskHandoffType, at: e.At, subject: sub, source: e.Rel,
			actor: event.Actor{Role: event.RoleSession, ID: e.Author},
			data: event.TaskHandoff{File: e.Rel, TextSHA256: sha256Hex(e.Text),
				StatusLine:    firstLine(secs["STATUS"], 200),
				HasFollowups:  nonEmptySection(secs["FOLLOW-UPS"]),
				HasDecisions:  nonEmptySection(secs["DECISIONS RAISED"]),
				HasDeviations: nonEmptySection(secs["DEVIATIONS"])}})
	}
	return nil
}

func (b *builder) decision(d *legacyDecision) {
	rec := d.File
	oldID := d.ID
	id := b.ref(rec, d.ID)
	sub := event.Subject{Kind: event.KindDecision, ID: id}
	legacy := yamlExtra{}
	for k, v := range d.Extra {
		legacy[k] = v
		b.note(rec, "info", "unknown field %q kept as legacy data", k)
	}

	// raised_by
	var rb event.RaisedBy
	for _, kv := range mappingPairs(&d.RaisedBy) {
		k, v := kv[0].Value, kv[1]
		switch k {
		case "task":
			if !isNull(v) {
				t := v.Value
				n, _ := b.ids.newID(t)
				if event.ValidateIDOfKind(n, event.KindTask) == nil && b.hasTask(n) {
					rb.Task = &n
				} else {
					legacy["raised_by_task"] = t
					b.note(rec, "legacy-shape", "raised_by.task %q is not a task; imported as null, kept as legacy.raised_by_task", t)
				}
			}
		case "session":
			rb.Session = v.Value
		case "on":
			rb.On = v.Value
		case "true":
			// YAML 1.1 writers turn the key "on" into the boolean true.
			rb.On = v.Value
			b.note(rec, "malformed", "raised_by has key `true:` instead of `on:` (a YAML 1.1 writer's boolean); read as on: %s", v.Value)
		default:
			legacy["raised_by_"+k] = nodeValue(v)
		}
	}
	raisedAt := b.noon(rec, "raised_by.on", rb.On)

	var aliasesOut []string
	if oldID != id {
		aliasesOut = []string{oldID}
	}
	aliasesOut = append(aliasesOut, d.Aliases...)
	opts := make([]event.Option, 0, len(d.Options))
	for _, o := range d.Options {
		opts = append(opts, event.Option{ID: o.ID, Tradeoff: o.Tradeoff})
	}
	blocks, impact := []string{}, []string{}
	if d.Blocks == nil {
		b.note(rec, "legacy-shape", "no blocks field; imported as []")
	} else {
		blocks = b.refs(rec, *d.Blocks)
	}
	if d.ContractImpact == nil {
		b.note(rec, "legacy-shape", "no contract_impact field; imported as []")
	} else {
		impact = b.refs(rec, *d.ContractImpact)
	}
	if strings.TrimSpace(d.Context.Summary) == "" {
		b.note(rec, "legacy-shape", "no context.summary")
	}
	if len(d.Questions.Content) > 0 {
		legacy["questions"] = nodeValue(&d.Questions)
		b.note(rec, "info", "hand-written questions kept as legacy data")
	}
	raised := event.DecisionRaised{
		ID: id, Aliases: aliasesOut, Title: d.Title, Tier: d.Tier, RaisedBy: rb, Question: d.Question,
		Options: opts, Recommendation: d.Recommendation, DefaultIfNoAnswer: d.DefaultIfNoAnswer,
		Deadline: d.Deadline, Blocks: blocks, ContractImpact: impact,
		Context: event.DecisionContext{Summary: d.Context.Summary, Evidence: d.Context.Evidence,
			PriorDecisions: b.refs(rec, d.Context.PriorDecisions)},
	}
	if len(raised.Context.PriorDecisions) == 0 {
		raised.Context.PriorDecisions = nil
	}
	if len(legacy) == 0 {
		legacy = nil
	}
	b.add(planned{typ: event.DecisionRaisedType, at: raisedAt, subject: sub, source: rec,
		actor: b.raiser(rb.Session), data: decisionRaised{raised, legacy}})

	// Lifecycle.
	decidedAt := b.noon(rec, "decided_on", d.DecidedOn)
	var note yamlExtra
	if d.OrchestratorNote != "" {
		note = yamlExtra{"orchestrator_note": d.OrchestratorNote}
		b.note(rec, "info", "orchestrator_note kept as legacy.orchestrator_note on the %s event", map[string]string{
			"decided": "DecisionDecided", "defaulted": "DecisionDefaulted"}[d.Status])
	}
	switch d.Status {
	case "decided":
		if d.DecidedBy != "" && d.DecidedBy != "lead" && d.DecidedBy != b.lead.ID {
			b.note(rec, "malformed", "decided_by %q is not the lead", d.DecidedBy)
		}
		b.add(planned{typ: event.DecisionDecidedType, at: decidedAt, subject: sub, actor: b.lead, source: rec,
			data: decisionDecided{event.DecisionDecided{Answer: d.Answer, Note: d.AnswerNote}, note}})
	case "defaulted":
		lg := yamlExtra{}
		if d.AnswerNote != "" {
			lg["note"] = d.AnswerNote
		}
		if d.OrchestratorNote != "" {
			lg["orchestrator_note"] = d.OrchestratorNote
		}
		if len(lg) == 0 {
			lg = nil
		}
		b.add(planned{typ: event.DecisionDefaultedType, at: decidedAt, subject: sub, source: rec,
			actor: event.Actor{Role: event.RoleLoop, ID: loopID},
			data:  decisionDefaulted{event.DecisionDefaulted{Answer: d.Answer, Deadline: deref(d.Deadline)}, lg}})
	case "pending", "":
		if d.OrchestratorNote != "" {
			b.note(rec, "malformed", "orchestrator_note on a pending decision is not imported")
		}
	case "superseded":
		if d.SupersededBy == "" {
			b.note(rec, "malformed", "superseded with no superseded_by")
		}
		by := d.SupersededBy
		if by != "" {
			by = b.ref(rec, by)
		}
		b.add(planned{typ: event.DecisionSupersededType, at: decidedAt, subject: sub, actor: b.lead, source: rec,
			data: event.DecisionSuperseded{By: by, Note: d.AnswerNote}})
	default:
		b.note(rec, "malformed", "status %q is not a decision status; lifecycle not imported", d.Status)
	}
	if d.AppliedOn != "" {
		b.add(planned{typ: event.DecisionAppliedType, at: b.noon(rec, "applied_on", d.AppliedOn), subject: sub,
			actor: event.Actor{Role: event.RoleLoop, ID: loopID}, source: rec, data: b.applied(rec, &d.Applied)})
	}
}

// applied maps a hand-written `applied` block. Old records list
// {amended, unblocked, notified_sessions} with free text and contract ids;
// task ids go into the typed lists and the block is kept whole as legacy
// data when anything in it is not a task id.
func (b *builder) applied(rec string, n *yaml.Node) decisionApplied {
	out := decisionApplied{DecisionApplied: event.DecisionApplied{
		AmendedTasks: []string{}, UnblockedTasks: []string{}, NotifiedSessions: []string{}}}
	lossy := false
	for _, kv := range mappingPairs(n) {
		var list []string
		if err := kv[1].Decode(&list); err != nil {
			lossy = true
			continue
		}
		switch kv[0].Value {
		case "amended", "amended_tasks", "unblocked", "unblocked_tasks":
			dst := &out.AmendedTasks
			if strings.HasPrefix(kv[0].Value, "unblocked") {
				dst = &out.UnblockedTasks
			}
			for _, s := range list {
				id, ok := b.ids.newID(s)
				if ok && event.KindOf(id) == event.KindTask && b.hasTask(id) {
					*dst = append(*dst, id)
				} else {
					lossy = true
				}
			}
			if kv[0].Value == "amended" || kv[0].Value == "unblocked" {
				lossy = true
			}
		case "notified_sessions":
			out.NotifiedSessions = append(out.NotifiedSessions, list...)
		default:
			lossy = true
		}
	}
	if lossy {
		out.Legacy = yamlExtra{"applied": nodeValue(n)}
		b.note(rec, "legacy-shape", "applied %v names no task ids in C-decision-record's shape; kept whole as legacy.applied", nodeValue(n))
	}
	return out
}

func (b *builder) hasTask(id string) bool {
	return slices.ContainsFunc(b.in.Tasks, func(t *legacyTask) bool { return t.ID == id })
}

// raiser is the actor of a DecisionRaised: the lead, the orchestrator, or
// the named session.
func (b *builder) raiser(session string) event.Actor {
	switch session {
	case "lead", b.lead.ID:
		return b.lead
	case "orchestrator":
		return event.Actor{Role: event.RoleOrchestrator, ID: orchestratorID}
	}
	return event.Actor{Role: event.RoleSession, ID: cmp.Or(session, "unknown")}
}

// finish fixes ids. An event id is a ULID whose time is the event's `at`
// and whose entropy is the lifecycle rank followed by the first nine bytes
// of a SHA-256 of the event's content, so the same inputs always give the
// same ids and bytes.
func finish(ps []planned) ([]event.Event, map[string]string, error) {
	out := make([]event.Event, 0, len(ps))
	sources := map[string]string{}
	for _, p := range ps {
		raw, err := json.Marshal(p.data)
		if err != nil {
			return nil, nil, err
		}
		h := sha256.New()
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00", p.typ, p.subject.Kind, p.subject.ID,
			p.actor.Role, p.actor.ID, p.at.UTC().Format(time.RFC3339Nano))
		h.Write(raw)
		sum := h.Sum(nil)
		var ent [10]byte
		ent[0] = rank[p.typ]
		copy(ent[1:], sum[:9])
		id, err := event.ULIDFromParts(uint64(p.at.UnixMilli()), ent)
		if err != nil {
			return nil, nil, err
		}
		e, err := event.New(id, p.typ, p.at, p.actor, p.subject, json.RawMessage(raw))
		if err != nil {
			return nil, nil, err
		}
		if err := e.Validate(); err != nil {
			return nil, nil, errorf("%s %s/%s from %s: %w", p.typ, p.subject.Kind, p.subject.ID, p.source, err)
		}
		out = append(out, e)
		sources[id] = p.source
	}
	slices.SortFunc(out, func(a, b event.Event) int { return strings.Compare(a.ID, b.ID) })
	for i := 1; i < len(out); i++ {
		if out[i].ID == out[i-1].ID {
			return nil, nil, errorf("two events with id %s (identical content)", out[i].ID)
		}
	}
	return out, sources, nil
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
