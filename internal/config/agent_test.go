package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestCompleteAgent(t *testing.T) {
	dir := t.TempDir()
	if added, err := CompleteAgent(filepath.Join(dir, "none.yaml")); err != nil || added != nil {
		t.Fatalf("a missing file stays missing: %v %v", added, err)
	}
	p := filepath.Join(dir, "agent.yaml")
	old := "# Linager agent\nmodules:\n  metrics: true\n  terminal: false # no shell here\n  docker: true\n\n# the end\nother: 1\n"
	if err := os.WriteFile(p, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	added, err := CompleteAgent(p)
	if err != nil || len(added) != len(AgentModules)-3 {
		t.Fatalf("every module that was missing is added: %v %v", added, err)
	}
	b, _ := os.ReadFile(p) //nolint:gosec // a file of this test
	text := string(b)
	for _, keep := range []string{"# Linager agent\n", "  terminal: false # no shell here\n", "# the end\nother: 1\n"} {
		if !strings.Contains(text, keep) {
			t.Fatalf("%q must stay as it was:\n%s", keep, text)
		}
	}
	c, err := LoadAgent(p)
	if err != nil || c.Enabled("terminal") || !c.Enabled("disk") || !c.Enabled("firewall") {
		t.Fatalf("an explicit false stays false, the new ones are on: %+v %v", c, err)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("the mode stays: %v", st.Mode())
	}
	if again, _ := CompleteAgent(p); again != nil {
		t.Fatalf("a complete file is not touched again: %v", again)
	}
	// a file that has no block-style modules is left alone
	flow := filepath.Join(dir, "flow.yaml")
	_ = os.WriteFile(flow, []byte("modules: {metrics: true}\n"), 0o600)
	if added, _ := CompleteAgent(flow); added != nil {
		t.Fatalf("flow style is not edited: %v", added)
	}
}
