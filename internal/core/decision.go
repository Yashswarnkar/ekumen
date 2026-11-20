package core

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// Limits from C-decision-record §1.
const (
	MaxTitleLength  = 100 // characters
	MaxSummaryWords = 150
	MinOptions      = 2
)

// FieldError is one broken rule in a decision record, naming the field.
// Rule is the number of the rule in C-decision-record §2, or 0 for a §1
// field rule that §2 does not list.
type FieldError struct {
	Field string
	Rule  int
	Msg   string
}

func (e FieldError) Error() string {
	if e.Rule > 0 {
		return fmt.Sprintf("%s: %s (C-decision-record §2 rule %d)", e.Field, e.Msg, e.Rule)
	}
	return fmt.Sprintf("%s: %s (C-decision-record §1)", e.Field, e.Msg)
}

// ValidationError lists every broken rule found in one record.
type ValidationError []FieldError

func (v ValidationError) Error() string {
	msgs := make([]string, len(v))
	for i, e := range v {
		msgs[i] = e.Error()
	}
	return "invalid decision record: " + strings.Join(msgs, "; ")
}

// Fields returns the names of the fields with errors, in order.
func (v ValidationError) Fields() []string {
	out := make([]string, len(v))
	for i, e := range v {
		out[i] = e.Field
	}
	return out
}

// IDLookup answers whether an id or alias is in use. *State implements it.
type IDLookup interface {
	Has(id string) bool
	Resolve(id string) string
}

var optionIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const maxOptionIDLength = 40

func validDate(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// ValidateDecision checks a decision record against C-decision-record §1 and
// every rule of §2. ids answers rules 6 and 7 (existence and ids in use); it
// should describe the log before this record is added. It returns nil or a
// ValidationError naming each bad field.
func ValidateDecision(rec event.DecisionRaised, ids IDLookup) error {
	var errs ValidationError
	add := func(field string, rule int, format string, args ...any) {
		errs = append(errs, FieldError{Field: field, Rule: rule, Msg: fmt.Sprintf(format, args...)})
	}

	// Rule 7: id format and not in use.
	if err := event.ValidateIDOfKind(rec.ID, event.KindDecision); err != nil {
		add("id", 7, "%v", err)
	} else if ids.Has(rec.ID) {
		add("id", 7, "%q is already in use", rec.ID)
	}
	for i, a := range rec.Aliases {
		f := fmt.Sprintf("aliases[%d]", i)
		switch err := event.ValidateAlias(a); {
		case err != nil:
			add(f, 0, "%v", err)
		case a == rec.ID:
			add(f, 7, "%q is the record's own id", a)
		case slices.Contains(rec.Aliases[:i], a):
			add(f, 7, "%q is listed twice", a)
		case ids.Has(a):
			add(f, 7, "%q is already in use", a)
		}
	}

	if strings.TrimSpace(rec.Title) == "" {
		add("title", 0, "required")
	} else if n := utf8.RuneCountInString(rec.Title); n > MaxTitleLength {
		add("title", 0, "%d characters, limit %d", n, MaxTitleLength)
	}
	if rec.Tier != 1 && rec.Tier != 2 {
		add("tier", 0, "must be 1 or 2, got %d", rec.Tier)
	}

	if rec.RaisedBy.Task != nil {
		if t := *rec.RaisedBy.Task; event.KindOf(ids.Resolve(t)) != event.KindTask || !ids.Has(t) {
			add("raised_by.task", 0, "no task %q", t)
		}
	}
	if strings.TrimSpace(rec.RaisedBy.Session) == "" {
		add("raised_by.session", 0, "required")
	}
	if !validDate(rec.RaisedBy.On) {
		add("raised_by.on", 0, "must be a date (YYYY-MM-DD), got %q", rec.RaisedBy.On)
	}
	if strings.TrimSpace(rec.Question) == "" {
		add("question", 0, "required")
	}

	if len(rec.Options) < MinOptions {
		add("options", 0, "at least %d required, got %d", MinOptions, len(rec.Options))
	}
	var optionIDs []string
	for i, o := range rec.Options {
		f := fmt.Sprintf("options[%d]", i)
		switch {
		case !optionIDPattern.MatchString(o.ID) || len(o.ID) > maxOptionIDLength:
			add(f+".id", 0, "%q is not a short slug", o.ID)
		case slices.Contains(optionIDs, o.ID):
			add(f+".id", 0, "%q is not unique within the record", o.ID)
		}
		optionIDs = append(optionIDs, o.ID)
		if strings.TrimSpace(o.Tradeoff) == "" {
			add(f+".tradeoff", 0, "required")
		}
	}

	// Rule 4: recommendation and default must name an option.
	if r := rec.Recommendation; r != nil && !slices.Contains(optionIDs, *r) {
		add("recommendation", 4, "%q is not one of the options", *r)
	}
	if d := rec.DefaultIfNoAnswer; d != nil && !slices.Contains(optionIDs, *d) {
		add("default_if_no_answer", 4, "%q is not one of the options", *d)
	}

	// Rules 1 and 2: tier-dependent default and deadline.
	switch rec.Tier {
	case 1:
		if rec.DefaultIfNoAnswer == nil {
			add("default_if_no_answer", 1, "required for tier 1")
		}
		if rec.Deadline == nil {
			add("deadline", 1, "required for tier 1")
		}
	case 2:
		if rec.DefaultIfNoAnswer != nil {
			add("default_if_no_answer", 2, "must be null for tier 2")
		}
	}
	if rec.Deadline != nil && !validDate(*rec.Deadline) {
		add("deadline", 0, "must be a date (YYYY-MM-DD), got %q", *rec.Deadline)
	}

	// Rule 6: blocks and contract_impact name existing items of the right kind.
	if rec.Blocks == nil {
		add("blocks", 0, "required (use [] for none)")
	}
	for i, b := range rec.Blocks {
		if r := ids.Resolve(b); event.KindOf(r) != event.KindTask || !ids.Has(b) {
			add(fmt.Sprintf("blocks[%d]", i), 6, "no task %q", b)
		}
	}
	if rec.ContractImpact == nil {
		add("contract_impact", 0, "required (use [] for none)")
	}
	for i, c := range rec.ContractImpact {
		if r := ids.Resolve(c); event.KindOf(r) != event.KindContract || !ids.Has(c) {
			add(fmt.Sprintf("contract_impact[%d]", i), 6, "no contract %q", c)
		}
	}
	// Rule 3: a contract change is always tier 2.
	if len(rec.ContractImpact) > 0 && rec.Tier != 2 {
		add("contract_impact", 3, "non-empty, so tier must be 2 (got %d)", rec.Tier)
	}

	// Rule 5: summary length.
	if strings.TrimSpace(rec.Context.Summary) == "" {
		add("context.summary", 0, "required")
	} else if n := len(strings.Fields(rec.Context.Summary)); n > MaxSummaryWords {
		add("context.summary", 5, "%d words, limit %d", n, MaxSummaryWords)
	}
	for i, ev := range rec.Context.Evidence {
		if strings.TrimSpace(ev) == "" {
			add(fmt.Sprintf("context.evidence[%d]", i), 0, "empty")
		}
	}
	for i, p := range rec.Context.PriorDecisions {
		if event.ValidateIDOfKind(p, event.KindDecision) != nil && event.KindOf(ids.Resolve(p)) != event.KindDecision {
			add(fmt.Sprintf("context.prior_decisions[%d]", i), 0, "%q is not a decision id or alias", p)
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ValidateDecisionRaised checks a DecisionRaised event as the CLI must before
// writing it (C-decision-record §2): the payload is a valid record per
// ValidateDecision, its id is the event's subject id, and it carries no
// lifecycle fields (C-event-log §6: "minus lifecycle fields"). s is the
// state before the event.
func ValidateDecisionRaised(s *State, e event.Event) error {
	if e.Type != event.DecisionRaisedType {
		return fmt.Errorf("not a %s event: %s", event.DecisionRaisedType, e.Type)
	}
	var errs ValidationError
	var all map[string]json.RawMessage
	if err := json.Unmarshal(e.Data, &all); err != nil {
		return ValidationError{{Field: "data", Msg: err.Error()}}
	}
	for _, f := range event.DecisionLifecycleFields {
		if _, ok := all[f]; ok {
			errs = append(errs, FieldError{Field: f, Msg: "lifecycle field; set by events only, never by the raiser"})
		}
	}
	p, err := e.Payload()
	if err != nil {
		return append(errs, FieldError{Field: "data", Msg: err.Error()})
	}
	rec := *p.(*event.DecisionRaised)
	if rec.ID != e.Subject.ID {
		errs = append(errs, FieldError{Field: "id", Rule: 7, Msg: fmt.Sprintf("%q differs from subject.id %q", rec.ID, e.Subject.ID)})
	}
	if err := ValidateDecision(rec, s); err != nil {
		errs = append(errs, err.(ValidationError)...)
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}
