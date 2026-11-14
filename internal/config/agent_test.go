package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentConfigDefaultsAndOverride(t *testing.T) {
	c, err := LoadAgent(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil || len(c.List()) != len(AgentModules) {
		t.Fatalf("missing file must give all modules: %v %v", c, err)
	}
	p := filepath.Join(t.TempDir(), "agent.yaml")
	_ = os.WriteFile(p, []byte("modules:\n  terminal: false\n  logs: false\n"), 0o600)
	c, err = LoadAgent(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Enabled("terminal") || c.Enabled("logs") || !c.Enabled("services") || !c.Enabled("metrics") {
		t.Fatalf("override misapplied: %+v", c.Modules)
	}
}
