// Package skills embeds the agent skills shipped with the ekumen binary:
// every folder here holding a SKILL.md.
package skills

import (
	"embed"
	"io/fs"
	"path"
)

//go:embed */*
var embedded embed.FS

// Skill is one skill folder.
type Skill struct {
	Name  string
	Files []string
}

// FS holds the skill folders.
var FS fs.FS = embedded

// List returns the skills in name order, each with its files in path order.
func List() ([]Skill, error) {
	dirs, err := fs.ReadDir(FS, ".")
	if err != nil {
		return nil, err
	}
	var out []Skill
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if _, err := fs.Stat(FS, path.Join(d.Name(), "SKILL.md")); err != nil {
			continue
		}
		s := Skill{Name: d.Name()}
		err := fs.WalkDir(FS, d.Name(), func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return err
			}
			s.Files = append(s.Files, p[len(d.Name())+1:])
			return nil
		})
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
