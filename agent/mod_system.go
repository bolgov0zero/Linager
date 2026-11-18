//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

// sysReboot reboots the host a moment after answering. It is always a dangerous action.
func sysReboot(_ context.Context, _ json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	go func() {
		time.Sleep(2 * time.Second) // let the result reach the server first
		_ = exec.Command("systemctl", "reboot").Run()
	}()
	return map[string]bool{"ok": true}, nil
}
