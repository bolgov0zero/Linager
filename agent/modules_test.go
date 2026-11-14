package agent

import "testing"

func TestParseUnits(t *testing.T) {
	lu := "  ssh.service loaded active running OpenBSD Secure Shell server\n● foo.service loaded failed failed Foo daemon\n  gone.service not-found inactive dead gone.service\n  getty@.service loaded inactive dead tmpl\n"
	lf := "ssh.service enabled enabled\nfoo.service disabled enabled\ncron.service enabled enabled\ngetty@.service enabled enabled\n"
	u := parseUnits(lu, lf)
	if len(u) != 3 {
		t.Fatalf("expected ssh, foo and cron, got %+v", u)
	}
	got := map[string]Unit{}
	for _, x := range u {
		got[x.Name] = x
	}
	if got["foo.service"].Active != "failed" || got["foo.service"].Enabled != "disabled" || got["ssh.service"].Description != "OpenBSD Secure Shell server" {
		t.Fatalf("%+v", got)
	}
	if got["cron.service"].Active != "inactive" || got["cron.service"].Enabled != "enabled" {
		t.Fatalf("unit known only from unit files must be inactive: %+v", got["cron.service"])
	}
}

func TestServiceNamesAndDanger(t *testing.T) {
	for _, ok := range []string{"ssh.service", "systemd-journald.service", "foo@bar.service"} {
		if !unitRe.MatchString(ok) {
			t.Errorf("%s must be valid", ok)
		}
	}
	for _, bad := range []string{"ssh", "a b.service", "../x.service", "x;rm.service", "$(id).service", ""} {
		if unitRe.MatchString(bad) {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if !IsDangerousService("ssh.service", "stop") || !IsDangerousService("linager-agent.service", "restart") || IsDangerousService("ssh.service", "start") || IsDangerousService("cron.service", "stop") {
		t.Fatal("danger classification wrong")
	}
}

func TestParseAptInst(t *testing.T) {
	out := "Inst libc6 [2.36-9] (2.36-9+deb12u7 Debian-Security:12/stable-security [amd64])\nInst vim [2:9.0] (2:9.0.1 Debian:12.5/stable [amd64])\nInst linux-image-6.1 (6.1.99 Debian:12/stable [amd64])\nConf vim\n"
	p := parseAptInst(out)
	if len(p) != 3 || !p[0].Security || !p[0].Reboot || p[1].Security || p[1].Candidate != "2:9.0.1" || p[2].Current != "" || !p[2].Reboot {
		t.Fatalf("%+v", p)
	}
}

func TestParseProcStat(t *testing.T) {
	line := "123 (my (weird) proc) S 1 123 123 0 -1 4194560 100 0 0 0 50 25 0 0 20 0 1 0 100 1000000 321 18446744073709551615"
	ps, ok := parseProcStat(line)
	if !ok || ps.comm != "my (weird) proc" || ps.state != "S" || ps.ticks != 75 || ps.rss != 321 {
		t.Fatalf("%+v %v", ps, ok)
	}
	if _, ok := parseProcStat("garbage"); ok {
		t.Fatal("garbage must not parse")
	}
}
