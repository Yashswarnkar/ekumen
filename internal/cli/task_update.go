package cli

import (
	"strings"

	"github.com/Yashswarnkar/ekumen/internal/core"
	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/project"
)

var updateFlags = []struct{ flag, field, help string }{
	{"repo", "repo", "repository name"},
	{"base-branch", "base_branch", "branch the task's branch starts from"},
	{"branch", "branch", "the task's branch"},
	{"worktree", "worktree", "worktree path, relative to the repository root"},
	{"brief", "brief", "brief path, relative to .ekumen/"},
	{"title", "title", "task title"},
}

const taskUpdateUsage = "task update <id> [--repo r] [--base-branch b] [--branch b] [--worktree w] [--brief p] [--title t] --reason t"

// taskUpdate writes TaskUpdated (D-placeholder-task-fields): applied at
// once on an unapproved task, pending the lead's next `task approve` on an
// approved one.
func (r *run) taskUpdate(args []string) error {
	fs := r.newFlags("task update")
	vals := map[string]*string{}
	for _, f := range updateFlags {
		vals[f.field] = fs.String(f.flag, "", f.help)
	}
	reason := fs.String("reason", "", "why the fields change")
	pos, err := r.parse(fs, args, 1, 1, taskUpdateUsage)
	if err != nil {
		return err
	}
	a, _, err := r.actor(true)
	if err != nil {
		return err
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	id, err := r.findTask(s, pos[0])
	if err != nil {
		return err
	}
	sub := event.Subject{Kind: event.KindTask, ID: id}
	if err := r.probe(s, a, event.TaskUpdatedType, sub); err != nil {
		return err
	}
	if strings.TrimSpace(*reason) == "" {
		return fieldErr(CodeUsage, "reason", "--reason is required")
	}
	given := map[string]string{}
	for _, f := range updateFlags {
		if !flagSet(fs, f.flag) {
			continue
		}
		v := *vals[f.field]
		if err := checkUpdateField(s, f.field, v); err != nil {
			return fieldErr(CodeValidation, f.field, "--%s: %v", f.flag, err)
		}
		given[f.field] = v
	}
	if len(given) == 0 {
		return errf(CodeUsage, "task update: give at least one field (usage: ekumen %s)", taskUpdateUsage)
	}
	t := s.State.Tasks[id]
	switch st := s.TaskStatus(id); st {
	case core.TaskDone, core.TaskDeferred:
		return fieldErr(CodeValidation, "id", "%s is %s; a %s task is not updated", id, st, st)
	}
	cur := map[string]string{"title": t.Title, "brief": t.Brief, "repo": t.Repo,
		"base_branch": t.BaseBranch, "branch": t.Branch, "worktree": t.Worktree}
	if t.PendingUpdate != nil {
		for k, v := range t.PendingUpdate.Fields {
			cur[k] = v
		}
	}
	d := event.TaskUpdated{Reason: *reason}
	n := 0
	for _, f := range event.TaskUpdateFields {
		if v, ok := given[f]; ok && v != cur[f] {
			setUpdateField(&d, f, v)
			n++
		}
	}
	if n == 0 {
		return r.noop("no change: " + id + " already has these values")
	}
	e, err := r.mkEvent(a, event.TaskUpdatedType, sub, d)
	if err != nil {
		return err
	}
	human := "updated " + id
	if t.Approved {
		human = "proposed an update to " + id + "; it takes effect when the lead runs `ekumen task approve " + id + "`"
	}
	return r.commit(s, []event.Event{e}, nil, human)
}

func setUpdateField(d *event.TaskUpdated, field, v string) {
	p := &v
	switch field {
	case "title":
		d.Title = p
	case "brief":
		d.Brief = p
	case "repo":
		d.Repo = p
	case "base_branch":
		d.BaseBranch = p
	case "branch":
		d.Branch = p
	case "worktree":
		d.Worktree = p
	}
}

func checkUpdateField(s *project.Snapshot, field, v string) error {
	if err := event.CheckTaskField(field, v); err != nil {
		return err
	}
	if field == "brief" {
		_, err := s.ResolvePath(v)
		return err
	}
	return nil
}
