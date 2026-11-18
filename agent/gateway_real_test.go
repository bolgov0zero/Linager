package agent

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/bolgov0zero/linager/internal/gwnet"
	"github.com/bolgov0zero/linager/internal/protocol"
)

// TestGatewayRealSockets scans 127.0.0.1 with the real probes (no fakes): a plain web server on 8080 and a TLS one on
// 8006, as devices in a LAN have them. It needs those two ports free, so it runs only when asked:
//
//	LINAGER_REAL_NET=1 go test ./agent -run TestGatewayRealSockets
func TestGatewayRealSockets(t *testing.T) {
	if os.Getenv("LINAGER_REAL_NET") == "" {
		t.Skip("LINAGER_REAL_NET is not set")
	}
	serve := func(addr string, tlsOn bool, server, title string) {
		l, err := net.Listen("tcp4", addr)
		if err != nil {
			t.Skipf("%s is busy: %v", addr, err)
		}
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Server", server)
			_, _ = w.Write([]byte("<html><head><title>" + title + "</title></head><body>hello</body></html>"))
		}))
		_ = srv.Listener.Close()
		srv.Listener = l
		if tlsOn {
			srv.EnableHTTP2 = false
			srv.StartTLS()
		} else {
			srv.Start()
		}
		t.Cleanup(srv.Close)
	}
	serve("127.0.0.1:8080", false, "lighttpd/1.4.69", "Router login")
	serve("127.0.0.1:8006", true, "pve-api-daemon/3.0", "Proxmox Virtual Environment")
	_ = tls.VersionTLS12

	oldPorts, oldLive, oldLocal, oldIfs := gwPortList, gwLivePortList, gwLocalAddrs, gwInterfaces
	gwPortList, gwLivePortList = []int{8080, 8006, 9}, []int{8080, 8006, 9}
	gwLocalAddrs = func() map[netip.Addr]bool { return map[netip.Addr]bool{} }
	gwInterfaces = func() []gwIface { return nil }
	gwnet.AllowLoopback = true
	t.Cleanup(func() {
		gwPortList, gwLivePortList, gwLocalAddrs, gwInterfaces = oldPorts, oldLive, oldLocal, oldIfs
		gwnet.AllowLoopback = false
	})

	m := newGatewayModule(t.TempDir(), nil)
	m.pingOK = false // raw ICMP needs root; the TCP probes are what this test is about
	n, err := gwnet.Parse("127.0.0.0/30")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pass := m.scanNet(ctx, "real", n, protocol.GatewayNetCfg{CIDR: "127.0.0.0/30", On: true, Ports: true, Names: true}, nil, 1, 1)
	if pass.Err != "" || pass.Probed != 2 {
		t.Fatalf("pass: %+v", pass)
	}
	var dev *protocol.GatewayDevice
	for i := range pass.Devices {
		if pass.Devices[i].IP == "127.0.0.1" {
			dev = &pass.Devices[i]
		}
	}
	if dev == nil {
		t.Fatalf("127.0.0.1 must answer: %+v", pass.Devices)
	}
	got := map[int]string{}
	for _, p := range dev.Ports {
		got[p.Port] = p.Banner
	}
	if len(got) != 2 {
		t.Errorf("open ports are 8006 and 8080 (9 is closed): %v", got)
	}
	if got[8080] != "lighttpd/1.4.69 · Router login" {
		t.Errorf("the banner of the plain web server: %q", got[8080])
	}
	if got[8006] != "pve-api-daemon/3.0 · Proxmox Virtual Environment" {
		t.Errorf("the banner of the TLS web server: %q", got[8006])
	}
	t.Logf("names: %v, via: %v", dev.Names, dev.Via)
}
