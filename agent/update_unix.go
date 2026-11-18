//go:build !windows

package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

func armRollback(dir, exe string) error {
	args := []string{"--unit=linager-agent-rollback", "--on-active=" + fmt.Sprint(int(RollbackDelay.Seconds())), "--description=Linager agent update rollback",
		prevPath(dir), "agent", "rollback-update", "--data-dir", dir, "--binary", exe}
	_ = exec.Command("systemctl", "stop", "linager-agent-rollback.timer", "linager-agent-rollback.service").Run()
	_ = exec.Command("systemctl", "reset-failed", "linager-agent-rollback.service").Run()
	if out, err := exec.Command("systemd-run", args...).CombinedOutput(); err != nil { //nolint:gosec // fixed binary, arguments are paths we built
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// stopRollbackTimer cancels the rollback timer of an update.
func stopRollbackTimer() {
	_ = exec.Command("systemctl", "stop", "linager-agent-rollback.timer").Run()
}

// restartService restarts the agent service.
func restartService() error {
	return exec.Command("systemctl", "restart", "linager-agent").Run()
}

// serviceState is the state of the agent service as the init system reports it.
func serviceState() string {
	out, _ := exec.Command("systemctl", "is-active", "linager-agent").Output()
	if s := strings.TrimSpace(string(out)); s != "" {
		return s
	}
	return "unknown"
}
