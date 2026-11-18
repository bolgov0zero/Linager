//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

// sysReboot restarts the host a moment after answering. It is always a dangerous action.
func sysReboot(_ context.Context, _ json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	go func() {
		time.Sleep(2 * time.Second) // let the result reach the server first
		_ = exec.Command("shutdown", "/r", "/t", "0", "/d", "p:4:1", "/c", "Restart requested from Linager").Run()
	}()
	return map[string]bool{"ok": true}, nil
}
