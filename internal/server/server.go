// Package server implements the HTTP server behind `ekumen serve`.
package server

import (
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/Yashswarnkar/ekumen/internal/buildinfo"
)

// DefaultAddr is localhost-only by design.
const DefaultAddr = "127.0.0.1:7477"

// Handler returns the root handler: the API under /api/ and the UI
// (a filesystem whose root contains index.html) everywhere else.
func Handler(ui fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", health)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", http.FileServerFS(ui))
	return mux
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok", Version: buildinfo.Version})
}
