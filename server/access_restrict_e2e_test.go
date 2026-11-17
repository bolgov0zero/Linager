package server_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/bolgov0zero/linager/internal/config"
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

// A restriction stored in the database must be in force from the very first request of a fresh server: there is no
// window after the start in which the panel is open (the guard used to be loaded only by the periodic reload).
func TestAccessRestrictionAppliesAtStart(t *testing.T) {
	base, db := startAccountsServer(t)
	admin := mustLogin(t, base, "admin", "a-long-test-password")
	cfg := server.AccessConfig{Mode: "white", White: []server.AccessRule{{CIDR: "127.0.0.1"}}, Proxies: []server.AccessRule{{CIDR: "127.0.0.1"}}}
	if c := admin.do("PUT", "/api/access", cfg, nil); c != 200 {
		t.Fatalf("enable white list: %d", c)
	}

	// a second server over the same database, as after a restart; its reload loop is never started
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	adm, err := server.NewAdmin(db, nil, nil, nil, nil, nil, &config.Server{}, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(adm.Handler())
	t.Cleanup(ts.Close)
	if got := rawStatus(t, ts.URL, "198.51.100.9"); got != http.StatusForbidden {
		t.Fatalf("an unlisted address must be refused from the first request: %d", got)
	}
	if got := rawStatus(t, ts.URL, "127.0.0.1"); got != http.StatusUnauthorized {
		t.Fatalf("a listed address must pass: %d", got)
	}
}

// dnsTable is a resolver the test controls.
type dnsTable struct {
	mu   sync.Mutex
	m    map[string][]string
	fail bool
}

func (d *dnsTable) set(host string, ips ...string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.m[host] = ips
}

func (d *dnsTable) setFail(v bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fail = v
}

func (d *dnsTable) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ips, ok := d.m[host]
	if d.fail || !ok {
		return nil, errors.New("no such host")
	}
	var out []netip.Addr
	for _, s := range ips {
		out = append(out, netip.MustParseAddr(s))
	}
	return out, nil
}

// A host name (DDNS) in the white list stands for the addresses it points to now: a new address takes effect when the
// name is refreshed, a DNS outage keeps the last known address, and the last known address survives a restart.
func TestAccessRestrictionHostNames(t *testing.T) {
	dns := &dnsTable{m: map[string][]string{"home.example.org": {"203.0.113.50"}}}
	server.SetHostResolver(dns)
	t.Cleanup(func() { server.SetHostResolver(nil) })
	base, db := startAccountsServer(t)
	admin := mustLogin(t, base, "admin", "a-long-test-password")

	type resolved struct {
		Host   string            `json:"host"`
		Status server.HostStatus `json:"status"`
		Error  string            `json:"error"`
	}
	var rs resolved
	if c := admin.do("POST", "/api/access/resolve", map[string]string{"host": "nx.example.org"}, &rs); c != http.StatusUnprocessableEntity || rs.Error != "unresolved" {
		t.Fatalf("a name that does not exist: %d %+v", c, rs)
	}
	if c := admin.do("POST", "/api/access/resolve", map[string]string{"host": "not a name"}, nil); c != http.StatusBadRequest {
		t.Fatalf("a bad name: %d", c)
	}
	if c := admin.do("POST", "/api/access/resolve", map[string]string{"host": "Home.Example.org"}, &rs); c != 200 || rs.Host != "home.example.org" || len(rs.Status.IPs) != 1 || rs.Status.IPs[0] != "203.0.113.50" {
		t.Fatalf("resolve: %d %+v", c, rs)
	}

	// a name that does not resolve must not be applied; the check says which one
	var pr accessProblems
	cfg := server.AccessConfig{Mode: "white", White: []server.AccessRule{{CIDR: "127.0.0.1"}, {CIDR: "nx.example.org"}}, Proxies: []server.AccessRule{{CIDR: "127.0.0.1"}}}
	if c := admin.do("PUT", "/api/access", cfg, &pr); c != http.StatusUnprocessableEntity || len(pr.Problems) != 1 || pr.Problems[0].Code != "host_unresolved" {
		t.Fatalf("unresolved name: %d %+v", c, pr)
	}
	// a name among the trusted proxies is refused
	if c := admin.do("PUT", "/api/access", server.AccessConfig{Mode: "off", Proxies: []server.AccessRule{{CIDR: "home.example.org"}}}, nil); c != http.StatusBadRequest {
		t.Fatalf("a name as a proxy: %d", c)
	}

	var st struct {
		Config server.AccessConfig          `json:"config"`
		Hosts  map[string]server.HostStatus `json:"hosts"`
	}
	cfg.White[1] = server.AccessRule{CIDR: "Home.Example.org", Note: "my home"}
	if c := admin.do("PUT", "/api/access", cfg, &st); c != 200 || st.Config.White[1].CIDR != "home.example.org" || len(st.Hosts["home.example.org"].IPs) != 1 {
		t.Fatalf("apply: %d %+v", c, st)
	}
	if rawStatus(t, base, "203.0.113.50") != http.StatusUnauthorized {
		t.Fatal("the address of the name must pass")
	}
	if rawStatus(t, base, "203.0.113.51") != http.StatusForbidden {
		t.Fatal("another address must be refused")
	}
	if raw := db.SettingRaw(context.Background(), "access_hosts"); !strings.Contains(string(raw), "203.0.113.50") {
		t.Fatalf("the answer must be stored: %s", raw)
	}

	// DNS is down: the last known address stays in force and the status says so
	dns.setFail(true)
	if c := admin.do("POST", "/api/access/resolve", map[string]string{"host": "home.example.org"}, &rs); c != 200 || !rs.Status.Failing || len(rs.Status.IPs) != 1 {
		t.Fatalf("outage: %d %+v", c, rs)
	}
	if rawStatus(t, base, "203.0.113.50") != http.StatusUnauthorized {
		t.Fatal("the last known address must stay in force")
	}

	// a restarted server (DNS still down) knows the last address from the database
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	adm, err := server.NewAdmin(db, nil, nil, nil, nil, nil, &config.Server{}, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(adm.Handler())
	t.Cleanup(ts.Close)
	if rawStatus(t, ts.URL, "203.0.113.50") != http.StatusUnauthorized || rawStatus(t, ts.URL, "203.0.113.51") != http.StatusForbidden {
		t.Fatal("a restarted server must judge by the stored address")
	}

	// the provider gave the home a new address; refreshing the name puts it in force at once
	dns.setFail(false)
	dns.set("home.example.org", "203.0.113.51")
	if c := admin.do("POST", "/api/access/resolve", map[string]string{"host": "home.example.org"}, &rs); c != 200 || rs.Status.IPs[0] != "203.0.113.51" || rs.Status.Failing {
		t.Fatalf("refresh: %d %+v", c, rs)
	}
	if rawStatus(t, base, "203.0.113.51") != http.StatusUnauthorized || rawStatus(t, base, "203.0.113.50") != http.StatusForbidden {
		t.Fatal("the new address must replace the old one")
	}
}
