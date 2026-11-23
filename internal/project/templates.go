package project

import (
	_ "embed"
	"strings"
)

//go:embed templates/spine.md
var spineTemplate string

// SpineTemplate is the spine.md `ekumen init` writes: the section headings
// with placeholder text, titled with the project's name.
func SpineTemplate(name string) []byte {
	return []byte(strings.ReplaceAll(spineTemplate, "{{project}}", name))
}
