// Package files stores Ekumen's event log and brief entries as plain files
// under a project's .ekumen/ folder, laid out exactly as C-event-log §8:
//
//	events/<subject.kind>/<subject.id>/<event id>.<type>.json
//	briefs/<task id>/brief.md
//	briefs/<task id>/amendments/<timestamp>-<slug>.md
//	briefs/<task id>/handoffs/<timestamp>-<session>.md
//
// The store is append-only: every write creates a new file with a unique
// name, atomically (temp file in the same directory, then a no-replace
// commit), and an existing file is never modified or deleted. Because every
// change is a new file, several writers on several git branches can append
// independently and a merge is the union of their files.
//
// Event payloads are opaque here. The store reads only the envelope fields
// it needs to place a file (id, type, subject.kind, subject.id); typed
// events live elsewhere.
package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Directory and file modes for everything the store creates.
const (
	DirMode  os.FileMode = 0o755
	FileMode os.FileMode = 0o644
)

// tempPrefix starts the name of every temp file the store creates. Temp
// files never match the layout, so readers report leftovers as warnings.
const tempPrefix = ".tmp-"

// ErrInvalid is wrapped by every error caused by an id, kind, type or name
// that fails validation. Use errors.Is(err, ErrInvalid).
var ErrInvalid = errors.New("invalid")

// Store reads and writes the files under one project's .ekumen/ folder.
// A Store is safe for concurrent use by multiple goroutines, and several
// processes may write to the same folder at once.
type Store struct {
	root string

	// Test hooks. Nil means the default.
	now          func() time.Time            // clock for entry timestamps
	link         func(old, new string) error // no-replace commit
	beforeCommit func(tmp string) error      // runs between temp write and commit

	mu sync.Mutex // serializes the fallback commit path within this process
}

// New returns a Store rooted at root, the path of a project's .ekumen/
// folder. Directories are created on first write; root need not exist yet.
func New(root string) *Store {
	return &Store{root: filepath.Clean(root)}
}

// Root returns the folder the store was opened on.
func (s *Store) Root() string { return s.root }

// Rel returns path relative to the store root, slash-separated, as recorded
// in events (for example "briefs/T-core/handoffs/20261120T091403Z-t-core.md").
// It fails if path is outside the root.
func (s *Store) Rel(path string) (string, error) {
	rel, err := filepath.Rel(s.root, filepath.Clean(path))
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("files: %q is outside store root %q: %w", path, s.root, ErrInvalid)
	}
	return filepath.ToSlash(rel), nil
}

// Warning reports a file or directory a reader skipped because it does not
// match the layout of C-event-log §8, or whose contents disagree with its
// path. Skipped files are never modified or deleted.
type Warning struct {
	Path   string // full path of the skipped file or directory
	Reason string
}

func (w Warning) String() string { return w.Path + ": " + w.Reason }

func (s *Store) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// join builds a path under the root from already-validated components and
// re-checks that the result stays inside the root (defense in depth).
func (s *Store) join(parts ...string) (string, error) {
	p := filepath.Join(append([]string{s.root}, parts...)...)
	if _, err := s.Rel(p); err != nil {
		return "", err
	}
	return p, nil
}
