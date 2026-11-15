package agent

import (
	"strings"
	"testing"
)

const setsTable = `table inet filter {
	set office {
		type ipv4_addr
		flags interval
		elements = { 10.20.0.0/24, 198.51.100.20,
			203.0.113.7 }
	}
	set empty6 {
		type ipv6_addr
		flags interval
	}
	set ports {
		type inet_service
		elements = { 22, 80 }
	}
	chain input {
		type filter hook input priority filter; policy drop;
		ip saddr @office tcp dport 22 counter packets 1 bytes 60 accept
	}
}
`

func TestParseSets(t *testing.T) {
	got := parseSets(setsTable)
	if len(got) != 2 {
		t.Fatalf("only address sets are managed: %+v", got)
	}
	if got[0].Name != "office" || got[0].Family != "ipv4" || strings.Join(got[0].Elements, " ") != "10.20.0.0/24 198.51.100.20 203.0.113.7" {
		t.Fatalf("office: %+v", got[0])
	}
	if got[1].Name != "empty6" || got[1].Family != "ipv6" || len(got[1].Elements) != 0 {
		t.Fatalf("empty6: %+v", got[1])
	}
	if c := parseFilterTable(setsTable); len(c[0].Rules) != 1 {
		t.Fatalf("sets must not leak into the chains: %+v", c[0])
	}
}

func TestRenderTableWithSets(t *testing.T) {
	d := fwDraft{Sets: []FwSet{{Name: "office", Family: "ipv4", Elements: []string{"10.0.0.0/24", "192.0.2.1"}}, {Name: "v6", Family: "ipv6"}}}
	out, err := renderTable(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"set office {", "type ipv4_addr", "flags interval", "elements = { 10.0.0.0/24, 192.0.2.1 }", "type ipv6_addr"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing:\n%s", want, out)
		}
	}
	if strings.Contains(out, "elements = {  }") {
		t.Errorf("an empty list has no elements line:\n%s", out)
	}
	for _, bad := range []FwSet{
		{Name: "Bad", Family: "ipv4"},
		{Name: "1x", Family: "ipv4"},
		{Name: "x", Family: "ipv5"},
		{Name: "x", Family: "ipv4", Elements: []string{"2001:db8::1"}},
		{Name: "x", Family: "ipv6", Elements: []string{"10.0.0.1"}},
		{Name: "x", Family: "ipv4", Elements: []string{"1.2.3.4 }; flush ruleset"}},
	} {
		if _, err := renderTable(fwDraft{Sets: []FwSet{bad}}); err == nil {
			t.Errorf("%+v must be rejected", bad)
		}
	}
	if _, err := renderTable(fwDraft{Sets: []FwSet{{Name: "a", Family: "ipv4"}, {Name: "a", Family: "ipv4"}}}); err == nil {
		t.Error("a duplicate name must be rejected")
	}
}

func TestParseListeners(t *testing.T) {
	out := `tcp   LISTEN 0 128  0.0.0.0:22     0.0.0.0:*  users:(("sshd",pid=812,fd=3))
tcp   LISTEN 0 128  [::]:22        [::]:*     users:(("sshd",pid=812,fd=4))
tcp   LISTEN 0 4096 0.0.0.0:6379   0.0.0.0:*  users:(("redis-server",pid=1488,fd=6))
tcp   LISTEN 0 244  127.0.0.1:5432 0.0.0.0:*  users:(("postgres",pid=1102,fd=5))
tcp   LISTEN 0 4096 0.0.0.0:3306   0.0.0.0:*  users:(("docker-proxy",pid=2210,fd=4))
udp   UNCONN 0 0    127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=701,fd=13))
udp   UNCONN 0 0    *:5353         *:*
`
	ls := parseListeners(out)
	if len(ls) != 6 {
		t.Fatalf("22 on 0.0.0.0 and :: must be one row: %+v", ls)
	}
	var ssh, redis, pg, dock Listener
	for _, l := range ls {
		switch l.Port {
		case 22:
			ssh = l
		case 6379:
			redis = l
		case 5432:
			pg = l
		case 3306:
			dock = l
		}
	}
	if ssh.Scope != "all" || len(ssh.Addrs) != 2 || ssh.Process != "sshd" || ssh.PID != 812 {
		t.Errorf("ssh: %+v", ssh)
	}
	if pg.Scope != "local" || dock.Scope != "docker" || !redis.Risky {
		t.Errorf("scopes: pg=%+v docker=%+v redis=%+v", pg, dock, redis)
	}
}

func TestPortVerdictAndClassify(t *testing.T) {
	input := FwChain{Name: "input", Policy: "drop", Rules: []FwRule{
		{Text: "ct state established,related accept"},
		{Text: `tcp dport 22 ip saddr @office counter accept comment "ssh only for the office"`},
		{Text: "tcp dport { 80, 443 } counter accept"},
		{Text: "tcp dport 8000-8100 counter drop"},
		{Text: "ip saddr 192.0.2.9 tcp dport 25 drop"},
	}}
	ls := []Listener{
		{Proto: "tcp", Port: 22, Scope: "all"}, {Proto: "tcp", Port: 443, Scope: "all"}, {Proto: "tcp", Port: 8050, Scope: "all"},
		{Proto: "tcp", Port: 25, Scope: "all"}, {Proto: "udp", Port: 443, Scope: "all"}, {Proto: "tcp", Port: 5432, Scope: "local"},
	}
	classify(ls, input)
	want := []string{"restricted", "allowed", "blocked", "policy", "policy", "local"}
	for i, l := range ls {
		if l.Firewall != want[i] {
			t.Errorf("%s/%d: got %s want %s", l.Proto, l.Port, l.Firewall, want[i])
		}
	}
	classify(ls[3:4], FwChain{Policy: "accept"})
	if ls[3].Firewall != "open" {
		t.Errorf("no rule with policy accept is open: %s", ls[3].Firewall)
	}
}

func TestParseConnections(t *testing.T) {
	out := `0 0 10.0.0.5:443 203.0.113.5:51000
0 0 10.0.0.5:443 203.0.113.5:51001
0 0 10.0.0.5:22 198.51.100.20:40000
0 0 [::ffff:10.0.0.5]:443 [::ffff:192.0.2.44]:5555
`
	per, rem := parseConnections(out)
	if per[443] != 3 || per[22] != 1 {
		t.Fatalf("per port: %v", per)
	}
	if len(rem) != 3 || rem[0].Addr != "203.0.113.5" || rem[0].Conns != 2 || rem[0].Port != 443 {
		t.Fatalf("remotes: %+v", rem)
	}
}

func TestRenderTableAlwaysAllowsEstablished(t *testing.T) {
	var d fwDraft
	for _, name := range []string{"input", "forward", "output"} {
		c := struct {
			Name   string `json:"name"`
			Policy string `json:"policy"`
			Rules  []struct {
				Text string `json:"text"`
			} `json:"rules"`
		}{Name: name, Policy: "drop"}
		c.Rules = append(c.Rules, struct {
			Text string `json:"text"`
		}{"tcp dport 22 accept"}, struct {
			Text string `json:"text"`
		}{"ct state established,related counter accept"})
		d.Chains = append(d.Chains, c)
	}
	out, err := renderTable(d)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out, "ct state established,related accept"); n != 3 {
		t.Fatalf("one rule per chain, a copy from the draft is dropped: %d\n%s", n, out)
	}
	if strings.Contains(out, "counter accept") {
		t.Fatalf("the copy with a counter must not be written twice:\n%s", out)
	}
	for _, chunk := range strings.Split(out, "chain ")[1:] {
		lines := strings.Split(chunk, "\n")
		if !strings.Contains(lines[2], "ct state established,related accept") {
			t.Fatalf("the rule must come first in the chain: %q", lines[2])
		}
	}
	for _, text := range []string{"ct state established,related accept", "ct state { established, related } accept", "ct state established,related counter packets 4 bytes 60 accept"} {
		if !isEstablishedRule(text) {
			t.Errorf("%q is the system rule", text)
		}
	}
	for _, text := range []string{"ct state new accept", "ct state established drop", "ip saddr 10.0.0.1 ct state established,related accept"} {
		if isEstablishedRule(text) {
			t.Errorf("%q is not the system rule", text)
		}
	}
}
