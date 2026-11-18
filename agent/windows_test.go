//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/bolgov0zero/linager/internal/config"
)

// These tests run on a real Windows host (the windows workflow).

func TestWindowsCollector(t *testing.T) {
	c := NewCollector(context.Background())
	c.Collect(time.Now()) // primes the counters
	time.Sleep(1200 * time.Millisecond)
	s := c.Collect(time.Now())
	if s.MemTotal == 0 || s.MemPct <= 0 || s.MemPct > 100 {
		t.Errorf("memory: %+v", s)
	}
	if len(s.Disks) == 0 || s.Disks[0].Total == 0 {
		t.Errorf("no disks: %+v", s.Disks)
	}
	for _, n := range s.Net {
		t.Logf("adapter %q up=%v ip=%s rx=%d tx=%d speed=%d", n.Iface, n.Up, n.IP, n.RxTotal, n.TxTotal, n.SpeedMbps)
	}
	counted := false
	for _, n := range s.Net {
		if n.RxTotal > 0 || n.TxTotal > 0 {
			counted = true
		}
	}
	if !counted {
		t.Errorf("no network counters: %+v", s.Net)
	}
	if s.Uptime <= 0 || s.Procs <= 0 {
		t.Errorf("uptime %d, procs %d", s.Uptime, s.Procs)
	}
	si := c.System("test")
	if !strings.HasPrefix(si.OS, "Windows") || si.CPUCores == 0 || si.Hostname == "" || si.Kernel == "" {
		t.Errorf("system info: %+v", si)
	}
}

func TestWindowsOctetsUsesTheParts(t *testing.T) {
	row := windows.MibIfRow2{InUcastOctets: 100, InMulticastOctets: 20, InBroadcastOctets: 3, OutUcastOctets: 50}
	if rx, tx := octets(&row); rx != 123 || tx != 50 {
		t.Errorf("parts only: rx=%d tx=%d", rx, tx)
	}
	row.InOctets, row.OutOctets = 500, 40
	if rx, tx := octets(&row); rx != 500 || tx != 50 {
		t.Errorf("the larger one: rx=%d tx=%d", rx, tx)
	}
}

func TestWindowsServicesList(t *testing.T) {
	res, err := svcList(context.Background(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	units := res.(map[string]any)["services"].([]Unit)
	if len(units) < 20 {
		t.Fatalf("only %d services", len(units))
	}
	found := false
	for _, u := range units {
		if strings.EqualFold(u.Name, "EventLog") && u.Active == "active" {
			found = true
		}
	}
	if !found {
		t.Error("EventLog must be listed and running")
	}
}

func TestWindowsProcesses(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"sort": "mem", "n": 5})
	res, err := procList(context.Background(), raw, false)
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["total"].(int) < 10 || len(m["processes"].([]ProcInfo)) != 5 {
		t.Errorf("processes: %v", m["total"])
	}
	for _, pid := range []int{0, 4, 1} {
		raw, _ := json.Marshal(map[string]any{"pid": pid})
		if _, err := procKill(context.Background(), raw, true); err == nil {
			t.Errorf("pid %d must be refused", pid)
		}
	}
}

func TestWindowsFiles(t *testing.T) {
	root, ok := listRoot("/")
	if !ok || len(root.(map[string]any)["entries"].([]FileEntry)) == 0 {
		t.Fatal("the drives are not listed")
	}
	dir := t.TempDir()
	if _, err := filesList(context.Background(), mustJSON(map[string]string{"path": outPath(dir)}), false); err != nil {
		t.Fatal(err)
	}
	if !isProtected(`C:\`) || !isProtected(`C:\Windows`) || isProtected(dir) {
		t.Error("protected paths")
	}
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func TestWindowsTerminal(t *testing.T) {
	root := os.Getenv("SystemRoot")
	ps := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := filepath.Join(root, "System32", "cmd.exe")
	cases := []struct{ name, line, send, want string }{
		{"cmd", `"` + cmd + `"`, "echo lin^ager-ok\r\n", "linager-ok"},
		{"powershell-plain", `"` + ps + `" -NoLogo -NoProfile`, "Write-Output ('lin'+'ager-ok')\r\n", "linager-ok"},
		{"powershell-default", shellCommand(), "Write-Output ('lin'+'ager-ok')\r\n", "linager-ok"},
	}
	ok := 0
	for _, c := range cases {
		out, err := tryShell(c.line, c.send, c.want)
		t.Logf("%s: err=%v answered=%v output=%q", c.name, err, strings.Contains(out, c.want), out)
		if err == nil && strings.Contains(out, c.want) {
			ok++
		}
	}
	if ok == 0 {
		t.Fatal("no shell answered in the pseudo console")
	}
}

// tryShell starts a shell in a pseudo console, types a line and waits for the answer.
func tryShell(cmdline, send, want string) (string, error) {
	pty, err := startConPTY(cmdline, 100, 30)
	if err != nil {
		return "", err
	}
	defer pty.close()
	var mu sync.Mutex
	var sb strings.Builder
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := pty.out.Read(buf)
			mu.Lock()
			sb.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	seen := func() string { mu.Lock(); defer mu.Unlock(); return sb.String() }
	time.Sleep(3 * time.Second) // the shell starts
	if _, err := pty.in.Write([]byte(send)); err != nil {
		return seen(), err
	}
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if strings.Contains(seen(), want) {
			return seen(), nil
		}
	}
	return seen(), errors.New("no answer")
}

func TestWindowsModules(t *testing.T) {
	reg := NewRegistry(config.DefaultAgent(), NewCollector(context.Background()), nil, t.TempDir())
	for _, m := range reg.Modules() {
		switch m {
		case "apt", "firewall", "network", "users", "docker", "disk", "security":
			t.Errorf("a Linux-only module is enabled: %s", m)
		}
	}
	if _, ok := reg.cmds["system.reboot"]; !ok {
		t.Error("reboot is missing")
	}
	if _, ok := reg.cmds["update.apply"]; !ok {
		t.Error("the panel updates a Windows agent: update.apply is missing")
	}
}

func TestWindowsReplaceRunningStyleAndRestore(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "linager.exe")
	// a program that really runs from this file, as the agent does: its file cannot be overwritten or deleted
	src, err := os.ReadFile(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, src, 0o700); err != nil {
		t.Fatal(err)
	}
	running := exec.Command(exe, "/c", "ping -n 60 127.0.0.1 >nul")
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = running.Process.Kill(); _ = running.Wait() }()
	time.Sleep(500 * time.Millisecond)
	if err := os.WriteFile(exe, []byte("x"), 0o600); err == nil {
		t.Fatal("a running program must not be overwritable: the test would prove nothing")
	}
	tmp := exe + ".linager-new"
	if err := os.WriteFile(tmp, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutable(tmp, exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Fatalf("not replaced: %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); len(b) != len(src) {
		t.Fatalf("the old file must be kept aside, got %d bytes", len(b))
	}
	// restore from the saved copy (the file that is aside is still in use: the second replacement must cope)
	prev := filepath.Join(dir, prevFileName)
	if err := os.WriteFile(prev, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = running.Process.Kill()
	_ = running.Wait()
	if err := restoreExecutable(prev, exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatalf("not restored: %q", b)
	}
}

func TestWindowsRollbackTask(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(prevPath(dir), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "linager.exe")
	if err := armRollback(dir, exe); err != nil {
		t.Fatalf("the task was not created: %v", err)
	}
	if err := exec.Command("schtasks.exe", "/Query", "/TN", rollbackTask).Run(); err != nil {
		t.Fatalf("the task is not there: %v", err)
	}
	stopRollbackTimer()
	if err := exec.Command("schtasks.exe", "/Query", "/TN", rollbackTask).Run(); err == nil {
		t.Fatal("the task must be gone after the confirmation")
	}
}

func TestWindowsRollbackUpdateRestoresTheOldBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "linager.exe")
	for path, content := range map[string]string{exe: "broken new", prevPath(dir): "good old", markerPath(dir): "{}"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// there is no service in the test, so the final restart fails; the binary must be back anyway
	_ = RollbackUpdate(dir, exe)
	if b, _ := os.ReadFile(exe); string(b) != "good old" {
		t.Fatalf("the old binary was not restored: %q", b)
	}
	if _, err := os.Stat(markerPath(dir)); err == nil {
		t.Fatal("the marker must be removed")
	}
}

func TestWindowsDesktopState(t *testing.T) {
	st := desktopState()
	if st.Port <= 0 || st.Port > 65535 || st.Edition == "" {
		t.Fatalf("state: %+v", st)
	}
	t.Logf("remote desktop: enabled=%v listening=%v port=%d nla=%v service=%s supported=%v", st.Enabled, st.Listening, st.Port, st.NLA, st.Service, st.Supported)
	if _, err := desktopEnableCmd(context.Background(), nil, false); err == nil {
		t.Fatal("turning the remote desktop on is a dangerous action: it needs the confirmation")
	}
}
