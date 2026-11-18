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
	// An error is written as plain text on stderr: with a redirected stderr PowerShell would write CLIXML.
	wrapped := psPrelude + "try {\n" + script + "\n} catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }"
	cmd := exec.CommandContext(ctx, powerShellPath(), "-NoLogo", "-NoProfile", "-NonInteractive", "-OutputFormat", "Text", "-ExecutionPolicy", "Bypass", //nolint:gosec // fixed binary, constant script text
		"-EncodedCommand", encodePowerShell(wrapped))
	cmd.Env = psEnv()
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

// psEnv is the environment of a child PowerShell: that of the agent without PSModulePath. A parent that is another
// PowerShell (PowerShell 7 started by hand, a CI shell) hands down its own module path, which Windows PowerShell
// cannot load its modules from; without the variable it computes the right one.
func psEnv() []string {
	var out []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(e), "PSMODULEPATH=") {
			out = append(out, e)
		}
	}
	return out
}
