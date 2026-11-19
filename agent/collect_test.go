//go:build !windows

package agent

import (
	"testing"

	"github.com/bolgov0zero/linager/internal/protocol"
)

func TestParseCPU(t *testing.T) {
	busy, total, ok := parseCPU("cpu  100 0 50 800 50 0 0 0 0 0\ncpu0 1 1 1 1 1\n")
	if !ok || total != 1000 || busy != 150 {
		t.Fatalf("busy=%d total=%d ok=%v", busy, total, ok)
	}
	if _, _, ok := parseCPU("garbage"); ok {
		t.Fatal("garbage must not parse")
	}
}

func TestParseMeminfo(t *testing.T) {
	m := parseMeminfo("MemTotal: 2048 kB\nMemAvailable: 1024 kB\nSwapTotal: 512 kB\nSwapFree: 256 kB\n")
	if m.total != 2048*1024 || m.avail != 1024*1024 || m.swapTotal != 512*1024 || m.swapFree != 256*1024 {
		t.Fatalf("%+v", m)
	}
}

func TestParseNetDev(t *testing.T) {
	in := "Inter-|   Receive\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets\n  eth0: 1000 10 0 0 0 0 0 0 2000 20 0 0 0 0 0 0\n    lo: 5 1 0 0 0 0 0 0 5 1 0 0 0 0 0 0\n"
	m := parseNetDev(in)
	if m["eth0"].rx != 1000 || m["eth0"].tx != 2000 || len(m) != 2 {
		t.Fatalf("%+v", m)
	}
}

func TestParseMounts(t *testing.T) {
	in := "/dev/vda1 / ext4 rw 0 0\nproc /proc proc rw 0 0\n/dev/vda1 /var/lib/docker/x ext4 rw 0 0\n/dev/sdb1 /data xfs rw 0 0\ntmpfs /run tmpfs rw 0 0\noverlay /merged overlay rw 0 0\n"
	m := parseMounts(in)
	if len(m) != 2 || m[0].point != "/" || m[1].point != "/data" {
		t.Fatalf("%+v", m)
	}
}

func TestVirtualIface(t *testing.T) {
	for _, n := range []string{"lo", "docker0", "veth1234", "br-abc", "wg0"} {
		if !virtualIface(n) {
			t.Errorf("%s must be virtual", n)
		}
	}
	if virtualIface("ens18") || virtualIface("eth0") {
		t.Error("physical names must not be virtual")
	}
}

func TestCountApt(t *testing.T) {
	// a kernel update brings a new package (no installed version) and a meta package that needs it:
	// both must be counted, the dashboard and the Updates tab show the same number
	out := "Inst linux-image-6.12.117+deb13-amd64 (6.12.117-1 Debian-Security:13/stable-security [amd64])\n" +
		"Inst linux-image-amd64 [6.12.116-1] (6.12.117-1 Debian-Security:13/stable-security [amd64])\n" +
		"Inst vim [1] (2 Debian:13/stable [amd64])\nConf vim\n"
	if tot, sec := countApt(parseAptInst(out)); tot != 3 || sec != 2 {
		t.Fatalf("tot=%d sec=%d", tot, sec)
	}
}

func TestDefaultRouteIface(t *testing.T) {
	route := "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\tMTU\tWindow\tIRTT\n" +
		"eth1\t00000000\t0101A8C0\t0003\t0\t0\t200\t00000000\t0\t0\t0\n" +
		"eth0\t00000000\t0100A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"eth0\t0000A8C0\t00000000\t0001\t0\t0\t100\t00FFFFFF\t0\t0\t0\n"
	if got := defaultRouteIface(route); got != "eth0" {
		t.Fatalf("lowest metric: %q", got)
	}
	if got := defaultRouteIface("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\n"); got != "" {
		t.Fatalf("no default route: %q", got)
	}
}

func TestTrafficTotals(t *testing.T) {
	phys := func(n string) bool { return n == "ens18" }
	nic := func(name string, up bool, rx, tx float64) protocol.NetIface {
		return protocol.NetIface{Iface: name, Up: up, RxBps: rx, TxBps: tx}
	}
	// A host with a physical card: only the card counts (the bridge and the veth ends carry the same bytes).
	rx, tx := trafficTotals([]protocol.NetIface{nic("ens18", true, 100, 10), nic("docker0", true, 90, 9), nic("br0", true, 80, 8)}, phys, "br0")
	if rx != 100 || tx != 10 {
		t.Fatalf("physical host: %v %v", rx, tx)
	}
	// An LXC container: eth0 has no device in sysfs, so it is not physical; the default route picks it.
	rx, tx = trafficTotals([]protocol.NetIface{nic("eth0", true, 500, 50), nic("eth1", true, 7, 7)}, phys, "eth0")
	if rx != 500 || tx != 50 {
		t.Fatalf("container with a default route: %v %v", rx, tx)
	}
	// No default route: every interface that is up and not virtual by name.
	rx, tx = trafficTotals([]protocol.NetIface{nic("eth0", true, 5, 1), nic("eth1", false, 7, 7), nic("veth12", true, 9, 9)}, phys, "")
	if rx != 5 || tx != 1 {
		t.Fatalf("container without a default route: %v %v", rx, tx)
	}
	// The default route names an interface that is not in the list: fall through to the last rule.
	rx, tx = trafficTotals([]protocol.NetIface{nic("eth0", true, 5, 1)}, phys, "gone0")
	if rx != 5 || tx != 1 {
		t.Fatalf("stale default route: %v %v", rx, tx)
	}
}
