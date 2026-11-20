package event

import (
	"errors"
	"testing"
)

func TestTaskUpdatedCheck(t *testing.T) {
	s := func(v string) *string { return &v }
	bad := []TaskUpdated{
		{Reason: "r"},
		{Branch: s(""), Reason: "r"},
		{Branch: s("HEAD"), Reason: "r"},
		{Branch: s("build/.hidden"), Reason: "r"},
		{Branch: s("build/x.lock/y"), Reason: "r"},
		{Branch: s("build//x"), Reason: "r"},
		{BaseBranch: s("a..b"), Reason: "r"},
		{Worktree: s("/abs/wt"), Reason: "r"},
		{Worktree: s("../outside"), Reason: "r"},
		{Brief: s("briefs/../../x.md"), Reason: "r"},
		{Brief: s(`C:\x.md`), Reason: "r"},
		{Repo: s("two words"), Reason: "r"},
	}
	for _, u := range bad {
		if err := u.Check(); err == nil {
			t.Errorf("accepted %v", u.Fields())
		}
	}
	var fe *FieldError
	if err := (TaskUpdated{Worktree: s("/abs")}).Check(); !errors.As(err, &fe) || fe.Field != "worktree" {
		t.Errorf("error %v does not name the field", err)
	}
	good := TaskUpdated{Repo: s("ekumen"), BaseBranch: s("main"), Branch: s("build/t-a"),
		Worktree: s(".worktrees/t-a"), Brief: s(".ekumen/briefs/T-a/brief.md"), Title: s("A"), Reason: "r"}
	if err := good.Check(); err != nil {
		t.Errorf("refused %v: %v", good.Fields(), err)
	}
}
