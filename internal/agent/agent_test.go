package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestNoToolOrVendorNames_Spine12(t *testing.T) {
	banned := regexp.MustCompile(`(?i)claude|codex|gemini|opencode|cursor|copilot|anthropic|openai`)
	var files []string
	for _, pat := range []string{"*.go", "conformance/*.go"} {
		m, err := filepath.Glob(pat)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for i, line := range strings.Split(string(b), "\n") {
			if m := banned.FindString(line); m != "" {
				t.Errorf("%s:%d names %q; tool and vendor names belong in adapters", f, i+1, m)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test files checked")
	}
}

func TestCapsSupportDefaultsToUnsupported(t *testing.T) {
	c := Caps{Ops: map[string]string{OpList: Supported}}
	if c.Support(OpList) != Supported || c.Support(OpSend) != Unsupported {
		t.Errorf("Support: %q %q", c.Support(OpList), c.Support(OpSend))
	}
}
