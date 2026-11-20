package observe

import "testing"

func TestParseGH(t *testing.T) {
	out := []byte(`[
 {"number":4,"title":"a","headRefName":"build/a","isDraft":false,"state":"MERGED","url":"u4","statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"}]},
 {"number":6,"title":"b","headRefName":"build/b","isDraft":true,"state":"OPEN","url":"u6","statusCheckRollup":[{"status":"IN_PROGRESS","conclusion":""}]},
 {"number":7,"title":"c","headRefName":"build/c","isDraft":false,"state":"OPEN","url":"u7","statusCheckRollup":[{"conclusion":"SUCCESS"},{"conclusion":"FAILURE"}]},
 {"number":8,"title":"d","headRefName":"build/a","isDraft":false,"state":"CLOSED","url":"u8","statusCheckRollup":[]}
]`)
	prs, err := ParseGH(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ state, checks string }{{StateMerged, ChecksSuccess}, {StateDraft, ChecksPending}, {StateOpen, ChecksFailure}, {StateClosed, ChecksNone}}
	for i, w := range want {
		if prs[i].State != w.state || prs[i].Checks != w.checks {
			t.Errorf("pr %d: %+v, want %v", prs[i].Number, prs[i], w)
		}
	}
	by := ByBranch(prs)
	if by["build/a"].Number != 4 {
		t.Errorf("build/a picked #%d, want the merged #4 over the closed #8", by["build/a"].Number)
	}
}
