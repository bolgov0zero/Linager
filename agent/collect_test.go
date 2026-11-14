package agent

import "testing"

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

func TestCountAptInst(t *testing.T) {
	out := "Inst libc6 [1] (2 Debian:13/stable-security [amd64])\nInst vim [1] (2 Debian:13/stable [amd64])\nConf vim\n"
	if tot, sec := countAptInst(out); tot != 2 || sec != 1 {
		t.Fatalf("tot=%d sec=%d", tot, sec)
	}
}
