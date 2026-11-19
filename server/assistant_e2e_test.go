package server_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/bolgov0zero/linager/internal/assist"
)

// The Assistant is for administrators and for operators who were given the module "assistant" (it is the access of
// SYSTEM to a screen); an operator with other modules and an auditor are refused. The turning off of RDP is a
// confirmed action that needs the module "desktop".
func TestAssistantRights(t *testing.T) {
	base, db := startAccountsServer(t)
	ctx := context.Background()
	admin := mustLogin(t, base, "admin", "a-long-test-password")
	var id string
	if err := db.Pool.QueryRow(ctx, `INSERT INTO servers (name,hostname,address,agent_pubkey,os,agent_version,protocol)
		VALUES ('ws','ws','10.0.0.2','\x01','Windows 11','0.4.0',1) RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	mk := func(login, role string, modules []string) {
		if c := admin.do("POST", "/api/users", map[string]any{"login": login, "password": "operator-long-password", "role": role,
			"servers": []string{id}, "modules": modules}, nil); c != 200 {
			t.Fatalf("create %s: %d", login, c)
		}
	}
	mk("plain", "operator", []string{"services"})
	mk("helper", "operator", []string{"assistant"})
	mk("rdp", "operator", []string{"desktop"})
	mk("viewer", "auditor", nil)

	path := "/api/servers/" + id + "/assistant/settings"
	set := assist.Settings{Colors: assist.Colors256, FPS: 12, Mbit: 3, Auto: true}
	var got assist.Settings
	if c := admin.do("PUT", path, set, nil); c != 200 {
		t.Fatalf("admin saves: %d", c)
	}
	for _, tc := range []struct {
		login            string
		read, write, off int
	}{
		{"plain", 403, 403, 403},
		{"viewer", 403, 403, 403},
		{"helper", 200, 200, 403},
		{"rdp", 403, 403, 409}, // the right to the desktop: it gets as far as the confirmation
	} {
		c := mustLogin(t, base, tc.login, "operator-long-password")
		if code := c.do("GET", path, nil, &got); code != tc.read {
			t.Errorf("%s reads the settings: %d, want %d", tc.login, code, tc.read)
		}
		if code := c.do("PUT", path, set, nil); code != tc.write {
			t.Errorf("%s saves the settings: %d, want %d", tc.login, code, tc.write)
		}
		if code := c.do("POST", "/api/servers/"+id+"/desktop/disable", map[string]any{}, nil); code != tc.off {
			t.Errorf("%s turns RDP off without confirmation: %d, want %d", tc.login, code, tc.off)
		}
		if code := c.do("GET", "/api/servers/"+id+"/assistant?session=console", nil, nil); tc.login != "helper" && code != 403 {
			t.Errorf("%s opens the Assistant: %d, want 403", tc.login, code)
		}
	}
	// the policy for Ctrl+Alt+Del: confirmed, and for the module "assistant" only
	for login, want := range map[string]int{"helper": 409, "plain": 403, "rdp": 403, "viewer": 403} {
		c := mustLogin(t, base, login, "operator-long-password")
		if code := c.do("POST", "/api/servers/"+id+"/assistant/sas", map[string]any{"enable": true}, nil); code != want {
			t.Errorf("%s changes the policy without confirmation: %d, want %d", login, code, want)
		}
	}
	if code := admin.do("POST", "/api/servers/"+id+"/assistant/sas", map[string]any{"enable": true, "confirm": true}, nil); code != http.StatusServiceUnavailable {
		t.Errorf("the policy with an agent that is offline: %d", code)
	}
	// the confirmed turning off reaches the agent, which is not here
	if code := admin.do("POST", "/api/servers/"+id+"/desktop/disable", map[string]any{"confirm": true}, nil); code != http.StatusServiceUnavailable {
		t.Errorf("RDP off with an agent that is offline: %d", code)
	}
	var n int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='assistant.settings'`).Scan(&n); err != nil || n < 2 {
		t.Errorf("the journal records the changes of the settings: %d %v", n, err)
	}
}
