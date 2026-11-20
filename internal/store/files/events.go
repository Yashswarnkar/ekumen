package files

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// eventsDir is the top-level folder for events (C-event-log §8).
const eventsDir = "events"

// eventNameRe matches "<event id>.<type>.json".
var eventNameRe = regexp.MustCompile(`^([0-9A-Z]+)\.([A-Za-z0-9]+)\.json$`)

// envelope holds the only fields the store reads from an event.
type envelope struct {
	ID          string
	Type        string
	SubjectKind string
	SubjectID   string
}

// parseEnvelope reads id, type, subject.kind and subject.id from one JSON
// object. Keys are matched exactly (encoding/json's struct decoding would
// match them case-insensitively); everything else is left alone.
func parseEnvelope(raw []byte) (envelope, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return envelope{}, invalid("event is not a JSON object (%v)", err)
	}
	var env envelope
	var subj map[string]json.RawMessage
	if err := field(top, "id", &env.ID); err != nil {
		return envelope{}, err
	}
	if err := field(top, "type", &env.Type); err != nil {
		return envelope{}, err
	}
	if err := field(top, "subject", &subj); err != nil {
		return envelope{}, err
	}
	if err := field(subj, "kind", &env.SubjectKind); err != nil {
		return envelope{}, fmt.Errorf("subject: %w", err)
	}
	if err := field(subj, "id", &env.SubjectID); err != nil {
		return envelope{}, fmt.Errorf("subject: %w", err)
	}
	return env, nil
}

func field(m map[string]json.RawMessage, key string, dst any) error {
	v, ok := m[key]
	if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
		return invalid("event has no %q field", key)
	}
	if err := json.Unmarshal(v, dst); err != nil {
		return invalid("event field %q has the wrong type (%v)", key, err)
	}
	return nil
}

func (e envelope) validate() error {
	if err := validULID(e.ID); err != nil {
		return err
	}
	if err := validType(e.Type); err != nil {
		return err
	}
	return validSubject(e.SubjectKind, e.SubjectID)
}

func (e envelope) fileName() string { return e.ID + "." + e.Type + ".json" }

// Append validates the envelope of one raw event and writes it, byte for
// byte, to events/<subject.kind>/<subject.id>/<event id>.<type>.json. It
// returns the full path written.
//
// The write is atomic and never overwrites: if the path already exists the
// error wraps fs.ErrExist. Validation failures wrap ErrInvalid. Only the
// envelope fields id, type, subject.kind and subject.id are read; the rest
// of the event is opaque to the store.
func (s *Store) Append(raw []byte) (string, error) {
	env, err := parseEnvelope(raw)
	if err != nil {
		return "", err
	}
	if err := env.validate(); err != nil {
		return "", err
	}
	dir, err := s.join(eventsDir, env.SubjectKind, env.SubjectID)
	if err != nil {
		return "", err
	}
	return s.writeNew(dir, env.fileName(), raw)
}

type rawEvent struct {
	id   string
	path string
	data []byte
}

// ReadAll returns every event in the log as raw JSON, sorted by event id
// (ties, which only a hand-made copy can cause, are broken by path).
//
// Files and directories that don't match the layout of C-event-log §8, or
// whose envelope disagrees with their path, are skipped and reported as
// warnings. Temp files of writes in flight, and files removed between
// listing and reading, are skipped silently. A missing events folder is an
// empty log.
func (s *Store) ReadAll() ([][]byte, []Warning, error) {
	root := filepath.Join(s.root, eventsDir)
	kinds, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var evs []rawEvent
	var warns []Warning
	for _, k := range kinds {
		kpath := filepath.Join(root, k.Name())
		if !k.IsDir() {
			warns = append(warns, Warning{kpath, "not a subject-kind directory"})
			continue
		}
		if k.Name() != KindProject {
			if _, ok := kindLetter[k.Name()]; !ok {
				warns = append(warns, Warning{kpath, "unknown subject kind"})
				continue
			}
		}
		subjects, err := os.ReadDir(kpath)
		if err != nil {
			return nil, nil, err
		}
		for _, sub := range subjects {
			spath := filepath.Join(kpath, sub.Name())
			if !sub.IsDir() {
				warns = append(warns, Warning{spath, "not a subject directory"})
				continue
			}
			if err := validSubject(k.Name(), sub.Name()); err != nil {
				warns = append(warns, Warning{spath, "subject id does not match C-event-log §1 for kind " + k.Name()})
				continue
			}
			e, w, err := readSubjectDir(spath, k.Name(), sub.Name())
			if err != nil {
				return nil, nil, err
			}
			evs = append(evs, e...)
			warns = append(warns, w...)
		}
	}
	return sortEvents(evs), warns, nil
}

// ReadSubject returns the events of one subject as raw JSON, sorted by
// event id, with warnings as for ReadAll. An unknown kind or malformed id
// is an error wrapping ErrInvalid; a subject with no events returns none.
func (s *Store) ReadSubject(kind, id string) ([][]byte, []Warning, error) {
	if err := validSubject(kind, id); err != nil {
		return nil, nil, err
	}
	dir, err := s.join(eventsDir, kind, id)
	if err != nil {
		return nil, nil, err
	}
	evs, warns, err := readSubjectDir(dir, kind, id)
	if err != nil {
		return nil, nil, err
	}
	return sortEvents(evs), warns, nil
}

var readFile = os.ReadFile

func readSubjectDir(dir, kind, id string) ([]rawEvent, []Warning, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var evs []rawEvent
	var warns []Warning
	for _, de := range ents {
		p := filepath.Join(dir, de.Name())
		if strings.HasPrefix(de.Name(), tempPrefix) {
			continue
		}
		if !de.Type().IsRegular() {
			warns = append(warns, Warning{p, "not a regular file"})
			continue
		}
		m := eventNameRe.FindStringSubmatch(de.Name())
		if m == nil || validULID(m[1]) != nil || validType(m[2]) != nil {
			warns = append(warns, Warning{p, "name is not <event id>.<type>.json"})
			continue
		}
		data, err := readFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		env, err := parseEnvelope(data)
		if err != nil {
			warns = append(warns, Warning{p, "unreadable envelope: " + err.Error()})
			continue
		}
		if env.ID != m[1] || env.Type != m[2] || env.SubjectKind != kind || env.SubjectID != id {
			warns = append(warns, Warning{p, "envelope does not match path"})
			continue
		}
		evs = append(evs, rawEvent{id: env.ID, path: p, data: data})
	}
	return evs, warns, nil
}

func sortEvents(evs []rawEvent) [][]byte {
	sort.Slice(evs, func(i, j int) bool {
		if evs[i].id != evs[j].id {
			return evs[i].id < evs[j].id
		}
		return evs[i].path < evs[j].path
	})
	if len(evs) == 0 {
		return nil
	}
	out := make([][]byte, len(evs))
	for i, e := range evs {
		out[i] = e.data
	}
	return out
}
