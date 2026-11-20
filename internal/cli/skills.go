package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/Yashswarnkar/ekumen/skills"
)

type skillView struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Files       []string `json:"files"`
}

func (r *run) skillsCmd(sub string, args []string) error {
	switch sub {
	case "list":
		return r.skillsList(args)
	case "install":
		return r.skillsInstall(args)
	}
	return errf(CodeUsage, "unknown command %q", strings.TrimSpace("skills "+sub))
}

func skillFrontMatter(b []byte) (map[string]any, error) {
	rest, ok := bytes.CutPrefix(b, []byte("---\n"))
	if !ok {
		return nil, errors.New("no front matter")
	}
	head, _, ok := bytes.Cut(rest, []byte("\n---\n"))
	if !ok {
		return nil, errors.New("unterminated front matter")
	}
	var m map[string]any
	if err := yaml.Unmarshal(head, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func readSkillFile(name, file string) ([]byte, error) {
	return fs.ReadFile(skills.FS, path.Join(name, file))
}

func (r *run) skillsList(args []string) error {
	fs := r.newFlags("skills list")
	if _, err := r.parse(fs, args, 0, 0, "skills list"); err != nil {
		return err
	}
	list, err := skills.List()
	if err != nil {
		return errf(CodeInternal, "read embedded skills: %v", err)
	}
	views := []skillView{}
	for _, s := range list {
		b, err := readSkillFile(s.Name, "SKILL.md")
		if err != nil {
			return errf(CodeInternal, "%v", err)
		}
		m, err := skillFrontMatter(b)
		if err != nil {
			return errf(CodeInternal, "skill %s: %v", s.Name, err)
		}
		desc, _ := m["description"].(string)
		views = append(views, skillView{Name: s.Name, Description: desc, Files: s.Files})
	}
	r.out(struct {
		Skills []skillView `json:"skills"`
	}{views}, func(w io.Writer) {
		for _, v := range views {
			d := v.Description
			if i := strings.IndexAny(d, ".\n"); i > 0 {
				d = d[:i]
			}
			fmt.Fprintf(w, "/%-10s %s\n", v.Name, d)
		}
	})
	return nil
}

func (r *run) skillsInstall(args []string) error {
	fs := r.newFlags("skills install")
	dir := fs.String("dir", "", "folder to install into; each skill goes in <dir>/<name>/")
	force := fs.Bool("force", false, "overwrite files that differ")
	if _, err := r.parse(fs, args, 0, 0, "skills install --dir <path> [--force]"); err != nil {
		return err
	}
	if *dir == "" {
		return fieldErr(CodeUsage, "dir", "skills install: --dir <path> is required")
	}
	base := *dir
	if !filepath.IsAbs(base) {
		base = filepath.Join(r.env.Dir, base)
	}
	list, err := skills.List()
	if err != nil {
		return errf(CodeInternal, "read embedded skills: %v", err)
	}
	type pending struct {
		shown, dst string
		data       []byte
	}
	var todo []pending
	for _, s := range list {
		for _, f := range s.Files {
			data, err := readSkillFile(s.Name, f)
			if err != nil {
				return errf(CodeInternal, "%v", err)
			}
			rel := filepath.Join(s.Name, filepath.FromSlash(f))
			p := pending{shown: filepath.ToSlash(filepath.Join(*dir, rel)), dst: filepath.Join(base, rel), data: data}
			old, err := os.ReadFile(p.dst)
			switch {
			case err == nil && bytes.Equal(old, data):
				continue
			case err == nil && !*force:
				return fieldErr(CodeValidation, "force", "skills install: %s exists and differs; pass --force to overwrite it", p.shown)
			case err != nil && !errors.Is(err, os.ErrNotExist):
				return errf(CodeInternal, "read %s: %v", p.shown, err)
			}
			todo = append(todo, p)
		}
	}
	res := writeResult{OK: true, Events: []string{}, Paths: []string{}}
	for _, p := range todo {
		if err := os.MkdirAll(filepath.Dir(p.dst), 0o755); err != nil {
			return errf(CodeInternal, "install: %v", err)
		}
		if err := writeFileAtomic(p.dst, p.data); err != nil {
			return errf(CodeInternal, "install %s: %v", p.shown, err)
		}
		res.Paths = append(res.Paths, p.shown)
	}
	r.out(res, func(w io.Writer) {
		fmt.Fprintf(w, "installed %d skill(s) in %s: %d file(s) written\n", len(list), *dir, len(res.Paths))
		for _, p := range res.Paths {
			fmt.Fprintf(w, "  %s\n", p)
		}
	})
	return nil
}
