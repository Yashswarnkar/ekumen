//go:build embedui

package web

import "embed"

// Fails to compile if web/dist is missing: run `npm run build` in web/ first.
//
//go:embed all:dist
var files embed.FS

const root = "dist"

// Embedded reports whether the built UI (web/dist) is embedded.
const Embedded = true
