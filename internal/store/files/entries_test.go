package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestWriteEntryPathRules(t *testing.T) {
	// C-event-log §8: briefs/<task>/amendments/<timestamp>-<slug>.md and
	// briefs/<task>/handoffs/<timestamp>-<session>.md, timestamp in UTC.
	ist := time.FixedZone("IST", 5*3600+1800)
	at := time.Date(2026, 11, 20, 14, 44, 3, 999, ist) // 09:14:03Z
	cases := []struct {
		kind   EntryKind
		author string
		want   string
	}{
		{Amendments, "orchestrator", "briefs/T-file-store/amendments/20261120T091403Z-orchestrator.md"},
		{Handoffs, "t-file-store", "briefs/T-file-store/handoffs/20261120T091403Z-t-file-store.md"},
	}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			root := t.TempDir()
			s := New(root)
			s.now = fixedClock(at)
			body := []byte("STATUS: done\n")
			p, h, err := s.WriteEntry("T-file-store", c.kind, c.author, body)
			if err != nil {
				t.Fatal(err)
			}
			if p != filepath.Join(root, filepath.FromSlash(c.want)) {
				t.Fatalf("path = %s, want %s", p, c.want)
			}
			if h != sum(body) {
				t.Fatalf("sha = %s", h)
			}
			got, _ := os.ReadFile(p)
			if !bytes.Equal(got, body) {
				t.Fatal("body mismatch")
			}
			fi, _ := os.Stat(p)
			if fi.Mode().Perm() != FileMode {
				t.Fatalf("mode %v", fi.Mode().Perm())
			}
		})
	}
}

func TestWriteEntryClashSuffix(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	s.now = fixedClock(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	var names []string
	for i := 0; i < 12; i++ {
		p, _, err := s.WriteEntry("T-a", Handoffs, "t-a", []byte{byte('a' + i)})
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, filepath.Base(p))
	}
	if names[0] != "20261120T090000Z-t-a.md" || names[1] != "20261120T090000Z-t-a-2.md" || names[11] != "20261120T090000Z-t-a-12.md" {
		t.Fatalf("names = %v", names)
	}
	// Never overwritten: each file keeps its own body.
	for i, n := range names {
		got, _ := os.ReadFile(filepath.Join(root, "briefs", "T-a", "handoffs", n))
		if !bytes.Equal(got, []byte{byte('a' + i)}) {
			t.Fatalf("%s overwritten", n)
		}
	}
	// ReadBrief returns them in write order (numeric suffix order).
	if err := os.WriteFile(filepath.Join(root, "briefs", "T-a", "brief.md"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, warns, err := s.ReadBrief("T-a")
	if err != nil || len(warns) != 0 {
		t.Fatal(err, warns)
	}
	for i, e := range b.Handoffs {
		if e.Name != names[i] {
			t.Fatalf("handoff %d = %s, want %s", i, e.Name, names[i])
		}
	}
}

func TestWriteEntryConcurrentSameSecond(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	s.now = fixedClock(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	const n = 40
	var wg sync.WaitGroup
	paths := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, _, err := s.WriteEntry("T-a", Amendments, "orchestrator", []byte(strings.Repeat("x", i+1)))
			if err != nil {
				t.Error(err)
			}
			paths[i] = p
		}(i)
	}
	wg.Wait()
	seen := map[string]bool{}
	for i, p := range paths {
		if seen[p] {
			t.Fatalf("duplicate path %s", p)
		}
		seen[p] = true
		got, _ := os.ReadFile(p)
		if len(got) != i+1 {
			t.Fatalf("%s corrupted", p)
		}
	}
	assertNoTemps(t, filepath.Join(root, "briefs", "T-a", "amendments"))
}

func TestWriteEntryValidation(t *testing.T) {
	cases := []struct {
		task   string
		kind   EntryKind
		author string
	}{
		{"../T-a", Handoffs, "a"},
		{"T-a/../../x", Handoffs, "a"},
		{"D-a", Handoffs, "a"},
		{"T0", Handoffs, "a"},
		{"T-a", "notes", "a"},
		{"T-a", "../handoffs", "a"},
		{"T-a", "", "a"},
		{"T-a", Handoffs, ""},
		{"T-a", Handoffs, "../../x"},
		{"T-a", Handoffs, "T-Core"},
		{"T-a", Handoffs, "a b"},
		{"T-a", Handoffs, "a--b"},
		{"T-a", Handoffs, strings.Repeat("a", 65)},
	}
	for _, c := range cases {
		root := t.TempDir()
		s := New(root)
		if _, _, err := s.WriteEntry(c.task, c.kind, c.author, []byte("x")); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: err = %v, want ErrInvalid", c, err)
		}
		if ents, _ := os.ReadDir(root); len(ents) != 0 {
			t.Errorf("%+v: created %v", c, ents)
		}
	}
}

func TestReadBrief(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	dir := filepath.Join(root, "briefs", "T-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sectionA := []byte("---\nid: T-a\n---\n# T-a\n")
	if err := os.WriteFile(filepath.Join(dir, "brief.md"), sectionA, 0o644); err != nil {
		t.Fatal(err)
	}
	times := []time.Time{
		time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 19, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
	}
	var wantA, wantH []string
	for i, at := range times {
		s.now = fixedClock(at)
		pa, _, err := s.WriteEntry("T-a", Amendments, "orchestrator", []byte("amend "+string(rune('0'+i))))
		if err != nil {
			t.Fatal(err)
		}
		ph, _, err := s.WriteEntry("T-a", Handoffs, "t-a", []byte("handoff "+string(rune('0'+i))))
		if err != nil {
			t.Fatal(err)
		}
		wantA = append(wantA, filepath.Base(pa))
		wantH = append(wantH, filepath.Base(ph))
	}
	sort.Strings(wantA)
	sort.Strings(wantH)
	// Placeholder and strays.
	for _, f := range []string{"amendments/.gitkeep", "handoffs/.gitkeep", "handoffs/notes.md", "handoffs/20261320T000000Z-t-a.md", "handoffs/.tmp-1"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(f)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	b, warns, err := s.ReadBrief("T-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 3 {
		t.Fatalf("warnings = %v", warns)
	}
	if !bytes.Equal(b.Text, sectionA) || b.SHA256 != sum(sectionA) || b.Rel != "briefs/T-a/brief.md" {
		t.Fatalf("brief = %+v", b)
	}
	check := func(got []Entry, want []string, kind EntryKind) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: got %d, want %d", kind, len(got), len(want))
		}
		for i, e := range got {
			if e.Name != want[i] || e.Kind != kind || e.SHA256 != sum(e.Body) || e.Rel != "briefs/T-a/"+string(kind)+"/"+e.Name {
				t.Fatalf("%s[%d] = %+v, want %s", kind, i, e, want[i])
			}
			h, err := HashFile(e.Path)
			if err != nil || h != e.SHA256 {
				t.Fatalf("HashFile = %s, %v", h, err)
			}
		}
	}
	check(b.Amendments, wantA, Amendments)
	check(b.Handoffs, wantH, Handoffs)

	if _, _, err := s.ReadBrief("T-missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing brief: err = %v", err)
	}
	if _, _, err := s.ReadBrief("../T-a"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad id: err = %v", err)
	}
}

func TestHashFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	data := []byte("hello\n")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := HashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if h != "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03" {
		t.Fatalf("h = %s", h)
	}
	if _, err := HashFile(p + "x"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestLessName(t *testing.T) {
	names := []string{
		"20261120T090000Z-t-a-10.md",
		"20261120T090000Z-t-a-2.md",
		"20261120T090001Z-a.md",
		"20261120T090000Z-t-a.md",
		"20261120T090000Z-orchestrator.md",
	}
	sort.Slice(names, func(i, j int) bool { return lessName(names[i], names[j]) })
	want := []string{
		"20261120T090000Z-orchestrator.md",
		"20261120T090000Z-t-a.md",
		"20261120T090000Z-t-a-2.md",
		"20261120T090000Z-t-a-10.md",
		"20261120T090001Z-a.md",
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("order = %v", names)
		}
	}
}

func TestRel(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	if _, err := s.Rel(filepath.Join(root, "..", "x")); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if r, err := s.Rel(filepath.Join(root, "a", "b")); err != nil || r != "a/b" {
		t.Fatal(r, err)
	}
}

// TestNeverModifies checks that no store operation changes or removes an
// existing file: snapshot, run every operation, compare.
func TestNeverModifies(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	s.now = fixedClock(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	raw := event(t, newULID(t), "TaskDrafted", "task", "T-a")
	if _, err := s.Append(raw); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.WriteEntry("T-a", Handoffs, "t-a", []byte("one")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "briefs", "T-a", "brief.md"), []byte("A"), 0o644)
	os.WriteFile(filepath.Join(root, "events", "task", "T-a", ".tmp-stray"), []byte("x"), 0o644)
	snap := snapshot(t, root)

	s.Append(raw)
	s.WriteEntry("T-a", Handoffs, "t-a", []byte("two"))
	s.ReadAll()
	s.ReadSubject("task", "T-a")
	s.ReadBrief("T-a")

	after := snapshot(t, root)
	for p, data := range snap {
		if got, ok := after[p]; !ok || !bytes.Equal(got, data) {
			t.Fatalf("%s modified or removed", p)
		}
	}
	if len(after) != len(snap)+1 {
		t.Fatalf("expected exactly one new file, got %d", len(after)-len(snap))
	}
}

func snapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	m := map[string][]byte{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			m[p] = data
		}
		return nil
	})
	return m
}
