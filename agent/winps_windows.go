//go:build windows

package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

// psPrelude is put in front of every script: errors stop the script, the output is UTF-8, no progress bars.
const psPrelude = `$ErrorActionPreference = 'Stop'; $ProgressPreference = 'SilentlyContinue'; [Console]::OutputEncoding = [System.Text.Encoding]::UTF8; `

// runPS runs a PowerShell script and returns what it wrote. Values from the panel reach the script only through the
// environment (the script reads $env:LINAGER_...), never as part of its text.
func runPS(ctx context.Context, script string, env map[string]string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, powerShellPath(), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", //nolint:gosec // fixed binary, constant script text
		"-EncodedCommand", encodePowerShell(psPrelude+script))
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, "LINAGER_"+k+"="+v)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexAny(msg, "\r\n"); i > 0 {
			msg = msg[:i]
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return bytes.TrimPrefix(bytes.TrimSpace(out), []byte("\xef\xbb\xbf")), nil
}
