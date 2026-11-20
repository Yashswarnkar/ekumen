package legacyimport

import (
	"regexp"

	"github.com/Yashswarnkar/ekumen/internal/event"
)

// IDMap is the old-id → new-id table from the T-import brief, decided by the
// orchestrator. Contract aliases (C1…C5) come from contract front matter and
// the registry, not from this table.
var IDMap = map[string]string{
	"R1": "T-acp-research",
	"R2": "T-landscape-research",
	"T0": "T-scaffold",

	"D-001": "D-target-user",
	"D-002": "D-agent-agnostic",
	"D-003": "D-core-language",
	"D-004": "D-ui-stack",
	"D-005": "D-project-name",
	"D-006": "D-v01-ui-scope",
	"D-007": "D-code-over-tokens",
	"D-008": "D-project-folder",
	"D-009": "D-license",
	"D-010": "D-plain-vocabulary",
	"D-011": "D-research-location",
	"D-012": "D-control-loop",
	"D-013": "D-claude-status-source",
	"D-014": "D-decision-delivery",
	"D-015": "D-adapter-base",
	"D-016": "D-managed-observed-sessions",
	"D-017": "D-claude-auth",
	"D-018": "D-acp-version",
	"D-019": "D-id-scheme",
}

// legacyNumericRe matches ids from the numbered scheme D-019 replaced
// (D-012, R1, T0, C1). D-012 is also a valid slug id, so it needs its own
// check: an unmapped one is an item missing from the mapping.
var legacyNumericRe = regexp.MustCompile(`^(D-[0-9]+|[RTC][0-9]+)$`)

// aliases resolves old ids (the table above plus contract aliases found in
// the inputs) to slug ids.
type aliases map[string]string

func newAliases() aliases {
	a := aliases{}
	for k, v := range IDMap {
		a[k] = v
	}
	return a
}

// newID returns the slug id for an old id, or the id itself when it is
// already a slug id with no mapping. ok is false when old is neither mapped
// nor a valid slug id: an item missing from the mapping.
func (a aliases) newID(old string) (string, bool) {
	if n, ok := a[old]; ok {
		return n, true
	}
	if legacyNumericRe.MatchString(old) {
		return old, false // an old numbered id the mapping does not cover
	}
	if event.ValidateID(old) == nil {
		return old, true
	}
	return old, false
}
