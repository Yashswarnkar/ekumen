package files

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func TestAppendPathRule(t *testing.T) {
	// C-event-log §8: events/<subject.kind>/<subject.id>/<event id>.<type>.json
	cases := []struct{ kind, sid, typ string }{
		{"task", "T-core", "TaskDrafted"},
		{"decision", "D-event-ids", "DecisionRaised"},
		{"contract", "C-event-log", "ContractFrozen"},
		{"external", "X-sandbox-creds", "ExternalAdded"},
		{"project", "project", "RoleClaimed"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			root := t.TempDir()
			s := New(root)
			id := newULID(t)
			p, err := s.Append(event(t, id, c.typ, c.kind, c.sid))
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(root, "events", c.kind, c.sid, id+"."+c.typ+".json")
			if p != want {
				t.Fatalf("path = %s, want %s", p, want)
			}
			rel, err := s.Rel(p)
			if err != nil {
				t.Fatal(err)
			}
			if rel != "events/"+c.kind+"/"+c.sid+"/"+id+"."+c.typ+".json" {
				t.Fatalf("rel = %s", rel)
			}
		})
	}
}

func TestAppendRoundTrip(t *testing.T) {
	s := New(t.TempDir())
	raw := event(t, newULID(t), "TaskHandoff", "task", "T-file-store")
	p, err := s.Append(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("file content changed:\n%s\n%s", got, raw)
	}
	all, warns, err := s.ReadAll()
	if err != nil || len(warns) != 0 {
		t.Fatalf("ReadAll: %v %v", err, warns)
	}
	if len(all) != 1 || !bytes.Equal(all[0], raw) {
		t.Fatalf("ReadAll = %q", all)
	}
	sub, warns, err := s.ReadSubject("task", "T-file-store")
	if err != nil || len(warns) != 0 || len(sub) != 1 || !bytes.Equal(sub[0], raw) {
		t.Fatalf("ReadSubject = %q %v %v", sub, warns, err)
	}
}

func TestAppendModes(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	root := filepath.Join(t.TempDir(), ".ekumen")
	s := New(root)
	p, err := s.Append(event(t, newULID(t), "TaskDrafted", "task", "T-core"))
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("file mode = %v, want 0644", fi.Mode().Perm())
	}
	for _, d := range []string{root, filepath.Join(root, "events"), filepath.Join(root, "events", "task"), filepath.Dir(p)} {
		fi, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o755 {
			t.Errorf("%s mode = %v, want 0755", d, fi.Mode().Perm())
		}
	}
}

func TestAppendRefusesOverwrite(t *testing.T) {
	s := New(t.TempDir())
	id := newULID(t)
	first := event(t, id, "DecisionRaised", "decision", "D-x")
	p, err := s.Append(first)
	if err != nil {
		t.Fatal(err)
	}
	second := []byte(strings.Replace(string(first), "hello", "changed", 1))
	if _, err := s.Append(second); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second append: err = %v, want fs.ErrExist", err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, first) {
		t.Fatal("existing event was modified")
	}
	assertNoTemps(t, filepath.Dir(p))
}

func TestAppendFallbackWithoutHardLinks(t *testing.T) {
	s := New(t.TempDir())
	s.link = func(_, _ string) error { return &os.LinkError{Op: "link", Err: syscall.EPERM} }
	raw := event(t, newULID(t), "TaskDrafted", "task", "T-core")
	p, err := s.Append(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, raw) {
		t.Fatal("content mismatch")
	}
	if _, err := s.Append(raw); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("err = %v, want fs.ErrExist", err)
	}
	assertNoTemps(t, filepath.Dir(p))
}

func TestAppendValidation(t *testing.T) {
	good := newULID(t)
	cases := map[string][]byte{
		"not json":             []byte("nope"),
		"array":                []byte("[]"),
		"two objects":          []byte(`{} {}`),
		"missing id":           []byte(`{"type":"TaskDrafted","subject":{"kind":"task","id":"T-a"}}`),
		"null id":              []byte(`{"id":null,"type":"TaskDrafted","subject":{"kind":"task","id":"T-a"}}`),
		"numeric id":           []byte(`{"id":5,"type":"TaskDrafted","subject":{"kind":"task","id":"T-a"}}`),
		"wrong-case key":       []byte(`{"ID":"` + good + `","type":"TaskDrafted","subject":{"kind":"task","id":"T-a"}}`),
		"missing subject":      []byte(`{"id":"` + good + `","type":"TaskDrafted"}`),
		"lowercase ulid":       event(t, strings.ToLower(good), "TaskDrafted", "task", "T-a"),
		"short ulid":           event(t, good[:25], "TaskDrafted", "task", "T-a"),
		"ulid overflow":        event(t, "8"+good[1:], "TaskDrafted", "task", "T-a"),
		"ulid with I":          event(t, good[:25]+"I", "TaskDrafted", "task", "T-a"),
		"ulid traversal":       event(t, "../../../../etc/passwd", "TaskDrafted", "task", "T-a"),
		"type with dot":        event(t, good, "Task.Drafted", "task", "T-a"),
		"type with slash":      event(t, good, "../Task", "task", "T-a"),
		"type lowercase":       event(t, good, "taskDrafted", "task", "T-a"),
		"type empty":           event(t, good, "", "task", "T-a"),
		"unknown kind":         event(t, good, "TaskDrafted", "tasks", "T-a"),
		"kind traversal":       event(t, good, "TaskDrafted", "..", "T-a"),
		"id traversal":         event(t, good, "TaskDrafted", "task", "../../x"),
		"id slash":             event(t, good, "TaskDrafted", "task", "T-a/b"),
		"id uppercase":         event(t, good, "TaskDrafted", "task", "T-Core"),
		"id six words":         event(t, good, "TaskDrafted", "task", "T-a-b-c-d-e-f"),
		"id trailing dash":     event(t, good, "TaskDrafted", "task", "T-a-"),
		"id slug too long":     event(t, good, "TaskDrafted", "task", "T-"+strings.Repeat("a", 41)),
		"id wrong letter":      event(t, good, "TaskDrafted", "task", "D-a"),
		"legacy alias":         event(t, good, "TaskDrafted", "task", "T0"),
		"project with slug id": event(t, good, "RoleClaimed", "project", "T-a"),
		"decision as project":  event(t, good, "DecisionRaised", "decision", "project"),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			s := New(root)
			if _, err := s.Append(raw); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if _, err := os.Stat(filepath.Join(root, "events")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("events folder created for an invalid event")
			}
		})
	}
	// Boundary that must pass: 5 words, 40-character slug.
	s := New(t.TempDir())
	long := "T-" + strings.Repeat("a", 8) + "-" + strings.Repeat("b", 8) + "-" + strings.Repeat("c", 8) + "-" + strings.Repeat("d", 6) + "-" + strings.Repeat("e", 6)
	if len(long)-2 != 40 {
		t.Fatalf("bad fixture length %d", len(long)-2)
	}
	if _, err := s.Append(event(t, newULID(t), "TaskDrafted", "task", long)); err != nil {
		t.Fatalf("40-char 5-word slug: %v", err)
	}
}

func TestReadSortedByID(t *testing.T) {
	s := New(t.TempDir())
	var ids []string
	subjects := []subject{{"task", "T-a"}, {"decision", "D-b"}, {"task", "T-c"}, {"project", "project"}}
	for i := 0; i < 40; i++ {
		ids = append(ids, newULID(t))
	}
	// Append in a scrambled order across subjects.
	order := []int{}
	for i := range ids {
		order = append(order, (i*17)%len(ids))
	}
	for k, i := range order {
		sub := subjects[k%len(subjects)]
		if _, err := s.Append(event(t, ids[i], "SomethingHappened", sub.Kind, sub.ID)); err != nil {
			t.Fatal(err)
		}
	}
	all, warns, err := s.ReadAll()
	if err != nil || len(warns) != 0 {
		t.Fatal(err, warns)
	}
	if len(all) != len(ids) {
		t.Fatalf("got %d events, want %d", len(all), len(ids))
	}
	got := make([]string, len(all))
	for i, raw := range all {
		got[i] = idOf(t, raw)
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("not sorted: %v", got)
	}
	sub, _, err := s.ReadSubject("task", "T-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 10 {
		t.Fatalf("ReadSubject got %d, want 10", len(sub))
	}
	var subIDs []string
	for _, raw := range sub {
		subIDs = append(subIDs, idOf(t, raw))
	}
	if !sort.StringsAreSorted(subIDs) {
		t.Fatalf("subject not sorted: %v", subIDs)
	}
}

func TestReadEmpty(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "missing"))
	all, warns, err := s.ReadAll()
	if err != nil || all != nil || warns != nil {
		t.Fatal(all, warns, err)
	}
	sub, warns, err := s.ReadSubject("task", "T-none")
	if err != nil || sub != nil || warns != nil {
		t.Fatal(sub, warns, err)
	}
	if _, _, err := s.ReadSubject("task", "../x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := s.ReadSubject("nope", "T-x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadWarnsOnStrayFiles(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	good := event(t, newULID(t), "TaskDrafted", "task", "T-a")
	if _, err := s.Append(good); err != nil {
		t.Fatal(err)
	}
	ev := filepath.Join(root, "events")
	otherID := newULID(t)
	write := func(rel string, data []byte) {
		p := filepath.Join(ev, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", []byte("x"))                                                     // file at kind level
	write("tasks/T-a/"+otherID+".TaskDrafted.json", good)                               // unknown kind
	write("task/notes.txt", []byte("x"))                                                // file at subject level
	write("task/t-lower/"+otherID+".TaskDrafted.json", good)                            // bad subject id
	write("task/D-wrong/"+otherID+".TaskDrafted.json", good)                            // id letter mismatch
	write("task/T-a/notes.md", []byte("x"))                                             // bad event name
	write("task/T-a/.tmp-123", []byte("{"))                                             // temp file, skipped silently
	write("task/T-a/"+otherID+".TaskDrafted.json", good)                                // envelope id != name
	write("task/T-a/"+newULID(t)+".TaskDrafted.json", []byte("{broken"))                // bad JSON
	write("task/T-a/"+newULID(t)+".TaskApproved.json", event(t, "X", "Y", "task", "T")) // id mismatch too
	if err := os.MkdirAll(filepath.Join(ev, "task", "T-a", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	all, warns, err := s.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || !bytes.Equal(all[0], good) {
		t.Fatalf("ReadAll returned %d events", len(all))
	}
	if len(warns) != 10 {
		for _, w := range warns {
			t.Log(w)
		}
		t.Fatalf("got %d warnings, want 10", len(warns))
	}
	_, warns, err = s.ReadSubject("task", "T-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 5 {
		t.Fatalf("ReadSubject got %d warnings, want 5: %v", len(warns), warns)
	}
}

func TestConcurrentAppends(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	// A second Store on the same folder stands in for another process.
	s2 := New(root)
	const writers, each = 16, 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	written := map[string][]byte{}
	errs := make(chan error, writers*each)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			st := s
			if w%2 == 1 {
				st = s2
			}
			for i := 0; i < each; i++ {
				// Half the writers share one subject directory.
				sid := "T-shared"
				if w%2 == 0 {
					sid = fmt.Sprintf("T-w%d", w)
				}
				raw := event(t, newULID(t), "TaskHandoff", "task", sid)
				p, err := st.Append(raw)
				if err != nil {
					errs <- err
					return
				}
				mu.Lock()
				written[p] = raw
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	all, warns, err := s.ReadAll()
	if err != nil || len(warns) != 0 {
		t.Fatal(err, warns)
	}
	if len(all) != writers*each || len(written) != writers*each {
		t.Fatalf("read %d, wrote %d, want %d", len(all), len(written), writers*each)
	}
	for p, raw := range written {
		got, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatalf("%s corrupted: %v", p, err)
		}
	}
}

func TestConcurrentSameEvent(t *testing.T) {
	// Many writers racing on the same path: exactly one wins.
	s := New(t.TempDir())
	raw := event(t, newULID(t), "TaskDrafted", "task", "T-race")
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins, exists := 0, 0
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Append(raw)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, fs.ErrExist):
				exists++
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 || exists != 31 {
		t.Fatalf("wins=%d exists=%d", wins, exists)
	}
}

func TestInjectedFailureLeavesNoPartialEvent(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	boom := errors.New("crash before commit")
	var sawTemp string
	s.beforeCommit = func(tmp string) error {
		sawTemp = tmp
		if _, err := os.Stat(tmp); err != nil {
			t.Errorf("temp file missing at commit point: %v", err)
		}
		return boom
	}
	raw := event(t, newULID(t), "TaskDrafted", "task", "T-a")
	if _, err := s.Append(raw); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if sawTemp == "" {
		t.Fatal("hook not called")
	}
	dir := filepath.Join(root, "events", "task", "T-a")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Fatalf("directory not empty after failed append: %v", ents)
	}
	all, warns, err := s.ReadAll()
	if err != nil || len(all) != 0 || len(warns) != 0 {
		t.Fatal(all, warns, err)
	}

	// A hard crash, or a write in flight, leaves a temp file; readers
	// neither return it as an event nor report it.
	if err := os.WriteFile(filepath.Join(dir, ".tmp-999"), raw[:len(raw)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	all, warns, err = s.ReadAll()
	if err != nil || len(all) != 0 || len(warns) != 0 {
		t.Fatal(all, warns, err)
	}

	// The same event can still be appended afterwards.
	s.beforeCommit = nil
	if _, err := s.Append(raw); err != nil {
		t.Fatal(err)
	}
}

func TestBranchesMergeByCopy(t *testing.T) {
	base := t.TempDir()
	a, b, merged := filepath.Join(base, "a"), filepath.Join(base, "b"), filepath.Join(base, "merged")
	sa, sb := New(a), New(b)
	var want []string
	// Common history on both branches.
	common := event(t, newULID(t), "TaskDrafted", "task", "T-x")
	for _, s := range []*Store{sa, sb} {
		if _, err := s.Append(common); err != nil {
			t.Fatal(err)
		}
	}
	want = append(want, idOf(t, common))
	for i := 0; i < 10; i++ {
		ra := event(t, newULID(t), "TaskHandoff", "task", "T-x")
		rb := event(t, newULID(t), "DecisionRaised", "decision", "D-y")
		if _, err := sa.Append(ra); err != nil {
			t.Fatal(err)
		}
		if _, err := sb.Append(rb); err != nil {
			t.Fatal(err)
		}
		want = append(want, idOf(t, ra), idOf(t, rb))
	}
	// Entries on both branches in the same second, by their own authors.
	if _, _, err := sa.WriteEntry("T-x", Handoffs, "t-x", []byte("handoff on a")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := sb.WriteEntry("T-x", Amendments, "orchestrator", []byte("amendment on b")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := sb.WriteEntry("T-x", Handoffs, "t-x-review", []byte("handoff on b")); err != nil {
		t.Fatal(err)
	}

	// "git merge": copy both trees; same-named files must be identical.
	for _, src := range []string{a, b} {
		err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, p)
			dst := filepath.Join(merged, rel)
			if d.IsDir() {
				return os.MkdirAll(dst, 0o755)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if existing, err := os.ReadFile(dst); err == nil && !bytes.Equal(existing, data) {
				return fmt.Errorf("merge conflict on %s", rel)
			}
			return os.WriteFile(dst, data, 0o644)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sm := New(merged)
	all, warns, err := sm.ReadAll()
	if err != nil || len(warns) != 0 {
		t.Fatal(err, warns)
	}
	if err := os.WriteFile(filepath.Join(merged, "briefs", "T-x", "brief.md"), []byte("# T-x"), 0o644); err != nil {
		t.Fatal(err)
	}
	br, warns, err := sm.ReadBrief("T-x")
	if err != nil || len(warns) != 0 {
		t.Fatal(err, warns)
	}
	if len(br.Amendments) != 1 || len(br.Handoffs) != 2 {
		t.Fatalf("merged brief has %d amendments, %d handoffs", len(br.Amendments), len(br.Handoffs))
	}
	sort.Strings(want)
	if len(all) != len(want) {
		t.Fatalf("merged log has %d events, want %d", len(all), len(want))
	}
	for i, raw := range all {
		if idOf(t, raw) != want[i] {
			t.Fatalf("event %d = %s, want %s", i, idOf(t, raw), want[i])
		}
	}
}

func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), tempPrefix) {
			t.Fatalf("leftover temp file %s", e.Name())
		}
	}
}
