package project

import (
	"bytes"
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"

	"github.com/Yashswarnkar/ekumen/internal/core"
)

// ContractFreeze is one ContractFrozen.
type ContractFreeze struct {
	Version    int    `json:"version"`
	TextSHA256 string `json:"text_sha256"`
	At         string `json:"at"`
}

// ContractVersion is one ContractVersioned.
type ContractVersion struct {
	Version    int    `json:"version"`
	ByDecision string `json:"by_decision"`
	TextSHA256 string `json:"text_sha256"`
	At         string `json:"at"`
}

// ContractDetail is the `contract` of `contract show`: the contract as
// derived from events, plus consumers and producers from its front matter.
type ContractDetail struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Aliases   []string          `json:"aliases"`
	Status    string            `json:"status"`
	Version   int               `json:"version"`
	FrozenOn  *string           `json:"frozen_on"`
	Path      *string           `json:"path"`
	Consumers []string          `json:"consumers"`
	Producers []string          `json:"producers"`
	BlockedBy []string          `json:"blocked_by"`
	Freezes   []ContractFreeze  `json:"freezes"`
	Versions  []ContractVersion `json:"versions"`
	Pending   *ContractVersion  `json:"pending"`
}

// ContractShow returns a drafted contract and its file's body (the text
// after the front matter). A contract with no path has an empty body; a
// path that cannot be read, or bad front matter, is an error.
func (s *Snapshot) ContractShow(id string) (ContractDetail, string, error) {
	c := s.State.Contracts[id]
	v := ContractDetail{
		ID: id, Title: c.Title, Aliases: list(c.Aliases), Status: c.Status, Version: c.Version,
		Path: ptr(s.RootPath(c.Path)), Consumers: []string{}, Producers: []string{},
		BlockedBy: s.resolveAll(c.BlockedBy), Freezes: []ContractFreeze{}, Versions: []ContractVersion{},
	}
	for _, f := range c.Freezes {
		at := s.atOf(f.Event)
		v.Freezes = append(v.Freezes, ContractFreeze{Version: f.Version, TextSHA256: f.TextSHA256, At: at})
		if f.Version == c.Version && f.TextSHA256 == c.TextSHA256 && v.FrozenOn == nil && len(at) >= 10 {
			v.FrozenOn = ptr(at[:10])
		}
	}
	for _, f := range c.Versions {
		v.Versions = append(v.Versions, s.contractVersion(f))
	}
	if c.Pending != nil {
		p := s.contractVersion(*c.Pending)
		v.Pending = &p
	}
	if c.Path == "" {
		return v, "", nil
	}
	p, err := s.ResolvePath(c.Path)
	if err != nil {
		return v, "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return v, "", fmt.Errorf("contract %s: %w", id, err)
	}
	fm, body := splitFrontMatter(b)
	var meta struct {
		Consumers []string `yaml:"consumers"`
		Producers []string `yaml:"producers"`
	}
	if len(fm) > 0 {
		if err := yaml.Unmarshal(fm, &meta); err != nil {
			return v, string(body), fmt.Errorf("contract %s: front matter: %w", id, err)
		}
	}
	v.Consumers, v.Producers = list(meta.Consumers), list(meta.Producers)
	return v, string(body), nil
}

func (s *Snapshot) contractVersion(f core.Freeze) ContractVersion {
	return ContractVersion{Version: f.Version, ByDecision: s.State.Resolve(f.ByDecision), TextSHA256: f.TextSHA256, At: s.atOf(f.Event)}
}

// splitFrontMatter splits "---\n<yaml>\n---\n<body>"; text without front
// matter is all body.
func splitFrontMatter(b []byte) (fm, body []byte) {
	if !bytes.HasPrefix(b, []byte("---\n")) && !bytes.HasPrefix(b, []byte("---\r\n")) {
		return nil, b
	}
	rest := b[bytes.IndexByte(b, '\n')+1:]
	for off := 0; off < len(rest); {
		end := bytes.IndexByte(rest[off:], '\n')
		line := rest[off:]
		if end >= 0 {
			line = rest[off : off+end]
		}
		if string(bytes.TrimRight(line, "\r")) == "---" {
			body := []byte{}
			if end >= 0 {
				body = bytes.TrimLeft(rest[off+end+1:], "\r\n")
			}
			return rest[:off], body
		}
		if end < 0 {
			break
		}
		off += end + 1
	}
	return nil, b
}
