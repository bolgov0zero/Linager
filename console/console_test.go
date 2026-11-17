package console

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bolgov0zero/linager/installer"
)

func layoutIn(t *testing.T, server, agent bool) Layout {
	t.Helper()
	d := t.TempDir()
	l := Layout{
		ServerConfig: filepath.Join(d, "server.yaml"), ServerUnit: filepath.Join(d, "linager-server.service"),
		AgentDir: filepath.Join(d, "agent"), AgentConfig: filepath.Join(d, "agent.yaml"), AgentUnit: filepath.Join(d, "linager-agent.service"),
	}
	if server {
		if err := os.WriteFile(l.ServerUnit, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if agent {
		if err := os.WriteFile(l.AgentUnit, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func TestDetect(t *testing.T) {
	cases := []struct {
		server, agent bool
		want          []Role
	}{
		{false, false, nil},
		{true, false, []Role{RoleServer}},
		{false, true, []Role{RoleAgent}},
		{true, true, []Role{RoleServer, RoleAgent}},
	}
	for _, c := range cases {
		got := layoutIn(t, c.server, c.agent).Detect()
		if len(got) != len(c.want) {
			t.Fatalf("server=%v agent=%v: got %v, want %v", c.server, c.agent, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		}
	}
	// a state file alone is enough for the agent, a closed directory is not
	l := layoutIn(t, false, false)
	if err := os.MkdirAll(l.AgentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.AgentDir, "state.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := l.Detect(); len(got) != 1 || got[0] != RoleAgent {
		t.Fatalf("state file: got %v", got)
	}
}

func newTestModel(t *testing.T, server, agent bool) *model {
	t.Helper()
	p := NewProbe(layoutIn(t, server, agent), "0.1.0")
	m := newModel(Options{Lang: installer.EN, Version: "0.1.0"}, p)
	m.root = true
	return m
}

func press(m *model, k string) tea.Cmd {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	}
	_, cmd := m.Update(msg)
	return cmd
}

func isQuit(c tea.Cmd) bool {
	if c == nil {
		return false
	}
	_, ok := c().(tea.QuitMsg)
	return ok
}

func TestPickerAndNavigation(t *testing.T) {
	m := newTestModel(t, true, true)
	if m.scr != scrPick {
		t.Fatalf("both roles must start at the picker, got %v", m.scr)
	}
	if v := m.View(); !strings.Contains(v, "╦═╗") || !strings.Contains(v, "SERVER") || !strings.Contains(v, "AGENT") {
		t.Fatalf("picker view misses the logo or cards:\n%s", v)
	}
	press(m, "2")
	if m.scr != scrMenu || m.role != RoleAgent {
		t.Fatalf("digit 2 must open the agent menu, got scr=%v role=%v", m.scr, m.role)
	}
	press(m, "0") // back to the picker, not out of the program
	if m.scr != scrPick {
		t.Fatalf("0 in a menu with two roles goes back, got %v", m.scr)
	}
	if !isQuit(press(m, "0")) {
		t.Fatal("0 on the picker must quit")
	}
}

func TestSingleRoleAndInstall(t *testing.T) {
	m := newTestModel(t, true, false)
	if m.scr != scrMenu || m.role != RoleServer {
		t.Fatalf("one role opens its menu, got %v %v", m.scr, m.role)
	}
	if !isQuit(press(m, "0")) {
		t.Fatal("0 in the only menu must quit")
	}
	n := newTestModel(t, false, false)
	if n.scr != scrInstall || !strings.Contains(n.View(), "Install the server") {
		t.Fatalf("no roles: install menu expected, got %v", n.scr)
	}
	// the roles are re-read: after an installation the menu follows
	if err := os.WriteFile(n.probe.Layout.AgentUnit, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	n.Update(statusMsg{roles: n.probe.Layout.Detect()})
	if n.scr != scrMenu || n.role != RoleAgent {
		t.Fatalf("after install the agent menu expected, got %v %v", n.scr, n.role)
	}
}

func TestConfirmDefaultsToNo(t *testing.T) {
	m := newTestModel(t, true, false)
	ran := false
	m.askConfirm("sure?", func() tea.Cmd { ran = true; return nil })
	press(m, "enter")
	if ran || m.scr != scrMenu {
		t.Fatal("Enter must cancel a dangerous action")
	}
	m.askConfirm("sure?", func() tea.Cmd { ran = true; return nil })
	press(m, "y")
	if !ran {
		t.Fatal("y must confirm")
	}
}

func TestPasswordFormChecksRepeat(t *testing.T) {
	m := newTestModel(t, true, false)
	m.srv = &ServerStatus{Users: 1}
	m.openPassword()
	press(m, "enter") // login "admin"
	for _, r := range "secret" {
		press(m, string(r))
	}
	press(m, "enter")
	for _, r := range "other" {
		press(m, string(r))
	}
	press(m, "enter")
	if m.scr != scrForm || m.form.err == "" || m.form.idx != 1 {
		t.Fatalf("a mismatch must stay in the form on the password field: scr=%v err=%q idx=%d", m.scr, m.form.err, m.form.idx)
	}
}

func TestDictionariesMatch(t *testing.T) {
	for k := range dict[installer.RU] {
		if _, ok := dict[installer.EN][k]; !ok {
			t.Errorf("key %q has no English text", k)
		}
	}
	for k := range dict[installer.EN] {
		if _, ok := dict[installer.RU][k]; !ok {
			t.Errorf("key %q has no Russian text", k)
		}
	}
}

func TestSpan(t *testing.T) {
	cases := map[time.Duration]string{
		10 * time.Second:             "less than a minute",
		7 * time.Minute:              "7 min",
		5*time.Hour + 12*time.Minute: "5 h 12 min",
		3*24*time.Hour + 4*time.Hour: "3 d 4 h",
	}
	for d, want := range cases {
		if got := span(installer.EN, d); got != want {
			t.Errorf("span(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestNarrowAndNoData(t *testing.T) {
	m := newTestModel(t, true, true)
	m.w = 44
	if v := m.View(); !strings.Contains(v, "SERVER") {
		t.Fatalf("narrow picker broke:\n%s", v)
	}
	press(m, "1")
	m.srv = &ServerStatus{Service: Service{State: "active"}, NeedRoot: true, Clients: -1, Pending: -1}
	m.root = false
	if v := m.View(); !strings.Contains(v, "sudo linager") || !strings.Contains(v, "root needed") {
		t.Fatalf("a user without root must see the hint:\n%s", v)
	}
}
