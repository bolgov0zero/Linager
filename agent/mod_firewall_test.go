//go:build !windows

package agent

import (
	"strings"
	"testing"
)

const sampleTable = `table inet filter {
	chain input {
		type filter hook input priority filter; policy drop;
		ct state established,related accept
		tcp dport 22 counter packets 42 bytes 3000 accept
		iif "lo" accept
	}
	chain forward {
		type filter hook forward priority filter; policy accept;
	}
}
`

func TestParseFilterTable(t *testing.T) {
	c := parseFilterTable(sampleTable)
	if len(c) != 3 || c[0].Name != "input" || c[0].Policy != "drop" || len(c[0].Rules) != 3 {
		t.Fatalf("%+v", c)
	}
	r := c[0].Rules[1]
	if r.Packets != 42 || r.Bytes != 3000 || r.Text != "tcp dport 22 counter accept" {
		t.Fatalf("counters must be split from the text: %+v", r)
	}
	if c[2].Name != "output" || c[2].Policy != "accept" || len(c[2].Rules) != 0 {
		t.Fatalf("missing chains default to accept/empty: %+v", c[2])
	}
}

func TestRenderTableAndValidation(t *testing.T) {
	var d fwDraft
	d.Chains = append(d.Chains, struct {
		Name   string `json:"name"`
		Policy string `json:"policy"`
		Rules  []struct {
			Text string `json:"text"`
		} `json:"rules"`
	}{Name: "input", Policy: "drop", Rules: []struct {
		Text string `json:"text"`
	}{{Text: "tcp dport { 22, 443 } accept"}}})
	out, err := renderTable(d)
	if err != nil || !strings.Contains(out, "policy drop;") || !strings.Contains(out, "\t\ttcp dport { 22, 443 } accept") {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, bad := range []string{"", "accept\n}\n}\ntable ip x {", "tcp dport 22 } accept", "tcp dport { 22 accept"} {
		if validateRule(bad) == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	d.Chains[0].Name = "prerouting"
	if _, err := renderTable(d); err == nil {
		t.Fatal("only input/forward/output may be managed")
	}
}

func TestReplaceTableInConf(t *testing.T) {
	conf := "#!/usr/sbin/nft -f\n\nflush ruleset\n\ntable inet filter {\n\tchain input {\n\t\ttype filter hook input priority filter; policy drop;\n\t}\n}\n\ntable ip nat {\n\tchain x {\n\t}\n}\n"
	out := replaceTableInConf(conf, "table inet filter {\n\tchain input {\n\t\tpolicy accept;\n\t}\n}\n")
	if strings.Contains(out, "policy drop") || !strings.Contains(out, "policy accept") || !strings.Contains(out, "table ip nat") || !strings.HasPrefix(out, "#!/usr/sbin/nft -f") {
		t.Fatalf("%s", out)
	}
	if strings.Count(out, "table inet filter") != 1 {
		t.Fatalf("filter table must appear once:\n%s", out)
	}
	appended := replaceTableInConf("flush ruleset\n", "table inet filter {\n}\n")
	if !strings.Contains(appended, "flush ruleset") || !strings.Contains(appended, "table inet filter") {
		t.Fatal(appended)
	}
	if !strings.Contains(replaceTableInConf("", "table inet filter {\n}\n"), "flush ruleset") {
		t.Fatal("a new file gets the standard header")
	}
}

func TestCleanNftError(t *testing.T) {
	in := "/dev/stdin:4:19-23: Error: syntax error, unexpected string\ntcp dport 22 acept\n                  ^^^^^\n"
	if got := cleanNftError(in); got != "syntax error, unexpected string" {
		t.Fatalf("%q", got)
	}
	if cleanNftError("") == "" {
		t.Fatal("must not return an empty message")
	}
}
