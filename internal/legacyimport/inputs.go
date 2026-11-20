package legacyimport

import (
	"errors"
	"io/fs"
	"path"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The hand-written inputs, as read. Field names follow the files.

type registry struct {
	Project   string               `yaml:"project"`
	Lead      string               `yaml:"lead"`
	Tasks     map[string]yaml.Node `yaml:"tasks"`
	External  yaml.Node            `yaml:"external"`
	Contracts map[string]yaml.Node `yaml:"contracts"`
}

// legacyTask is one registry task.
type legacyTask struct {
	OldID      string    `yaml:"-"`
	ID         string    `yaml:"-"`
	Title      string    `yaml:"title"`
	Kind       string    `yaml:"kind"`
	Brief      *string   `yaml:"brief"`
	Approved   *string   `yaml:"approved"`
	BlockedBy  []string  `yaml:"blocked_by"`
	Repo       string    `yaml:"repo"`
	BaseBranch string    `yaml:"base_branch"`
	Branch     string    `yaml:"branch"`
	Worktree   string    `yaml:"worktree"`
	Session    string    `yaml:"session"`
	Status     string    `yaml:"status"`
	TierGrants []string  `yaml:"tier_grants"`
	Extra      yamlExtra `yaml:"-"`
}

var taskKeys = []string{"title", "kind", "brief", "approved", "blocked_by", "repo", "base_branch",
	"branch", "worktree", "session", "status", "tier_grants"}

type yamlExtra = map[string]any

// legacyContract is a contract: front matter of contracts/<id>.md, or a
// registry entry for a planned contract with no file yet.
type legacyContract struct {
	ID        string   `yaml:"id"`
	Aliases   []string `yaml:"aliases"`
	Title     string   `yaml:"title"`
	Status    string   `yaml:"status"`
	FrozenOn  string   `yaml:"frozen_on"`
	Version   int      `yaml:"version"`
	Path      *string  `yaml:"path"`
	BlockedBy []string `yaml:"blocked_by"`
	// Set by the reader.
	File       string // path relative to .ekumen/, "" when there is no file
	TextSHA256 string
	// RegistryStatus is the status the registry gives, if listed there.
	RegistryStatus string
}

// legacyDecision is one decisions/<id>.yaml file.
type legacyDecision struct {
	File              string        `yaml:"-"`
	ID                string        `yaml:"id"`
	Aliases           []string      `yaml:"aliases"`
	Title             string        `yaml:"title"`
	Tier              int           `yaml:"tier"`
	RaisedBy          yaml.Node     `yaml:"raised_by"`
	Question          string        `yaml:"question"`
	Options           []legacyOpt   `yaml:"options"`
	Recommendation    *string       `yaml:"recommendation"`
	DefaultIfNoAnswer *string       `yaml:"default_if_no_answer"`
	Deadline          *string       `yaml:"deadline"`
	Blocks            *[]string     `yaml:"blocks"`
	ContractImpact    *[]string     `yaml:"contract_impact"`
	Context           legacyContext `yaml:"context"`
	Status            string        `yaml:"status"`
	Answer            string        `yaml:"answer"`
	AnswerNote        string        `yaml:"answer_note"`
	DecidedBy         string        `yaml:"decided_by"`
	DecidedOn         string        `yaml:"decided_on"`
	SupersededBy      string        `yaml:"superseded_by"`
	AppliedOn         string        `yaml:"applied_on"`
	Applied           yaml.Node     `yaml:"applied"`
	OrchestratorNote  string        `yaml:"orchestrator_note"`
	Questions         yaml.Node     `yaml:"questions"`
	Extra             yamlExtra     `yaml:"-"`
}

var decisionKeys = []string{"id", "aliases", "title", "tier", "raised_by", "question", "options",
	"recommendation", "default_if_no_answer", "deadline", "blocks", "contract_impact", "context",
	"status", "answer", "answer_note", "decided_by", "decided_on", "superseded_by", "applied_on",
	"applied", "orchestrator_note", "questions"}

type legacyOpt struct {
	ID       string `yaml:"id"`
	Tradeoff string `yaml:"tradeoff"`
}

type legacyContext struct {
	Summary        string   `yaml:"summary"`
	Evidence       []string `yaml:"evidence"`
	PriorDecisions []string `yaml:"prior_decisions"`
}

// Inputs are all hand-written records under one .ekumen/ folder.
type Inputs struct {
	Project   string
	Lead      string
	Tasks     []*legacyTask     // sorted by new id
	Decisions []*legacyDecision // sorted by file name
	Contracts []*legacyContract // sorted by id
	// Externals is the registry's external section, which must be empty:
	// no hand-written external dependencies exist to import.
	Externals any
	fsys      fs.FS
}

// readInputs reads the registry, decisions and contracts. Briefs are read
// per task later, by readBrief.
func readInputs(fsys fs.FS) (*Inputs, error) {
	in := &Inputs{fsys: fsys}
	raw, err := fs.ReadFile(fsys, "registry.yaml")
	if err != nil {
		return nil, errorf("read registry: %w", err)
	}
	var reg registry
	if err := yaml.Unmarshal(raw, &reg); err != nil {
		return nil, errorf("registry.yaml: %w", err)
	}
	in.Project, in.Lead = reg.Project, reg.Lead
	in.Externals = nodeValue(&reg.External)

	ids := newAliases()
	for old, node := range reg.Tasks {
		t := &legacyTask{OldID: old}
		if err := node.Decode(t); err != nil {
			return nil, errorf("registry.yaml: task %s: %w", old, err)
		}
		t.Extra = extraKeys(&node, taskKeys)
		t.ID, _ = ids.newID(old)
		in.Tasks = append(in.Tasks, t)
	}
	slices.SortFunc(in.Tasks, func(a, b *legacyTask) int { return strings.Compare(a.ID, b.ID) })

	// Contracts: files first, then registry entries without a file.
	files, err := fs.Glob(fsys, "contracts/*.md")
	if err != nil {
		return nil, err
	}
	byID := map[string]*legacyContract{}
	for _, f := range files {
		text, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		fm, _, ok := frontMatter(text)
		if !ok {
			return nil, errorf("%s: no front matter", f)
		}
		c := &legacyContract{}
		if err := yaml.Unmarshal(fm, c); err != nil {
			return nil, errorf("%s: front matter: %w", f, err)
		}
		c.File, c.TextSHA256 = f, sha256Hex(text)
		byID[c.ID] = c
	}
	for id, node := range reg.Contracts {
		var rc legacyContract
		if err := node.Decode(&rc); err != nil {
			return nil, errorf("registry.yaml: contract %s: %w", id, err)
		}
		c, ok := byID[id]
		if !ok {
			rc.ID = id
			c = &rc
			byID[id] = c
		} else {
			c.BlockedBy = rc.BlockedBy
			if len(c.Aliases) == 0 {
				c.Aliases = rc.Aliases
			}
		}
		c.RegistryStatus = rc.Status
		if c.Status == "" {
			c.Status = rc.Status
		}
	}
	for _, c := range byID {
		in.Contracts = append(in.Contracts, c)
	}
	slices.SortFunc(in.Contracts, func(a, b *legacyContract) int { return strings.Compare(a.ID, b.ID) })

	files, err = fs.Glob(fsys, "decisions/*.yaml")
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	for _, f := range files {
		raw, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		var node yaml.Node
		if err := yaml.Unmarshal(raw, &node); err != nil {
			return nil, errorf("%s: %w", f, err)
		}
		d := &legacyDecision{File: f}
		if err := node.Decode(d); err != nil {
			return nil, errorf("%s: %w", f, err)
		}
		d.Extra = extraKeys(&node, decisionKeys)
		in.Decisions = append(in.Decisions, d)
	}
	return in, nil
}

// readFileIfExists returns the file's bytes, or nil and no error when it
// does not exist.
func readFileIfExists(fsys fs.FS, name string) ([]byte, error) {
	b, err := fs.ReadFile(fsys, name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// listEntries returns the .md file names in dir, in entry order.
func listEntries(fsys fs.FS, dir string) ([]string, error) {
	des, err := fs.ReadDir(fsys, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, de := range des {
		if de.IsDir() || path.Ext(de.Name()) != ".md" {
			continue
		}
		out = append(out, de.Name())
	}
	slices.SortFunc(out, entryLess)
	return out, nil
}
