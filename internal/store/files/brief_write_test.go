package files

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// C-event-log §8: an entry written at a given time is named from that time
// (UTC, to the second), with the same "-N" clash rule as WriteEntry, and
// does not read the store's clock.
func TestWriteEntryAt_EventLog8(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	s.now = func() time.Time { t.Fatal("WriteEntryAt read the clock"); return time.Time{} }
	ist := time.FixedZone("IST", 5*3600+1800)
	at := time.Date(2026, 11, 20, 17, 30, 0, 5, ist) // 12:00:00Z
	var got []string
	for range 3 {
		p, h, err := s.WriteEntryAt("T-acp-research", Amendments, "orchestrator", at, []byte("x\n"))
		if err != nil {
			t.Fatal(err)
		}
		if h != sum([]byte("x\n")) {
			t.Errorf("sha = %s", h)
		}
		rel, _ := s.Rel(p)
		got = append(got, rel)
	}
	want := []string{
		"briefs/T-acp-research/amendments/20261120T120000Z-orchestrator.md",
		"briefs/T-acp-research/amendments/20261120T120000Z-orchestrator-2.md",
		"briefs/T-acp-research/amendments/20261120T120000Z-orchestrator-3.md",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %s, want %s", i, got[i], want[i])
		}
	}
	if _, _, err := s.WriteEntryAt("T-acp-research", Handoffs, "s", time.Time{}, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("zero time: %v", err)
	}
	if _, _, err := s.WriteEntryAt("T-acp-research", Handoffs, "Bad Author", at, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad author: %v", err)
	}
}

// C-event-log §8: brief.md (§A) is created once and never overwritten.
func TestWriteBrief_EventLog8(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	text := []byte("# T-x\n")
	p, h, err := s.WriteBrief("T-x", text)
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(root, "briefs", "T-x", "brief.md") || h != sum(text) {
		t.Fatalf("path %s sha %s", p, h)
	}
	if _, _, err := s.WriteBrief("T-x", []byte("other\n")); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second write: %v, want ErrExist", err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, text) {
		t.Fatalf("brief overwritten: %q", got)
	}
	b, _, err := s.ReadBrief("T-x")
	if err != nil || !bytes.Equal(b.Text, text) {
		t.Fatalf("ReadBrief: %v", err)
	}
	if _, _, err := s.WriteBrief("../x", text); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad task: %v", err)
	}
}
