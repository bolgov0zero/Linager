package oui

import (
	"strings"
	"testing"
)

func TestLookup(t *testing.T) {
	for mac, want := range map[string]string{
		"a4:5e:60:1c:07:2b": "Apple",
		"A4-5E-60-1C-07-2B": "Apple",
		"a45e.601c.072b":    "Apple",
		"00:11:32:9a:44:10": "Synology",
		"bc:24:11:8a:3d:90": "Proxmox",
	} {
		if got := Lookup(mac); !strings.Contains(got, want) {
			t.Errorf("%s: %q does not contain %q", mac, got, want)
		}
	}
	if Lookup("zz:zz") != "" || Lookup("") != "" || Lookup("a4:5e") != "" {
		t.Errorf("not a MAC address: no answer")
	}
	if len(table) < 30000 {
		t.Errorf("the table is too small: %d", len(table))
	}
}

func TestRandom(t *testing.T) {
	if !Random("da:a1:19:6b:55:0c") || !Random("02:00:00:00:00:01") {
		t.Errorf("a locally administered address is random")
	}
	if Random("a4:5e:60:1c:07:2b") || Random("junk") {
		t.Errorf("a registered address is not random")
	}
	if Lookup("da:a1:19:6b:55:0c") != "" {
		t.Errorf("a random address has no manufacturer")
	}
}
