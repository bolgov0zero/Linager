package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIgnoreListSurvivesARestartAndIsPrivate(t *testing.T) {
	dir := t.TempDir()
	loadIgnores(dir)
	if err := ignores.set("apt", "openssl", true); err != nil {
		t.Fatal(err)
	}
	if err := ignores.set("docker", "web-1", true); err != nil {
		t.Fatal(err)
	}
	loadIgnores(dir) // the agent restarts
	if !ignores.isApt("openssl") || !ignores.isDocker("web-1") || ignores.isApt("curl") {
		t.Fatalf("the lists were not restored: %+v", ignores)
	}
	fi, err := os.Stat(filepath.Join(dir, "ignored.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("the file must be private, got %v", fi.Mode().Perm())
	}
	if err := ignores.set("apt", "openssl", false); err != nil || ignores.isApt("openssl") {
		t.Fatalf("remove: %v", err)
	}
}

func TestIgnoreListRefusesBadNames(t *testing.T) {
	loadIgnores(t.TempDir())
	for _, n := range []string{"", "a b", "../etc", "-x", strings.Repeat("a", 200), "a;b"} {
		if err := ignores.set("apt", n, true); err == nil {
			t.Errorf("package name %q must be refused", n)
		}
		if err := ignores.set("docker", n, true); err == nil {
			t.Errorf("container name %q must be refused", n)
		}
	}
	if err := ignores.set("systemd", "x", true); err == nil {
		t.Error("an unknown kind must be refused")
	}
}

func TestIgnoreCommand(t *testing.T) {
	loadIgnores(t.TempDir())
	called := 0
	fn := ignoreCmd("apt", func() { called++ })
	if _, err := fn(context.Background(), json.RawMessage(`{"name":"openssl","ignore":true}`), false); err != nil || !ignores.isApt("openssl") || called != 1 {
		t.Fatalf("ignore: %v %v %d", err, ignores.isApt("openssl"), called)
	}
	if _, err := fn(context.Background(), json.RawMessage(`{"name":"openssl","ignore":false}`), false); err != nil || ignores.isApt("openssl") {
		t.Fatalf("unignore: %v", err)
	}
	if _, err := fn(context.Background(), json.RawMessage(`{"name":"bad name"}`), false); err == nil {
		t.Fatal("a bad name must be refused")
	}
}
