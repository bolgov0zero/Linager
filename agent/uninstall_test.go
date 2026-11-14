package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUninstallPurge(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	_ = os.MkdirAll(data, 0o700)
	unit, cfg := filepath.Join(dir, "u.service"), filepath.Join(dir, "agent.yaml")
	for _, f := range []string{filepath.Join(data, keyFile), filepath.Join(data, certFile), filepath.Join(data, stateFile), unit, cfg} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var calls [][]string
	if err := uninstall(true, data, unit, cfg, func(a ...string) error { calls = append(calls, a); return nil }); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{data, unit, cfg} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s must be removed", f)
		}
	}
	if len(calls) != 3 || calls[2][0] != "stop" {
		t.Fatalf("unexpected systemctl calls: %v", calls)
	}
}

func TestUninstallKeepsKeyWithoutPurge(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{keyFile, certFile, stateFile} {
		_ = os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600)
	}
	_ = uninstall(false, dir, filepath.Join(dir, "none"), filepath.Join(dir, "none2"), func(...string) error { return nil })
	if _, err := os.Stat(filepath.Join(dir, keyFile)); err != nil {
		t.Fatal("the key must survive a non-purge uninstall")
	}
	if _, err := os.Stat(filepath.Join(dir, stateFile)); !os.IsNotExist(err) {
		t.Fatal("state must be removed")
	}
}
