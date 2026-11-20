package project

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/store/files"
)

func writeEvent(t *testing.T, root string, e event.Event) {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, Dir, "events", string(e.Subject.Kind), e.Subject.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, e.ID+"."+string(e.Type)+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDoesNotReduceInvalidEvents(t *testing.T) {
	root := t.TempDir()
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	id, err := event.ULIDFromParts(uint64(at.UnixMilli()), [10]byte{9: 1})
	if err != nil {
		t.Fatal(err)
	}
	e, err := event.New(id, event.TaskDraftedType, at, event.Actor{Role: event.RoleOrchestrator, ID: "orch"},
		event.Subject{Kind: event.KindTask, ID: "T-future"}, event.TaskDrafted{
			Title: "future", Kind: event.TaskKindBuild, Brief: "briefs/T-future/brief.md", Repo: "r",
			BaseBranch: "main", BlockedBy: []string{}, TierGrants: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	e.Schema = 2
	writeEvent(t, root, e)

	s, err := Load(context.Background(), Options{Root: root, Now: at})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 0 || s.State.Has("T-future") {
		t.Errorf("a schema 2 event was reduced: events=%d", len(s.Events))
	}
	if len(s.Invalid) != 1 {
		t.Errorf("invalid = %v, want the schema 2 event", s.Invalid)
	}
}

func TestResolvePathStaysInsideEkumen(t *testing.T) {
	root := t.TempDir()
	s := &Snapshot{Root: root, Store: files.New(filepath.Join(root, Dir))}
	ek := filepath.Join(root, Dir)
	for p, want := range map[string]string{
		"contracts/C-x.md":           filepath.Join(ek, "contracts", "C-x.md"),
		".ekumen/contracts/C-x.md":   filepath.Join(ek, "contracts", "C-x.md"),
		"briefs/T-x/../T-y/brief.md": filepath.Join(ek, "briefs", "T-y", "brief.md"),
	} {
		if got, err := s.ResolvePath(p); err != nil || got != want {
			t.Errorf("ResolvePath(%q) = %q, %v; want %q", p, got, err, want)
		}
	}
	for _, p := range []string{"/etc/passwd", "../../etc/passwd", ".ekumen/../../go.mod", "contracts/../../go.mod", "..", "README.md/../../x"} {
		if got, err := s.ResolvePath(p); err == nil {
			t.Errorf("ResolvePath(%q) = %q, want an error", p, got)
		}
	}
}
