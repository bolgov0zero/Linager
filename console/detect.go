// Package console implements the interactive menu shown by `linager` without arguments: it finds out which role
// (server, agent or both) this host has, shows its state and starts the usual subcommands.
package console

import (
	"os"
	"path/filepath"
)

// Role is what Linager does on this host.
type Role string

// Roles.
const (
	RoleServer Role = "server"
	RoleAgent  Role = "agent"
)

// Layout holds the places where the installer puts things; tests point it to a temporary directory.
type Layout struct {
	ServerConfig string
	ServerUnit   string
	AgentDir     string
	AgentConfig  string
	AgentUnit    string
}

// DefaultLayout is what `linager install` creates.
func DefaultLayout() Layout {
	return Layout{
		ServerConfig: "/etc/linager/server.yaml",
		ServerUnit:   "/etc/systemd/system/linager-server.service",
		AgentDir:     "/var/lib/linager-agent",
		AgentConfig:  "/etc/linager/agent.yaml",
		AgentUnit:    "/etc/systemd/system/linager-agent.service",
	}
}

// exists is true only when the file can be seen: a directory closed to a non-root user counts as "no".
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Detect lists the roles found on this host: the server in front, then the agent. An empty list means that
// Linager is not installed. A leftover unit or config is enough, so a half-removed installation stays reachable.
func (l Layout) Detect() []Role {
	var out []Role
	if exists(l.ServerConfig) || exists(l.ServerUnit) {
		out = append(out, RoleServer)
	}
	if exists(filepath.Join(l.AgentDir, "state.json")) || exists(l.AgentUnit) {
		out = append(out, RoleAgent)
	}
	return out
}
