package observe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func fakeGH(t *testing.T, out string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf %s '" + out + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// Issue #15: a list as long as the limit is returned, with ErrTruncated.
func TestGHTruncated_Issue15(t *testing.T) {
	fakeGH(t, `[{"number":2,"headRefName":"b","state":"OPEN"},{"number":1,"headRefName":"a","state":"MERGED"}]`)
	prs, err := GH{Dir: t.TempDir(), Limit: 2}.PullRequests(context.Background())
	if !errors.Is(err, ErrTruncated) || errors.Is(err, ErrUnavailable) || len(prs) != 2 {
		t.Fatalf("at the limit: %d PRs, err %v", len(prs), err)
	}
	prs, err = GH{Dir: t.TempDir(), Limit: 3}.PullRequests(context.Background())
	if err != nil || len(prs) != 2 {
		t.Fatalf("under the limit: %d PRs, err %v", len(prs), err)
	}
}
