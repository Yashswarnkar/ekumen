package claudecode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// store is the adapter's state directory. Observations are not events
// (C-event-log §7 rule 2), so nothing here is under .ekumen/. Layout:
//
//	sessions/<session id>.json   managed sessions this adapter started or forked
//	keys/<op>-<key hash>.json    Start and Fork keys, written before the tool runs
//	deliveries/<key hash>.json   Send outcomes (delivered, uncertain)
//	hooks/<key hash>/r.*         one hook input per file
//	settings/<key hash>.json     the --settings file of a managed session
type store struct{ dir string }

type sessionRecord struct {
	ID       string    `json:"id"`
	Key      string    `json:"key"`
	Op       string    `json:"op"`
	Name     string    `json:"name,omitempty"`
	Cwd      string    `json:"cwd"`
	Task     string    `json:"task,omitempty"`
	From     string    `json:"from,omitempty"`
	HooksDir string    `json:"hooks_dir,omitempty"`
	Result   string    `json:"result,omitempty"`
	Pending  bool      `json:"pending,omitempty"`
	At       time.Time `json:"at"`
}

type deliveryRecord struct {
	Key   string    `json:"key"`
	To    string    `json:"to"`
	State string    `json:"state"`
	Error string    `json:"error,omitempty"`
	At    time.Time `json:"at"`
}

func keyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:16]
}

func (s *store) path(parts ...string) string {
	return filepath.Join(append([]string{s.dir}, parts...)...)
}

func (s *store) read(p string, v any) bool {
	if s.dir == "" {
		return false
	}
	b, err := os.ReadFile(p)
	return err == nil && json.Unmarshal(b, v) == nil
}

func (s *store) write(p string, v any) error {
	if s.dir == "" {
		return errors.New("no state directory")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func validID(id string) bool {
	return id != "" && !strings.ContainsAny(id, `/\`) && id != "." && id != ".."
}

func (s *store) session(id string) (sessionRecord, bool) {
	var r sessionRecord
	if !validID(id) {
		return r, false
	}
	return r, s.read(s.path("sessions", id+".json"), &r)
}

func (s *store) saveSession(r sessionRecord) error {
	if !validID(r.ID) {
		return fmt.Errorf("invalid session id %q", r.ID)
	}
	return s.write(s.path("sessions", r.ID+".json"), r)
}

func (s *store) key(op, key string) (sessionRecord, bool) {
	var r sessionRecord
	return r, s.read(s.path("keys", op+"-"+keyHash(key)+".json"), &r)
}

func (s *store) saveKey(r sessionRecord) error {
	return s.write(s.path("keys", r.Op+"-"+keyHash(r.Key)+".json"), r)
}

func (s *store) dropKey(op, key string) {
	if s.dir != "" {
		os.Remove(s.path("keys", op+"-"+keyHash(key)+".json"))
	}
}

func (s *store) delivery(key string) (deliveryRecord, bool) {
	var r deliveryRecord
	return r, s.read(s.path("deliveries", keyHash(key)+".json"), &r)
}

func (s *store) saveDelivery(r deliveryRecord) error {
	return s.write(s.path("deliveries", keyHash(r.Key)+".json"), r)
}

func (s *store) dropDelivery(key string) {
	if s.dir != "" {
		os.Remove(s.path("deliveries", keyHash(key)+".json"))
	}
}
