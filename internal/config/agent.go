package config

import (
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

// AgentModules lists all agent modules in display order.
var AgentModules = []string{"metrics", "terminal", "services", "apt", "processes", "logs", "files", "network", "firewall", "docker"}

// Agent is /etc/linager/agent.yaml. Every module can be switched off.
type Agent struct {
	Modules map[string]bool `yaml:"modules"`
}

// DefaultAgent enables every module.
func DefaultAgent() *Agent {
	m := map[string]bool{}
	for _, n := range AgentModules {
		m[n] = true
	}
	return &Agent{Modules: m}
}

// LoadAgent reads the agent config; a missing file yields the defaults.
func LoadAgent(path string) (*Agent, error) {
	c := DefaultAgent()
	b, err := os.ReadFile(path) //nolint:gosec // operator-supplied path
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	var in Agent
	if err := yaml.Unmarshal(b, &in); err != nil {
		return nil, err
	}
	for k, v := range in.Modules { // unspecified modules keep their default (enabled)
		c.Modules[k] = v
	}
	return c, nil
}

// Enabled reports whether a module is on.
func (a *Agent) Enabled(name string) bool { return a.Modules[name] }

// List returns the enabled modules in display order.
func (a *Agent) List() []string {
	var out []string
	for _, n := range AgentModules {
		if a.Modules[n] {
			out = append(out, n)
		}
	}
	return out
}
