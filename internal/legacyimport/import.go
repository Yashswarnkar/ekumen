// Package legacyimport turns the records written by hand before the ekumen
// CLI existed (decisions/*.yaml, registry.yaml, contract front matter, old
// single-file briefs and new-layout briefs) into C-event-log events, with
// slug ids and the old ids kept as aliases (T-import).
//
// The import is deterministic: event ids are ULIDs made from each event's
// time and a hash of its content, so the same inputs always give the same
// bytes, and a re-run on unchanged inputs writes nothing. Before anything is
// written, the events are reduced with core.Reduce and compared with every
// hand-written record; any mismatch fails the import.
package legacyimport

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/store/files"
)

// Plan is what an import would do, and what it found.
type Plan struct {
	// Events is every event the inputs describe, in log order.
	Events []event.Event `json:"-"`
	// New are the events not already in the log; Unchanged counts the rest.
	New       []event.Event `json:"-"`
	Unchanged int           `json:"unchanged_events"`
	// Drift lists creating or lifecycle events the inputs now describe
	// differently from what an earlier run already wrote. They are not
	// written again (the first one stands); they need a correction event.
	Drift []string `json:"drift"`

	// Writes are brief files the split of old briefs creates; Removes the
	// old single-file briefs it replaces.
	Writes  []string `json:"writes"`
	Removes []string `json:"removes"`

	Mapped     Counts            `json:"mapped"`
	ByType     map[string]int    `json:"events_by_type"`
	Aliases    map[string]string `json:"aliases"`
	Mismatches []Mismatch        `json:"mismatches"`
	Problems   []RecordProblem   `json:"record_problems"`
	// CoreLagProblems are validation failures caused by internal/core
	// lagging the contracts, not by the records.
	CoreLagProblems []RecordProblem `json:"core_lag_record_problems"`
	Notes           []Note          `json:"notes"`
	// Conflicts are C-event-log §5 conflicts the core finds in the log
	// after the import. CoreLagConflicts are lane conflicts that C-event-log
	// v3 allows but internal/core, built on v1, still reports.
	Conflicts        []core.Conflict `json:"conflicts"`
	CoreLagConflicts []core.Conflict `json:"core_lag_conflicts"`

	writes []newFile
	root   string
}

// Counts are records mapped to events, by kind.
type Counts struct {
	Decisions int `json:"decisions"`
	Tasks     int `json:"tasks"`
	Contracts int `json:"contracts"`
	Briefs    int `json:"briefs"`
	Entries   int `json:"entries"` // amendments and handoffs
}

// OK reports whether the plan may be applied: no mismatches and no
// conflicts beyond the known core lag.
func (p *Plan) OK() bool { return len(p.Mismatches) == 0 && len(p.Conflicts) == 0 }

// Prepare reads the .ekumen/ folder at root and plans the import. It writes
// nothing.
func Prepare(root string) (*Plan, error) {
	return prepare(os.DirFS(root), files.New(root), root)
}

// existingLog returns the events already in the store.
func existingLog(st *files.Store) ([]event.Event, error) {
	if st == nil {
		return nil, nil
	}
	raws, _, err := st.ReadAll()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]event.Event, 0, len(raws))
	for _, r := range raws {
		e, err := event.Decode(r)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// singletonKey names events that a subject has at most once in an import:
// a second one with other content means the inputs changed after an
// earlier run.
func singletonKey(e event.Event) string {
	switch e.Type {
	case event.TaskDraftedType, event.TaskApprovedType, event.TaskSessionAttachedType,
		event.DecisionRaisedType, event.DecisionDecidedType, event.DecisionDefaultedType,
		event.DecisionSupersededType, event.DecisionAppliedType, event.ContractDraftedType:
		return string(e.Type) + " " + e.Subject.ID
	case event.ContractFrozenType:
		p, err := e.Payload()
		if err == nil {
			return fmt.Sprintf("%s %s v%d", e.Type, e.Subject.ID, p.(*event.ContractFrozen).Version)
		}
	case event.TaskAmendedType, event.TaskHandoffType:
		var d struct {
			File string `json:"file"`
		}
		if p, err := e.Payload(); err == nil {
			switch x := p.(type) {
			case *event.TaskAmended:
				d.File = x.File
			case *event.TaskHandoff:
				d.File = x.File
			}
		}
		return string(e.Type) + " " + d.File
	}
	return ""
}

func prepare(fsys fs.FS, st *files.Store, root string) (*Plan, error) {
	in, err := readInputs(fsys)
	if err != nil {
		return nil, err
	}
	b := &builder{in: in}
	if err := b.build(); err != nil {
		return nil, err
	}
	evs, sources, err := finish(b.events)
	if err != nil {
		return nil, err
	}
	p := &Plan{Events: evs, root: root, ByType: map[string]int{}, Aliases: map[string]string{},
		Mismatches: []Mismatch{}, Problems: []RecordProblem{}, CoreLagProblems: []RecordProblem{}, Drift: []string{},
		Conflicts: []core.Conflict{}, CoreLagConflicts: []core.Conflict{}}

	// Merge with what is already in the log.
	existing, err := existingLog(st)
	if err != nil {
		return nil, err
	}
	have := map[string][]byte{}
	haveKey := map[string]bool{}
	for _, e := range existing {
		raw, _ := event.Encode(e)
		have[e.ID] = raw
		if k := singletonKey(e); k != "" {
			haveKey[k] = true
		}
	}
	for _, e := range evs {
		p.ByType[string(e.Type)]++
		raw, err := event.Encode(e)
		if err != nil {
			return nil, err
		}
		if old, ok := have[e.ID]; ok {
			if !bytes.Equal(old, raw) {
				return nil, errorf("event %s is already in the log with other content", e.ID)
			}
			p.Unchanged++
			continue
		}
		if k := singletonKey(e); k != "" && haveKey[k] {
			p.Drift = append(p.Drift, fmt.Sprintf("%s (from %s): already in the log with other content; not written", k, sources[e.ID]))
			continue
		}
		p.New = append(p.New, e)
	}
	all := append(slices.Clone(existing), p.New...)
	state := core.Reduce(all)

	// Counts and aliases.
	p.Mapped = Counts{Decisions: len(in.Decisions), Tasks: len(in.Tasks), Contracts: len(in.Contracts)}
	for _, t := range in.Tasks {
		if br := b.briefs[t.ID]; br != nil {
			p.Mapped.Briefs++
			p.Mapped.Entries += len(br.Amendments) + len(br.Handoffs)
			if br.Remove != "" {
				p.Removes = append(p.Removes, br.Remove)
			}
			for _, w := range br.Write {
				have, err := readFileIfExists(fsys, w.Rel)
				if err != nil {
					return nil, err
				}
				if have == nil {
					p.writes = append(p.writes, w)
					p.Writes = append(p.Writes, w.Rel)
				}
			}
		}
	}
	for k, v := range state.Aliases {
		p.Aliases[k] = v
	}

	p.Mismatches = append(p.Mismatches, verify(b, &state)...)
	for _, rp := range checkRecords(&state, evs, sources) {
		if strings.HasPrefix(rp.Field, "aliases[") && strings.Contains(rp.Text, "is a slug id, not an alias") {
			// D-001…D-019 match the slug regex; internal/core's
			// ValidateAlias refuses them although C-event-log §1 names
			// D-012 as an alias.
			p.CoreLagProblems = append(p.CoreLagProblems, rp)
			continue
		}
		p.Problems = append(p.Problems, rp)
	}
	p.Notes = b.notes

	// Conflicts, with the current text of every brief and contract so that
	// frozen-text-changed is checked too.
	texts := map[string]string{}
	for _, br := range b.briefs {
		texts[br.Rel] = sha256Hex(br.Text)
	}
	for _, c := range in.Contracts {
		if c.File != "" {
			texts[c.File] = c.TextSHA256
		}
	}
	byID := map[string]event.Event{}
	for _, e := range all {
		byID[e.ID] = e
	}
	for _, c := range core.Conflicts(&state, all, core.ConflictInputs{TextSHA256: texts, AliveSessions: map[string]bool{}}) {
		if lanesAllowedByV3(&state, c, byID) {
			p.CoreLagConflicts = append(p.CoreLagConflicts, c)
		} else {
			p.Conflicts = append(p.Conflicts, c)
		}
	}
	return p, nil
}

// Written is what Apply wrote.
type Written struct {
	Events  []string `json:"events"`  // paths of event files
	Files   []string `json:"files"`   // brief files from the split of old briefs
	Removed []string `json:"removed"` // old single-file briefs
}

// Apply writes a plan: brief files first, then the new events through the
// file store, then removes the old single-file briefs (their content stays
// in git history). It refuses a plan that is not OK.
func Apply(p *Plan) (*Written, error) {
	if !p.OK() {
		return nil, errorf("refusing to write: %d mismatches, %d conflicts", len(p.Mismatches), len(p.Conflicts))
	}
	st := files.New(p.root)
	w := &Written{Events: []string{}, Files: []string{}, Removed: []string{}}
	for _, f := range p.writes {
		full := filepath.Join(p.root, filepath.FromSlash(f.Rel))
		if err := writeNewFile(full, f.Text); err != nil {
			return w, err
		}
		w.Files = append(w.Files, f.Rel)
	}
	for _, e := range p.New {
		raw, err := event.Encode(e)
		if err != nil {
			return w, err
		}
		path, err := st.Append(raw)
		if err != nil {
			return w, err
		}
		if rel, err := st.Rel(path); err == nil {
			path = rel
		}
		w.Events = append(w.Events, path)
	}
	for _, r := range p.Removes {
		if err := os.Remove(filepath.Join(p.root, filepath.FromSlash(r))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return w, err
		}
		w.Removed = append(w.Removed, r)
	}
	return w, nil
}

// writeNewFile creates a file that must not exist yet, or that already
// holds exactly this text.
func writeNewFile(path string, text []byte) error {
	if have, err := os.ReadFile(path); err == nil {
		if bytes.Equal(have, text) {
			return nil
		}
		return errorf("%s exists with other content", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), files.DirMode); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, files.FileMode)
	if err != nil {
		return err
	}
	if _, err := f.Write(text); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Report writes a human-readable summary of a plan.
func (p *Plan) Report(sb *strings.Builder) {
	fmt.Fprintf(sb, "records mapped: %d decisions, %d tasks, %d contracts, %d briefs (%d entries)\n",
		p.Mapped.Decisions, p.Mapped.Tasks, p.Mapped.Contracts, p.Mapped.Briefs, p.Mapped.Entries)
	fmt.Fprintf(sb, "events: %d total, %d new, %d already in the log\n", len(p.Events), len(p.New), p.Unchanged)
	types := make([]string, 0, len(p.ByType))
	for t := range p.ByType {
		types = append(types, t)
	}
	slices.Sort(types)
	for _, t := range types {
		fmt.Fprintf(sb, "  %-20s %d\n", t, p.ByType[t])
	}
	fmt.Fprintf(sb, "aliases: %d\n", len(p.Aliases))
	keys := make([]string, 0, len(p.Aliases))
	for k := range p.Aliases {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, entryLess)
	for _, k := range keys {
		fmt.Fprintf(sb, "  %s → %s\n", k, p.Aliases[k])
	}
	section := func(title string, lines []string) {
		fmt.Fprintf(sb, "%s: %d\n", title, len(lines))
		for _, l := range lines {
			fmt.Fprintf(sb, "  %s\n", l)
		}
	}
	section("brief files to write", p.Writes)
	section("old briefs to remove", p.Removes)
	section("drift", p.Drift)
	var ls []string
	for _, m := range p.Mismatches {
		ls = append(ls, m.String())
	}
	section("verification mismatches", ls)
	ls = nil
	for _, c := range p.Conflicts {
		ls = append(ls, fmt.Sprintf("%s %v: %s", c.Rule, c.Events, c.Detail))
	}
	section("conflicts", ls)
	ls = nil
	for _, c := range p.CoreLagConflicts {
		ls = append(ls, fmt.Sprintf("%s %v: %s", c.Rule, c.Events, c.Detail))
	}
	section("lane conflicts allowed by C-event-log v3 but not yet by internal/core", ls)
	ls = nil
	for _, r := range p.Problems {
		ls = append(ls, fmt.Sprintf("%s: %s (rule %d): %s", r.Record, r.Field, r.Rule, r.Text))
	}
	section("hand-written records breaking C-decision-record §2 (reported, not fixed)", ls)
	ls = nil
	for _, r := range p.CoreLagProblems {
		ls = append(ls, fmt.Sprintf("%s: %s: %s", r.Record, r.Field, r.Text))
	}
	section("validation failures caused by internal/core, not by the records", ls)
	ls = nil
	for _, n := range p.Notes {
		ls = append(ls, fmt.Sprintf("[%s] %s: %s", n.Kind, n.Record, n.Text))
	}
	section("notes", ls)
}
