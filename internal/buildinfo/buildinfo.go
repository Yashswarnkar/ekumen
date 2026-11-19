// Package buildinfo holds version metadata injected at link time with
// -ldflags "-X github.com/Yashswarnkar/ekumen/internal/buildinfo.Version=...".
package buildinfo

// Set with -ldflags -X; each defaults to "dev" for plain `go build`.
var (
	Version = "dev"
	Commit  = "dev"
	Date    = "dev"
)
