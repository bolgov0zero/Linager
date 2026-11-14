package agent

import (
	"strings"
	"testing"
)

const sampleInterfaces = `# comment
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet dhcp
    dns-nameservers 1.1.1.1
    hwaddress ether 52:54:00:aa:bb:cc

auto eth1
iface eth1 inet static
    address 10.1.0.5/24
    gateway 10.1.0.1
    mtu 9000

source /etc/network/interfaces.d/*
`

func TestParseIfaceBlocks(t *testing.T) {
	b := parseIfaceBlocks(strings.Split(sampleInterfaces, "\n"))
	if len(b) != 3 || b[1].name != "eth0" || b[1].method != "dhcp" || len(b[1].opts) != 2 || b[2].method != "static" {
		t.Fatalf("%+v", b)
	}
}

func TestApplyStaticKeepsUnmanagedOptions(t *testing.T) {
	out, err := applyNetChanges(sampleInterfaces, []NetChange{{Iface: "eth0", Mode: "static", Address: "192.168.1.10/24", Gateway: "192.168.1.1", MTU: 1400, Up: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"iface eth0 inet static", "    address 192.168.1.10/24", "    gateway 192.168.1.1", "    mtu 1400", "    dns-nameservers 1.1.1.1", "    hwaddress ether 52:54:00:aa:bb:cc", "iface eth1 inet static", "source /etc/network/interfaces.d/*", "# comment"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "inet dhcp") {
		t.Fatal("old dhcp stanza must be replaced")
	}
	if strings.Count(out, "auto eth0") != 1 {
		t.Fatalf("auto line must stay exactly once:\n%s", out)
	}
}

func TestApplyDhcpDropsAddressAndDown(t *testing.T) {
	out, _ := applyNetChanges(sampleInterfaces, []NetChange{{Iface: "eth1", Mode: "dhcp", MTU: 1500, Up: false}})
	if strings.Contains(out, "10.1.0.5") || strings.Contains(out, "mtu 9000") || strings.Contains(out, "auto eth1") {
		t.Fatalf("dhcp+down must drop address, mtu and auto:\n%s", out)
	}
	if !strings.Contains(out, "iface eth1 inet dhcp") {
		t.Fatalf("stanza missing:\n%s", out)
	}
}

func TestApplyCreatesMissingStanza(t *testing.T) {
	out, _ := applyNetChanges("auto lo\niface lo inet loopback\n", []NetChange{{Iface: "ens19", Mode: "static", Address: "10.9.9.2/24", Up: true}})
	if !strings.Contains(out, "auto ens19") || !strings.Contains(out, "iface ens19 inet static") || !strings.Contains(out, "address 10.9.9.2/24") {
		t.Fatalf("%s", out)
	}
}
