package server

import (
	"slices"
	"testing"
)

func TestClassifyDevice(t *testing.T) {
	why := func(w ...gwWhy) []gwWhy { return w }
	cases := []struct {
		name string
		in   gwClassIn
		kind string
		why  []gwWhy
	}{
		{"router: the default gateway with Winbox", gwClassIn{Vendor: "MikroTik", Ports: []int{22, 80, 443, 8291}, Router: true}, "router",
			why(gwWhy{"gateway", ""}, gwWhy{"port", "8291"}, gwWhy{"vendor", "MikroTik"})},
		{"router: UPnP gateway", gwClassIn{UPnPType: "InternetGatewayDevice"}, "router", why(gwWhy{"upnp", "InternetGatewayDevice"})},
		{"hypervisor", gwClassIn{Ports: []int{22, 8006}, Banners: "ssh-2.0-openssh_9.2p1 debian proxmox virtual environment"}, "hypervisor",
			why(gwWhy{"port", "8006"}, gwWhy{"banner", "proxmox"})},
		{"nas", gwClassIn{Vendor: "Synology Incorporated", Ports: []int{22, 80, 443, 5000, 5001}, Banners: "synology diskstation"}, "nas",
			why(gwWhy{"vendor", "Synology Incorporated"}, gwWhy{"banner", "diskstation"})},
		{"printer by service", gwClassIn{Vendor: "HP Inc.", Ports: []int{80, 9100}, Svc: []string{"_ipp._tcp"}}, "printer",
			why(gwWhy{"service", "_ipp._tcp"}, gwWhy{"port", "9100"}, gwWhy{"vendor", "HP Inc."})},
		{"a PC of a printer maker is not a printer", gwClassIn{Vendor: "Hewlett Packard", Ports: []int{445}}, "", nil},
		{"camera", gwClassIn{Vendor: "Hangzhou Hikvision Digital Technology", Ports: []int{80, 554}}, "camera",
			why(gwWhy{"port", "554"}, gwWhy{"vendor", "Hangzhou Hikvision Digital Technology"})},
		{"tv by UPnP", gwClassIn{Vendor: "Samsung Electronics", Ports: []int{8001}, UPnPType: "MediaRenderer", Svc: []string{"_airplay._tcp"}}, "tv",
			why(gwWhy{"upnp", "MediaRenderer"}, gwWhy{"port", "8001"}, gwWhy{"service", "_airplay._tcp"})},
		{"a media renderer of an unknown maker is not enough for a tv", gwClassIn{UPnPType: "MediaRenderer"}, "", nil},
		{"speaker", gwClassIn{Vendor: "Sonos, Inc.", Ports: []int{1400}, UPnPType: "ZonePlayer"}, "speaker",
			why(gwWhy{"port", "1400"}, gwWhy{"upnp", "ZonePlayer"}, gwWhy{"vendor", "Sonos, Inc."})},
		{"windows by RDP", gwClassIn{Ports: []int{3389, 445}, NameSrc: "netbios"}, "windows", why(gwWhy{"port", "3389"}, gwWhy{"name", ""}, gwWhy{"port", "445"})},
		{"windows by a share and the NetBIOS name", gwClassIn{Ports: []int{445}, NameSrc: "netbios"}, "windows", why(gwWhy{"name", ""}, gwWhy{"port", "445"})},
		{"a share alone is not enough", gwClassIn{Ports: []int{445}}, "", nil},
		{"phone by service", gwClassIn{Vendor: "Apple, Inc.", Svc: []string{"_companion-link._tcp", "_apple-mobdev2._tcp"}}, "phone", why(gwWhy{"service", "_companion-link._tcp"})},
		{"phone by a random MAC", gwClassIn{RandomMAC: true}, "phone", why(gwWhy{"random_mac", ""})},
		{"a random MAC with open ports is no phone", gwClassIn{RandomMAC: true, Ports: []int{22}}, "", nil},
		{"smart home by vendor", gwClassIn{Vendor: "Espressif Inc."}, "iot", why(gwWhy{"vendor", "Espressif Inc."})},
		{"smart home by MQTT and a service", gwClassIn{Ports: []int{1883}, Svc: []string{"_hap._tcp"}}, "iot", why(gwWhy{"service", "_hap._tcp"}, gwWhy{"port", "1883"})},
		{"a Linux server only when nothing says more", gwClassIn{Ports: []int{22}, Banners: "ssh-2.0-openssh_9.2p1 debian-2+deb12u3"}, "server", why(gwWhy{"banner", "debian"})},
		{"nothing is known", gwClassIn{}, "", nil},
		{"a single weak hint is not enough", gwClassIn{Vendor: "MikroTik"}, "", nil},
	}
	for _, c := range cases {
		kind, w := classifyDevice(c.in)
		if kind != c.kind {
			t.Errorf("%s: kind %q, want %q (%v)", c.name, kind, c.kind, w)
			continue
		}
		if c.kind != "" && !slices.Equal(w, c.why) {
			t.Errorf("%s: reasons %v, want %v", c.name, w, c.why)
		}
	}

	// a Debian host that is a Proxmox server is a hypervisor, not a server: the stronger evidence wins
	if kind, _ := classifyDevice(gwClassIn{Ports: []int{8006}, Banners: "debian"}); kind != "hypervisor" {
		t.Errorf("the stronger evidence wins: %q", kind)
	}
	// every kind has a place in the tie-break order, and no kind is listed twice
	seen := map[string]bool{}
	for _, k := range gwKindOrder {
		if seen[k] {
			t.Errorf("%s is listed twice", k)
		}
		seen[k] = true
	}
}
