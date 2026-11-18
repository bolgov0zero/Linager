package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bolgov0zero/linager/internal/gwnet"
	"github.com/bolgov0zero/linager/internal/protocol"
)

const arpFixture = `IP address       HW type     Flags       HW address            Mask     Device
192.168.1.1      0x1         0x2         A4:5E:60:1C:07:2B     *        eth0
192.168.1.133    0x1         0x2         24:6f:28:b3:0c:5a     *        eth0
192.168.1.200    0x1         0x0         00:00:00:00:00:00     *        eth0
192.168.1.201    0x1         0x2         00:00:00:00:00:00     *        eth0
192.168.20.15    0x1         0x6         50:8a:06:44:d2:19     *        eth0.20
garbage
`

func TestParseARP(t *testing.T) {
	got := parseARP(strings.NewReader(arpFixture))
	if len(got) != 3 {
		t.Fatalf("the complete entries only: %v", got)
	}
	if got[0].IP.String() != "192.168.1.1" || got[0].MAC != "a4:5e:60:1c:07:2b" || got[0].Iface != "eth0" {
		t.Errorf("the first entry (the MAC is lower case): %+v", got[0])
	}
	if got[2].Iface != "eth0.20" {
		t.Errorf("a permanent entry (flags 0x6 has the complete bit) is kept: %+v", got[2])
	}
}

const routeFixture = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
eth0	00000000	0101A8C0	0003	0	0	100	00000000	0	0	0
eth0	0001A8C0	00000000	0001	0	0	100	00FFFFFF	0	0	0
eth0.20	0014A8C0	00000000	0001	0	0	0	00FFFFFF	0	0	0
eth1	0000A8C0	0200A8C0	0000	0	0	0	0000FFFF	0	0	0
`

func TestRoutes(t *testing.T) {
	routes := parseRoutes(strings.NewReader(routeFixture))
	if len(routes) != 3 { // the one that is not up (flags 0) is dropped
		t.Fatalf("routes: %+v", routes)
	}
	r, ok := bestRoute(routes, netip.MustParseAddr("192.168.1.77"))
	if !ok || !r.Direct || r.Iface != "eth0" || r.Dest.String() != "192.168.1.0/24" {
		t.Errorf("an attached network wins over the default route: %+v", r)
	}
	r, ok = bestRoute(routes, netip.MustParseAddr("10.10.0.5"))
	if !ok || r.Direct || r.Gate.String() != "192.168.1.1" || r.Dest.Bits() != 0 {
		t.Errorf("anything else goes to the default gateway: %+v", r)
	}
	if _, ok := bestRoute(nil, netip.MustParseAddr("10.10.0.5")); ok {
		t.Errorf("no routes, no way")
	}
	if maskBits(netip.MustParseAddr("0.255.255.255")) != -1 {
		t.Errorf("a mask with holes is not a mask")
	}
}

func fakeIfs() []gwIface {
	return []gwIface{
		{Name: "eth0", Addr: netip.MustParseAddr("192.168.1.40"), Subnet: netip.MustParsePrefix("192.168.1.0/24")},
		{Name: "eth0.20", Addr: netip.MustParseAddr("192.168.20.2"), Subnet: netip.MustParsePrefix("192.168.20.0/24")},
		{Name: "eth2", Addr: netip.MustParseAddr("10.1.2.3"), Subnet: netip.MustParsePrefix("10.1.0.0/22")},
	}
}

func TestOwnNets(t *testing.T) {
	own := gwOwnNets(fakeIfs())
	if len(own) != 3 || own[1].VLAN != "VLAN 20" || own[0].VLAN != "" {
		t.Fatalf("own networks: %+v", own)
	}
	if own[2].CIDR != "10.1.2.0/24" { // a card on a /22 offers the /24 around its address
		t.Errorf("the /24 around the address: %+v", own[2])
	}
	if f, ok := gwOwnIface(fakeIfs(), netip.MustParsePrefix("10.1.1.0/24")); !ok || f.Name != "eth2" {
		t.Errorf("another /24 of the same /22 is directly connected too: %+v %v", f, ok)
	}
	if _, ok := gwOwnIface(fakeIfs(), netip.MustParsePrefix("10.10.0.0/24")); ok {
		t.Errorf("a network behind a router is not own")
	}
	for _, n := range []string{"docker0", "br-1a2b", "veth12", "virbr0", "lo"} {
		if !gwVirtualName(n) {
			t.Errorf("%s is a virtual card", n)
		}
	}
	if gwVirtualName("eth0") || gwVirtualName("enp3s0") || gwVirtualName("wlan0") {
		t.Errorf("a real card is not virtual")
	}
}

func TestValidateGatewayConfig(t *testing.T) {
	routes := []gwRouteEntry{{Iface: "eth0", Dest: netip.MustParsePrefix("0.0.0.0/0"), Gate: netip.MustParseAddr("192.168.1.1")}}
	cfg := func(nets ...protocol.GatewayNetCfg) protocol.GatewayConfig {
		return protocol.GatewayConfig{Enabled: true, Interval: 300, Nets: nets}
	}
	net := func(c string, public bool) protocol.GatewayNetCfg {
		return protocol.GatewayNetCfg{CIDR: c, On: true, Ports: true, Names: true, Public: public}
	}
	if err := validateGatewayConfig(cfg(net("192.168.1.0/24", false), net("10.10.0.0/24", false)), fakeIfs(), routes); err != nil {
		t.Fatalf("a good configuration: %v", err)
	}
	bad := map[string]protocol.GatewayConfig{
		"interval too short": {Enabled: true, Interval: 10, Nets: nil},
		"interval too long":  {Enabled: true, Interval: 999999},
		"loopback":           cfg(net("127.0.0.0/24", false)),
		"too big":            cfg(net("10.0.0.0/16", false)),
		"public unconfirmed": cfg(net("203.0.113.0/24", false)),
		"overlap":            cfg(net("10.10.0.0/24", false), net("10.10.0.0/25", false)),
		"syntax":             cfg(net("hello", false)),
	}
	for name, c := range bad {
		if validateGatewayConfig(c, fakeIfs(), routes) == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if err := validateGatewayConfig(cfg(net("203.0.113.0/24", true)), fakeIfs(), routes); err != nil {
		t.Errorf("a confirmed public network is allowed: %v", err)
	}
	many := cfg()
	for i := 0; i < 17; i++ {
		many.Nets = append(many.Nets, net("10."+string(rune('0'+i/10))+"."+string(rune('0'+i%10))+".0/24", false))
	}
	// 17 networks, and the names above are not even valid: the count is checked first
	if err := validateGatewayConfig(many, fakeIfs(), routes); err == nil {
		t.Errorf("17 networks must be refused")
	}
	if err := validateGatewayConfig(cfg(net("10.10.0.0/24", false)), fakeIfs(), nil); err == nil || !strings.Contains(err.Error(), "no route") {
		t.Errorf("a network the agent cannot reach is refused with a reason: %v", err)
	}
	if err := validateGatewayConfig(cfg(net("192.168.1.0/24", false)), fakeIfs(), nil); err != nil {
		t.Errorf("an own network needs no route: %v", err)
	}
	off := cfg(net("10.10.0.0/24", false))
	off.Enabled = false
	if err := validateGatewayConfig(off, fakeIfs(), nil); err != nil {
		t.Errorf("routes are not checked while scanning is off: %v", err)
	}
}

func TestBanners(t *testing.T) {
	raw := []byte("HTTP/1.1 200 OK\r\nServer: nginx/1.22.1\r\nContent-Type: text/html\r\n\r\n<html><head><TITLE> Synology\n DiskStation </TITLE></head>")
	if got := parseHTTPBanner(raw); got != "nginx/1.22.1 · Synology DiskStation" {
		t.Errorf("server and title: %q", got)
	}
	if got := parseHTTPBanner([]byte("HTTP/1.0 200 OK\r\nServer: lighttpd\r\n\r\n")); got != "lighttpd" {
		t.Errorf("server only: %q", got)
	}
	if got := parseHTTPBanner([]byte("garbage")); got != "" {
		t.Errorf("no banner: %q", got)
	}
	if got := sanitizeBanner("a\x00b\x1b[31m\tc\r\n" + strings.Repeat("x", 300)); len([]rune(got)) > gwBannerMax || strings.ContainsAny(got, "\x00\x1b\r\n\t") {
		t.Errorf("control characters out and the text cut: %q", got)
	}

	for _, c := range []struct{ in, want string }{
		{"SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u3\r\nrest", "SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u3"},
		{"220 smtp ready\r\n", ""}, // not SSH
	} {
		a, b := net.Pipe()
		go func(b net.Conn, data string) { _, _ = b.Write([]byte(data)); _ = b.Close() }(b, c.in)
		if got := readSSHBanner(a); got != c.want {
			t.Errorf("banner of %q: %q, want %q", c.in, got, c.want)
		}
		_ = a.Close()
	}
}

func TestICMPChecksum(t *testing.T) {
	p := echoRequest(0x1234, 7)
	if icmpChecksum(p) != 0 {
		t.Errorf("a packet with its checksum sums to zero")
	}
	if p[0] != 8 || p[4] != 0x12 || p[5] != 0x34 || p[7] != 7 {
		t.Errorf("echo request fields: %v", p[:8])
	}
}

func TestTCPProbe(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback listener")
	}
	port := l.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	ctx := context.Background()
	open, alive := gwTCP(ctx, netip.MustParseAddr("127.0.0.1"), port)
	if !open || !alive {
		t.Errorf("an open port: open=%v alive=%v", open, alive)
	}
	_ = l.Close()
	open, alive = gwTCP(ctx, netip.MustParseAddr("127.0.0.1"), port)
	if open || !alive {
		t.Errorf("a closed port still shows the host is there: open=%v alive=%v", open, alive)
	}
}

func TestLimiterAndEach(t *testing.T) {
	l := newGwLimiter(20) // one every 50 ms
	start := time.Now()
	for i := 0; i < 5; i++ {
		if err := l.wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < 150*time.Millisecond {
		t.Errorf("five probes at 20 per second take about 200 ms, took %v", d)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.wait(ctx); err == nil {
		t.Errorf("a cancelled context stops the wait")
	}

	var mu sync.Mutex
	sum := 0
	gwEach(context.Background(), 4, 100, func(i int) { mu.Lock(); sum += i; mu.Unlock() })
	if sum != 4950 {
		t.Errorf("every item once: %d", sum)
	}
}

// gwFake replaces the probes with a small fake network for the duration of a test.
func gwFake(t *testing.T) {
	t.Helper()
	oi, ol, or, op, ot, ob, on, ow := gwInterfaces, gwLocalAddrs, gwRoutes, gwPing, gwTCP, gwBanner, gwPTR, gwARPTable
	om, onb, oss := gwMDNS, gwNetBIOS, gwSSDP
	gwnet.AllowLoopback = true
	gwInterfaces = func() []gwIface { return nil }
	gwLocalAddrs = func() map[netip.Addr]bool { return map[netip.Addr]bool{} }
	gwRoutes = func() []gwRouteEntry {
		return []gwRouteEntry{
			{Iface: "lo", Dest: netip.MustParsePrefix("127.0.0.0/8"), Direct: true},
			{Iface: "lo", Dest: netip.MustParsePrefix("0.0.0.0/0"), Gate: netip.MustParseAddr("127.0.0.2")},
		}
	}
	gwMDNS = func(_ context.Context, ip netip.Addr) (string, []string) {
		if ip.String() == "127.0.0.1" {
			return "box-" + ip.String(), []string{"_ipp._tcp", "_http._tcp"}
		}
		return "", nil
	}
	gwNetBIOS = func(_ context.Context, ip netip.Addr) string {
		if ip.String() == "127.0.0.2" {
			return "WINBOX"
		}
		return ""
	}
	gwSSDP = func(context.Context, netip.Addr, map[netip.Addr]bool) map[netip.Addr]gwUPnPInfo { return nil }
	gwARPTable = func() []arpEntry { return nil }
	gwPing = func(_ context.Context, hosts []netip.Addr, _ *gwLimiter) (map[netip.Addr]bool, error) {
		return map[netip.Addr]bool{netip.MustParseAddr("127.0.0.2"): true}, nil
	}
	gwTCP = func(_ context.Context, ip netip.Addr, port int) (bool, bool) {
		switch ip.String() {
		case "127.0.0.1":
			return port == 22 || port == 80, true
		case "127.0.0.2":
			return false, true // refused
		}
		return false, false
	}
	gwBanner = func(_ context.Context, _ netip.Addr, port int) string {
		if port == 22 {
			return "SSH-2.0-test"
		}
		return ""
	}
	gwPTR = func(_ context.Context, ip netip.Addr) string { return "host-" + ip.String() }
	t.Cleanup(func() {
		gwnet.AllowLoopback = false
		gwInterfaces, gwLocalAddrs, gwRoutes, gwPing, gwTCP, gwBanner, gwPTR, gwARPTable = oi, ol, or, op, ot, ob, on, ow
		gwMDNS, gwNetBIOS, gwSSDP = om, onb, oss
	})
}

func newTestGateway(t *testing.T) (*gwModule, chan protocol.GatewayPass) {
	t.Helper()
	gwFake(t)
	got := make(chan protocol.GatewayPass, 16)
	m := newGatewayModule(t.TempDir(), func(typ string, payload any) error {
		if typ == protocol.TypeGatewayPass {
			got <- payload.(protocol.GatewayPass)
		}
		return nil
	})
	m.pingOK = true
	t.Cleanup(m.close)
	return m, got
}

func gwConfigJSON(t *testing.T, c protocol.GatewayConfig) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGatewayPass(t *testing.T) {
	m, got := newTestGateway(t)
	m.start()
	cfg := protocol.GatewayConfig{Enabled: true, Interval: 300, Nets: []protocol.GatewayNetCfg{{CIDR: "127.0.0.0/30", On: true, Ports: true, Names: true}}}
	if _, err := m.configureCmd(context.Background(), gwConfigJSON(t, cfg), false); !errors.Is(err, errDangerous) {
		t.Fatalf("changing what is scanned needs the confirmation mark: %v", err)
	}
	if _, err := m.configureCmd(context.Background(), gwConfigJSON(t, cfg), true); err != nil {
		t.Fatal(err)
	}
	var pass protocol.GatewayPass
	select {
	case pass = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("no result of the pass")
	}
	if pass.CIDR != "127.0.0.0/30" || !pass.Last || pass.Err != "" || pass.Probed != 2 || pass.Answered != 2 || len(pass.Devices) != 2 {
		t.Fatalf("pass: %+v", pass)
	}
	d1, d2 := pass.Devices[0], pass.Devices[1]
	if d1.IP != "127.0.0.1" || len(d1.Ports) != 2 || d1.Ports[0].Port != 22 || d1.Ports[0].Banner != "SSH-2.0-test" {
		t.Errorf("the first device: %+v", d1)
	}
	srcs := map[string]string{}
	for _, n := range d1.Names {
		srcs[n.Src] = n.Name
	}
	if srcs["mdns"] != "box-127.0.0.1" || srcs["ptr"] != "host-127.0.0.1" || len(d1.Svc) != 2 || d1.Svc[0] != "_ipp._tcp" || d1.Router {
		t.Errorf("names and services of the first device: %+v", d1)
	}
	if len(d2.Names) != 2 || d2.Names[0].Src != "netbios" || d2.Names[0].Name != "WINBOX" || !d2.Router {
		t.Errorf("the second device is the default gateway and has a NetBIOS name: %+v", d2)
	}
	if strings.Join(d1.Via, ",") != "ports" || strings.Join(d2.Via, ",") != "ping,ports" || len(d2.Ports) != 0 {
		t.Errorf("how they were found: %v %v", d1.Via, d2.Via)
	}

	// the module reports itself
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && m.liveState().NextPassAt == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	live := m.liveState()
	if !live.Enabled || live.Running || live.LastPassAt == 0 || live.NextPassAt <= live.LastPassAt || !live.PingOK {
		t.Errorf("live state: %+v", live)
	}

	// "scan now" needs the mark and runs a second pass at once
	if _, err := m.scanCmd(context.Background(), nil, false); !errors.Is(err, errDangerous) {
		t.Errorf("scan now needs the mark: %v", err)
	}
	if _, err := m.scanCmd(context.Background(), nil, true); err != nil {
		t.Fatal(err)
	}
	select {
	case p2 := <-got:
		if p2.PassID == pass.PassID {
			t.Errorf("a new pass has a new id")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scan now did not start a pass")
	}

	// a reconnect: the server sends the configuration again and the latest results are replayed with the same id
	for len(got) > 0 {
		<-got
	}
	if _, err := m.configureCmd(context.Background(), gwConfigJSON(t, cfg), true); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-got:
		if r.CIDR != "127.0.0.0/30" || len(r.Devices) != 2 {
			t.Errorf("replay: %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Error("the latest result was not replayed")
	}
}

func TestGatewayConfigPersistsAndOffMeansIdle(t *testing.T) {
	m, got := newTestGateway(t)
	dir := m.dir
	cfg := protocol.GatewayConfig{Enabled: false, Interval: 300, Nets: []protocol.GatewayNetCfg{{CIDR: "127.0.0.0/30", On: true, Ports: true}}}
	if _, err := m.configureCmd(context.Background(), gwConfigJSON(t, cfg), true); err != nil {
		t.Fatal(err)
	}
	m.start()
	select {
	case p := <-got:
		t.Fatalf("scanning is off, yet a pass was sent: %+v", p)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := m.scanCmd(context.Background(), nil, true); err == nil {
		t.Errorf("scan now while off must be refused")
	}
	m2 := newGatewayModule(dir, nil)
	if len(m2.cfg.Nets) != 1 || m2.cfg.Nets[0].CIDR != "127.0.0.0/30" || m2.cfg.Interval != 300 {
		t.Errorf("the configuration survives a restart: %+v", m2.cfg)
	}
	bad := protocol.GatewayConfig{Enabled: true, Interval: 300, Nets: []protocol.GatewayNetCfg{{CIDR: "10.0.0.0/8", On: true}}}
	if _, err := m.configureCmd(context.Background(), gwConfigJSON(t, bad), true); err == nil {
		t.Errorf("an invalid configuration is refused by the agent too")
	}
	if len(m.cfg.Nets) != 1 || m.cfg.Nets[0].CIDR != "127.0.0.0/30" {
		t.Errorf("a refused configuration changes nothing: %+v", m.cfg)
	}
}
