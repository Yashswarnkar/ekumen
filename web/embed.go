// Package web exposes the UI as an fs.FS for `ekumen serve`.
//
// Which files are embedded is chosen by the `embedui` build tag:
//
//   - default (plain `go build ./...`, no Node needed): web/placeholder,
//     a committed one-page placeholder.
//   - `-tags embedui` (used by `make build` and CI after `npm run build`):
//     web/dist, the Vite build output.
//
// See docs/development.md.
package web

import "io/fs"

// UI returns the embedded UI with index.html at its root.
func UI() fs.FS {
	sub, err := fs.Sub(files, root)
	if err != nil {
		panic(err) // root is a constant matched by the embed pattern
	}
	return sub
}
