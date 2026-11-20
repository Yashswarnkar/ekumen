package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadersSkipFilesThatVanishOrAreInFlight(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	keep := event(t, newULID(t), "TaskDrafted", "task", "T-a")
	gone := event(t, newULID(t), "TaskDeferred", "task", "T-a")
	var gonePath string
	for _, raw := range [][]byte{keep, gone} {
		p, err := s.Append(raw)
		if err != nil {
			t.Fatal(err)
		}
		gonePath = p
	}
	if _, _, err := s.WriteBrief("T-a", []byte("brief\n")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.WriteEntry("T-a", Handoffs, "t-a", []byte("one\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "events", "task", "T-a", tempPrefix+"1"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "briefs", "T-a", "handoffs", tempPrefix+"2"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entryDir := filepath.Join(root, "briefs", "T-a", "handoffs")
	ents, err := os.ReadDir(entryDir)
	if err != nil {
		t.Fatal(err)
	}
	var goneEntry string
	for _, e := range ents {
		if e.Name()[0] != '.' {
			goneEntry = filepath.Join(entryDir, e.Name())
		}
	}

	readFile = func(p string) ([]byte, error) {
		if p == gonePath || p == goneEntry {
			os.Remove(p)
		}
		return os.ReadFile(p)
	}
	t.Cleanup(func() { readFile = os.ReadFile })

	all, warns, err := s.ReadAll()
	if err != nil || len(all) != 1 || len(warns) != 0 {
		t.Errorf("ReadAll: %d events, warnings %v, err %v", len(all), warns, err)
	}
	b, warns, err := s.ReadBrief("T-a")
	if err != nil || len(warns) != 0 || len(b.Handoffs) != 0 {
		t.Errorf("ReadBrief: handoffs %d, warnings %v, err %v", len(b.Handoffs), warns, err)
	}
}
