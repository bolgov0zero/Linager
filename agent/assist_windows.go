//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// noSession is what Windows returns when no session is at the console (a moment of a switch of users).
const noSession = 0xFFFFFFFF

// Classes of WTSQuerySessionInformation (wtsUserName and wtsString are in the users module).
const (
	wtsDomainName         = 7
	wtsClientName         = 10
	wtsClientAddress      = 14
	wtsClientProtocolType = 16
)

var wtsStateNames = []string{"active", "connected", "connecting", "shadow", "disconnected", "idle", "listen", "reset", "down", "init"}

// wtsQuery returns a block of memory of WTSQuerySessionInformation; free it with windows.WTSFreeMemory.
func wtsQuery(session uint32, class uint32) (unsafe.Pointer, uint32, bool) {
	var buf unsafe.Pointer
	var n uint32
	r, _, _ := procWTSQuerySessionInfo.Call(0, uintptr(session), uintptr(class), uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&n)))
	if r == 0 || buf == nil {
		return nil, 0, false
	}
	return buf, n, true
}

func wtsClientAddr(session uint32) string {
	buf, n, ok := wtsQuery(session, wtsClientAddress)
	if !ok || n < 24 {
		return ""
	}
	defer windows.WTSFreeMemory(uintptr(buf))
	b := unsafe.Slice((*byte)(buf), n)
	switch fam := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24; fam {
	case 2: // AF_INET: the address is in Address[2:6]
		return net.IP(b[4+2 : 4+6]).String()
	case 23: // AF_INET6
		return net.IP(b[4 : 4+16]).String()
	}
	return ""
}

func wtsProtocol(session uint32) int {
	buf, n, ok := wtsQuery(session, wtsClientProtocolType)
	if !ok || n < 2 {
		return 0
	}
	defer windows.WTSFreeMemory(uintptr(buf))
	b := unsafe.Slice((*byte)(buf), n)
	return int(b[0]) | int(b[1])<<8
}

// sessionUser is DOMAIN\name of the person in a session (empty on the logon screen).
func sessionUser(session uint32) string {
	name := wtsString(session, wtsUserName)
	if name == "" {
		return ""
	}
	if dom := wtsString(session, wtsDomainName); dom != "" {
		return dom + `\` + name
	}
	return name
}

// listAssistSessions lists the sessions that have a person, and the console even when nobody is signed in.
func listAssistSessions() []AssistSession {
	var info *windows.WTS_SESSION_INFO
	var n uint32
	if err := windows.WTSEnumerateSessions(0, 0, 1, &info, &n); err != nil || info == nil {
		return nil
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(info)))
	console := windows.WTSGetActiveConsoleSessionId()
	var out []AssistSession
	for _, s := range unsafe.Slice(info, n) {
		if s.SessionID == 0 || s.State == 6 { // the services session and the listeners
			continue
		}
		user := sessionUser(s.SessionID)
		isConsole := s.SessionID == console
		if user == "" && !isConsole {
			continue
		}
		st := "unknown"
		if int(s.State) < len(wtsStateNames) {
			st = wtsStateNames[s.State]
		}
		typ := "other"
		switch {
		case isConsole:
			typ = "console"
		case wtsProtocol(s.SessionID) == 2:
			typ = "rdp"
		}
		client := ""
		if typ == "rdp" {
			client = wtsClientAddr(s.SessionID)
			if client == "" {
				client = wtsString(s.SessionID, wtsClientName)
			}
		}
		out = append(out, AssistSession{ID: s.SessionID, Type: typ, User: user, State: st, Client: client, Console: isConsole,
			Screen: s.State == 0 || (isConsole && s.State == 1)})
	}
	return out
}

const sasPolicyKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System`

// sasState is the policy "Software SAS generation": 0 off, 1 services, 2 Ease of Access apps, 3 both. The agent is a
// service, so 1 and 3 let it send Ctrl+Alt+Del.
func sasState() map[string]any {
	v, _ := regDWORD(sasPolicyKey, "SoftwareSASGeneration")
	return map[string]any{"policy": v, "enabled": v == 1 || v == 3}
}

func assistSessionsCmd(context.Context, json.RawMessage, bool) (any, error) {
	return map[string]any{"sessions": listAssistSessions(), "sas": sasState()}, nil
}

// assistSasCmd lets Windows accept Ctrl+Alt+Del from services or stops it. It changes a Windows policy, so it is a
// dangerous action: the panel asks for a confirmation and writes it to the audit log.
func assistSasCmd(_ context.Context, params json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p struct {
		Enable bool `json:"enable"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, errors.New("bad request")
	}
	cur, _ := regDWORD(sasPolicyKey, "SoftwareSASGeneration")
	val := uint32(0)
	if p.Enable {
		val = 1
		if cur == 2 || cur == 3 {
			val = 3 // keep what the administrator allowed to the Ease of Access apps
		}
	} else if cur == 3 {
		val = 2
	}
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, sasPolicyKey, registry.SET_VALUE)
	if err != nil {
		return nil, err
	}
	defer func() { _ = k.Close() }()
	if err := k.SetDWordValue("SoftwareSASGeneration", val); err != nil {
		return nil, err
	}
	return sasState(), nil
}

// resolveAssistSession turns what the panel asked for ("console" or a number) into a session that has a screen.
func resolveAssistSession(s string) (uint32, error) {
	if s == "" || s == "console" {
		id := windows.WTSGetActiveConsoleSessionId()
		if id == noSession {
			return 0, errors.New("no session at the console now")
		}
		return id, nil
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, errors.New("invalid session")
	}
	for _, x := range listAssistSessions() {
		if uint64(x.ID) == n {
			if !x.Screen {
				return 0, fmt.Errorf("session %d is %s: it has no screen to show", n, x.State)
			}
			return x.ID, nil
		}
	}
	return 0, fmt.Errorf("no session %d", n)
}

// assistantStream is the Assistant of one session: the helper is started in the session and the frames are carried.
func assistantStream(ctx context.Context, params json.RawMessage, sio *StreamIO) error {
	var p struct {
		Session string `json:"session"`
	}
	_ = json.Unmarshal(params, &p)
	// Ctrl+Alt+Del reaches the console session only: that is where a service can send it
	cad := func() error {
		if id, err := resolveAssistSession(p.Session); err != nil || id != windows.WTSGetActiveConsoleSessionId() {
			return errors.New("Ctrl+Alt+Del can be sent to the console session only")
		}
		return sendSAS()
	}
	return runAssistRelay(ctx, sio, p.Session, launchAssistHelper, cad)
}

// winHelper is the helper process in a session.
type winHelper struct {
	mu     sync.Mutex
	h      windows.Handle
	closed bool
	done   chan struct{}
	err    error
}

func (w *winHelper) Done() <-chan struct{} { return w.done }
func (w *winHelper) Err() error            { return w.err }

func (w *winHelper) Kill() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		_ = windows.TerminateProcess(w.h, 1)
	}
}

// launchAssistHelper starts `linager agent assistant-helper` in the session with the rights of this service (SYSTEM),
// on the desktop the person works on. Only a service can do it: it copies its own token, gives the copy the number of
// the session and starts the process with it.
func launchAssistHelper(_ context.Context, session, addr, token string) (assistHelper, error) {
	id, err := resolveAssistSession(session)
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	enablePrivileges("SeTcbPrivilege", "SeAssignPrimaryTokenPrivilege", "SeIncreaseQuotaPrivilege")
	var self windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY, &self); err != nil {
		return nil, fmt.Errorf("own token: %w", err)
	}
	defer func() { _ = self.Close() }()
	var tok windows.Token
	if err := windows.DuplicateTokenEx(self, windows.MAXIMUM_ALLOWED, nil, windows.SecurityImpersonation, windows.TokenPrimary, &tok); err != nil {
		return nil, fmt.Errorf("duplicate token: %w", err)
	}
	defer func() { _ = tok.Close() }()
	if err := windows.SetTokenInformation(tok, windows.TokenSessionId, (*byte)(unsafe.Pointer(&id)), 4); err != nil {
		return nil, fmt.Errorf("set the session of the token (the agent must run as SYSTEM): %w", err)
	}

	var blk *uint16
	if err := windows.CreateEnvironmentBlock(&blk, tok, false); err != nil {
		return nil, fmt.Errorf("environment: %w", err)
	}
	env := readEnvBlock(blk)
	_ = windows.DestroyEnvironmentBlock(blk)
	env = append(env, assistTokenEnv+"="+token)
	envBlock := buildEnvBlock(env)

	cmdline, err := windows.UTF16PtrFromString(fmt.Sprintf(`"%s" agent assistant-helper --addr %s --session %d`, exe, addr, id))
	if err != nil {
		return nil, err
	}
	si := windows.StartupInfo{Desktop: windows.StringToUTF16Ptr(`winsta0\default`)}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	if err := windows.CreateProcessAsUser(tok, nil, cmdline, nil, nil, false,
		windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_NO_WINDOW, &envBlock[0], nil, &si, &pi); err != nil {
		return nil, fmt.Errorf("start the helper in session %d: %w", id, err)
	}
	_ = windows.CloseHandle(pi.Thread)
	w := &winHelper{h: pi.Process, done: make(chan struct{})}
	go func() {
		_, _ = windows.WaitForSingleObject(w.h, windows.INFINITE)
		var code uint32
		_ = windows.GetExitCodeProcess(w.h, &code)
		w.mu.Lock()
		if code != 0 && code != 1 { // 1 is the exit after a Kill
			w.err = errors.New(helperExitText(code))
		}
		w.closed = true
		_ = windows.CloseHandle(w.h)
		w.mu.Unlock()
		close(w.done)
	}()
	return w, nil
}

// readEnvBlock splits an environment block (strings that end with a zero, the block ends with an empty one).
func readEnvBlock(p *uint16) []string {
	var out []string
	for p != nil {
		n := 0
		for *(*uint16)(unsafe.Add(unsafe.Pointer(p), n*2)) != 0 {
			n++
		}
		if n == 0 {
			break
		}
		out = append(out, windows.UTF16ToString(unsafe.Slice(p, n)))
		p = (*uint16)(unsafe.Add(unsafe.Pointer(p), (n+1)*2))
	}
	return out
}

func buildEnvBlock(env []string) []uint16 {
	var b []uint16
	for _, e := range env {
		if strings.ContainsRune(e, 0) {
			continue
		}
		b = append(b, windows.StringToUTF16(e)...) // StringToUTF16 ends with the zero
	}
	return append(b, 0)
}

// enablePrivileges turns on privileges the service token has but may hold switched off. Moving a token to another
// session and starting a process with it need them; where the token lacks one the call that needs it reports the error.
func enablePrivileges(names ...string) {
	var t windows.Token
	if windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &t) != nil {
		return
	}
	defer func() { _ = t.Close() }()
	for _, n := range names {
		var luid windows.LUID
		if windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr(n), &luid) != nil {
			continue
		}
		tp := windows.Tokenprivileges{PrivilegeCount: 1}
		tp.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
		_ = windows.AdjustTokenPrivileges(t, false, &tp, 0, nil, nil)
	}
}
