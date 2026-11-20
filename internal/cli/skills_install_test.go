package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Yashswarnkar/ekumen/skills"
)

func TestSkillsList(t *testing.T) {
	h := newHarness(t)
	code, out, errOut := h.run("skills", "list", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	golden(t, "skills-list.json", out)
}

func TestSkillsInstall(t *testing.T) {
	h := newHarness(t)
	code, out, errOut := h.run("skills", "install", "--dir", "inst", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	golden(t, "skills-install.json", out)

	if code, out, _ = h.run("skills", "install", "--dir", "inst", "--json"); code != 0 || !strings.Contains(out, `"paths": []`) {
		t.Fatalf("identical files: want a no-op, got exit %d\n%s", code, out)
	}

	h.write("inst/decide/SKILL.md", "changed\n")
	code, out, _ = h.run("skills", "install", "--dir", "inst", "--json")
	if code != ExitRefused || !strings.Contains(out, "inst/decide/SKILL.md exists and differs") {
		t.Fatalf("differing file: want exit 2 naming it, got %d\n%s", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(h.root, "inst/decide/SKILL.md")); string(b) != "changed\n" {
		t.Fatal("refused install overwrote the file")
	}

	code, out, _ = h.run("skills", "install", "--dir", "inst", "--force", "--json")
	if code != 0 || !strings.Contains(out, `"inst/decide/SKILL.md"`) || strings.Contains(out, "blocked") {
		t.Fatalf("--force: want only the differing file written, got exit %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(h.root, ".ekumen", "events")); err == nil {
		t.Fatal("skills install wrote under .ekumen/")
	}
}

func TestEmbeddedSkillsMatchDisk(t *testing.T) {
	list, err := skills.List()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range list {
		names = append(names, s.Name)
		for _, f := range s.Files {
			want, err := os.ReadFile(filepath.Join(skillsDir, s.Name, f))
			if err != nil {
				t.Fatalf("embedded %s/%s is not on disk: %v", s.Name, f, err)
			}
			got, _ := fs.ReadFile(skills.FS, s.Name+"/"+f)
			if string(got) != string(want) {
				t.Errorf("embedded %s/%s differs from disk", s.Name, f)
			}
		}
	}
	onDisk := []string{}
	for name := range skillTexts(t) {
		onDisk = append(onDisk, name)
	}
	slices.Sort(onDisk)
	if !slices.Equal(names, onDisk) {
		t.Fatalf("embedded skills %v, on disk %v", names, onDisk)
	}
}
