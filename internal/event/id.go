package event

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"regexp"
	"sync"
	"time"
)

// Item kinds named by the first letter of a slug id (C-event-log §1).
const (
	KindLetterTask     = 'T'
	KindLetterDecision = 'D'
	KindLetterContract = 'C'
	KindLetterExternal = 'X'
)

// MaxSlugLength is the longest the slug part of an id may be (C-event-log
// v2 §1, D-slug-length-scope): the slug itself, without the kind letter and
// hyphen.
const MaxSlugLength = 40

// MaxIDLength is the longest a whole id may be: kind letter, hyphen and a
// slug of MaxSlugLength.
const MaxIDLength = MaxSlugLength + 2

var idPattern = regexp.MustCompile(`^[TDCX]-[a-z0-9]+(-[a-z0-9]+){0,4}$`)

// ValidateID checks a task, decision, contract or external id against
// C-event-log v2 §1: the regex `^[TDCX]-[a-z0-9]+(-[a-z0-9]+){0,4}$` and a
// slug (the part after "<kind letter>-") of at most 40 characters, so the
// whole id is at most 42.
func ValidateID(id string) error {
	if len(id) > MaxIDLength {
		return fmt.Errorf("id %q: slug longer than %d characters", id, MaxSlugLength)
	}
	if !idPattern.MatchString(id) {
		return fmt.Errorf("id %q: does not match %s", id, idPattern)
	}
	return nil
}

// ValidateIDOfKind is ValidateID plus a check that the id names the given
// subject kind (task, decision, contract or external).
func ValidateIDOfKind(id string, kind SubjectKind) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if k := KindOf(id); k != kind {
		return fmt.Errorf("id %q: names a %s, want a %s", id, k, kind)
	}
	return nil
}

// KindOf returns the subject kind named by a slug id's first letter, or ""
// when the id has no known kind letter. It does not validate the rest.
func KindOf(id string) SubjectKind {
	if len(id) < 2 || id[1] != '-' {
		return ""
	}
	switch id[0] {
	case KindLetterTask:
		return KindTask
	case KindLetterDecision:
		return KindDecision
	case KindLetterContract:
		return KindContract
	case KindLetterExternal:
		return KindExternal
	}
	return ""
}

var aliasPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(-[A-Za-z0-9]+)*$`)

// ValidateAlias checks an alias: an id from before C-event-log v1, such as
// `R1`, `T0`, `D-012` or `C1` (C-event-log §1, "Aliases"). The contract gives
// no format for aliases; this accepts letters, digits and single hyphens,
// starting with a letter, at most 42 characters, and refuses anything that is
// itself a valid slug id (an alias must not shadow an id).
func ValidateAlias(alias string) error {
	if len(alias) == 0 || len(alias) > MaxIDLength {
		return fmt.Errorf("alias %q: must be 1 to %d characters", alias, MaxIDLength)
	}
	if !aliasPattern.MatchString(alias) {
		return fmt.Errorf("alias %q: must be letters, digits and single hyphens, starting with a letter", alias)
	}
	if ValidateID(alias) == nil {
		return fmt.Errorf("alias %q: is a slug id, not an alias", alias)
	}
	return nil
}

// ULIDs (C-event-log §1, "Events use a ULID").

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ULIDLength is the length of a ULID's canonical text form.
const ULIDLength = 26

// maxULIDTime is the largest millisecond timestamp a ULID can hold (48 bits).
const maxULIDTime = 1<<48 - 1

// ULIDFromParts encodes a millisecond Unix timestamp and 80 bits of entropy
// as a canonical (uppercase Crockford base32) ULID.
func ULIDFromParts(ms uint64, entropy [10]byte) (string, error) {
	if ms > maxULIDTime {
		return "", fmt.Errorf("ulid: timestamp %d out of range", ms)
	}
	var b [16]byte
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	binary.BigEndian.PutUint32(b[2:6], uint32(ms))
	copy(b[6:], entropy[:])
	return encodeULID(b), nil
}

// encodeULID writes 128 bits as 26 base32 digits, the first holding only the
// top 3 bits.
func encodeULID(b [16]byte) string {
	out := make([]byte, ULIDLength)
	// Bit position (from the most significant end of a 130-bit number whose
	// top two bits are zero) of each 5-bit group.
	for i := range ULIDLength {
		v := 0
		for j := range 5 {
			bit := i*5 + j - 2 // index into the 128 real bits
			if bit < 0 {
				continue
			}
			if b[bit/8]&(0x80>>(bit%8)) != 0 {
				v |= 1 << (4 - j)
			}
		}
		out[i] = crockford[v]
	}
	return string(out)
}

// ValidateULID checks that s is a canonical ULID: 26 uppercase Crockford
// base32 characters whose first character is at most '7' (so it fits in 128
// bits).
func ValidateULID(s string) error {
	if len(s) != ULIDLength {
		return fmt.Errorf("ulid %q: want %d characters, got %d", s, ULIDLength, len(s))
	}
	for i := range len(s) {
		c := s[i]
		if indexCrockford(c) < 0 {
			return fmt.Errorf("ulid %q: invalid character %q at %d", s, c, i)
		}
	}
	if s[0] > '7' {
		return fmt.Errorf("ulid %q: overflows 128 bits", s)
	}
	return nil
}

func indexCrockford(c byte) int {
	for i := range len(crockford) {
		if crockford[i] == c {
			return i
		}
	}
	return -1
}

// ULIDTime returns the timestamp encoded in a valid ULID.
func ULIDTime(s string) (time.Time, error) {
	if err := ValidateULID(s); err != nil {
		return time.Time{}, err
	}
	var ms uint64
	for i := range 10 {
		ms = ms<<5 | uint64(indexCrockford(s[i]))
	}
	return time.UnixMilli(int64(ms)).UTC(), nil
}

// ULIDGenerator makes ULIDs that sort by creation time. Within one
// millisecond it increments the previous entropy, so ids from one generator
// are strictly increasing. It is safe for concurrent use.
type ULIDGenerator struct {
	mu      sync.Mutex
	lastMS  uint64
	last    [10]byte
	entropy func([]byte) error
}

// NewULIDGenerator returns a generator. entropy fills a buffer with random
// bytes; nil means crypto/rand.
func NewULIDGenerator(entropy func([]byte) error) *ULIDGenerator {
	if entropy == nil {
		entropy = func(b []byte) error { _, err := rand.Read(b); return err }
	}
	return &ULIDGenerator{entropy: entropy}
}

// New returns a ULID for time t.
func (g *ULIDGenerator) New(t time.Time) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ms := uint64(t.UnixMilli())
	if ms <= g.lastMS && g.lastMS != 0 {
		// Same (or earlier) millisecond: stay monotonic.
		ms = g.lastMS
		if !increment(&g.last) {
			return "", fmt.Errorf("ulid: entropy exhausted within one millisecond")
		}
	} else {
		if err := g.entropy(g.last[:]); err != nil {
			return "", fmt.Errorf("ulid: entropy: %w", err)
		}
		g.lastMS = ms
	}
	return ULIDFromParts(ms, g.last)
}

func increment(b *[10]byte) bool {
	for i := len(b) - 1; i >= 0; i-- {
		b[i]++
		if b[i] != 0 {
			return true
		}
	}
	return false
}

var defaultGenerator = NewULIDGenerator(nil)

// NewULID returns a new ULID for time t from a shared generator.
func NewULID(t time.Time) (string, error) { return defaultGenerator.New(t) }
