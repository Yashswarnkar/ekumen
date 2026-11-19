//go:build !embedui

package web

import "embed"

//go:embed placeholder
var files embed.FS

const root = "placeholder"

// Embedded reports whether the built UI (web/dist) is embedded.
const Embedded = false
