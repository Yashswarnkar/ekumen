package files

import (
	"fmt"
	"regexp"
)

// Subject kinds from C-event-log §2.
const (
	KindTask     = "task"
	KindDecision = "decision"
	KindContract = "contract"
	KindExternal = "external"
	KindProject  = "project"
)

// ProjectSubjectID is the subject id of project-wide events (C-event-log §2).
const ProjectSubjectID = "project"

// kindLetter maps each subject kind with slug ids to its id letter
// (C-event-log §1). The project kind has the single id "project".
var kindLetter = map[string]byte{
	KindTask:     'T',
	KindDecision: 'D',
	KindContract: 'C',
	KindExternal: 'X',
}

// maxSlugLen is the slug limit from C-event-log §1 ("≤ 40 characters in
// total"), read as applying to the slug after "<letter>-".
const maxSlugLen = 40

var (
	// C-event-log §1.
	idRe = regexp.MustCompile(`^[TDCX]-[a-z0-9]+(-[a-z0-9]+){0,4}$`)
	// Crockford base32, upper case, 26 characters; the first character is
	// at most 7 so the value fits in 128 bits.
	ulidRe = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)
	// Event types are PascalCase identifiers (C-event-log §6). The store
	// checks the shape only, so new types (a minor change, §9) need no
	// store change.
	typeRe = regexp.MustCompile(`^[A-Z][A-Za-z0-9]{0,63}$`)
	// Author slugs in entry names: lowercase words joined by "-".
	authorRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

const maxAuthorLen = 64

func invalid(format string, args ...any) error {
	return fmt.Errorf("files: "+format+": %w", append(args, ErrInvalid)...)
}

func validULID(id string) error {
	if !ulidRe.MatchString(id) {
		return invalid("event id %q is not an upper-case ULID", id)
	}
	return nil
}

func validType(t string) error {
	if !typeRe.MatchString(t) {
		return invalid("event type %q is not a PascalCase name", t)
	}
	return nil
}

// validSubject checks a subject kind and id together: the kind must be
// known, and the id must match the C-event-log §1 format with the letter
// for that kind ("project" for the project kind).
func validSubject(kind, id string) error {
	if kind == KindProject {
		if id != ProjectSubjectID {
			return invalid("subject id for kind %q must be %q, got %q", kind, ProjectSubjectID, id)
		}
		return nil
	}
	letter, ok := kindLetter[kind]
	if !ok {
		return invalid("unknown subject kind %q", kind)
	}
	if err := validID(id); err != nil {
		return err
	}
	if id[0] != letter {
		return invalid("subject id %q does not start with %q, the letter for kind %q", id, string(letter), kind)
	}
	return nil
}

// validID checks an id against C-event-log §1 (regex and slug length).
func validID(id string) error {
	if !idRe.MatchString(id) {
		return invalid("id %q does not match C-event-log §1", id)
	}
	if len(id)-2 > maxSlugLen {
		return invalid("id %q has a slug longer than %d characters", id, maxSlugLen)
	}
	return nil
}

func validTask(id string) error {
	if err := validID(id); err != nil {
		return err
	}
	if id[0] != 'T' {
		return invalid("task id %q does not start with \"T-\"", id)
	}
	return nil
}

func validAuthor(a string) error {
	if len(a) > maxAuthorLen || !authorRe.MatchString(a) {
		return invalid("author %q is not a lowercase slug of at most %d characters", a, maxAuthorLen)
	}
	return nil
}
