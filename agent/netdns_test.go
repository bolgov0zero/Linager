//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPickDNSBackend(t *testing.T) {
	cases := []struct {
		name     string
		p        dnsProbe
		backend  string
		editable bool
	}{
		{"netplan host", dnsProbe{Netplan: true, Link: true, Target: "../run/systemd/resolve/stub-resolv.conf"}, "netplan", false},
		{"resolved stub", dnsProbe{IfUpdown: true, Link: true, Target: "../run/systemd/resolve/stub-resolv.conf"}, dnsResolved, true},
		{"resolved absolute", dnsProbe{Link: true, Target: "/run/systemd/resolve/resolv.conf"}, dnsResolved, true},
		{"resolved wins over resolvconf stanza", dnsProbe{IfUpdown: true, ResolvconfBin: true, StanzaHasDNS: true, StanzaFound: true, Link: true, Target: "/run/systemd/resolve/stub-resolv.conf"}, dnsResolved, true},
		{"resolvconf with dns lines", dnsProbe{IfUpdown: true, ResolvconfBin: true, StanzaHasDNS: true, StanzaFound: true, Link: true, Target: "../run/resolvconf/resolv.conf"}, dnsResolvconf, true},
		{"resolvconf link without dns lines", dnsProbe{IfUpdown: true, ResolvconfBin: true, StanzaFound: true, Link: true, Target: "/run/resolvconf/resolv.conf"}, dnsResolvconf, true},
		{"resolvconf, regular file, dns lines", dnsProbe{IfUpdown: true, ResolvconfBin: true, StanzaHasDNS: true, StanzaFound: true}, dnsResolvconf, true},
		{"resolvconf but stanza not editable", dnsProbe{IfUpdown: true, ResolvconfBin: true, StanzaHasDNS: true}, dnsResolvconf, false},
		{"dns lines but no resolvconf binary", dnsProbe{IfUpdown: true, StanzaHasDNS: true, StanzaFound: true}, dnsFile, true},
		{"plain file", dnsProbe{IfUpdown: true, StanzaFound: true}, dnsFile, true},
		{"plain file, no backend", dnsProbe{}, dnsFile, true},
		{"foreign symlink", dnsProbe{Link: true, Target: "/etc/foo/resolv.conf"}, "unknown", false},
		{"resolvconf link without the binary", dnsProbe{IfUpdown: true, Link: true, Target: "/run/resolvconf/resolv.conf"}, "unknown", false},
	}
	for _, c := range cases {
		b, e := pickDNSBackend(c.p)
		if b != c.backend || e != c.editable {
			t.Errorf("%s: got %s/%v, want %s/%v", c.name, b, e, c.backend, c.editable)
		}
	}
}

func TestValidateDNS(t *testing.T) {
	cases := []struct {
		name       string
		ns, search []string
		wantNS     []string
		wantSearch []string
		bad        bool
	}{
		{"ok", []string{"1.1.1.1", " 2606:4700:4700::1111 "}, []string{"Example.COM.", "lan"}, []string{"1.1.1.1", "2606:4700:4700::1111"}, []string{"example.com", "lan"}, false},
		{"dedupe", []string{"8.8.8.8", "8.8.8.8"}, nil, []string{"8.8.8.8"}, nil, false},
		{"empty rows are dropped", []string{"", "9.9.9.9", " "}, []string{""}, []string{"9.9.9.9"}, nil, false},
		{"none", nil, nil, nil, nil, true},
		{"four", []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4"}, nil, nil, nil, true},
		{"hostname instead of ip", []string{"dns.google"}, nil, nil, nil, true},
		{"zone id", []string{"fe80::1%eth0"}, nil, nil, nil, true},
		{"bad ip", []string{"300.1.1.1"}, nil, nil, nil, true},
		{"bad domain underscore", []string{"1.1.1.1"}, []string{"a_b.com"}, nil, nil, true},
		{"bad domain hyphen", []string{"1.1.1.1"}, []string{"-a.com"}, nil, nil, true},
		{"bad domain empty label", []string{"1.1.1.1"}, []string{"a..com"}, nil, nil, true},
		{"seven domains", []string{"1.1.1.1"}, []string{"a.x", "b.x", "c.x", "d.x", "e.x", "f.x", "g.x"}, nil, nil, true},
		{"six domains", []string{"1.1.1.1"}, []string{"a.x", "b.x", "c.x", "d.x", "e.x", "f.x"}, []string{"1.1.1.1"}, []string{"a.x", "b.x", "c.x", "d.x", "e.x", "f.x"}, false},
		{"too long search line", []string{"1.1.1.1"}, []string{strings.Repeat("a", 60) + ".com", strings.Repeat("b", 60) + ".com", strings.Repeat("c", 60) + ".com", strings.Repeat("d", 60) + ".com", strings.Repeat("e", 60) + ".com"}, nil, nil, true},
	}
	for _, c := range cases {
		ns, search, err := validateDNS(c.ns, c.search)
		if (err != nil) != c.bad {
			t.Errorf("%s: err=%v, want bad=%v", c.name, err, c.bad)
			continue
		}
		if c.bad {
			continue
		}
		if !reflect.DeepEqual(ns, c.wantNS) || !reflect.DeepEqual(search, c.wantSearch) {
			t.Errorf("%s: got %v %v, want %v %v", c.name, ns, search, c.wantNS, c.wantSearch)
		}
	}
}

func TestRenderResolvConf(t *testing.T) {
	cases := []struct{ name, old, want string }{
		{"keeps options and comments", "# Generated\noptions edns0 timeout:2\nnameserver 10.0.0.1\nnameserver 10.0.0.2\nsearch old.lan\n",
			"# Generated\noptions edns0 timeout:2\nnameserver 1.1.1.1\nnameserver 9.9.9.9\nsearch example.com lan\n"},
		{"domain line becomes search", "domain old.lan\nnameserver 10.0.0.1\n", "search example.com lan\nnameserver 1.1.1.1\nnameserver 9.9.9.9\n"},
		{"no previous lines", "options rotate\n", "options rotate\nnameserver 1.1.1.1\nnameserver 9.9.9.9\nsearch example.com lan\n"},
		{"empty file", "", "nameserver 1.1.1.1\nnameserver 9.9.9.9\nsearch example.com lan\n"},
	}
	for _, c := range cases {
		if got := renderResolvConf(c.old, []string{"1.1.1.1", "9.9.9.9"}, []string{"example.com", "lan"}); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
	if got := renderResolvConf("nameserver 1.1.1.1\nsearch a.b\noptions x\n", []string{"8.8.8.8"}, nil); got != "nameserver 8.8.8.8\noptions x\n" {
		t.Errorf("empty search removes the line, got %q", got)
	}
	rc := parseResolvConf(renderResolvConf("", []string{"::1", "1.1.1.1"}, []string{"a.b"}))
	if !reflect.DeepEqual(rc.ns, []string{"::1", "1.1.1.1"}) || !reflect.DeepEqual(rc.search, []string{"a.b"}) {
		t.Errorf("round trip: %+v", rc)
	}
}

func TestRenderResolvedDropIn(t *testing.T) {
	got := renderResolvedDropIn([]string{"1.1.1.1", "2606:4700:4700::1111"}, []string{"example.com"})
	for _, want := range []string{"[Resolve]\n", "DNS=1.1.1.1 2606:4700:4700::1111\n", "Domains=example.com\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(renderResolvedDropIn([]string{"1.1.1.1"}, nil), "Domains=") {
		t.Error("no Domains line without search domains")
	}
}

func TestSetStanzaDNS(t *testing.T) {
	ns, search, has := stanzaDNS(sampleInterfaces, "eth0")
	if !has || !reflect.DeepEqual(ns, []string{"1.1.1.1"}) || search != nil {
		t.Fatalf("stanzaDNS: %v %v %v", ns, search, has)
	}
	if _, _, has := stanzaDNS(sampleInterfaces, "eth1"); has {
		t.Fatal("eth1 has no dns lines")
	}
	out, err := setStanzaDNS(sampleInterfaces, "eth0", []string{"9.9.9.9", "8.8.8.8"}, []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"    dns-nameservers 9.9.9.9 8.8.8.8\n", "    dns-search example.com\n", "    hwaddress ether 52:54:00:aa:bb:cc", "iface eth1 inet static", "    address 10.1.0.5/24", "source /etc/network/interfaces.d/*", "# comment"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "dns-nameservers 1.1.1.1") {
		t.Errorf("the old dns line must go:\n%s", out)
	}
	// a stanza without dns lines gets them at its end, other stanzas untouched
	out, err = setStanzaDNS(sampleInterfaces, "eth1", []string{"1.0.0.1"}, nil)
	if err != nil || !strings.Contains(out, "    mtu 9000\n    dns-nameservers 1.0.0.1\n") || strings.Contains(out, "dns-search") {
		t.Errorf("eth1: %v\n%s", err, out)
	}
	if _, err := setStanzaDNS(sampleInterfaces, "eth9", []string{"1.0.0.1"}, nil); err == nil {
		t.Error("an interface without a stanza is refused")
	}
	// a later edit of the same text reads back what was written
	back, bsearch, _ := stanzaDNS(out, "eth1")
	if !reflect.DeepEqual(back, []string{"1.0.0.1"}) || bsearch != nil {
		t.Errorf("read back %v %v", back, bsearch)
	}
}

func newDNSTestModule(t *testing.T, resolv string) (*netModule, *[]string) {
	t.Helper()
	dir := t.TempDir()
	var calls []string
	pend := NewPending(filepath.Join(dir, "data"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	m := &netModule{pend: pend, dir: filepath.Join(dir, "data"), ifPath: filepath.Join(dir, "interfaces"), npDir: filepath.Join(dir, "np"),
		resolvPath: resolv, resolvedD: filepath.Join(dir, "resolved.conf.d"),
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return nil, nil
		}}
	pend.Register("network", m.kind())
	return m, &calls
}

func TestDNSFileBackendAppliesAndRollsBack(t *testing.T) {
	probe := newNetModule(nil, t.TempDir())
	if probe.backend() == "netplan" {
		t.Skip("the host runs netplan, which disables DNS editing")
	}
	resolv := filepath.Join(t.TempDir(), "resolv.conf")
	orig := "# Generated by hand\noptions edns0\nnameserver 10.0.0.1\nsearch old.lan\n"
	if err := os.WriteFile(resolv, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	m, _ := newDNSTestModule(t, resolv)
	req := json.RawMessage(`{"id":"c1","nameservers":["1.1.1.1","9.9.9.9"],"search":["example.com"]}`)
	if _, err := m.dns(context.Background(), req, false); err == nil {
		t.Fatal("a DNS change needs the dangerous flag")
	}
	if _, err := m.dns(context.Background(), json.RawMessage(`{"id":"c1","nameservers":["nope"]}`), true); err == nil {
		t.Fatal("an invalid address is refused")
	}
	if _, err := m.dns(context.Background(), req, true); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(resolv) //nolint:gosec // a temp file of this test
	if string(got) != "# Generated by hand\noptions edns0\nnameserver 1.1.1.1\nnameserver 9.9.9.9\nsearch example.com\n" {
		t.Fatalf("new content: %q", got)
	}
	if fi, _ := os.Stat(resolv); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v, want 0644", fi.Mode().Perm())
	}
	if _, err := m.dns(context.Background(), json.RawMessage(`{"id":"c2","nameservers":["8.8.8.8"]}`), true); err == nil {
		t.Fatal("only one pending change at a time")
	}
	if err := m.pend.Rollback("c1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(resolv); string(got) != orig { //nolint:gosec // a temp file of this test
		t.Fatalf("the old content must come back, got %q", got)
	}
}

func TestDNSResolvedDropInRollbackAndTimeout(t *testing.T) {
	old := PendingWindow
	PendingWindow = 80 * time.Millisecond
	defer func() { PendingWindow = old }()
	dir := t.TempDir()
	stub := filepath.Join(dir, "stub-resolv.conf")
	if err := os.WriteFile(stub, []byte("nameserver 127.0.0.53\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "resolv.conf")
	if err := os.Symlink("../run/systemd/resolve/stub-resolv.conf", link); err != nil {
		t.Skip("no symlinks here")
	}
	m, calls := newDNSTestModule(t, link)
	if m.backend() == "netplan" {
		t.Skip("the host runs netplan, which disables DNS editing")
	}
	if _, err := m.dns(context.Background(), json.RawMessage(`{"id":"r1","nameservers":["1.1.1.1"],"search":["example.com"]}`), true); err != nil {
		t.Fatal(err)
	}
	dropIn := filepath.Join(m.resolvedD, "linager.conf")
	if b, _ := os.ReadFile(dropIn); string(b) != renderResolvedDropIn([]string{"1.1.1.1"}, []string{"example.com"}) { //nolint:gosec // a temp file of this test
		t.Fatalf("drop-in: %q", b)
	}
	if len(*calls) == 0 || !strings.Contains((*calls)[len(*calls)-1], "restart systemd-resolved") {
		t.Fatalf("resolved was not restarted: %v", *calls)
	}
	time.Sleep(400 * time.Millisecond) // not confirmed: the agent rolls back by itself
	if _, err := os.Stat(dropIn); err == nil {
		t.Fatal("an unconfirmed drop-in must be removed")
	}
	if n := len(*calls); !strings.Contains((*calls)[n-1], "restart systemd-resolved") || n < 2 {
		t.Fatalf("resolved must be restarted after the rollback: %v", *calls)
	}
}

func TestDNSResolvconfRollsBackLiveEntry(t *testing.T) {
	dir := t.TempDir()
	m, _ := newDNSTestModule(t, filepath.Join(dir, "none"))
	var stdin []string
	m.runStdin = func(_ context.Context, in, name string, args ...string) ([]byte, error) {
		stdin = append(stdin, name+" "+strings.Join(args, " ")+" <<"+in)
		return nil, nil
	}
	d := netPendingData{Cand: filepath.Join(dir, "cand"), DNS: &dnsPending{Backend: dnsResolvconf, Iface: "eth0", HadDNS: true, OldNS: []string{"1.1.1.1"}, OldSearch: []string{"a.b"}}}
	if err := m.dnsRollback(d); err != nil {
		t.Fatal(err)
	}
	if len(stdin) != 1 || stdin[0] != "resolvconf -a eth0.inet <<nameserver 1.1.1.1\nsearch a.b\n" {
		t.Fatalf("old entry not restored: %q", stdin)
	}
	d.DNS.HadDNS, d.DNS.OldNS = false, nil
	var calls []string
	m.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	if err := m.dnsRollback(d); err != nil || len(calls) != 1 || calls[0] != "resolvconf -d eth0.inet" {
		t.Fatalf("entry not removed: %v %v", err, calls)
	}
}
