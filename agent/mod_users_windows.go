//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WinUser is a local account of this Windows computer as the panel shows it.
type WinUser struct {
	Name        string   `json:"name"`
	FullName    string   `json:"full_name"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	SID         string   `json:"sid"`
	Builtin     bool     `json:"builtin"` // the built-in administrator (SID ending in -500)
	LastLogon   int64    `json:"last_logon"`
	PwLastSet   int64    `json:"pw_last_set"`
	PwExpires   int64    `json:"pw_expires"`
	Groups      []string `json:"groups"`
}

// WinSession is a signed-in session (the console or a remote desktop).
type WinSession struct {
	ID    uint32 `json:"id"`
	User  string `json:"user"`
	State string `json:"state"`
	Type  string `json:"type"` // console | rdp | other
}

const usersStateScript = `
function Unix($d) { if ($d) { [DateTimeOffset]::new($d).ToUnixTimeSeconds() } else { 0 } }
$map = @{}
foreach ($grp in @(Get-LocalGroup)) {
  try { foreach ($m in @(Get-LocalGroupMember -Group $grp.Name)) { if (-not $map.ContainsKey($m.SID.Value)) { $map[$m.SID.Value] = @() }; $map[$m.SID.Value] += $grp.Name } } catch { }
}
$users = @(Get-LocalUser | ForEach-Object {
  [pscustomobject]@{ name = $_.Name; full_name = [string]$_.FullName; description = [string]$_.Description; enabled = [bool]$_.Enabled; sid = $_.SID.Value
    last_logon = (Unix $_.LastLogon); pw_last_set = (Unix $_.PasswordLastSet); pw_expires = (Unix $_.PasswordExpires); groups = @($map[$_.SID.Value]) }
})
[pscustomobject]@{ users = $users; groups = @(Get-LocalGroup | ForEach-Object { $_.Name }) } | ConvertTo-Json -Depth 5 -Compress
`

func winSessions() []WinSession {
	var info *windows.WTS_SESSION_INFO
	var n uint32
	if err := windows.WTSEnumerateSessions(0, 0, 1, &info, &n); err != nil {
		return nil
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(info)))
	rows := unsafe.Slice(info, int(n))
	var out []WinSession
	for _, r := range rows {
		user := wtsString(r.SessionID, wtsUserName)
		if user == "" {
			continue // the services session and listeners have no user
		}
		s := WinSession{ID: r.SessionID, User: user}
		switch r.State {
		case windows.WTSActive:
			s.State = "active"
		case windows.WTSDisconnected:
			s.State = "disconnected"
		default:
			s.State = "other"
		}
		name := windows.UTF16PtrToString(r.WindowStationName)
		switch {
		case strings.EqualFold(name, "Console"):
			s.Type = "console"
		case strings.HasPrefix(strings.ToUpper(name), "RDP"):
			s.Type = "rdp"
		default:
			s.Type = "other"
		}
		out = append(out, s)
	}
	return out
}

var (
	wtsapi                  = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSQuerySessionInfo = wtsapi.NewProc("WTSQuerySessionInformationW")
	procWTSLogoffSession    = wtsapi.NewProc("WTSLogoffSession")
)

const wtsUserName = 5 // WTS_INFO_CLASS WTSUserName

// wtsString asks for one string of a session (its user name).
func wtsString(session uint32, class uint32) string {
	var buf *uint16
	var n uint32
	r, _, _ := procWTSQuerySessionInfo.Call(0, uintptr(session), uintptr(class), uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&n)))
	if r == 0 || buf == nil {
		return ""
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(buf)))
	return windows.UTF16PtrToString(buf)
}

// wtsLogoff ends a session; wait makes the call return when it is over.
func wtsLogoff(session uint32) error {
	r, _, err := procWTSLogoffSession.Call(0, uintptr(session), 1)
	if r == 0 {
		return err
	}
	return nil
}

func usersStateCmd(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	out, err := runPS(ctx, usersStateScript, nil)
	if err != nil {
		return nil, errors.New("cannot read the local users: " + err.Error())
	}
	var st struct {
		Users  []WinUser `json:"users"`
		Groups []string  `json:"groups"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return nil, errors.New("cannot read the local users")
	}
	for i := range st.Users {
		st.Users[i].Builtin = builtinAdminSID(st.Users[i].SID)
		if st.Users[i].Groups == nil {
			st.Users[i].Groups = []string{}
		}
	}
	if st.Users == nil {
		st.Users = []WinUser{}
	}
	sessions := winSessions()
	if sessions == nil {
		sessions = []WinSession{}
	}
	return map[string]any{"users": st.Users, "groups": st.Groups, "sessions": sessions}, nil
}

// enabledAdmins counts the enabled members of the local Administrators group (found by its well-known SID).
const enabledAdminsScript = `
$n = 0
foreach ($m in @(Get-LocalGroupMember -SID 'S-1-5-32-544')) { if ($m.ObjectClass -eq 'User' -and $m.PrincipalSource -eq 'Local') { try { if ((Get-LocalUser -SID $m.SID).Enabled) { $n++ } } catch { } } else { $n++ } }
$n
`

func enabledAdmins(ctx context.Context) int {
	out, err := runPS(ctx, enabledAdminsScript, nil)
	if err != nil {
		return 99 // unknown: do not block the action on a failed check
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

type winUserReq struct {
	Name     string    `json:"name"`
	FullName *string   `json:"full_name"`
	Password string    `json:"password"`
	Groups   *[]string `json:"groups"`
	Enabled  *bool     `json:"enabled"`
}

func parseUserReq(raw json.RawMessage) (*winUserReq, error) {
	var r winUserReq
	if err := json.Unmarshal(raw, &r); err != nil || !validWinUser(r.Name) {
		return nil, errors.New("invalid user")
	}
	if r.Groups != nil {
		if err := validWinGroups(*r.Groups); err != nil {
			return nil, err
		}
	}
	if len(r.Password) > 127 || (r.FullName != nil && len(*r.FullName) > 256) {
		return nil, errors.New("the value is too long")
	}
	return &r, nil
}

// userInfo returns the SID and the group list of a local account.
func userInfo(ctx context.Context, name string) (sid string, enabled bool, err error) {
	out, err := runPS(ctx, `$u = Get-LocalUser -Name $env:LINAGER_NAME; "$($u.SID.Value)|$($u.Enabled)"`, map[string]string{"NAME": name})
	if err != nil {
		return "", false, errors.New("no such user")
	}
	sidv, en, _ := strings.Cut(strings.TrimSpace(string(out)), "|")
	return sidv, strings.EqualFold(en, "true"), nil
}

const userAddScript = `
$pw = ConvertTo-SecureString $env:LINAGER_PASSWORD -AsPlainText -Force
$args1 = @{ Name = $env:LINAGER_NAME; Password = $pw; PasswordNeverExpires = $false; AccountNeverExpires = $true }
if ($env:LINAGER_FULLNAME) { $args1.FullName = $env:LINAGER_FULLNAME }
New-LocalUser @args1 | Out-Null
foreach ($g in @(($env:LINAGER_GROUPS | ConvertFrom-Json))) { if ($g) { Add-LocalGroupMember -Group $g -Member $env:LINAGER_NAME } }
'ok'
`

func userAddCmd(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	r, err := parseUserReq(raw)
	if err != nil {
		return nil, err
	}
	if len(r.Password) < 1 {
		return nil, errors.New("a password is required: Windows does not allow a network sign-in without one")
	}
	groups := []string{}
	if r.Groups != nil {
		groups = *r.Groups
	}
	gj, _ := json.Marshal(groups)
	full := ""
	if r.FullName != nil {
		full = *r.FullName
	}
	if _, err := runPS(ctx, userAddScript, map[string]string{"NAME": r.Name, "PASSWORD": r.Password, "FULLNAME": full, "GROUPS": string(gj)}); err != nil {
		return nil, errors.New("cannot create the user: " + err.Error())
	}
	return map[string]bool{"ok": true}, nil
}

const userUpdateScript = `
$name = $env:LINAGER_NAME
if ($env:LINAGER_ENABLED -eq 'true') { Enable-LocalUser -Name $name } elseif ($env:LINAGER_ENABLED -eq 'false') { Disable-LocalUser -Name $name }
if ($env:LINAGER_SETFULL -eq '1') { Set-LocalUser -Name $name -FullName $env:LINAGER_FULLNAME }
if ($env:LINAGER_SETGROUPS -eq '1') {
  $want = @(($env:LINAGER_GROUPS | ConvertFrom-Json) | Where-Object { $_ })
  $u = Get-LocalUser -Name $name
  $have = @()
  foreach ($grp in @(Get-LocalGroup)) { try { if (@(Get-LocalGroupMember -Group $grp.Name | Where-Object { $_.SID -eq $u.SID }).Count -gt 0) { $have += $grp.Name } } catch { } }
  foreach ($g in $want) { if ($have -notcontains $g) { Add-LocalGroupMember -Group $g -Member $name } }
  foreach ($g in $have) { if ($want -notcontains $g) { Remove-LocalGroupMember -Group $g -Member $name } }
}
'ok'
`

func userUpdateCmd(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	r, err := parseUserReq(raw)
	if err != nil {
		return nil, err
	}
	sid, _, err := userInfo(ctx, r.Name)
	if err != nil {
		return nil, err
	}
	if builtinAdminSID(sid) && r.Enabled != nil && !*r.Enabled {
		return nil, errors.New("the built-in administrator account is not disabled from here")
	}
	if (r.Enabled != nil && !*r.Enabled) || (r.Groups != nil && !containsFold(*r.Groups, "Administrators")) {
		if enabledAdmins(ctx) <= 1 && isAdministrator(ctx, r.Name) {
			return nil, errors.New("this is the last enabled administrator: the change would leave nobody to manage the computer")
		}
	}
	env := map[string]string{"NAME": r.Name, "GROUPS": "[]"}
	if r.Enabled != nil {
		env["ENABLED"] = strconv.FormatBool(*r.Enabled)
	}
	if r.FullName != nil {
		env["SETFULL"], env["FULLNAME"] = "1", *r.FullName
	}
	if r.Groups != nil {
		gj, _ := json.Marshal(*r.Groups)
		env["SETGROUPS"], env["GROUPS"] = "1", string(gj)
	}
	if _, err := runPS(ctx, userUpdateScript, env); err != nil {
		return nil, errors.New("cannot change the user: " + err.Error())
	}
	return map[string]bool{"ok": true}, nil
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

func isAdministrator(ctx context.Context, name string) bool {
	out, err := runPS(ctx, `$u = Get-LocalUser -Name $env:LINAGER_NAME; @(Get-LocalGroupMember -SID 'S-1-5-32-544' | Where-Object { $_.SID -eq $u.SID }).Count`, map[string]string{"NAME": name})
	return err != nil || strings.TrimSpace(string(out)) != "0"
}

func userPasswdCmd(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	r, err := parseUserReq(raw)
	if err != nil {
		return nil, err
	}
	if r.Password == "" {
		return nil, errors.New("a password is required")
	}
	if _, err := runPS(ctx, `Set-LocalUser -Name $env:LINAGER_NAME -Password (ConvertTo-SecureString $env:LINAGER_PASSWORD -AsPlainText -Force); 'ok'`, map[string]string{"NAME": r.Name, "PASSWORD": r.Password}); err != nil {
		return nil, errors.New("cannot set the password: " + err.Error())
	}
	return map[string]bool{"ok": true}, nil
}

func userDeleteCmd(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	r, err := parseUserReq(raw)
	if err != nil {
		return nil, err
	}
	sid, _, err := userInfo(ctx, r.Name)
	if err != nil {
		return nil, err
	}
	if builtinAdminSID(sid) {
		return nil, errors.New("the built-in administrator account cannot be deleted")
	}
	if enabledAdmins(ctx) <= 1 && isAdministrator(ctx, r.Name) {
		return nil, errors.New("this is the last administrator: deleting it would leave nobody to manage the computer")
	}
	if _, err := runPS(ctx, `Remove-LocalUser -Name $env:LINAGER_NAME; 'ok'`, map[string]string{"NAME": r.Name}); err != nil {
		return nil, errors.New("cannot delete the user: " + err.Error())
	}
	return map[string]bool{"ok": true}, nil
}

// userLogoffCmd ends a signed-in session of a user.
func userLogoffCmd(_ context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p struct {
		ID uint32 `json:"id"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	found := false
	for _, s := range winSessions() {
		if s.ID == p.ID {
			found = true
		}
	}
	if !found {
		return nil, errors.New("no such session")
	}
	if err := wtsLogoff(p.ID); err != nil {
		return nil, errors.New("cannot end the session: " + err.Error())
	}
	return map[string]bool{"ok": true}, nil
}
