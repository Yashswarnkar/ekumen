// Package project loads one Ekumen project (its .ekumen/ folder plus fresh
// observations), builds the read views of C-cli-json §4 from it, and writes
// the projections of C-event-log §8: decisions/<id>.yaml, registry.yaml and
// state.json. All domain logic is in internal/core; this package adapts it
// to files and output shapes.
package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/observe"
	"github.com/Yashswarnkar/ekumen/internal/store/files"
)

// Dir is the name of a project's Ekumen folder.
const Dir = ".ekumen"

// Invalid is a file that failed validation (C-cli-json §2, `check`).
type Invalid struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// Snapshot is one consistent read of a project: events, the state reduced
// from them, and observations made at load time.
type Snapshot struct {
	// Root is the project root (the folder containing .ekumen/).
	Root  string
	Store *files.Store
	// Events are the events that decoded and validated, in log order.
	Events []event.Event
	// Invalid lists event files the store skipped or that failed to decode
	// or validate.
	Invalid []Invalid
	State   core.State
	Now     time.Time
	// PRs maps a branch to its observed PR. Nil means observation was
	// unavailable, so observed state is unknown.
	PRs      map[string]observe.PR
	Observed core.Observed
	// PRsObserved is ObserveOK, ObserveUnavailable or ObserveTruncated.
	PRsObserved string

	byID map[string]event.Event
	name string
}

// How pull requests were observed at load time.
const (
	ObserveOK          = "ok"
	ObserveUnavailable = "unavailable"
	// ObserveTruncated: the observer hit its limit, so older PRs may be
	// missing (issue #15).
	ObserveTruncated = "truncated"
)

// Options configure Load.
type Options struct {
	Root     string
	Now      time.Time
	Observer observe.Observer
	// Warn receives warnings (for stderr). Nil drops them.
	Warn func(string)
	// Name is the project's name when the caller knows it (e.g. from the
	// git remote); see Snapshot.ProjectName.
	Name string
}

// FindRoot returns the nearest directory at or above dir that contains
// .ekumen/ (C-cli-json §1).
func FindRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for d := abs; ; d = filepath.Dir(d) {
		if fi, err := os.Stat(filepath.Join(d, Dir)); err == nil && fi.IsDir() {
			return d, nil
		}
		if filepath.Dir(d) == d {
			return "", fmt.Errorf("no %s/ folder in %s or any parent", Dir, abs)
		}
	}
}

// Load reads the project at opt.Root.
func Load(ctx context.Context, opt Options) (*Snapshot, error) {
	warn := opt.Warn
	if warn == nil {
		warn = func(string) {}
	}
	ek := filepath.Join(opt.Root, Dir)
	if fi, err := os.Stat(ek); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", ek)
	}
	s := &Snapshot{Root: opt.Root, Store: files.New(ek), Now: opt.Now.UTC(), byID: map[string]event.Event{}, name: opt.Name}
	if err := s.readEvents(); err != nil {
		return nil, err
	}

	obs := opt.Observer
	if obs == nil {
		obs = observe.None{}
	}
	prs, err := obs.PullRequests(ctx)
	switch {
	case errors.Is(err, observe.ErrTruncated):
		warn("observed state may be incomplete: " + err.Error())
		s.PRs, s.PRsObserved = observe.ByBranch(prs), ObserveTruncated
	case err != nil:
		warn("observed state unknown: " + err.Error())
		s.PRsObserved = ObserveUnavailable
	default:
		s.PRs, s.PRsObserved = observe.ByBranch(prs), ObserveOK
	}
	s.Observed = s.observed()
	return s, nil
}

// readEvents reads, decodes and validates the event log and reduces it.
func (s *Snapshot) readEvents() error {
	raws, warns, err := s.Store.ReadAll()
	if err != nil {
		return fmt.Errorf("read events: %w", err)
	}
	s.Events, s.Invalid, s.byID = nil, nil, map[string]event.Event{}
	for _, w := range warns {
		s.Invalid = append(s.Invalid, Invalid{Path: s.rel(w.Path), Error: w.Reason})
	}
	for _, raw := range raws {
		e, err := event.Decode(raw)
		if err != nil {
			s.Invalid = append(s.Invalid, Invalid{Path: "events", Error: err.Error()})
			continue
		}
		if err := e.Validate(); err != nil {
			s.Invalid = append(s.Invalid, Invalid{Path: s.eventPath(e), Error: err.Error()})
			continue
		}
		s.Events = append(s.Events, e)
		s.byID[e.ID] = e
	}
	s.State = core.Reduce(s.Events)
	return nil
}

// Refresh reads the event log again (events another writer may have
// added) and derives state again, keeping the observations made at load
// time. It never observes.
func (s *Snapshot) Refresh() error {
	if err := s.readEvents(); err != nil {
		return err
	}
	s.Observed = s.observed()
	return nil
}

// Absorb adds events already written to the log to the snapshot and
// derives state again, keeping the observations made at load time.
func (s *Snapshot) Absorb(evs ...event.Event) {
	for _, e := range evs {
		if _, ok := s.byID[e.ID]; ok {
			continue
		}
		s.Events = append(s.Events, e)
		s.byID[e.ID] = e
	}
	s.State = core.Reduce(s.Events)
	s.Observed = s.observed()
}

func (s *Snapshot) observed() core.Observed {
	o := core.Observed{Tasks: map[string]core.TaskObservation{}}
	if s.PRs == nil {
		return o
	}
	for id, t := range s.State.Tasks {
		if pr, ok := s.PRs[t.Branch]; ok && t.Branch != "" {
			o.Tasks[id] = core.TaskObservation{PR: core.PRState(pr.State)}
		}
	}
	return o
}

// Event returns the event with this id.
func (s *Snapshot) Event(id string) (event.Event, bool) {
	e, ok := s.byID[id]
	return e, ok
}

// EkumenDir is the project's .ekumen/ folder.
func (s *Snapshot) EkumenDir() string { return s.Store.Root() }

func (s *Snapshot) rel(path string) string {
	if r, err := filepath.Rel(s.Root, path); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return path
}

// Rel returns path relative to the project root, slash-separated.
func (s *Snapshot) Rel(path string) string { return s.rel(path) }

func (s *Snapshot) eventPath(e event.Event) string {
	return filepath.ToSlash(filepath.Join(Dir, "events", string(e.Subject.Kind), e.Subject.ID, e.ID+"."+string(e.Type)+".json"))
}

// ResolvePath finds a file named in an event (a brief, contract or entry
// path). Paths may be relative to .ekumen/ ("contracts/C-x.md") or to the
// project root (".ekumen/contracts/C-x.md"). Absolute paths and paths that
// lead outside .ekumen/ are refused.
func (s *Snapshot) ResolvePath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || filepath.VolumeName(p) != "" {
		return "", fmt.Errorf("path %q is absolute; event paths are relative to %s/", p, Dir)
	}
	clean := filepath.Clean(filepath.FromSlash(p))
	c := filepath.Join(s.EkumenDir(), clean)
	if strings.HasPrefix(filepath.ToSlash(clean), Dir+"/") {
		c = filepath.Join(s.Root, clean)
	}
	rel, err := filepath.Rel(s.EkumenDir(), c)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q leads outside %s/", p, Dir)
	}
	return c, nil
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// TaskStatus is the derived status of a task.
func (s *Snapshot) TaskStatus(id string) core.TaskStatus {
	return core.DeriveTaskStatus(&s.State, s.Observed, id)
}

// FrozenHashes hashes the current text of every approved brief and frozen
// contract, keyed by the path recorded in events, for the
// frozen-text-changed rule, and the spine's under core.SpineFile for the
// spine-unapproved rule.
func (s *Snapshot) FrozenHashes() map[string]string {
	out := map[string]string{}
	add := func(p string) {
		if p == "" {
			return
		}
		if rp, err := s.ResolvePath(p); err == nil {
			if h, err := files.HashFile(rp); err == nil {
				out[p] = h
			}
		}
	}
	for _, t := range s.State.Tasks {
		if t.Approved {
			add(t.Brief)
		}
	}
	for _, c := range s.State.Contracts {
		if c.Status == core.ContractFrozen {
			add(c.Path)
		}
	}
	if h, err := files.HashFile(filepath.Join(s.EkumenDir(), core.SpineFile)); err == nil {
		out[core.SpineFile] = h
	}
	return out
}

// Conflicts runs the C-event-log §5 rules.
func (s *Snapshot) Conflicts() []core.Conflict {
	return core.Conflicts(&s.State, s.Events, core.ConflictInputs{TextSHA256: s.FrozenHashes(), Observed: s.Observed})
}

// ErrNotFound is wrapped when an id names nothing.
var ErrNotFound = errors.New("not found")

// ResolveID maps an id or alias to an existing item's id.
func (s *Snapshot) ResolveID(id string) (string, error) {
	r := s.State.Resolve(id)
	if !s.State.Has(r) {
		return "", fmt.Errorf("%q: %w", id, ErrNotFound)
	}
	return r, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
