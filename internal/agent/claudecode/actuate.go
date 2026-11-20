package claudecode

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Yashswarnkar/ekumen/internal/agent"
)

const (
	opStart = "start"
	opFork  = "fork"
)

var (
	uuidRe    = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	shortIDRe = regexp.MustCompile(`\b[0-9a-f]{8}\b`)
	slugRe    = regexp.MustCompile(`[^a-z0-9]+`)
)

// trustRefused reports Claude Code's refusal to start in a folder the user
// has not trusted; nothing was started or sent (observed on 2.1.329).
func trustRefused(stderr []byte) bool {
	return bytes.Contains(stderr, []byte("Workspace not trusted"))
}

func sessionName(task, key string) string {
	slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(task), "-"), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		slug = "task"
	}
	return "ekumen-" + slug + "-" + keyHash(key)[:8]
}

// uuidV5 derives a fork's session ID from its key, so the ID is known
// before the tool runs.
func uuidV5(name string) string {
	ns := []byte{0x6b, 0x1f, 0x52, 0x0e, 0x5a, 0x0c, 0x4e, 0x3b, 0x9d, 0x55, 0x2c, 0x2a, 0x6f, 0x0e, 0x7e, 0x11}
	h := sha1.Sum(append(ns, name...))
	h[6] = h[6]&0x0f | 0x50
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

// parseBg reads `claude --bg` stdout: the session ID it names (full UUID
// preferred, else the 8-character short id) and whether it printed a
// `note:` line, which means a copy was started under a new ID.
func parseBg(out []byte) (id string, note bool) {
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(strings.ToLower(line)), "note:") {
			note = true
		}
	}
	if m := uuidRe.Find(out); m != nil {
		return string(m), note
	}
	if m := shortIDRe.Find(out); m != nil {
		return string(m), note
	}
	return "", note
}

func sameSession(id, full string) bool {
	return id == full || (len(id) == 8 && strings.HasPrefix(full, id))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var hookEvents = []struct{ event, matcher string }{
	{"SessionStart", ""}, {"UserPromptSubmit", ""}, {"PermissionRequest", ""}, {"Notification", ""},
	{"Stop", ""}, {"StopFailure", ""}, {"SessionEnd", ""}, {"PreToolUse", "AskUserQuestion"},
}

// hookSettings is the --settings file of a managed session: hooks only.
// Each hook writes its stdin to a temporary file and renames it to r.*, so
// a reader never sees half a record.
func hookSettings(dir string) ([]byte, error) {
	q := shellQuote(dir)
	cmd := `f=$(mktemp ` + q + `/.t.XXXXXXXX) && cat > "$f" && mv "$f" ` + q + `/r."${f##*/.t.}"`
	type hook struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Async   bool   `json:"async"`
	}
	type group struct {
		Matcher string `json:"matcher,omitempty"`
		Hooks   []hook `json:"hooks"`
	}
	hooks := map[string][]group{}
	for _, h := range hookEvents {
		hooks[h.event] = []group{{Matcher: h.matcher, Hooks: []hook{{Type: "command", Command: cmd, Async: true}}}}
	}
	return json.MarshalIndent(map[string]any{"hooks": hooks}, "", "  ")
}

func notStarted(err error) error {
	return fmt.Errorf("%w: %v", agent.ErrUnavailable, err)
}

func (a *Adapter) ref(r sessionRecord) agent.SessionRef {
	return agent.SessionRef{Adapter: Name, ID: r.ID, Cwd: r.Cwd, Ownership: agent.Managed}
}

func (a *Adapter) finish(r sessionRecord, id string) (agent.SessionRef, error) {
	r.ID, r.Pending = id, false
	if err := a.state.saveSession(r); err != nil {
		return agent.SessionRef{}, err
	}
	if err := a.state.saveKey(r); err != nil {
		return agent.SessionRef{}, err
	}
	return a.ref(r), nil
}

func (a *Adapter) Start(ctx context.Context, r agent.StartReq) (agent.SessionRef, error) {
	if r.Key == "" || r.Cwd == "" || r.Prompt == "" {
		return agent.SessionRef{}, errors.New("claudecode: Start needs Key, Cwd and Prompt")
	}
	if rec, ok := a.state.key(opStart, r.Key); ok {
		if !rec.Pending {
			return a.ref(rec), nil
		}
		es, err := a.listing(ctx)
		if err != nil {
			return agent.SessionRef{}, err
		}
		if e := findByName(es, rec.Name); e != nil {
			return a.finish(rec, e.ref())
		}
		return agent.SessionRef{}, fmt.Errorf("%w: Start %q was attempted and %s is not listed", agent.ErrNotFound, r.Key, rec.Name)
	}
	name := sessionName(r.Task, r.Key)
	kh := keyHash(r.Key)
	rec := sessionRecord{Key: r.Key, Op: opStart, Name: name, Cwd: r.Cwd, Task: r.Task, HooksDir: a.state.path("hooks", kh), Pending: true, At: a.now()}
	es, err := a.listing(ctx)
	if err != nil {
		return agent.SessionRef{}, err
	}
	if e := findByName(es, name); e != nil {
		return a.finish(rec, e.ref())
	}
	settings := a.state.path("settings", kh+".json")
	b, err := hookSettings(rec.HooksDir)
	if err != nil {
		return agent.SessionRef{}, err
	}
	for _, d := range []string{rec.HooksDir, filepath.Dir(settings)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return agent.SessionRef{}, err
		}
	}
	if err := os.WriteFile(settings, b, 0o600); err != nil {
		return agent.SessionRef{}, err
	}
	if err := a.state.saveKey(rec); err != nil {
		return agent.SessionRef{}, err
	}
	res, err := a.exec(ctx, r.Cwd, "--bg", "--name", name, "--settings", settings, r.Prompt)
	if errors.Is(err, ErrNotStarted) {
		a.state.dropKey(opStart, r.Key)
		return agent.SessionRef{}, notStarted(err)
	}
	if err != nil {
		return agent.SessionRef{}, err
	}
	if res.Code != 0 {
		if trustRefused(res.Stderr) {
			a.state.dropKey(opStart, r.Key)
			return agent.SessionRef{}, fmt.Errorf("%w: %s", agent.ErrUnsupported, firstLine(res.Stderr))
		}
		return agent.SessionRef{}, fmt.Errorf("claude --bg exited %d: %s", res.Code, firstLine(res.Stderr))
	}
	if id, _ := parseBg(res.Stdout); len(id) == 36 {
		return a.finish(rec, id)
	}
	if es, err = a.listing(ctx); err == nil {
		if e := findByName(es, name); e != nil {
			return a.finish(rec, e.ref())
		}
	}
	return agent.SessionRef{}, fmt.Errorf("claude --bg started %s but it is not listed yet", name)
}

func (a *Adapter) Send(ctx context.Context, s agent.SessionRef, m agent.Message) (agent.Delivery, error) {
	switch m.Mode {
	case agent.ModeNextTurn:
	case agent.ModeSteer:
		return agent.Delivery{}, fmt.Errorf("%w: steer", agent.ErrUnsupported)
	default:
		return agent.Delivery{}, fmt.Errorf("claudecode: unknown message mode %q", m.Mode)
	}
	rec, managed := a.state.session(s.ID)
	if !managed {
		return agent.Delivery{}, fmt.Errorf("%w: observed session", agent.ErrUnsupported)
	}
	if m.Key == "" {
		return agent.Delivery{}, errors.New("claudecode: Send needs a Key")
	}
	if d, ok := a.state.delivery(m.Key); ok {
		return recorded(d)
	}
	st, err := a.Status(ctx, s)
	if err != nil || (st.State != agent.StateIdle && st.State != agent.StateDone) {
		return agent.Delivery{Key: m.Key, State: agent.Queued}, nil
	}
	d := deliveryRecord{Key: m.Key, To: s.ID, State: agent.Uncertain, Error: "send interrupted before its outcome was recorded", At: a.now()}
	if err := a.state.saveDelivery(d); err != nil {
		return agent.Delivery{}, err
	}
	res, err := a.exec(ctx, rec.Cwd, "--resume", s.ID, "--bg", m.Text)
	switch {
	case errors.Is(err, ErrNotStarted):
		a.state.dropDelivery(m.Key)
		return agent.Delivery{}, notStarted(err)
	case err != nil:
		d.Error = err.Error()
	case res.Code != 0 && trustRefused(res.Stderr):
		a.state.dropDelivery(m.Key)
		return agent.Delivery{}, fmt.Errorf("%w: %s", agent.ErrUnsupported, firstLine(res.Stderr))
	case res.Code != 0:
		d.Error = fmt.Sprintf("claude --resume --bg exited %d: %s", res.Code, firstLine(res.Stderr))
	default:
		id, note := parseBg(res.Stdout)
		if note || (id != "" && !sameSession(id, s.ID)) {
			d.Error = "claude started a copy instead of continuing the session: " + strings.TrimSpace(string(res.Stdout))
		} else {
			d.State, d.Error = agent.Delivered, ""
		}
	}
	if err := a.state.saveDelivery(d); err != nil {
		return agent.Delivery{Key: m.Key, State: agent.Uncertain}, err
	}
	return recorded(d)
}

func recorded(d deliveryRecord) (agent.Delivery, error) {
	out := agent.Delivery{Key: d.Key, State: d.State}
	if d.State == agent.Uncertain {
		return out, fmt.Errorf("delivery uncertain, not retried: %s", d.Error)
	}
	return out, nil
}

// Resume with a message is Send; any outcome other than delivered is an
// error naming the outcome. Without one it respawns a stopped background
// session and is a no-op for a live one.
func (a *Adapter) Resume(ctx context.Context, s agent.SessionRef, m *agent.Message) error {
	rec, managed := a.state.session(s.ID)
	if !managed {
		return fmt.Errorf("%w: observed session", agent.ErrUnsupported)
	}
	if m != nil {
		d, err := a.Send(ctx, s, *m)
		if err != nil {
			return err
		}
		if d.State != agent.Delivered {
			return fmt.Errorf("message %s %s", m.Key, d.State)
		}
		return nil
	}
	es, err := a.listing(ctx)
	if err != nil {
		return err
	}
	e := find(es, s.ID)
	if e == nil {
		return fmt.Errorf("%w: %s", agent.ErrNotFound, s.ID)
	}
	if e.State != "stopped" {
		return nil
	}
	res, err := a.exec(ctx, rec.Cwd, "respawn", e.ID)
	if errors.Is(err, ErrNotStarted) {
		return notStarted(err)
	}
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("claude respawn exited %d: %s", res.Code, firstLine(res.Stderr))
	}
	return nil
}
