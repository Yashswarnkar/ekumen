package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Config is .ekumen/config.yaml: optional, committed and edited by the
// project's owner. Every field is optional.
//
//	project: ekumen                       # the project's name (ProjectName)
//	lead: Yashswarnkar                    # the lead's handle (Lead)
//	agents:
//	  default: <agent>                    # which entry `task start` uses
//	  <agent>: {command: "<path or name>"}
//	setup: ["go build -o bin/ekumen ./cmd/ekumen"]   # run in each new worktree
//
// Agent names and commands live only here (and in DefaultAgents, which
// `ekumen init` writes): the core never names an agent tool (spine §5
// rule 12).
type Config struct {
	Project string
	Lead    string
	// DefaultAgent is agents.default ("" when unset).
	DefaultAgent string
	// Agents maps an agent name to the command that starts a session.
	Agents map[string]Agent
	// Setup are shell commands run inside each new worktree.
	Setup []string
}

// Agent is one agents.<name> entry.
type Agent struct {
	Command string `yaml:"command"`
}

// DefaultAgentName and DefaultAgentCommand are what `ekumen init` writes
// into a new project's config.yaml. They are defaults the owner edits, not
// something the core depends on: `task start` reads only the config file.
const (
	DefaultAgentName    = "claude-code"
	DefaultAgentCommand = "claude"
)

// AgentNames lists the configured agent names, sorted.
func (c Config) AgentNames() []string {
	out := make([]string, 0, len(c.Agents))
	for n := range c.Agents {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ReadConfig reads <ekumenDir>/config.yaml. A missing file is an empty
// Config and no error; a file that is not valid YAML, or has fields of the
// wrong shape, is an error naming the field.
func ReadConfig(ekumenDir string) (Config, error) {
	var c Config
	b, err := os.ReadFile(filepath.Join(ekumenDir, ConfigFile))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	var raw struct {
		Project string               `yaml:"project"`
		Lead    string               `yaml:"lead"`
		Agents  map[string]yaml.Node `yaml:"agents"`
		Setup   []string             `yaml:"setup"`
	}
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return c, fmt.Errorf("%s/%s: %v", Dir, ConfigFile, err)
	}
	c.Project, c.Lead, c.Setup = raw.Project, raw.Lead, raw.Setup
	for name, node := range raw.Agents {
		if name == "default" {
			if err := node.Decode(&c.DefaultAgent); err != nil {
				return c, fmt.Errorf("%s/%s: agents.default: want an agent name", Dir, ConfigFile)
			}
			continue
		}
		var a Agent
		if err := node.Decode(&a); err != nil || strings.TrimSpace(a.Command) == "" {
			return c, fmt.Errorf("%s/%s: agents.%s: want {command: \"…\"}", Dir, ConfigFile, name)
		}
		if c.Agents == nil {
			c.Agents = map[string]Agent{}
		}
		c.Agents[name] = a
	}
	return c, nil
}

// ConfigYAML is the config.yaml `ekumen init` writes.
func ConfigYAML(name, lead string) []byte {
	var b strings.Builder
	b.WriteString("# Ekumen project settings. Committed; edited by the project's owner.\n")
	fmt.Fprintf(&b, "project: %s\n", yamlScalar(name))
	if lead == "" {
		b.WriteString("lead: \"\"   # your handle, e.g. the one you use with --as lead:<handle>\n")
	} else {
		fmt.Fprintf(&b, "lead: %s\n", yamlScalar(lead))
	}
	b.WriteString("\n# The command that starts an agent session (`ekumen task start`).\n")
	b.WriteString("# Use an absolute path to avoid shell aliases.\n")
	b.WriteString("agents:\n")
	fmt.Fprintf(&b, "  default: %s\n", DefaultAgentName)
	fmt.Fprintf(&b, "  %s: {command: %s}\n", DefaultAgentName, yamlScalar(DefaultAgentCommand))
	b.WriteString("\n# Shell commands run inside each new worktree, e.g. a build step.\n")
	b.WriteString("setup: []\n")
	return []byte(b.String())
}

func yamlScalar(s string) string {
	b, err := yaml.Marshal(s)
	if err != nil {
		return `""`
	}
	return strings.TrimSpace(string(b))
}
