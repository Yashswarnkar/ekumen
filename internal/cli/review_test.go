package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectionFailureIsNotSuccess(t *testing.T) {
	h := gapsFixture(t)
	dir := filepath.Join(h.root, ".ekumen", "decisions")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	h.write("d-ro.yaml", strings.ReplaceAll(gateDecision, "D-gate", "D-readonly"))
	code, out, _ := h.run("--json", "--as", "lead:me", "decision", "raise", "--file", "d-ro.yaml")
	if code != ExitInternal {
		t.Fatalf("exit %d, want %d\n%s", code, ExitInternal, out)
	}
	e := decodeJSON(t, out)["error"].(map[string]any)
	if e["code"] != CodeInternal || !strings.Contains(e["message"].(string), "events recorded (") || !strings.Contains(e["message"].(string), "ekumen project") {
		t.Errorf("error %v", e)
	}
	if _, out, _ := h.run("decision", "show", "D-readonly", "--json"); !strings.Contains(out, `"D-readonly"`) {
		t.Errorf("event not kept: %s", out)
	}
}
