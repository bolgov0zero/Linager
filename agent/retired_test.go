package agent

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRemoveRetiredData(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"gateway.json", "gateway.json.tmp", "state.json"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := RemoveRetiredData(dir)
	if !slices.Equal(got, []string{"gateway.json", "gateway.json.tmp"}) {
		t.Errorf("removed: %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Errorf("the other files stay: %v", err)
	}
	if got := RemoveRetiredData(dir); len(got) != 0 {
		t.Errorf("nothing left to remove: %v", got)
	}
	if got := RemoveRetiredData(filepath.Join(dir, "missing")); len(got) != 0 {
		t.Errorf("a missing directory is fine: %v", got)
	}
}
