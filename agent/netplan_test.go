package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyNetplanRestoresFileWhenGenerateFails(t *testing.T) {
	dir, np := t.TempDir(), t.TempDir()
	path := filepath.Join(np, "01-netcfg.yaml")
	orig := "network:\n  version: 2\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &netModule{dir: dir, npDir: np, run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("Invalid YAML"), errors.New("exit status 1")
	}}
	if _, err := m.applyNetplan(context.Background(), "id1", "01-netcfg.yaml", "network:\n  version: 2\n  bad: [\n"); err == nil {
		t.Fatal("a failing netplan generate must reject the change")
	}
	if got, _ := os.ReadFile(path); string(got) != orig { //nolint:gosec // a temp file of this test
		t.Fatalf("the original file must be restored, got %q", got)
	}
	if _, err := m.applyNetplan(context.Background(), "id2", "../evil.yaml", "x: 1\n"); err == nil {
		t.Fatal("a file name outside the allowed pattern must be refused")
	}
	if !isCloudInitFile("50-cloud-init.yaml", "x") || isCloudInitFile("01-netcfg.yaml", "network:\n") {
		t.Fatal("cloud-init detection")
	}
}
