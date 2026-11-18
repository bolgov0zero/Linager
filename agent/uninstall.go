//go:build !windows

package agent

import (
	"os"
	"os/exec"
	"path/filepath"
)

// systemd paths and names used for self-removal.
const (
	serviceName = "linager-agent.service"
	unitPath    = "/etc/systemd/system/" + serviceName
	configPath  = "/etc/linager/agent.yaml"
)

// Uninstall removes the agent identity and its systemd service from this host.
// With purge it also deletes the data directory (keys) and the agent configuration.
// The binary itself is left in place because it may also serve the server role.
func Uninstall(purge bool, dataDir string) error {
	return uninstall(purge, dataDir, unitPath, configPath, func(args ...string) error {
		return exec.Command("systemctl", args...).Run() //nolint:gosec // fixed command, arguments are constants
	})
}

func uninstall(purge bool, dataDir, unit, config string, systemctl func(args ...string) error) error {
	// Always drop the identity so a stray restart cannot reconnect.
	for _, f := range []string{certFile, stateFile} {
		_ = os.Remove(filepath.Join(dataDir, f))
	}
	if purge {
		_ = os.RemoveAll(dataDir)
		_ = os.Remove(config)
	}
	_ = systemctl("disable", serviceName)
	_ = os.Remove(unit)
	_ = systemctl("daemon-reload")
	// --no-block: this very process is the service, stop must not wait for itself.
	// A missing unit (already removed) is not an error.
	_ = systemctl("stop", "--no-block", serviceName)
	return nil
}
