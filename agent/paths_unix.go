//go:build !windows

package agent

// Where the agent keeps its identity and its configuration.
const (
	DefaultDataDir    = "/var/lib/linager-agent"
	DefaultConfigPath = "/etc/linager/agent.yaml"
)

// secureDir limits a directory to its owner. On Linux the 0700 mode of MkdirAll already does it.
func secureDir(string) error { return nil }
