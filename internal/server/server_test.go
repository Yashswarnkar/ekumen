package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Yashswarnkar/ekumen/internal/buildinfo"
)

func testUI() fstest.MapFS {
	return fstest.MapFS{"index.html": {Data: []byte("<h1>Ekumen test UI</h1>")}}
}

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(testUI()).ServeHTTP(rec, httptest.NewRequest("GET", "/api/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" || body.Version != buildinfo.Version {
		t.Errorf("body = %+v, want status ok and version %q", body, buildinfo.Version)
	}
}

func TestIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(testUI()).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Ekumen test UI") {
		t.Errorf("body = %q, want the index page", rec.Body.String())
	}
}

func TestUnknownAPIRoute(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(testUI()).ServeHTTP(rec, httptest.NewRequest("GET", "/api/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
