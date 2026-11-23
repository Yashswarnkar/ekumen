package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path"
	"slices"
	"strings"
	"time"
)

// BranchFile is one file read from a branch on the git host.
type BranchFile struct {
	// Path is the file's path in the repository.
	Path string `json:"path"`
	Body []byte `json:"-"`
}

// BranchReader reads decision event files from branches on the git host,
// read-only (GitHub #38): nothing is fetched, merged or checked out. An
// Observer may also be a BranchReader; callers check with a type
// assertion and treat a missing one as "branches unknown".
type BranchReader interface {
	// DecisionRaises returns, by branch, the DecisionRaised event files
	// under <ekumen>/events/decision/ on each branch, skipping files whose
	// base name skip reports true (events already in the local log), so
	// their contents are never downloaded.
	DecisionRaises(ctx context.Context, branches []string, skip func(name string) bool) (map[string][]BranchFile, error)
}

// StaticBranches is an Observer and BranchReader with fixed contents, for
// tests.
type StaticBranches struct {
	PRs []PR
	// Files maps a branch to the files on it.
	Files map[string][]BranchFile
}

// PullRequests returns the fixed list.
func (s StaticBranches) PullRequests(context.Context) ([]PR, error) { return slices.Clone(s.PRs), nil }

// DecisionRaises returns the fixed files of each asked branch that are
// DecisionRaised events and not skipped.
func (s StaticBranches) DecisionRaises(_ context.Context, branches []string, skip func(string) bool) (map[string][]BranchFile, error) {
	out := map[string][]BranchFile{}
	for _, b := range branches {
		for _, f := range s.Files[b] {
			name := path.Base(f.Path)
			if strings.HasSuffix(name, raisedSuffix) && (skip == nil || !skip(name)) {
				out[b] = append(out[b], f)
			}
		}
	}
	return out, nil
}

const raisedSuffix = ".DecisionRaised.json"

// DecisionRaises reads the branches with two `gh api graphql` calls: one
// lists the decision event file names on every branch, the second
// downloads the DecisionRaised files not skipped. The events folder is
// found from Dir's place in the repository (`git rev-parse
// --show-prefix`).
func (g GH) DecisionRaises(ctx context.Context, branches []string, skip func(string) bool) (map[string][]BranchFile, error) {
	out := map[string][]BranchFile{}
	if len(branches) == 0 {
		return out, nil
	}
	gh, err := exec.LookPath("gh")
	if err != nil {
		return nil, fmt.Errorf("gh not found: %w", ErrUnavailable)
	}
	timeout := g.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	prefix := ""
	if git, err := exec.LookPath("git"); err == nil {
		cmd := exec.CommandContext(ctx, git, "rev-parse", "--show-prefix")
		cmd.Dir = g.Dir
		if b, err := cmd.Output(); err == nil {
			prefix = strings.TrimSpace(string(b))
		}
	}
	dir := prefix + ".ekumen/events/decision"

	var q strings.Builder
	q.WriteString("query($owner:String!,$repo:String!){repository(owner:$owner,name:$repo){")
	for i, b := range branches {
		expr, _ := json.Marshal(b + ":" + dir)
		fmt.Fprintf(&q, "b%d:object(expression:%s){...on Tree{entries{name object{...on Tree{entries{name oid}}}}}}", i, expr)
	}
	q.WriteString("}}")
	var listing struct {
		Data struct {
			Repository map[string]*struct {
				Entries []struct {
					Name   string `json:"name"`
					Object struct {
						Entries []struct {
							Name string `json:"name"`
							OID  string `json:"oid"`
						} `json:"entries"`
					} `json:"object"`
				} `json:"entries"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := g.graphql(ctx, gh, q.String(), &listing); err != nil {
		return nil, err
	}
	type want struct{ branch, path, oid string }
	var wants []want
	oids := map[string]bool{}
	for i, b := range branches {
		tree := listing.Data.Repository[fmt.Sprintf("b%d", i)]
		if tree == nil {
			continue
		}
		for _, d := range tree.Entries {
			for _, f := range d.Object.Entries {
				if !strings.HasSuffix(f.Name, raisedSuffix) || (skip != nil && skip(f.Name)) {
					continue
				}
				wants = append(wants, want{b, dir + "/" + d.Name + "/" + f.Name, f.OID})
				oids[f.OID] = true
			}
		}
	}
	if len(wants) == 0 {
		return out, nil
	}
	q.Reset()
	q.WriteString("query($owner:String!,$repo:String!){repository(owner:$owner,name:$repo){")
	keys := make([]string, 0, len(oids))
	for o := range oids {
		keys = append(keys, o)
	}
	slices.Sort(keys)
	alias := map[string]string{}
	for i, o := range keys {
		alias[o] = fmt.Sprintf("f%d", i)
		oid, _ := json.Marshal(o)
		fmt.Fprintf(&q, "f%d:object(oid:%s){...on Blob{text}}", i, oid)
	}
	q.WriteString("}}")
	var blobs struct {
		Data struct {
			Repository map[string]*struct {
				Text string `json:"text"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := g.graphql(ctx, gh, q.String(), &blobs); err != nil {
		return nil, err
	}
	for _, w := range wants {
		if b := blobs.Data.Repository[alias[w.oid]]; b != nil {
			out[w.branch] = append(out[w.branch], BranchFile{Path: w.path, Body: []byte(b.Text)})
		}
	}
	return out, nil
}

// graphql runs one read-only `gh api graphql` query for the repository of
// Dir and decodes its output into v.
func (g GH) graphql(ctx context.Context, gh, query string, v any) error {
	cmd := exec.CommandContext(ctx, gh, "api", "graphql", "-f", "query="+query, "-F", "owner={owner}", "-F", "repo={repo}")
	cmd.Dir = g.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh api graphql: %v: %s: %w", err, strings.TrimSpace(stderr.String()), ErrUnavailable)
	}
	if err := json.Unmarshal(stdout.Bytes(), v); err != nil {
		return fmt.Errorf("gh api graphql: decode: %w", err)
	}
	return nil
}
