package web

import (
	"io/fs"
	"testing"
)

func TestUIHasIndex(t *testing.T) {
	b, err := fs.ReadFile(UI(), "index.html")
	if err != nil {
		t.Fatalf("index.html not embedded (Embedded=%v): %v", Embedded, err)
	}
	if len(b) == 0 {
		t.Fatal("index.html is empty")
	}
}
