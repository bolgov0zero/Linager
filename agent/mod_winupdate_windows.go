//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

// WinUpdate is an update that Windows Update offers.
type WinUpdate struct {
	ID         string `json:"id"` // the update id (a GUID)
	Title      string `json:"title"`
	KB         string `json:"kb"`
	Size       int64  `json:"size"`
	Severity   string `json:"severity"`
	Security   bool   `json:"security"`
	Downloaded bool   `json:"downloaded"`
	NeedReboot bool   `json:"need_reboot"`
}

// updateSearch is the search of the Windows Update Agent: the updates that are not installed and not hidden.
const updateSearch = `
$session = New-Object -ComObject Microsoft.Update.Session
$searcher = $session.CreateUpdateSearcher()
$result = $searcher.Search("IsInstalled=0 and IsHidden=0 and Type='Software'")
$list = @()
foreach ($u in $result.Updates) {
  $kb = ''; if ($u.KBArticleIDs.Count -gt 0) { $kb = 'KB' + $u.KBArticleIDs.Item(0) }
  $sec = $false; foreach ($c in $u.Categories) { if ($c.Name -eq 'Security Updates' -or $c.Name -eq 'Critical Updates') { $sec = $true } }
  $list += [pscustomobject]@{ id = $u.Identity.UpdateID; title = $u.Title; kb = $kb; size = [int64]$u.MaxDownloadSize; severity = [string]$u.MsrcSeverity
    security = $sec; downloaded = [bool]$u.IsDownloaded; need_reboot = ($u.InstallationBehavior.RebootBehavior -ne 0) }
}
`

const updateListScript = updateSearch + `ConvertTo-Json -InputObject @($list) -Depth 4 -Compress`

var updateIDRe = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

var (
	updMu   sync.Mutex
	updSlow struct {
		total, security int
		ok              bool
	}
)

// searchUpdates asks Windows Update. The search can take a minute and fails when the update service is unreachable.
func searchUpdates(ctx context.Context) ([]WinUpdate, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := runPS(cctx, updateListScript, nil)
	if err != nil {
		return nil, errors.New("Windows Update did not answer: " + err.Error())
	}
	var list []WinUpdate
	if len(out) > 0 && json.Unmarshal(out, &list) != nil {
		return nil, errors.New("cannot read the answer of Windows Update")
	}
	if list == nil {
		list = []WinUpdate{}
	}
	return list, nil
}

func winUpdateListCmd(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	list, err := searchUpdates(ctx)
	if err != nil {
		return nil, err
	}
	sec := 0
	for _, u := range list {
		if u.Security {
			sec++
		}
	}
	updMu.Lock()
	updSlow.total, updSlow.security, updSlow.ok = len(list), sec, true
	updMu.Unlock()
	return map[string]any{"updates": list, "reboot_pending": rebootPending()}, nil
}

// updateCounts returns the numbers of the last search (the dashboard shows them); -1 when none was made yet.
func updateCounts() (total, security int) {
	updMu.Lock()
	defer updMu.Unlock()
	if !updSlow.ok {
		return -1, 0
	}
	return updSlow.total, updSlow.security
}

// refreshUpdateCounts searches in the background; it runs once an hour and after an installation.
func refreshUpdateCounts(ctx context.Context) {
	if list, err := searchUpdates(ctx); err == nil {
		sec := 0
		for _, u := range list {
			if u.Security {
				sec++
			}
		}
		updMu.Lock()
		updSlow.total, updSlow.security, updSlow.ok = len(list), sec, true
		updMu.Unlock()
	}
}

// updateInstallScript downloads and installs the updates whose ids are in $env:LINAGER_IDS (all when the list is empty)
// and writes a line for every step.
const updateInstallScript = updateSearch + `
$want = @(($env:LINAGER_IDS | ConvertFrom-Json) | Where-Object { $_ })
$pick = New-Object -ComObject Microsoft.Update.UpdateColl
foreach ($u in $result.Updates) { if ($want.Count -eq 0 -or $want -contains $u.Identity.UpdateID) { if (-not $u.EulaAccepted) { $u.AcceptEula() }; [void]$pick.Add($u) } }
if ($pick.Count -eq 0) { 'Nothing to install.'; exit 0 }
"Found $($pick.Count) update(s)."
foreach ($u in $pick) { '  ' + $u.Title }
$dl = $session.CreateUpdateDownloader(); $dl.Updates = $pick
'Downloading...'
$r = $dl.Download()
"Download finished (code $($r.ResultCode))."
$ready = New-Object -ComObject Microsoft.Update.UpdateColl
foreach ($u in $pick) { if ($u.IsDownloaded) { [void]$ready.Add($u) } }
if ($ready.Count -eq 0) { 'Nothing was downloaded.'; exit 1 }
$inst = $session.CreateUpdateInstaller(); $inst.Updates = $ready
'Installing...'
$ir = $inst.Install()
for ($i = 0; $i -lt $ready.Count; $i++) { $one = $ir.GetUpdateResult($i); '  ' + $ready.Item($i).Title + ' -> result ' + $one.ResultCode + ' (' + ('{0:X}' -f $one.HResult) + ')' }
"Installation finished (code $($ir.ResultCode))."
if ($ir.RebootRequired) { 'A restart is required.' } else { 'No restart is required.' }
if ($ir.ResultCode -ge 4) { exit 1 }
`

// winUpdateStream installs updates and streams the log.
func winUpdateStream(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p struct {
		IDs []string `json:"ids"`
	}
	_ = json.Unmarshal(raw, &p)
	if len(p.IDs) > 200 {
		return errors.New("too many updates")
	}
	for _, id := range p.IDs {
		if !updateIDRe.MatchString(id) {
			return errors.New("invalid update id")
		}
	}
	ids, _ := json.Marshal(p.IDs)
	cmd := exec.CommandContext(context.WithoutCancel(ctx), powerShellPath(), "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", //nolint:gosec // fixed binary, constant script text
		"-EncodedCommand", encodePowerShell(psPrelude+updateInstallScript))
	cmd.Env = append(psEnv(), "LINAGER_IDS="+string(ids))
	err := pipeCommand(cmd, sio)
	go refreshUpdateCounts(context.Background())
	return err
}
