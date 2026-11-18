//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
)

const (
	tsKey      = `SYSTEM\CurrentControlSet\Control\Terminal Server`
	rdpTcpKey  = tsKey + `\WinStations\RDP-Tcp`
	defaultRDP = 3389
)

// rdpPort is the port the Remote Desktop service listens on (3389 unless the administrator moved it).
func rdpPort() int {
	if v, ok := regDWORD(rdpTcpKey, "PortNumber"); ok && v > 0 && v < 65536 {
		return int(v)
	}
	return defaultRDP
}

// desktopStream carries a connection to the local Remote Desktop service. The panel's browser client speaks RDP
// over it; the address is fixed here, whatever the panel asks.
func desktopStream(ctx context.Context, _ json.RawMessage, sio *StreamIO) error {
	return pumpTCP(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(rdpPort())), sio)
}

// DesktopState describes the remote desktop of this computer for the panel.
type DesktopState struct {
	Enabled   bool   `json:"enabled"`   // connections are allowed in the settings
	Listening bool   `json:"listening"` // something answers on the RDP port
	Port      int    `json:"port"`
	NLA       bool   `json:"nla"`       // network level authentication is required
	Service   string `json:"service"`   // state of the Remote Desktop Services service
	Supported bool   `json:"supported"` // the edition can host a remote desktop (not Home)
	Edition   string `json:"edition"`
}

func desktopState() DesktopState {
	st := DesktopState{Port: rdpPort(), Edition: osName()}
	if v, ok := regDWORD(tsKey, "fDenyTSConnections"); ok {
		st.Enabled = v == 0
	}
	if v, ok := regDWORD(rdpTcpKey, "UserAuthentication"); ok {
		st.NLA = v == 1
	}
	st.Supported = !strings.Contains(strings.ToLower(regString(ntVersionKey, "EditionID")), "core")
	st.Service = serviceStateOf("TermService")
	if c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(st.Port)), 700*time.Millisecond); err == nil {
		st.Listening = true
		_ = c.Close()
	}
	return st
}

func desktopStateCmd(context.Context, json.RawMessage, bool) (any, error) { return desktopState(), nil }

// desktopEnableCmd turns the remote desktop on: the setting, the firewall group and the service. It is a
// dangerous action: it opens a way into the computer.
func desktopEnableCmd(ctx context.Context, _ json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	if !desktopState().Supported {
		return nil, errors.New("this edition of Windows cannot host a remote desktop")
	}
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, tsKey, registry.SET_VALUE)
	if err != nil {
		return nil, err
	}
	err = k.SetDWordValue("fDenyTSConnections", 0)
	_ = k.Close()
	if err != nil {
		return nil, err
	}
	// "@FirewallAPI.dll,-28752" is the group "Remote Desktop" in every language of Windows
	if out, err := exec.CommandContext(ctx, "netsh.exe", "advfirewall", "firewall", "set", "rule", "group=@FirewallAPI.dll,-28752", "new", "enable=Yes").CombinedOutput(); err != nil { //nolint:gosec // fixed command
		return nil, errors.New("the firewall rule was not enabled: " + strings.TrimSpace(string(out)))
	}
	if out, err := exec.CommandContext(ctx, "sc.exe", "config", "TermService", "start=", "demand").CombinedOutput(); err != nil { //nolint:gosec // fixed command
		return nil, errors.New("the service could not be configured: " + strings.TrimSpace(string(out)))
	}
	if out, err := exec.CommandContext(ctx, "sc.exe", "start", "TermService").CombinedOutput(); err != nil && !strings.Contains(string(out), "1056") { //nolint:gosec // 1056: already running
		return nil, errors.New("the service did not start: " + strings.TrimSpace(string(out)))
	}
	time.Sleep(time.Second)
	return desktopState(), nil
}

// serviceStateOf is the state of any service by name.
func serviceStateOf(name string) string {
	m, err := connectSCM()
	if err != nil {
		return "unknown"
	}
	defer func() { _ = m.Disconnect() }()
	s, err := openLimited(m, name)
	if err != nil {
		return "not installed"
	}
	defer func() { _ = s.Close() }()
	st, err := s.Query()
	if err != nil {
		return "unknown"
	}
	if st.State == svc.Running {
		return "running"
	}
	_, sub := stateNames(st.State)
	return sub
}
