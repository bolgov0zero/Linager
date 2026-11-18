//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// eventScript follows the event logs: it prints the last lines, then polls for new events every two seconds.
// The values come in environment variables, so nothing from the panel is ever part of the script text.
const eventScript = `
$ErrorActionPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$logs = @($env:LINAGER_LOGS -split '\|' | Where-Object { $_ })
$lines = [int]$env:LINAGER_LINES
$maxLevel = [int]$env:LINAGER_LEVEL
$f = @{ LogName = $logs }
if ($env:LINAGER_PROVIDER) { $f.ProviderName = $env:LINAGER_PROVIDER }
if ($maxLevel -gt 0) { $f.Level = 1..$maxLevel }
function Show($e) {
  $m = ''
  if ($e.Message) { $m = ($e.Message -replace '\s+', ' ').Trim() }
  $l = switch ($e.Level) { 1 { 'CRIT' } 2 { 'ERR' } 3 { 'WARN' } 5 { 'DEBUG' } default { 'INFO' } }
  '{0} {1} {2}[{3}] {4}: {5}' -f $e.TimeCreated.ToString('yyyy-MM-ddTHH:mm:ss'), $e.LogName, $e.ProviderName, $e.Id, $l, $m
}
$seen = @{}
$since = (Get-Date).AddSeconds(-1)
$first = @(Get-WinEvent -FilterHashtable $f -MaxEvents $lines)
if ($first.Count -gt 0) {
  $first | Sort-Object TimeCreated | ForEach-Object { Show $_; $seen["$($_.LogName)/$($_.RecordId)"] = 1 }
  $since = ($first | Measure-Object TimeCreated -Maximum).Maximum.AddSeconds(-1)
}
while ($true) {
  Start-Sleep -Seconds 2
  $g = $f.Clone(); $g.StartTime = $since
  foreach ($e in @(Get-WinEvent -FilterHashtable $g | Sort-Object TimeCreated)) {
    $k = "$($e.LogName)/$($e.RecordId)"
    if (-not $seen.ContainsKey($k)) {
      $seen[$k] = 1
      Show $e
      if ($e.TimeCreated.AddSeconds(-1) -gt $since) { $since = $e.TimeCreated.AddSeconds(-1) }
    }
  }
  if ($seen.Count -gt 5000) { $seen.Clear() }
}
`

// powerShellPath finds powershell.exe by its fixed place first.
func powerShellPath() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		p := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if _, err := os.Stat(p); err == nil { //nolint:gosec // a fixed place under the system folder
			return p
		}
	}
	if p, err := exec.LookPath("powershell.exe"); err == nil {
		return p
	}
	return "powershell.exe"
}

// logsStream follows the Windows event logs.
func logsStream(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p eventParams
	_ = json.Unmarshal(raw, &p)
	n := p.Lines
	if n <= 0 || n > 1000 {
		n = 200
	}
	logs, provider, level := eventQuery(p)
	cmd := exec.CommandContext(ctx, powerShellPath(), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", //nolint:gosec // fixed binary, the script is a constant
		"-EncodedCommand", encodePowerShell(eventScript))
	cmd.Env = append(os.Environ(),
		"LINAGER_LOGS="+strings.Join(logs, "|"), "LINAGER_PROVIDER="+provider,
		"LINAGER_LEVEL="+strconv.Itoa(level), "LINAGER_LINES="+strconv.Itoa(n))
	return pipeCommand(cmd, sio)
}
