package legacyimport

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/store/files"
)

var update = flag.Bool("update", false, "rewrite testdata/events.golden.jsonl")

// fixture copies testdata/ekumen (a copy of the real .ekumen/ records) to a
// temporary folder and returns its path.
func fixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".ekumen")
	if err := os.CopyFS(dir, os.DirFS("testdata/ekumen")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func encodeAll(t *testing.T, evs []event.Event) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, e := range evs {
		b, err := event.Encode(e)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func mustPrepare(t *testing.T, root string) *Plan {
	t.Helper()
	p, err := Prepare(root)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// snapshot returns every file under root with its bytes.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		out[p] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGoldenEvents(t *testing.T) {
	p := mustPrepare(t, fixture(t))
	got := encodeAll(t, p.Events)
	const golden = "testdata/events.golden.jsonl"
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("events differ from %s; run go test ./internal/legacyimport -run TestGoldenEvents -update after an intended change", golden)
	}
}

// TestDeterminism: the same inputs give byte-identical events, a real run
// followed by a re-run writes nothing new, and the events a re-run plans
// (now reading the split briefs) are the same bytes.
func TestDeterminism(t *testing.T) {
	root := fixture(t)
	first := encodeAll(t, mustPrepare(t, root).Events)
	second := encodeAll(t, mustPrepare(t, root).Events)
	if !bytes.Equal(first, second) {
		t.Fatal("two plans on the same inputs differ")
	}
	// Same records in another folder: same bytes (no path or clock leaks in).
	if other := encodeAll(t, mustPrepare(t, fixture(t)).Events); !bytes.Equal(first, other) {
		t.Fatal("plans on two copies of the same inputs differ")
	}

	p := mustPrepare(t, root)
	w, err := Apply(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Events) != len(p.Events) || len(w.Removed) != 3 || len(w.Files) != 11 {
		t.Fatalf("wrote %d events (want %d), %d files, removed %d", len(w.Events), len(p.Events), len(w.Files), len(w.Removed))
	}
	before := snapshot(t, root)

	again := mustPrepare(t, root)
	if len(again.New) != 0 || again.Unchanged != len(p.Events) || len(again.Writes) != 0 || len(again.Removes) != 0 {
		t.Fatalf("re-run plans %d new events, %d unchanged, %d writes, %d removes", len(again.New), again.Unchanged, len(again.Writes), len(again.Removes))
	}
	if got := encodeAll(t, again.Events); !bytes.Equal(got, first) {
		t.Fatal("re-run after the real run plans different events")
	}
	if !again.OK() {
		t.Fatalf("re-run not OK: %v %v", again.Mismatches, again.Conflicts)
	}
	if _, err := Apply(again); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, root); !mapsEqual(before, after) {
		t.Fatal("re-run changed files")
	}

	// The written log reads back through the store and reduces to the same
	// state as the plan.
	raws, warns, err := files.New(root).ReadAll()
	if err != nil || len(warns) > 0 {
		t.Fatalf("read back: %v %v", err, warns)
	}
	if len(raws) != len(p.Events) {
		t.Fatalf("read back %d events, want %d", len(raws), len(p.Events))
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestVerifyRealRecords: the fixture reduces to exactly the hand-written
// records.
func TestVerifyRealRecords(t *testing.T) {
	p := mustPrepare(t, fixture(t))
	if len(p.Mismatches) > 0 {
		t.Fatalf("mismatches: %v", p.Mismatches)
	}
	if len(p.Conflicts) > 0 {
		t.Fatalf("conflicts: %v", p.Conflicts)
	}
	if p.Mapped.Decisions != 28 || p.Mapped.Tasks != 13 || p.Mapped.Contracts != 5 {
		t.Fatalf("mapped %+v", p.Mapped)
	}
	for old, want := range IDMap {
		if p.Aliases[old] != want {
			t.Errorf("alias %s → %q, want %q", old, p.Aliases[old], want)
		}
	}
	for _, c := range []string{"C1", "C2", "C3", "C4", "C5"} {
		if !strings.HasPrefix(p.Aliases[c], "C-") {
			t.Errorf("contract alias %s → %q", c, p.Aliases[c])
		}
	}
	s := core.Reduce(p.Events)
	if d := s.Decisions["D-decision-delivery"]; d.Answer != "other" || d.AppliedOn != "2026-11-20" {
		t.Errorf("D-decision-delivery: %+v", d)
	}
	if d := s.Decisions["D-license"]; d.Status != core.StatusDefaulted {
		t.Errorf("D-license status %q", d.Status)
	}
	if d := s.Decisions["D-alias-field"]; d.RaisedBy.On != "2026-11-20" {
		t.Errorf("D-alias-field raised_by.on %q (the `true:` key)", d.RaisedBy.On)
	}
}

// TestVerifyCatchesMismatches: a record the events cannot reproduce fails
// the import with a report, and Apply refuses it.
func TestVerifyCatchesMismatches(t *testing.T) {
	root := fixture(t)
	edit := func(rel, old, new string) {
		t.Helper()
		p := filepath.Join(root, rel)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(b, []byte(old)) {
			t.Fatalf("%s has no %q", rel, old)
		}
		if err := os.WriteFile(p, bytes.Replace(b, []byte(old), []byte(new), 1), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// T-skills is a draft with no approval; claiming "ready" can't be true.
	edit("registry.yaml", "blocked_by: [T-cli]\n    status: draft", "blocked_by: [T-cli]\n    status: ready")
	// "applied" is not a decision status (it is applied_on); the events
	// can't give it.
	edit("decisions/D-001.yaml", "status: decided", "status: applied")

	p := mustPrepare(t, root)
	var fields []string
	for _, m := range p.Mismatches {
		fields = append(fields, m.Record+" "+m.Field)
	}
	for _, want := range []string{"registry.yaml task T-skills status", "decisions/D-001.yaml status"} {
		if !slices.Contains(fields, want) {
			t.Errorf("no mismatch %q in %v", want, fields)
		}
	}
	if p.OK() {
		t.Fatal("plan with mismatches is OK")
	}
	if _, err := Apply(p); err == nil {
		t.Fatal("Apply accepted a plan with mismatches")
	}
	if _, err := os.Stat(filepath.Join(root, "events")); !os.IsNotExist(err) {
		t.Fatal("events written despite mismatches")
	}
}

func TestMissingMappingEscalates(t *testing.T) {
	root := fixture(t)
	rec := "id: D-020\ntitle: x\ntier: 2\nraised_by: {task: null, session: orchestrator, on: 2026-11-20}\n" +
		"question: q\noptions: [{id: a, tradeoff: a}, {id: b, tradeoff: b}]\nstatus: pending\n"
	if err := os.WriteFile(filepath.Join(root, "decisions/D-020.yaml"), []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Prepare(root)
	if err == nil || !strings.Contains(err.Error(), "missing from the id mapping") {
		t.Fatalf("err = %v", err)
	}
}

func TestSplitOldBriefs(t *testing.T) {
	p := mustPrepare(t, fixture(t))
	s := core.Reduce(p.Events)
	r1 := s.Tasks["T-acp-research"]
	if r1.Brief != "briefs/T-acp-research/brief.md" || len(r1.Amendments) != 2 || len(r1.Handoffs) != 1 {
		t.Fatalf("T-acp-research: brief %q, %d amendments, %d handoffs", r1.Brief, len(r1.Amendments), len(r1.Handoffs))
	}
	if got := r1.Amendments[1].File; got != "briefs/T-acp-research/amendments/20261120T120000Z-orchestrator-2.md" {
		t.Errorf("second amendment %q", got)
	}
	h := r1.Handoffs[0]
	if h.By != "r1-acp" || !h.HasDecisions || !h.HasFollowups || !h.HasDeviations || h.StatusLine != "done (draft PR open, not merged)" {
		t.Errorf("R1 handoff %+v", h)
	}
	t0 := s.Tasks["T-scaffold"].Handoffs[0]
	if t0.HasDecisions || t0.HasDeviations || !t0.HasFollowups {
		t.Errorf("T0 handoff flags %+v (DECISIONS RAISED and DEVIATIONS start with none)", t0)
	}
	if !slices.Contains(p.Writes, "briefs/T-scaffold/brief.md") || !slices.Contains(p.Removes, "briefs/T0-scaffold.md") {
		t.Errorf("writes %v removes %v", p.Writes, p.Removes)
	}
}

func TestHandoffSections(t *testing.T) {
	old := "STATUS: done\nOUTPUT: x\nDECISIONS RAISED (drafts): \n- D-x\nFOLLOW-UPS: none\nDEVIATIONS: none from §A.\nFRICTION: y\n"
	s := handoffSections(old)
	if s["STATUS"] != "done" || !nonEmptySection(s["DECISIONS RAISED"]) || nonEmptySection(s["FOLLOW-UPS"]) || nonEmptySection(s["DEVIATIONS"]) {
		t.Errorf("old format: %q", s)
	}
	neu := "# T handoff\n\n## STATUS\n\nDone.\n\n## FOLLOW-UPS\n\n- a\n\n## DEVIATIONS\n\nNone.\n"
	s = handoffSections(neu)
	if firstLine(s["STATUS"], 200) != "Done." || !nonEmptySection(s["FOLLOW-UPS"]) || nonEmptySection(s["DEVIATIONS"]) {
		t.Errorf("new format: %q", s)
	}
}
