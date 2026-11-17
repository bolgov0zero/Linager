package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/bolgov0zero/linager/internal/config"
)

func mustRules(t *testing.T, cfg AccessConfig) *accessRules {
	t.Helper()
	_, ar, err := compileAccess(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ar
}

func rules(cidrs ...string) []AccessRule {
	out := []AccessRule{}
	for _, c := range cidrs {
		out = append(out, AccessRule{CIDR: c})
	}
	return out
}

func TestParseAccessRule(t *testing.T) {
	ok := map[string]string{
		"203.0.113.7":        "203.0.113.7/32",
		" 203.0.113.7 ":      "203.0.113.7/32",
		"203.0.113.77/24":    "203.0.113.0/24", // host bits are cleared
		"2001:db8::1":        "2001:db8::1/128",
		"::ffff:203.0.113.7": "203.0.113.7/32", // mapped form is the same address
		"fd00::/8":           "fd00::/8",
	}
	for in, want := range ok {
		got, _, err := ParseAccessRule(AccessRule{CIDR: in})
		if err != nil || got.CIDR != want {
			t.Errorf("%q: got %q err %v, want %q", in, got.CIDR, err, want)
		}
	}
	for _, in := range []string{"", "host.example", "300.1.1.1", "1.2.3.4/33", "1.2.3.4/x", "::ffff:1.2.3.4/120"} {
		if _, _, err := ParseAccessRule(AccessRule{CIDR: in}); err == nil {
			t.Errorf("%q must be refused", in)
		}
	}
	long := make([]rune, maxRuleNote+1)
	for i := range long {
		long[i] = 'я'
	}
	if _, _, err := ParseAccessRule(AccessRule{CIDR: "1.1.1.1", Note: string(long)}); err == nil {
		t.Error("a long comment must be refused")
	}
}

func TestCompileAccessDedupesAndValidates(t *testing.T) {
	cfg, _, err := compileAccess(AccessConfig{Mode: AccessWhite, White: rules("10.0.0.1", "10.0.0.1/32", "10.0.0.2")}, nil)
	if err != nil || len(cfg.White) != 2 || cfg.Retention != defaultAccessRetention {
		t.Fatalf("got %+v err %v", cfg, err)
	}
	if _, _, err := compileAccess(AccessConfig{Mode: "allow-all"}, nil); err == nil {
		t.Error("unknown mode must be refused")
	}
	if _, _, err := compileAccess(AccessConfig{Mode: AccessOff, Retention: 1000}, nil); err == nil {
		t.Error("retention above a year must be refused")
	}
}

func TestDecide(t *testing.T) {
	proxy := netip.MustParseAddr("192.168.1.5")
	lan := netip.MustParseAddr("192.168.1.40")
	cases := []struct {
		name    string
		cfg     AccessConfig
		peer    netip.Addr
		xff     string
		allowed bool
		ip      string
		reason  string
	}{
		{"off lets everybody in", AccessConfig{Mode: AccessOff}, lan, "", true, "192.168.1.40", reasonOff},
		{"white: listed address through the proxy", AccessConfig{Mode: AccessWhite, White: rules("203.0.113.7"), Proxies: rules("192.168.1.5")}, proxy, "203.0.113.7", true, "203.0.113.7", reasonInWhite},
		{"white: unlisted address through the proxy", AccessConfig{Mode: AccessWhite, White: rules("203.0.113.7", "192.168.1.0/24"), Proxies: rules("192.168.1.5")}, proxy, "198.51.100.9", false, "198.51.100.9", reasonNotInWhite},
		{"white: the local network connects directly", AccessConfig{Mode: AccessWhite, White: rules("192.168.1.0/24"), Proxies: rules("192.168.1.5")}, lan, "", true, "192.168.1.40", reasonInWhite},
		{"white: a forged header of a direct client is ignored", AccessConfig{Mode: AccessWhite, White: rules("203.0.113.7"), Proxies: rules("192.168.1.5")}, netip.MustParseAddr("198.51.100.9"), "203.0.113.7", false, "198.51.100.9", reasonNotInWhite},
		{"white: proxy that is not trusted is judged as the proxy itself", AccessConfig{Mode: AccessWhite, White: rules("192.168.1.0/24")}, proxy, "198.51.100.9", true, "192.168.1.5", reasonInWhite},
		{"black: listed address is refused", AccessConfig{Mode: AccessBlack, Black: rules("198.51.100.0/24"), Proxies: rules("192.168.1.5")}, proxy, "198.51.100.9", false, "198.51.100.9", reasonInBlack},
		{"black: other addresses pass", AccessConfig{Mode: AccessBlack, Black: rules("198.51.100.0/24"), Proxies: rules("192.168.1.5")}, proxy, "192.0.2.1", true, "192.0.2.1", reasonNotInBlack},
		{"black: forged header does not hide a banned client", AccessConfig{Mode: AccessBlack, Black: rules("198.51.100.9"), Proxies: rules("192.168.1.5")}, netip.MustParseAddr("198.51.100.9"), "8.8.8.8", false, "198.51.100.9", reasonInBlack},
		{"only through the proxy: direct client is refused", AccessConfig{Mode: AccessBlack, OnlyProxy: true, Proxies: rules("192.168.1.5")}, lan, "", false, "192.168.1.40", reasonOnlyProxy},
		{"only through the proxy: the proxy passes", AccessConfig{Mode: AccessBlack, OnlyProxy: true, Proxies: rules("192.168.1.5")}, proxy, "192.0.2.1", true, "192.0.2.1", reasonNotInBlack},
		{"ipv6 client", AccessConfig{Mode: AccessWhite, White: rules("2001:db8::/32"), Proxies: rules("192.168.1.5")}, proxy, "2001:db8::7", true, "2001:db8::7", reasonInWhite},
		{"mapped peer address", AccessConfig{Mode: AccessWhite, White: rules("192.168.1.0/24")}, netip.MustParseAddr("::ffff:192.168.1.40"), "", true, "192.168.1.40", reasonInWhite},
	}
	for _, c := range cases {
		v := mustRules(t, c.cfg).decide(c.peer, c.xff)
		if v.Allowed != c.allowed || v.IP != c.ip || v.Reason != c.reason {
			t.Errorf("%s: got %+v, want allowed=%v ip=%s reason=%s", c.name, v, c.allowed, c.ip, c.reason)
		}
	}
}

func TestProblems(t *testing.T) {
	proxy := netip.MustParseAddr("192.168.1.5")
	lan := netip.MustParseAddr("192.168.1.40")
	codes := func(ar *accessRules, peer netip.Addr, xff string) string {
		s := ""
		for _, p := range ar.problems(peer, xff) {
			s += p.Code + " "
		}
		return s
	}
	cases := []struct {
		name string
		cfg  AccessConfig
		peer netip.Addr
		xff  string
		want string
	}{
		{"off never complains", AccessConfig{Mode: AccessOff, Proxies: rules("0.0.0.0/0")}, proxy, "203.0.113.7", ""},
		{"trust of everybody", AccessConfig{Mode: AccessBlack, Proxies: rules("0.0.0.0/0")}, proxy, "203.0.113.7", "trust_all "},
		{"proxy that is not in the list", AccessConfig{Mode: AccessWhite, White: rules("192.168.1.0/24")}, proxy, "203.0.113.7", "proxy_unlisted "},
		{"empty white list locks out", AccessConfig{Mode: AccessWhite}, lan, "", "white_empty lockout "},
		{"own address missing from the white list", AccessConfig{Mode: AccessWhite, White: rules("10.0.0.0/8")}, lan, "", "lockout "},
		{"own address in the black list", AccessConfig{Mode: AccessBlack, Black: rules("192.168.1.40")}, lan, "", "lockout "},
		{"direct admin with only-through-proxy", AccessConfig{Mode: AccessBlack, OnlyProxy: true, Proxies: rules("192.168.1.5")}, lan, "", "lockout "},
		{"all good through the proxy", AccessConfig{Mode: AccessWhite, White: rules("203.0.113.7"), Proxies: rules("192.168.1.5")}, proxy, "203.0.113.7", ""},
		{"all good directly", AccessConfig{Mode: AccessWhite, White: rules("192.168.1.0/24")}, lan, "", ""},
	}
	for _, c := range cases {
		if got := codes(mustRules(t, c.cfg), c.peer, c.xff); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestGuardLetsTheLocalHealthProbeIn(t *testing.T) {
	g := newAccessGuard(nil, &ProxyTrust{}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	g.apply(AccessConfig{Mode: AccessWhite, White: rules("203.0.113.7"), OnlyProxy: true, Retention: 7}, true)
	a := &Admin{Cfg: &config.Server{}, Access: g}
	h := a.accessGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	cases := []struct {
		name, method, path, remote, xff, accept string
		want                                    int
	}{
		{"the updater probe from this host", "GET", "/", "127.0.0.1:50000", "", "", 200},
		{"the probe over IPv6 loopback", "GET", "/", "[::1]:50000", "", "", 200},
		{"a browser on this host is judged like anybody", "GET", "/", "127.0.0.1:50000", "", "text/html,application/xhtml+xml", 403},
		{"a request that came through a proxy is judged", "GET", "/", "127.0.0.1:50000", "198.51.100.9", "", 403},
		{"another path is guarded", "GET", "/api/servers", "127.0.0.1:50000", "", "", 403},
		{"a write is guarded", "POST", "/", "127.0.0.1:50000", "", "", 403},
		{"a probe from another host is refused", "GET", "/", "192.0.2.10:50000", "", "", 403},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		req.RemoteAddr = c.remote
		if c.xff != "" {
			req.Header.Set("X-Forwarded-For", c.xff)
		}
		if c.accept != "" {
			req.Header.Set("Accept", c.accept)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: got %d, want %d", c.name, rec.Code, c.want)
		}
	}

	a.Cfg.Admin.BasePath = "/panel"
	req := httptest.NewRequest("GET", "/panel/", nil)
	req.RemoteAddr = "127.0.0.1:50000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("probe under a base path: got %d, want 200", rec.Code)
	}
}
