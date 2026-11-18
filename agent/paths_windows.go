//go:build windows

package agent

import (
	"os"
	"path/filepath"
)

// Where the agent keeps its identity and its configuration.
var (
	// DefaultDataDir holds the keys and the state; only SYSTEM and the administrators can open it.
	DefaultDataDir = filepath.Join(programData(), "Linager", "agent")
	// DefaultConfigPath is the file with the enabled modules.
	DefaultConfigPath = filepath.Join(programData(), "Linager", "agent.yaml")
	// LogPath is the log of the service.
	LogPath = filepath.Join(programData(), "Linager", "agent.log")
)

func programData() string {
	if v := os.Getenv("ProgramData"); v != "" {
		return v
	}
	return `C:\ProgramData`
}
