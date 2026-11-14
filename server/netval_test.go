package server

import "testing"

func en(_, e string) string { return e }

func TestValidateNetDraft(t *testing.T) {
	ifs := []netIfaceInfo{
		{Name: "lo", Kind: "loopback", Addresses: []string{"127.0.0.1/8"}},
		{Name: "eth0", Kind: "physical", Editable: true, Primary: true, Addresses: []string{"10.0.0.5/24"}},
		{Name: "eth1", Kind: "physical", Editable: true, Addresses: []string{"192.168.1.5/24"}},
		{Name: "docker0", Kind: "virtual", Addresses: []string{"172.17.0.1/16"}},
	}
	cases := []struct {
		name  string
		c     NetChange
		field string // expected error field, "" = valid
	}{
		{"valid", NetChange{Iface: "eth0", Mode: "static", Address: "10.0.0.6/24", Gateway: "10.0.0.1", MTU: 1500, Up: true}, ""},
		{"bad address", NetChange{Iface: "eth0", Mode: "static", Address: "10.0.0.6", Up: true}, "address"},
		{"gateway outside", NetChange{Iface: "eth0", Mode: "static", Address: "10.0.0.6/24", Gateway: "10.9.0.1", Up: true}, "gateway"},
		{"overlap", NetChange{Iface: "eth0", Mode: "static", Address: "192.168.1.9/24", Up: true}, "address"},
		{"mtu", NetChange{Iface: "eth0", Mode: "dhcp", MTU: 20, Up: true}, "mtu"},
		{"not editable", NetChange{Iface: "docker0", Mode: "dhcp", Up: true}, "iface"},
		{"unknown", NetChange{Iface: "zz9", Mode: "dhcp", Up: true}, "iface"},
		{"dhcp ok", NetChange{Iface: "eth1", Mode: "dhcp", Up: true}, ""},
	}
	for _, c := range cases {
		errs := validateNetDraft([]NetChange{c.c}, ifs, en)
		switch {
		case c.field == "" && len(errs) != 0:
			t.Errorf("%s: unexpected %+v", c.name, errs)
		case c.field != "" && (len(errs) == 0 || errs[0].Field != c.field):
			t.Errorf("%s: expected %s error, got %+v", c.name, c.field, errs)
		}
	}
	// two interfaces moved to the same subnet in one draft overlap each other
	errs := validateNetDraft([]NetChange{
		{Iface: "eth0", Mode: "static", Address: "10.5.0.1/24", Up: true},
		{Iface: "eth1", Mode: "static", Address: "10.5.0.2/24", Up: true},
	}, ifs, en)
	if len(errs) == 0 {
		t.Fatal("overlap between two edited interfaces must be detected")
	}
}

func TestNetWarnings(t *testing.T) {
	ifs := []netIfaceInfo{{Name: "eth0", Primary: true}, {Name: "eth1"}}
	if w := netWarnings([]NetChange{{Iface: "eth1", Up: true}}, ifs, en); len(w) != 0 {
		t.Fatal("a secondary interface is safe to change")
	}
	w := netWarnings([]NetChange{{Iface: "eth0", Up: false}}, ifs, en)
	if len(w) != 2 || w[0].Code != "primary_iface" || w[1].Code != "agent_link" {
		t.Fatalf("%+v", w)
	}
}

func chain(name, policy string, rules ...string) FwDraftChain {
	c := FwDraftChain{Name: name, Policy: policy}
	for _, r := range rules {
		c.Rules = append(c.Rules, struct {
			Text string `json:"text"`
		}{r})
	}
	return c
}

func codes(w []Warning) string {
	s := ""
	for _, x := range w {
		s += x.Code + ","
	}
	return s
}

func TestFirewallWarnings(t *testing.T) {
	cases := []struct {
		name  string
		chain FwDraftChain
		want  string
	}{
		{"safe", chain("input", "drop", "ct state established,related accept", "tcp dport 22 accept"), ""},
		{"set syntax", chain("input", "drop", "ct state established accept", "tcp dport { 22, 443 } accept"), ""},
		{"ssh closed", chain("input", "drop", "ct state established,related accept", "tcp dport 80 accept"), "ssh_port,"},
		{"no established", chain("input", "drop", "tcp dport 22 accept"), "agent_link,"},
		{"blocked on accept policy", chain("input", "accept", "tcp dport 22 drop"), "ssh_port,"},
		{"open policy", chain("input", "accept"), ""},
		{"output drop", chain("output", "drop"), "agent_link,"},
	}
	for _, c := range cases {
		if got := codes(fwWarnings([]FwDraftChain{c.chain}, nil, en)); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	// sshd on a non-default port: opening 22 does not help, the real port must stay reachable
	moved := []int{2222}
	if got := codes(fwWarnings([]FwDraftChain{chain("input", "drop", "ct state established accept", "tcp dport 22 accept")}, moved, en)); got != "ssh_port," {
		t.Errorf("custom port closed: got %q", got)
	}
	if got := codes(fwWarnings([]FwDraftChain{chain("input", "drop", "ct state established accept", "tcp dport 2000-3000 accept")}, moved, en)); got != "" {
		t.Errorf("custom port open through a range: got %q", got)
	}
}
