//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

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
	if s.Uptime <= 0 || s.Procs <= 0 {
		t.Errorf("uptime %d, procs %d", s.Uptime, s.Procs)
	}
	si := c.System("test")
	if !strings.HasPrefix(si.OS, "Windows") || si.CPUCores == 0 || si.Hostname == "" || si.Kernel == "" {
		t.Errorf("system info: %+v", si)
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
	pty, err := startConPTY(shellCommand(), 100, 30)
	if err != nil {
		t.Fatal(err)
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
	// the text that is typed has a quote in it, the answer has not: only a running shell can print it
	time.Sleep(3 * time.Second) // the shell starts
	if _, err := pty.in.Write([]byte("Write-Output ('lin'+'ager-ok')\r\n")); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if strings.Contains(seen(), "linager-ok") {
			return
		}
	}
	t.Fatalf("the shell did not answer; it printed %q", seen())
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
	if _, ok := reg.cmds["update.apply"]; ok {
		t.Error("a Windows agent must not take update.apply")
	}
}
