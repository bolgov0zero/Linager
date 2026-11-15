package server_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// The roles work end to end: an operator reaches the servers and the modules the administrator gave and nothing
// else, an auditor only looks, and the access follows a group (a server added to the group later is included).
func TestRolesOperatorAndAuditor(t *testing.T) {
	base, db := startAccountsServer(t)
	ctx := context.Background()
	admin := mustLogin(t, base, "admin", "a-long-test-password")

	ids := map[string]string{}
	for i, name := range []string{"alpha", "beta", "gamma", "delta"} {
		var id string
		if err := db.Pool.QueryRow(ctx, `INSERT INTO servers (name, agent_pubkey) VALUES ($1,$2) RETURNING id::text`, name, []byte(fmt.Sprintf("key-%d", i))).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[name] = id
	}
	var grp struct {
		ID int64 `json:"id"`
	}
	if c := admin.do("POST", "/api/groups", map[string]string{"name": "web"}, &grp); c != 200 && c != 201 {
		t.Fatalf("create group: %d", c)
	}
	if c := admin.do("PUT", "/api/servers/"+ids["beta"]+"/group", map[string]any{"group_id": grp.ID}, nil); c != 200 {
		t.Fatalf("set group: %d", c)
	}

	// create the two users with their access
	if c := admin.do("POST", "/api/users", map[string]any{"login": "oper", "password": "operator-long-password", "role": "operator",
		"servers": []string{ids["alpha"]}, "groups": []int64{grp.ID}, "modules": []string{"services", "disk"}}, nil); c != 200 {
		t.Fatalf("create operator: %d", c)
	}
	if c := admin.do("POST", "/api/users", map[string]any{"login": "audi", "password": "auditor-long-password", "role": "auditor",
		"servers": []string{ids["alpha"]}, "modules": []string{"docker"}}, nil); c != 200 {
		t.Fatalf("create auditor: %d", c)
	}
	if c := admin.do("POST", "/api/users", map[string]any{"login": "bad", "password": "another-long-password", "role": "operator", "modules": []string{"rm -rf"}}, nil); c != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown module must be refused: %d", c)
	}
	if c := admin.do("POST", "/api/users", map[string]any{"login": "bad2", "password": "another-long-password", "role": "root"}, nil); c != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown role must be refused: %d", c)
	}

	names := func(c *apiClient) map[string]bool {
		var list []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if code := c.do("GET", "/api/servers", nil, &list); code != 200 {
			t.Fatalf("list servers: %d", code)
		}
		out := map[string]bool{}
		for _, s := range list {
			out[s.Name] = true
		}
		return out
	}
	oper := mustLogin(t, base, "oper", "operator-long-password")
	if got := names(oper); len(got) != 2 || !got["alpha"] || !got["beta"] {
		t.Fatalf("the operator sees the single server and the group: %v", got)
	}
	// a server that joins the group later is available at once
	if c := admin.do("PUT", "/api/servers/"+ids["delta"]+"/group", map[string]any{"group_id": grp.ID}, nil); c != 200 {
		t.Fatalf("set group: %d", c)
	}
	if got := names(oper); len(got) != 3 || !got["delta"] || got["gamma"] {
		t.Fatalf("the new server of the group is visible, the other one is not: %v", got)
	}

	code := func(c *apiClient, method, path string, body any) int { return c.do(method, path, body, nil) }
	s := func(name, tail string) string { return "/api/servers/" + ids[name] + tail }
	// the operator
	for _, tc := range []struct {
		method, path string
		body         any
		want         int // 0: the rule let the call through (the agent is offline, so the answer is 503 or similar)
	}{
		{"GET", s("alpha", ""), nil, 200},
		{"GET", s("gamma", ""), nil, 404},
		{"GET", s("gamma", "/services"), nil, 404},
		{"GET", s("alpha", "/services"), nil, 0},
		{"GET", s("alpha", "/disk"), nil, 0},
		{"GET", s("alpha", "/docker"), nil, 403},
		{"GET", s("alpha", "/firewall"), nil, 403},
		{"POST", s("alpha", "/security/ban"), map[string]any{"ip": "203.0.113.9", "confirm": true}, 403},
		{"GET", s("alpha", "/stream?kind=terminal"), nil, 403},
		{"GET", s("alpha", "/files?path=/"), nil, 403},
		{"POST", s("alpha", "/reboot"), map[string]any{"confirm": true}, 403},
		{"DELETE", s("alpha", ""), map[string]any{"name": "alpha"}, 403},
		{"PUT", s("alpha", "/group"), map[string]any{"group_id": grp.ID}, 403},
		{"GET", s("alpha", "/metrics?period=24h"), nil, 0},
		{"GET", "/api/users", nil, 403},
		{"POST", "/api/users", map[string]any{"login": "x1x", "password": "some-long-password-x"}, 403},
		{"PUT", "/api/users/1/access", map[string]any{"role": "admin"}, 403},
		{"GET", "/api/audit", nil, 403},
		{"GET", "/api/updates", nil, 403},
		{"POST", "/api/updates/agents", map[string]any{}, 403},
		{"GET", "/api/enrollments", nil, 403},
		{"GET", "/api/connect", nil, 403},
		{"POST", "/api/groups", map[string]string{"name": "x"}, 403},
		{"GET", "/api/snippets", nil, 403},
		{"POST", "/api/snippets", map[string]string{"name": "n", "command": "id"}, 403},
		{"POST", "/api/dashboard/items", map[string]string{"server_id": ids["gamma"]}, 403},
		{"GET", "/api/me", nil, 200},
		{"GET", "/api/dashboard", nil, 200},
	} {
		got := code(oper, tc.method, tc.path, tc.body)
		if tc.want == 0 && (got == 403 || got == 404) || tc.want != 0 && got != tc.want {
			t.Errorf("operator %s %s: got %d want %d", tc.method, tc.path, got, tc.want)
		}
	}
	var me struct {
		Role    string   `json:"role"`
		Modules []string `json:"modules"`
	}
	oper.do("GET", "/api/me", nil, &me)
	if me.Role != "operator" || len(me.Modules) != 2 {
		t.Fatalf("/api/me tells the role and the modules: %+v", me)
	}
	var dash struct {
		Items []struct{ Name string } `json:"items"`
	}
	oper.do("GET", "/api/dashboard", nil, &dash)
	if len(dash.Items) != 3 {
		t.Fatalf("the dashboard of an operator shows the servers they may see: %+v", dash)
	}

	// the auditor only looks
	audi := mustLogin(t, base, "audi", "auditor-long-password")
	if got := names(audi); len(got) != 1 || !got["alpha"] {
		t.Fatalf("the auditor sees one server: %v", got)
	}
	for _, tc := range []struct {
		method, path string
		body         any
		want         int
	}{
		{"GET", s("alpha", ""), nil, 200},
		{"GET", s("alpha", "/metrics?period=24h"), nil, 0},
		{"GET", s("beta", ""), nil, 404},
		{"GET", s("alpha", "/services"), nil, 403},
		{"GET", s("alpha", "/docker"), nil, 403},
		{"GET", s("alpha", "/stream?kind=logs"), nil, 403},
		{"POST", s("alpha", "/reboot"), map[string]any{"confirm": true}, 403},
		{"DELETE", s("alpha", ""), map[string]any{"name": "alpha"}, 403},
		{"GET", "/api/users", nil, 403},
		{"GET", "/api/audit", nil, 403},
		{"GET", "/api/snippets", nil, 403},
	} {
		got := code(audi, tc.method, tc.path, tc.body)
		if tc.want == 0 && (got == 403 || got == 404) || tc.want != 0 && got != tc.want {
			t.Errorf("auditor %s %s: got %d want %d", tc.method, tc.path, got, tc.want)
		}
	}

	// the administrator changes the access; it works at once, without a new sign-in
	var users struct {
		Users []struct {
			ID    int64
			Login string
			Role  string
		}
	}
	admin.do("GET", "/api/users", nil, &users)
	var operID, adminID int64
	for _, u := range users.Users {
		switch u.Login {
		case "oper":
			operID = u.ID
		case "admin":
			adminID = u.ID
		}
	}
	if c := admin.do("PUT", fmt.Sprintf("/api/users/%d/access", operID), map[string]any{"role": "operator", "servers": []string{ids["gamma"]}, "modules": []string{"docker"}}, nil); c != 200 {
		t.Fatalf("change access: %d", c)
	}
	if got := names(oper); len(got) != 1 || !got["gamma"] {
		t.Fatalf("the new access works at once: %v", got)
	}
	if got := code(oper, "GET", s("gamma", "/docker"), nil); got == 403 || got == 404 {
		t.Fatalf("the module that was given works: %d", got)
	}
	if got := code(oper, "GET", s("gamma", "/services"), nil); got != 403 {
		t.Fatalf("the module that was taken away is closed: %d", got)
	}
	if c := admin.do("PUT", fmt.Sprintf("/api/users/%d/access", adminID), map[string]any{"role": "auditor"}, nil); c != http.StatusConflict {
		t.Fatalf("an administrator cannot demote themselves: %d", c)
	}
	if c := admin.do("PUT", fmt.Sprintf("/api/users/%d/access", operID), map[string]any{"role": "operator", "servers": []string{"00000000-0000-0000-0000-000000000000"}}, nil); c != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown server is refused: %d", c)
	}
	// an operator promoted to administrator gets everything
	if c := admin.do("PUT", fmt.Sprintf("/api/users/%d/access", operID), map[string]any{"role": "admin"}, nil); c != 200 {
		t.Fatalf("promote: %d", c)
	}
	if got := names(oper); len(got) != 4 {
		t.Fatalf("an administrator sees every server: %v", got)
	}
	var n int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='user.access' AND result='ok'`).Scan(&n)
	if n != 2 {
		t.Fatalf("the changes of access go to the audit log: %d", n)
	}
}
