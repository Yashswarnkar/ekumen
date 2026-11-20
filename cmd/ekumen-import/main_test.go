package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".ekumen")
	if err := os.CopyFS(dir, os.DirFS("../../internal/legacyimport/testdata/ekumen")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDryRunWritesNothing(t *testing.T) {
	root := fixture(t)
	var out, errb bytes.Buffer
	if code := run([]string{"--root", root, "--dry-run"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "verification mismatches: 0") {
		t.Fatalf("report:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "events")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote events")
	}
	if _, err := os.Stat(filepath.Join(root, "briefs/R1-acp.md")); err != nil {
		t.Fatal("dry run removed an old brief")
	}
}

func TestRealRunThenRerun(t *testing.T) {
	root := fixture(t)
	var out, errb bytes.Buffer
	if code := run([]string{"--root", root, "--json"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var res struct {
		OK      bool `json:"ok"`
		Written struct {
			Events  []string `json:"events"`
			Removed []string `json:"removed"`
		} `json:"written"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK || len(res.Written.Events) == 0 || len(res.Written.Removed) != 3 {
		t.Fatalf("result %+v", res)
	}
	out.Reset()
	if code := run([]string{"--root", root}, &out, &errb); code != 0 {
		t.Fatalf("re-run exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "wrote 0 events, 0 brief files; removed 0 old briefs") {
		t.Fatalf("re-run report:\n%s", out.String())
	}
}

func TestBadUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"extra"}, &out, &errb); code != 2 {
		t.Fatalf("exit %d", code)
	}
}
