package agent

import "testing"

func TestValidWinUser(t *testing.T) {
	for _, ok := range []string{"alice", "Bob Smith", "svc_backup", "иван", "a.b"} {
		if !validWinUser(ok) {
			t.Errorf("%q must be accepted", ok)
		}
	}
	for _, bad := range []string{"", " lead", "trailing.", `dom\user`, "a/b", "a:b", "a;b", "a|b", "a=b", "a,b", "a+b", "a*b", "a?b", "a<b", `a"b`, "a@b", "x234567890123456789012", "a\x00b"} {
		if validWinUser(bad) {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestValidWinGroups(t *testing.T) {
	if err := validWinGroups([]string{"Administrators", "Remote Desktop Users", "Пользователи"}); err != nil {
		t.Fatal(err)
	}
	if err := validWinGroups([]string{"ok", `bad\name`}); err == nil {
		t.Fatal("a backslash must be refused")
	}
}

func TestBuiltinAdminSID(t *testing.T) {
	if !builtinAdminSID("S-1-5-21-1111-2222-3333-500") || builtinAdminSID("S-1-5-21-1111-2222-3333-1001") || builtinAdminSID("S-1-5-32-544") {
		t.Fatal("the built-in administrator is the SID that ends in -500")
	}
}

func TestValidateNetApply(t *testing.T) {
	good := []netApply{
		{Index: 3, Mode: "dhcp"},
		{Index: 3, Mode: "static", IP: "192.168.1.20", Prefix: 24, Gateway: "192.168.1.1", DNS: []string{"1.1.1.1", "8.8.8.8"}},
		{Index: 3, Mode: "static", IP: "10.0.0.5", Prefix: 8},
	}
	for _, r := range good {
		if err := validateNetApply(&r); err != nil {
			t.Errorf("%+v: %v", r, err)
		}
	}
	bad := map[string]netApply{
		"no adapter":          {Mode: "dhcp"},
		"bad mode":            {Index: 3, Mode: "auto"},
		"not an address":      {Index: 3, Mode: "static", IP: "300.1.1.1", Prefix: 24},
		"loopback":            {Index: 3, Mode: "static", IP: "127.0.0.1", Prefix: 8},
		"broadcast":           {Index: 3, Mode: "static", IP: "255.255.255.255", Prefix: 24},
		"prefix too long":     {Index: 3, Mode: "static", IP: "10.0.0.5", Prefix: 31},
		"prefix zero":         {Index: 3, Mode: "static", IP: "10.0.0.5", Prefix: 0},
		"gateway elsewhere":   {Index: 3, Mode: "static", IP: "192.168.1.20", Prefix: 24, Gateway: "10.0.0.1"},
		"gateway is the host": {Index: 3, Mode: "static", IP: "192.168.1.20", Prefix: 24, Gateway: "192.168.1.20"},
		"bad dns":             {Index: 3, Mode: "dhcp", DNS: []string{"example.com"}},
		"too many dns":        {Index: 3, Mode: "dhcp", DNS: []string{"1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4", "9.9.9.9"}},
	}
	for name, r := range bad {
		if err := validateNetApply(&r); err == nil {
			t.Errorf("%s: must be refused", name)
		}
	}
	// a DHCP request drops the static fields
	r := netApply{Index: 3, Mode: "dhcp", IP: "1.2.3.4", Prefix: 24, Gateway: "1.2.3.1"}
	_ = validateNetApply(&r)
	if r.IP != "" || r.Gateway != "" || r.Prefix != 0 {
		t.Fatalf("%+v", r)
	}
}
