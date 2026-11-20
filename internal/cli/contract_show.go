package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Yashswarnkar/ekumen/internal/event"
	"github.com/Yashswarnkar/ekumen/internal/project"
)

func (r *run) contractShow(args []string) error {
	fs := r.newFlags("contract show")
	pos, err := r.parse(fs, args, 1, 1, "contract show <id>")
	if err != nil {
		return err
	}
	s, err := r.load()
	if err != nil {
		return err
	}
	id, err := r.findContract(s, pos[0])
	if err != nil {
		return withKindHint(s, err, pos[0], event.KindContract)
	}
	c, text, err := s.ContractShow(id)
	if err != nil {
		r.warn("%v", err)
	}
	r.out(struct {
		Contract project.ContractDetail `json:"contract"`
		Text     string                 `json:"text"`
	}{c, text}, func(w io.Writer) {
		fmt.Fprintf(w, "%s · %s\n", c.ID, c.Title)
		if len(c.Aliases) > 0 {
			fmt.Fprintf(w, "aliases: %s\n", strings.Join(c.Aliases, ", "))
		}
		fmt.Fprintf(w, "status: %s, version %d", c.Status, c.Version)
		if c.FrozenOn != nil {
			fmt.Fprintf(w, ", frozen on %s", *c.FrozenOn)
		}
		fmt.Fprintln(w)
		if c.Path != nil {
			fmt.Fprintf(w, "path: %s\n", *c.Path)
		}
		fmt.Fprintf(w, "consumers: %s\nproducers: %s\n", orNone(c.Consumers), orNone(c.Producers))
		if len(c.BlockedBy) > 0 {
			fmt.Fprintf(w, "blocked by: %s\n", strings.Join(c.BlockedBy, ", "))
		}
		for _, f := range c.Freezes {
			fmt.Fprintf(w, "frozen   v%d %s sha256 %s\n", f.Version, f.At, f.TextSHA256)
		}
		for _, v := range c.Versions {
			fmt.Fprintf(w, "versioned v%d %s by %s sha256 %s\n", v.Version, v.At, v.ByDecision, v.TextSHA256)
		}
		if c.Pending != nil {
			fmt.Fprintf(w, "pending: version %d (%s) waits for `ekumen contract freeze %s`\n", c.Pending.Version, c.Pending.ByDecision, c.ID)
		}
		fmt.Fprintf(w, "\n%s", text)
	})
	return nil
}

func orNone(l []string) string {
	if len(l) == 0 {
		return "none"
	}
	return strings.Join(l, ", ")
}

var showCommand = map[event.SubjectKind]string{
	event.KindTask:     "ekumen task show",
	event.KindDecision: "ekumen decision show",
	event.KindContract: "ekumen contract show",
}

// withKindHint adds to a not-found error, when the id names another kind
// of item, which command shows it.
func withKindHint(s *project.Snapshot, err error, given string, want event.SubjectKind) error {
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != CodeNotFound {
		return err
	}
	id := s.State.Resolve(given)
	kind := event.KindOf(id)
	if kind == "" || kind == want {
		return err
	}
	hint := fmt.Sprintf("%s is a %s id; use `%s %s`", given, kind, showCommand[kind], given)
	if kind == event.KindExternal {
		hint = fmt.Sprintf("%s is an external dependency id; see `ekumen graph` or .ekumen/registry.yaml", given)
	}
	return &Error{Code: ce.Code, Field: ce.Field, Message: ce.Message + " (" + hint + ")"}
}
