//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// netWindowsRollback is how long a network change waits for the confirmation before it is rolled back.
const netRollbackDelay = 30 * time.Second

const netChangeTask = "LinagerNetRollback"

// WinAdapter is a network adapter of this computer.
type WinAdapter struct {
	Index       int      `json:"index"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	MAC         string   `json:"mac"`
	Up          bool     `json:"up"`
	Virtual     bool     `json:"virtual"`
	MTU         int      `json:"mtu"`
	SpeedMbps   int      `json:"speed_mbps"`
	DHCP        bool     `json:"dhcp"`
	IPv4        []string `json:"ipv4"` // address/prefix
	Gateway     string   `json:"gateway"`
	DNS         []string `json:"dns"`
}

const netStateScript = `
$list = @()
foreach ($a in @(Get-NetAdapter -IncludeHidden:$false)) {
  $ip = Get-NetIPInterface -InterfaceIndex $a.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue
  $addrs = @(Get-NetIPAddress -InterfaceIndex $a.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | ForEach-Object { "$($_.IPAddress)/$($_.PrefixLength)" })
  $gw = @(Get-NetRoute -InterfaceIndex $a.ifIndex -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | ForEach-Object { $_.NextHop }) | Select-Object -First 1
  $dns = @(Get-DnsClientServerAddress -InterfaceIndex $a.ifIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue | ForEach-Object { $_.ServerAddresses })
  $speed = 0; if ($a.Speed -gt 0 -and $a.Speed -lt 1e15) { $speed = [int]($a.Speed / 1000000) }
  $list += [pscustomobject]@{ index = [int]$a.ifIndex; name = $a.Name; description = $a.InterfaceDescription; mac = ([string]$a.MacAddress).Replace('-', ':').ToLower(); up = ($a.Status -eq 'Up')
    virtual = (-not $a.HardwareInterface); mtu = [int]$a.MtuSize; speed_mbps = $speed; dhcp = ($ip -ne $null -and $ip.Dhcp -eq 'Enabled'); ipv4 = $addrs; gateway = [string]$gw; dns = $dns }
}
ConvertTo-Json -InputObject @($list) -Depth 4 -Compress
`

func readAdapters(ctx context.Context) ([]WinAdapter, error) {
	out, err := runPS(ctx, netStateScript, nil)
	if err != nil {
		return nil, errors.New("cannot read the network adapters: " + err.Error())
	}
	var list []WinAdapter
	if len(out) > 0 && json.Unmarshal(out, &list) != nil {
		return nil, errors.New("cannot read the network adapters")
	}
	for i := range list {
		if list[i].IPv4 == nil {
			list[i].IPv4 = []string{}
		}
		if list[i].DNS == nil {
			list[i].DNS = []string{}
		}
	}
	if list == nil {
		list = []WinAdapter{}
	}
	return list, nil
}

// netPending is a change that waits for its confirmation; the snapshot lets the agent undo it.
type netPending struct {
	ID       string     `json:"id"`
	Deadline int64      `json:"deadline"`
	Adapter  WinAdapter `json:"adapter"` // the state before the change
	Applied  *netApply  `json:"applied,omitempty"`
}

var netMu sync.Mutex

func netPendingPath(dir string) string { return filepath.Join(dir, "netchange.json") }

// restoreScript puts one adapter back as the snapshot describes it.
const restoreScript = `
$s = $env:LINAGER_SNAPSHOT | ConvertFrom-Json
$i = [int]$s.index
Remove-NetRoute -InterfaceIndex $i -DestinationPrefix '0.0.0.0/0' -Confirm:$false -ErrorAction SilentlyContinue
foreach ($a in @(Get-NetIPAddress -InterfaceIndex $i -AddressFamily IPv4 -ErrorAction SilentlyContinue)) { Remove-NetIPAddress -InterfaceIndex $i -IPAddress $a.IPAddress -Confirm:$false -ErrorAction SilentlyContinue }
if ($s.dhcp) {
  Set-NetIPInterface -InterfaceIndex $i -AddressFamily IPv4 -Dhcp Enabled
  Set-DnsClientServerAddress -InterfaceIndex $i -ResetServerAddresses
} else {
  Set-NetIPInterface -InterfaceIndex $i -AddressFamily IPv4 -Dhcp Disabled
  $first = $true
  foreach ($c in @($s.ipv4)) {
    $ip, $pl = $c.Split('/')
    if ($first -and $s.gateway) { New-NetIPAddress -InterfaceIndex $i -IPAddress $ip -PrefixLength ([int]$pl) -DefaultGateway $s.gateway | Out-Null } else { New-NetIPAddress -InterfaceIndex $i -IPAddress $ip -PrefixLength ([int]$pl) | Out-Null }
    $first = $false
  }
  if (@($s.dns).Count -gt 0) { Set-DnsClientServerAddress -InterfaceIndex $i -ServerAddresses @($s.dns) } else { Set-DnsClientServerAddress -InterfaceIndex $i -ResetServerAddresses }
}
'ok'
`

const applyScript = `
$i = [int]$env:LINAGER_INDEX
Remove-NetRoute -InterfaceIndex $i -DestinationPrefix '0.0.0.0/0' -Confirm:$false -ErrorAction SilentlyContinue
if ($env:LINAGER_MODE -eq 'dhcp') {
  foreach ($a in @(Get-NetIPAddress -InterfaceIndex $i -AddressFamily IPv4 -PrefixOrigin Manual -ErrorAction SilentlyContinue)) { Remove-NetIPAddress -InterfaceIndex $i -IPAddress $a.IPAddress -Confirm:$false -ErrorAction SilentlyContinue }
  Set-NetIPInterface -InterfaceIndex $i -AddressFamily IPv4 -Dhcp Enabled
} else {
  Set-NetIPInterface -InterfaceIndex $i -AddressFamily IPv4 -Dhcp Disabled
  foreach ($a in @(Get-NetIPAddress -InterfaceIndex $i -AddressFamily IPv4 -ErrorAction SilentlyContinue)) { Remove-NetIPAddress -InterfaceIndex $i -IPAddress $a.IPAddress -Confirm:$false -ErrorAction SilentlyContinue }
  if ($env:LINAGER_GATEWAY) { New-NetIPAddress -InterfaceIndex $i -IPAddress $env:LINAGER_IP -PrefixLength ([int]$env:LINAGER_PREFIX) -DefaultGateway $env:LINAGER_GATEWAY | Out-Null }
  else { New-NetIPAddress -InterfaceIndex $i -IPAddress $env:LINAGER_IP -PrefixLength ([int]$env:LINAGER_PREFIX) | Out-Null }
}
$dns = @(($env:LINAGER_DNS | ConvertFrom-Json) | Where-Object { $_ })
if ($dns.Count -gt 0) { Set-DnsClientServerAddress -InterfaceIndex $i -ServerAddresses $dns } elseif ($env:LINAGER_MODE -eq 'dhcp') { Set-DnsClientServerAddress -InterfaceIndex $i -ResetServerAddresses }
'ok'
`

func networkStateCmd(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	list, err := readAdapters(ctx)
	if err != nil {
		return nil, err
	}
	res := map[string]any{"adapters": list, "pending": nil}
	if p := loadNetPending(); p != nil {
		res["pending"] = map[string]any{"id": p.ID, "deadline": p.Deadline}
	}
	return res, nil
}

var netDataDir = func() string { return DefaultDataDir }

func loadNetPending() *netPending {
	b, err := os.ReadFile(netPendingPath(netDataDir()))
	if err != nil {
		return nil
	}
	var p netPending
	if json.Unmarshal(b, &p) != nil {
		return nil
	}
	return &p
}

// networkApplyCmd changes one adapter. The old state is saved and a one-shot task restores it after 30 seconds unless the
// change is confirmed: a wrong address that cuts the computer off undoes itself.
func networkApplyCmd(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var req netApply
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, errors.New("invalid request")
	}
	if err := validateNetApply(&req); err != nil {
		return nil, err
	}
	netMu.Lock()
	defer netMu.Unlock()
	if loadNetPending() != nil {
		return nil, errors.New("a network change is waiting for its confirmation")
	}
	list, err := readAdapters(ctx)
	if err != nil {
		return nil, err
	}
	var before *WinAdapter
	for i := range list {
		if list[i].Index == req.Index {
			before = &list[i]
		}
	}
	if before == nil {
		return nil, errors.New("no such adapter")
	}
	p := &netPending{ID: fmt.Sprintf("%x", time.Now().UnixNano()), Deadline: time.Now().Add(netRollbackDelay).Unix(), Adapter: *before, Applied: &req}
	b, _ := json.Marshal(p)
	dir := netDataDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(netPendingPath(dir), b, 0o600); err != nil {
		return nil, err
	}
	if err := armNetRollback(dir); err != nil {
		_ = os.Remove(netPendingPath(dir))
		return nil, errors.New("cannot arm the rollback, nothing was changed: " + err.Error())
	}
	dns, _ := json.Marshal(req.DNS)
	env := map[string]string{"INDEX": strconv.Itoa(req.Index), "MODE": req.Mode, "IP": req.IP, "PREFIX": strconv.Itoa(req.Prefix), "GATEWAY": req.Gateway, "DNS": string(dns)}
	if _, err := runPS(context.WithoutCancel(ctx), applyScript, env); err != nil {
		rollbackNet() // a half-applied change is undone at once
		return nil, errors.New("the change failed and was undone: " + err.Error())
	}
	return map[string]any{"id": p.ID, "deadline": p.Deadline, "seconds": int(netRollbackDelay.Seconds())}, nil
}

// armNetRollback creates the one-shot task that runs the rollback; it uses the installed binary, so it needs
// nothing from the running agent.
func armNetRollback(dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	deleteTask(netChangeTask)
	data, err := taskXML(time.Now().Add(netRollbackDelay), exe, fmt.Sprintf(`agent rollback-change --data-dir "%s"`, dir))
	if err != nil {
		return err
	}
	f := filepath.Join(dir, "net-rollback-task.xml")
	if err := os.WriteFile(f, data, 0o600); err != nil {
		return err
	}
	defer func() { _ = os.Remove(f) }()
	if out, err := exec.Command("schtasks.exe", "/Create", "/TN", netChangeTask, "/XML", f, "/F").CombinedOutput(); err != nil { //nolint:gosec // fixed command, paths we built
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

// rollbackNet restores the adapter from the saved state and clears the pending change.
func rollbackNet() error {
	dir := netDataDir()
	p := loadNetPending()
	if p == nil {
		return nil
	}
	snap, _ := json.Marshal(p.Adapter)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := runPS(ctx, restoreScript, map[string]string{"SNAPSHOT": string(snap)})
	_ = os.Remove(netPendingPath(dir))
	deleteTask(netChangeTask)
	return err
}

// RollbackNetworkChange is `linager agent rollback-change`, run by the one-shot task.
func RollbackNetworkChange(dataDir string) error {
	netDataDir = func() string { return dataDir }
	return rollbackNet()
}

func networkConfirmCmd(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct{ ID string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	netMu.Lock()
	defer netMu.Unlock()
	cur := loadNetPending()
	if cur == nil || cur.ID != p.ID {
		return nil, errors.New("this change is no longer waiting: it was rolled back")
	}
	_ = os.Remove(netPendingPath(netDataDir()))
	deleteTask(netChangeTask)
	return map[string]bool{"ok": true}, nil
}

func networkRollbackCmd(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct{ ID string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	netMu.Lock()
	defer netMu.Unlock()
	if cur := loadNetPending(); cur == nil || cur.ID != p.ID {
		return nil, errors.New("this change is no longer waiting")
	}
	if err := rollbackNet(); err != nil {
		return nil, errors.New("the rollback reported an error: " + err.Error())
	}
	return map[string]bool{"ok": true}, nil
}

// recoverNetChange undoes a change that was left unconfirmed by a restart of the agent (or of the computer).
func recoverNetChange() {
	if p := loadNetPending(); p != nil && time.Now().Unix() >= p.Deadline {
		_ = rollbackNet()
	}
}
