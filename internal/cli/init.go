package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Yashswarnkar/ekumen/internal/project"
)

// initLayout are the folders `ekumen init` creates under .ekumen/. Each
// but events/ gets a .gitkeep so the empty layout can be committed; the
// store accepts only subject-kind folders in events/ and makes them on the
// first write.
var initLayout = []string{"events", "briefs", "contracts", "decisions", "research"}

// initIgnores are the lines `ekumen init` makes sure .gitignore has: the
// local projections (D-projections-in-git) and the task worktrees.
var initIgnores = []string{project.Dir + "/registry.yaml", project.Dir + "/state.json", ".worktrees/"}

type initResult struct {
	writeResult
	Project string   `json:"project"`
	Next    []string `json:"next"`
}

// initCmd is `ekumen init [--project name] [--lead handle]`: a new project
// in the current folder (or --root), which must be inside a git repository
// and have no .ekumen/ yet. It writes only inside that folder, records no
// events and never commits.
func (r *run) initCmd(args []string) error {
	fs := r.newFlags("init")
	name := fs.String("project", "", "the project's name (default: the origin repository's name, else the folder's)")
	lead := fs.String("lead", "", "the lead's handle")
	if _, err := r.parse(fs, args, 0, 0, "init [--project name] [--lead handle]"); err != nil {
		return err
	}
	root := r.g.root
	if root == "" {
		root = r.env.Dir
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return errf(CodeInternal, "%v", err)
	}
	if _, err := gitCmd(root, "rev-parse", "--show-toplevel"); err != nil {
		return errf(CodeValidation, "%s is not inside a git repository (run `git init` first)", root)
	}
	dir := filepath.Join(root, project.Dir)
	if _, err := os.Lstat(dir); err == nil {
		return errf(CodeValidation, "%s already exists; this is already an Ekumen project", project.Dir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return errf(CodeInternal, "%v", err)
	}
	if strings.ContainsAny(*lead, " \t\n:") {
		return fieldErr(CodeValidation, "lead", "%q is not a handle", *lead)
	}
	if *name == "" {
		*name = r.repoName(root)
	}
	if *name == "" {
		*name = filepath.Base(root)
	}

	var written []string
	write := func(p string, b []byte) error {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return err
		}
		written = append(written, p)
		return nil
	}
	for _, sub := range initLayout {
		if sub == "events" {
			if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
				return errf(CodeInternal, "%v", err)
			}
			continue
		}
		if err := write(filepath.Join(dir, sub, ".gitkeep"), nil); err != nil {
			return errf(CodeInternal, "%v", err)
		}
	}
	if err := write(filepath.Join(dir, "spine.md"), project.SpineTemplate(*name)); err != nil {
		return errf(CodeInternal, "%v", err)
	}
	if err := write(filepath.Join(dir, project.ConfigFile), project.ConfigYAML(*name, *lead)); err != nil {
		return errf(CodeInternal, "%v", err)
	}
	gi := filepath.Join(root, ".gitignore")
	if changed, err := ensureLines(gi, initIgnores); err != nil {
		return errf(CodeInternal, "%v", err)
	} else if changed {
		written = append(written, gi)
	}

	res := initResult{writeResult: writeResult{OK: true, Events: []string{}, Paths: []string{}}, Project: *name}
	for _, p := range written {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = p
		}
		res.Paths = append(res.Paths, filepath.ToSlash(rel))
	}
	handle := *lead
	if handle == "" {
		handle = "<handle>"
	}
	res.Next = []string{
		fmt.Sprintf("Fill in %s/spine.md, and %s/%s (lead, agents, setup).", project.Dir, project.Dir, project.ConfigFile),
		fmt.Sprintf("git add %s .gitignore && git commit -m \"Start an Ekumen project\"", project.Dir),
		"ekumen claim orchestrator --as orchestrator:<session>   (the session that writes briefs)",
		"Draft a task (ekumen task draft --file t.yaml, plus its brief.md) and approve it (ekumen task approve <id> --as lead:" + handle + ").",
		"ekumen task start <id> sets up its worktree and prints the command that opens its session.",
	}
	r.out(res, func(w io.Writer) {
		fmt.Fprintf(w, "created Ekumen project %q in %s:\n", *name, root)
		for _, p := range res.Paths {
			fmt.Fprintf(w, "  %s\n", p)
		}
		fmt.Fprintln(w, "\nNext steps (nothing was committed):")
		for i, s := range res.Next {
			fmt.Fprintf(w, "  %d. %s\n", i+1, s)
		}
	})
	return nil
}

// ensureLines appends each line missing from the file (created if absent).
func ensureLines(path string, lines []string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	var add []string
	for _, l := range lines {
		if !have[l] && !have["/"+l] {
			add = append(add, l)
		}
	}
	if len(add) == 0 {
		return false, nil
	}
	text := string(b)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += "# Ekumen: local projections and task worktrees\n" + strings.Join(add, "\n") + "\n"
	return true, os.WriteFile(path, []byte(text), 0o644)
}
