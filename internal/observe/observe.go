// Package observe reads state that Ekumen does not own and never stores as
// events (C-event-log §7 rule 2): today, pull requests from the git host
// through the `gh` CLI. Everything here is read fresh on each call.
//
// `gh` is a portable git-host tool, not an agent vendor; it is kept out of
// internal/core, which receives observations as plain values.
package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// PR is one pull request as observed.
type PR struct {
	Number int    `json:"number"`
	Branch string `json:"branch"`
	// State is draft, open (ready for review), merged or closed.
	State string `json:"state"`
	// Checks summarizes CI: success, failure, pending or none.
	Checks string `json:"checks"`
	Title  string `json:"title"`
	URL    string `json:"url"`
}

// PR states.
const (
	StateDraft  = "draft"
	StateOpen   = "open"
	StateMerged = "merged"
	StateClosed = "closed"
)

// Check summaries.
const (
	ChecksSuccess = "success"
	ChecksFailure = "failure"
	ChecksPending = "pending"
	ChecksNone    = "none"
)

// Observer lists pull requests. Tests use a fake.
type Observer interface {
	PullRequests(ctx context.Context) ([]PR, error)
}

// ErrUnavailable means the observer cannot observe at all (for example `gh`
// is not installed). Callers treat observed state as unknown.
var ErrUnavailable = errors.New("observer unavailable")

// ErrTruncated is returned with the pull requests when the observer hit its
// limit, so older ones may be missing (issue #15). The list returned with
// it is usable.
var ErrTruncated = errors.New("pull request list truncated")

// None is an Observer that is always unavailable.
type None struct{}

// PullRequests always fails with ErrUnavailable.
func (None) PullRequests(context.Context) ([]PR, error) {
	return nil, fmt.Errorf("observation turned off: %w", ErrUnavailable)
}

// Static is an Observer that returns fixed PRs. It is meant for tests.
type Static []PR

// PullRequests returns the fixed list.
func (s Static) PullRequests(context.Context) ([]PR, error) { return slices.Clone(s), nil }

// GH observes pull requests with `gh pr list`, run in Dir.
type GH struct {
	Dir     string
	Limit   int           // default 200
	Timeout time.Duration // default 20s
}

// ghPR is the subset of `gh pr list --json` this package reads.
type ghPR struct {
	Number            int    `json:"number"`
	Title             string `json:"title"`
	HeadRefName       string `json:"headRefName"`
	IsDraft           bool   `json:"isDraft"`
	State             string `json:"state"`
	URL               string `json:"url"`
	StatusCheckRollup []struct {
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		State      string `json:"state"`
	} `json:"statusCheckRollup"`
}

// PullRequests runs `gh pr list --state all --json …`. When it returns
// Limit pull requests, the error wraps ErrTruncated and the list is
// returned with it.
func (g GH) PullRequests(ctx context.Context) ([]PR, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return nil, fmt.Errorf("gh not found: %w", ErrUnavailable)
	}
	limit, timeout := g.Limit, g.Timeout
	if limit == 0 {
		limit = 200
	}
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "pr", "list", "--state", "all", "--limit", fmt.Sprint(limit),
		"--json", "number,title,headRefName,isDraft,state,url,statusCheckRollup")
	cmd.Dir = g.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh pr list: %v: %s: %w", err, strings.TrimSpace(stderr.String()), ErrUnavailable)
	}
	prs, err := ParseGH(stdout.Bytes())
	if err == nil && len(prs) >= limit {
		return prs, fmt.Errorf("gh pr list returned its limit of %d; older pull requests may be missing: %w", limit, ErrTruncated)
	}
	return prs, err
}

// ParseGH converts `gh pr list --json number,title,headRefName,isDraft,
// state,url,statusCheckRollup` output into PRs.
func ParseGH(b []byte) ([]PR, error) {
	var raw []ghPR
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("gh pr list: decode: %w", err)
	}
	out := make([]PR, 0, len(raw))
	for _, r := range raw {
		p := PR{Number: r.Number, Branch: r.HeadRefName, Title: r.Title, URL: r.URL, Checks: ChecksNone}
		switch strings.ToUpper(r.State) {
		case "MERGED":
			p.State = StateMerged
		case "CLOSED":
			p.State = StateClosed
		default:
			p.State = StateOpen
			if r.IsDraft {
				p.State = StateDraft
			}
		}
		var results []string
		for _, c := range r.StatusCheckRollup {
			v := c.Conclusion
			if v == "" {
				v = c.State
			}
			if v == "" {
				v = c.Status
			}
			results = append(results, strings.ToUpper(v))
		}
		p.Checks = summarize(results)
		out = append(out, p)
	}
	return out, nil
}

func summarize(results []string) string {
	if len(results) == 0 {
		return ChecksNone
	}
	pending := false
	for _, r := range results {
		switch r {
		case "SUCCESS", "NEUTRAL", "SKIPPED":
		case "FAILURE", "ERROR", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
			return ChecksFailure
		default:
			pending = true
		}
	}
	if pending {
		return ChecksPending
	}
	return ChecksSuccess
}

// ByBranch picks one PR per head branch: an open or draft PR first, then a
// merged one, then a closed one; the highest number wins a tie.
func ByBranch(prs []PR) map[string]PR {
	rank := map[string]int{StateOpen: 3, StateDraft: 3, StateMerged: 2, StateClosed: 1}
	out := map[string]PR{}
	for _, p := range prs {
		cur, ok := out[p.Branch]
		if !ok || rank[p.State] > rank[cur.State] || (rank[p.State] == rank[cur.State] && p.Number > cur.Number) {
			out[p.Branch] = p
		}
	}
	return out
}

// How a pull request is linked to a task (GitHub #39), in order of
// precedence.
const (
	// LinkBranch: the PR's head branch is the task's recorded branch.
	LinkBranch = "branch"
	// LinkStack: the head branch is the recorded branch plus "-<n>", a
	// stacked branch (build/t-x-2, build/t-x-3).
	LinkStack = "stack"
	// LinkTitle: the PR title starts with "<task id>:" (or an alias).
	LinkTitle = "title"
)

var linkOrder = map[string]int{LinkBranch: 0, LinkStack: 1, LinkTitle: 2}

// Linked is the pull request that stands for a task, and the rule that
// linked it.
type Linked struct {
	PR
	By string `json:"linked_by"`
}

// LinkTask picks the pull request that stands for a task among prs. A PR
// matches by the first rule that applies (LinkBranch, LinkStack, then
// LinkTitle; an empty branch matches no branch rule). Among matches, an
// open or draft PR wins over a merged one, and a merged one over a closed
// one, so a task with any PR still open is not done; then the earlier
// rule wins, then the higher number.
func LinkTask(prs []PR, branch string, ids ...string) (Linked, bool) {
	var best Linked
	found := false
	rank := map[string]int{StateOpen: 3, StateDraft: 3, StateMerged: 2, StateClosed: 1}
	for _, p := range prs {
		by := linkRule(p, branch, ids)
		if by == "" {
			continue
		}
		c := Linked{PR: p, By: by}
		switch {
		case !found:
		case rank[c.State] != rank[best.State]:
			if rank[c.State] < rank[best.State] {
				continue
			}
		case linkOrder[c.By] != linkOrder[best.By]:
			if linkOrder[c.By] > linkOrder[best.By] {
				continue
			}
		case c.Number < best.Number:
			continue
		}
		best, found = c, true
	}
	return best, found
}

func linkRule(p PR, branch string, ids []string) string {
	if branch != "" {
		if p.Branch == branch {
			return LinkBranch
		}
		if n, ok := strings.CutPrefix(p.Branch, branch+"-"); ok && n != "" && strings.Trim(n, "0123456789") == "" {
			return LinkStack
		}
	}
	for _, id := range ids {
		if id != "" && strings.HasPrefix(p.Title, id+":") {
			return LinkTitle
		}
	}
	return ""
}
