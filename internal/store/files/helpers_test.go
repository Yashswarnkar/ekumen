package files

import (
	"crypto/rand"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	ulidMu   sync.Mutex
	ulidLast [16]byte
)

// newULID returns a monotonic ULID for tests (48-bit ms time + 80 random
// bits, incremented within the same millisecond).
func newULID(t testing.TB) string {
	t.Helper()
	ulidMu.Lock()
	defer ulidMu.Unlock()
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	if [6]byte(b[:6]) == [6]byte(ulidLast[:6]) {
		b = ulidLast
		for i := 15; i >= 6; i-- {
			b[i]++
			if b[i] != 0 {
				break
			}
		}
	} else if _, err := rand.Read(b[6:]); err != nil {
		t.Fatal(err)
	}
	ulidLast = b
	return encodeULID(b)
}

func encodeULID(b [16]byte) string {
	// 128 bits -> 26 base32 chars (the first holds 3 bits).
	out := make([]byte, 26)
	var hi, lo uint64
	for i := 0; i < 8; i++ {
		hi = hi<<8 | uint64(b[i])
		lo = lo<<8 | uint64(b[8+i])
	}
	for i := 25; i >= 0; i-- {
		out[i] = crockford[lo&31]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out)
}

type subject struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// event builds a raw event with the given envelope fields.
func event(t testing.TB, id, typ, kind, sid string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"schema":  1,
		"id":      id,
		"type":    typ,
		"at":      "2026-11-20T09:14:03Z",
		"actor":   map[string]string{"role": "session", "id": "t-file-store"},
		"subject": subject{kind, sid},
		"data":    map[string]any{"note": "hello", "nested": map[string]int{"n": 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func idOf(t testing.TB, raw []byte) string {
	t.Helper()
	env, err := parseEnvelope(raw)
	if err != nil {
		t.Fatal(err)
	}
	return env.ID
}
