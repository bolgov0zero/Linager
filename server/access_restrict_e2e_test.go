package server_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/bolgov0zero/linager/server"
)

// rawStatus calls the panel without a session and with the given X-Forwarded-For; /api/me answers 401 when the
// request is let in and 403 when the access restriction refuses it.
func rawStatus(t *testing.T, base, xff string) int {
	t.Helper()
	req, _ := http.NewRequest("GET", base+"/api/me", nil)
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

type accessProblems struct {
	Error    string `json:"error"`
	Problems []struct {
		Code   string `json:"code"`
		IP     string `json:"ip"`
		Reason string `json:"reason"`
	} `json:"problems"`
}

func TestAccessRestriction(t *testing.T) {
	base, db := startAccountsServer(t)
	admin := mustLogin(t, base, "admin", "a-long-test-password")

	anon := newClient(t, base)
	if c := anon.do("GET", "/api/access", nil, nil); c != http.StatusUnauthorized {
		t.Fatalf("the settings need a session: %d", c)
	}

	// nothing is stored yet: no restriction, the admin is judged by the connection address
	var st struct {
		Config     server.AccessConfig `json:"config"`
		Configured bool                `json:"configured"`
		You        struct {
			IP          string `json:"ip"`
			PeerTrusted bool   `json:"peer_trusted"`
		} `json:"you"`
	}
	if c := admin.do("GET", "/api/access", nil, &st); c != 200 || st.Config.Mode != "off" || st.Configured || st.You.IP != "127.0.0.1" || st.You.PeerTrusted {
		t.Fatalf("initial state: %d %+v", c, st)
	}

	// a white list without the admin's own address must be refused (lockout), and so must an empty one
	var pr accessProblems
	cfg := server.AccessConfig{Mode: "white", White: []server.AccessRule{{CIDR: "10.0.0.0/8"}}}
	if c := admin.do("PUT", "/api/access", cfg, &pr); c != http.StatusUnprocessableEntity || pr.Error != "access_problems" || len(pr.Problems) != 1 || pr.Problems[0].Code != "lockout" || pr.Problems[0].IP != "127.0.0.1" {
		t.Fatalf("lockout: %d %+v", c, pr)
	}
	if c := admin.do("PUT", "/api/access", server.AccessConfig{Mode: "white", Proxies: []server.AccessRule{{CIDR: "0.0.0.0/0"}}, White: []server.AccessRule{{CIDR: "127.0.0.1"}}}, &pr); c != http.StatusUnprocessableEntity || pr.Problems[0].Code != "trust_all" {
		t.Fatalf("trust of everybody: %d %+v", c, pr)
	}
	if c := admin.do("PUT", "/api/access", server.AccessConfig{Mode: "sideways"}, nil); c != http.StatusBadRequest {
		t.Fatalf("unknown mode: %d", c)
	}
	if c := admin.do("PUT", "/api/access", server.AccessConfig{Mode: "off", White: []server.AccessRule{{CIDR: "not-an-address"}}}, nil); c != http.StatusBadRequest {
		t.Fatalf("bad address: %d", c)
	}
	// the draft check judges the caller and an arbitrary address without storing anything
	var chk struct {
		Problems []struct{ Code string } `json:"problems"`
		You      struct{ Allowed bool }  `json:"you"`
		Test     struct {
			Allowed bool   `json:"allowed"`
			Reason  string `json:"reason"`
		} `json:"test"`
	}
	body := map[string]any{"config": server.AccessConfig{Mode: "white", White: []server.AccessRule{{CIDR: "127.0.0.1"}}, Proxies: []server.AccessRule{{CIDR: "127.0.0.1"}}}, "test": map[string]any{"ip": "198.51.100.9", "proxy": true}}
	if c := admin.do("POST", "/api/access/check", body, &chk); c != 200 || len(chk.Problems) != 0 || !chk.You.Allowed || chk.Test.Allowed || chk.Test.Reason != "not_in_white" {
		t.Fatalf("check: %d %+v", c, chk)
	}
	if rawStatus(t, base, "198.51.100.9") != http.StatusUnauthorized {
		t.Fatal("the check must not change anything")
	}

	// white list: the admin's address, one proxy (the test connects from 127.0.0.1) and a public network
	cfg = server.AccessConfig{Mode: "white", White: []server.AccessRule{{CIDR: "127.0.0.1", Note: "me"}, {CIDR: "203.0.113.0/24"}},
		Proxies: []server.AccessRule{{CIDR: "127.0.0.1"}}}
	if c := admin.do("PUT", "/api/access", cfg, &st); c != 200 || st.Config.Mode != "white" || !st.Configured || !st.You.PeerTrusted {
		t.Fatalf("enable white list: %d %+v", c, st)
	}
	if got := rawStatus(t, base, "203.0.113.7"); got != http.StatusUnauthorized {
		t.Fatalf("a listed address through the proxy must pass: %d", got)
	}
	if got := rawStatus(t, base, "198.51.100.9"); got != http.StatusForbidden {
		t.Fatalf("an unlisted address through the proxy must be refused: %d", got)
	}
	if got := rawStatus(t, base, ""); got != http.StatusUnauthorized {
		t.Fatalf("the admin's own address is listed: %d", got)
	}
	if c := admin.do("GET", "/api/access", nil, nil); c != 200 {
		t.Fatalf("the admin keeps access: %d", c)
	}

	// the journal has the sign-in (before the restriction) and the refused request, with proxy and client address
	var lg struct {
		Rows []struct {
			ClientIP     string `json:"client_ip"`
			RemoteAddr   string `json:"remote_addr"`
			ViaProxy     bool   `json:"via_proxy"`
			ProxyTrusted bool   `json:"proxy_trusted"`
			Event        string `json:"event"`
			Login        string `json:"login"`
			Reason       string `json:"reason"`
		}
	}
	if c := admin.do("GET", "/api/access/log?filter=denied", nil, &lg); c != 200 || len(lg.Rows) != 1 {
		t.Fatalf("denied journal: %d %+v", c, lg)
	}
	if r := lg.Rows[0]; r.ClientIP != "198.51.100.9" || r.RemoteAddr != "127.0.0.1" || !r.ViaProxy || !r.ProxyTrusted || r.Event != "denied" || r.Reason != "not_in_white" {
		t.Fatalf("denied row: %+v", r)
	}
	if rawStatus(t, base, "198.51.100.9") != http.StatusForbidden {
		t.Fatal("still refused")
	}
	admin.do("GET", "/api/access/log?filter=denied", nil, &lg)
	if len(lg.Rows) != 1 {
		t.Fatalf("a repeated refusal of the same address within a minute is one row: %d", len(lg.Rows))
	}
	if c := admin.do("GET", "/api/access/log?filter=login", nil, &lg); c != 200 || len(lg.Rows) == 0 || lg.Rows[len(lg.Rows)-1].Event != "login.ok" || lg.Rows[len(lg.Rows)-1].Login != "admin" {
		t.Fatalf("login journal: %d %+v", c, lg)
	}
	if c := admin.do("GET", "/api/access/log?filter=nonsense", nil, nil); c != http.StatusBadRequest {
		t.Fatalf("bad filter: %d", c)
	}

	// a failed sign-in is journaled with the login that was tried
	bad := newClient(t, base)
	if code, _ := bad.login("admin", "wrong-password-1", nil); code != http.StatusUnauthorized {
		t.Fatalf("bad password: %d", code)
	}
	admin.do("GET", "/api/access/log?filter=fail", nil, &lg)
	if len(lg.Rows) != 1 || lg.Rows[0].Login != "admin" || lg.Rows[0].Reason != "failed" {
		t.Fatalf("failed sign-in journal: %+v", lg.Rows)
	}

	// black list
	cfg = server.AccessConfig{Mode: "black", Black: []server.AccessRule{{CIDR: "198.51.100.0/24"}}, Proxies: []server.AccessRule{{CIDR: "127.0.0.1"}}}
	if c := admin.do("PUT", "/api/access", cfg, nil); c != 200 {
		t.Fatalf("enable black list: %d", c)
	}
	if rawStatus(t, base, "198.51.100.9") != http.StatusForbidden || rawStatus(t, base, "192.0.2.1") != http.StatusUnauthorized {
		t.Fatal("black list: listed refused, others pass")
	}

	// the retention can be changed on its own and is kept in the stored settings
	if c := admin.do("PUT", "/api/access/retention", map[string]int{"days": 7}, &st); c != 200 || st.Config.Retention != 7 || st.Config.Mode != "black" {
		t.Fatalf("retention: %d %+v", c, st)
	}
	if c := admin.do("PUT", "/api/access/retention", map[string]int{"days": 0}, nil); c != http.StatusBadRequest {
		t.Fatalf("bad retention: %d", c)
	}

	// settings changes are in the audit log
	var au struct {
		Items []struct{ Action string } `json:"items"`
	}
	if c := admin.do("GET", "/api/audit?action=access.", nil, &au); c != 200 {
		t.Fatalf("audit: %d", c)
	}
	if len(au.Items) < 3 { // white, black, retention
		t.Fatalf("audit rows of the access settings: %+v", au.Items)
	}

	// `linager access reset`: the restriction goes off and the running server picks it up within seconds
	changed, err := server.ResetAccess(context.Background(), db)
	if err != nil || !changed {
		t.Fatalf("reset: %v %v", changed, err)
	}
	waitFor(t, "the server applies the reset", func() bool { return rawStatus(t, base, "198.51.100.9") == http.StatusUnauthorized })
	if changed, _ := server.ResetAccess(context.Background(), db); changed {
		t.Fatal("a second reset has nothing to change")
	}
}
