package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestParseBanTimesIgnoreAndLog(t *testing.T) {
	utc := time.UTC
	bans := parseBanTimes("198.51.100.7 \t2026-11-15 14:41:02 + 3600 = 2026-11-15 15:41:02\n203.0.113.9 \t2026-11-15 15:02:00 + -1 = 2026-11-15 15:02:00\n", "sshd", utc)
	if len(bans) != 2 || bans[0].IP != "198.51.100.7" || bans[0].UnbanAt-bans[0].BannedAt != 3600 || bans[1].UnbanAt != 0 {
		t.Fatalf("%+v", bans)
	}
	ign := parseF2BIgnore("These IP addresses/networks are ignored:\n|- 127.0.0.0/8\n|- ::1\n`- 10.0.0.0/24\n")
	if strings.Join(ign, " ") != "127.0.0.0/8 ::1 10.0.0.0/24" {
		t.Fatalf("%v", ign)
	}
	if len(parseF2BIgnore("No IP address/network is ignored\n")) != 0 {
		t.Fatal("an empty list")
	}
	log := `2026-11-15 14:41:02,123 fail2ban.filter         [812]: INFO    [sshd] Found 198.51.100.7 - 2026-11-15 14:41:02
2026-11-15 14:41:03,001 fail2ban.actions        [812]: NOTICE  [sshd] Ban 198.51.100.7
2026-11-15 15:41:03,001 fail2ban.actions        [812]: NOTICE  [sshd] Unban 198.51.100.7
2026-11-15 15:50:00,001 fail2ban.actions        [812]: NOTICE  [recidive] Restore Ban 192.0.2.5
`
	ev := parseF2BLog(log, utc)
	if len(ev) != 3 || ev[0].Event != "ban" || ev[1].Event != "unban" || ev[2].Jail != "recidive" || ev[2].Event != "ban" {
		t.Fatalf("%+v", ev)
	}
}

func TestF2BConfRoundTrip(t *testing.T) {
	s := f2bSettings{MaxRetry: 5, FindTime: 600, BanTime: 3600, Increment: true, Recidive: true, Ignore: []string{"127.0.0.1/8", "::1", "10.0.0.0/24"}}
	text := renderF2BConf(s, f2bConfOptions{Ports: []string{"22", "2200"}, BanAction: "nftables", AllPorts: "nftables[type=allports]", SystemdLogs: true})
	for _, want := range []string{"ignoreip = 127.0.0.1/8 ::1 10.0.0.0/24", "banaction = nftables", "bantime.increment = true", "port = 22,2200", "backend = systemd", "maxretry = 5", "findtime = 600", "bantime = 3600", "[recidive]"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q missing:\n%s", want, text)
		}
	}
	back, ok := parseF2BConf(text)
	if !ok || back.MaxRetry != 5 || back.FindTime != 600 || back.BanTime != 3600 || !back.Increment || !back.Recidive || len(back.Ignore) != 3 {
		t.Fatalf("%+v %v", back, ok)
	}
	plain := renderF2BConf(f2bSettings{MaxRetry: 3, FindTime: 600, BanTime: -1}, f2bConfOptions{})
	if strings.Contains(plain, "increment") || strings.Contains(plain, "recidive") || !strings.Contains(plain, "bantime = -1") {
		t.Fatalf("%s", plain)
	}
}

func TestValidateF2BSettings(t *testing.T) {
	ok := f2bSettings{MaxRetry: 5, FindTime: 600, BanTime: 3600, Ignore: []string{"10.0.0.0/24", "10.0.0.0/24", "2001:db8::1"}}
	if err := validateF2BSettings(&ok); err != nil || len(ok.Ignore) != 2 {
		t.Fatalf("%v %v", err, ok.Ignore)
	}
	for _, bad := range []f2bSettings{
		{MaxRetry: 0, FindTime: 600, BanTime: 3600},
		{MaxRetry: 5, FindTime: 10, BanTime: 3600},
		{MaxRetry: 5, FindTime: 600, BanTime: 5},
		{MaxRetry: 5, FindTime: 600, BanTime: 3600, Ignore: []string{"1.2.3.4\nbantime = 0"}},
		{MaxRetry: 5, FindTime: 600, BanTime: 3600, Ignore: []string{"not-an-ip"}},
	} {
		if validateF2BSettings(&bad) == nil {
			t.Errorf("%+v must be rejected", bad)
		}
	}
	forever := f2bSettings{MaxRetry: 5, FindTime: 600, BanTime: -1}
	if validateF2BSettings(&forever) != nil {
		t.Error("a permanent ban is allowed")
	}
	if got := withLoopback([]string{"10.0.0.0/24"}, "198.51.100.20"); strings.Join(got, " ") != "127.0.0.1/8 ::1 10.0.0.0/24 198.51.100.20" {
		t.Fatalf("%v", got)
	}
}

func f2bFake() (*fakeSys, *secModule) {
	f := &fakeSys{runs: map[string]string{
		"fail2ban-client get sshd ignoreip":            "These IP addresses/networks are ignored:\n|- 127.0.0.0/8\n`- 10.0.0.0/24\n",
		"fail2ban-client set sshd banip 203.0.113.9":   "1",
		"fail2ban-client set sshd unbanip 203.0.113.9": "1",
		"fail2ban-client unban --all":                  "2",
		"fail2ban-client reload":                       "OK",
		"fail2ban-client get sshd maxretry":            "5\n",
		"sshd -T":                                      "port 22\n",
	}, files: map[string]string{}}
	return f, f.module()
}

func TestF2BBan(t *testing.T) {
	f, m := f2bFake()
	ctx := context.Background()
	call := func(v any, dangerous bool) (any, error) {
		b, _ := json.Marshal(v)
		return m.f2bBanIP(ctx, b, dangerous)
	}
	if _, err := call(map[string]any{"ip": "203.0.113.9"}, false); !errors.Is(err, errDangerous) {
		t.Fatalf("a ban needs a confirmation: %v", err)
	}
	for name, p := range map[string]map[string]any{
		"own address": {"ip": "198.51.100.20", "self": "198.51.100.20"},
		"loopback":    {"ip": "127.0.0.1"},
		"ignored":     {"ip": "10.0.0.5"},
		"not an ip":   {"ip": "1.2.3.4; reboot"},
		"bad time":    {"ip": "203.0.113.9", "duration": 5},
	} {
		if _, err := call(p, true); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	for _, c := range f.ran {
		if strings.Contains(c, "banip") {
			t.Fatalf("a refused request must not reach fail2ban: %v", f.ran)
		}
	}
	if _, err := call(map[string]any{"ip": "203.0.113.9"}, true); err != nil {
		t.Fatal(err)
	}
	f.runs["/usr/bin/systemd-run --quiet --unit linager-unban-"+itoa(secNow*1_000_000_000)+" --on-active=3600 /usr/bin/fail2ban-client set sshd unbanip 203.0.113.9"] = ""
	res, err := call(map[string]any{"ip": "203.0.113.9", "duration": 3600}, true)
	if err != nil || res.(map[string]any)["timed"] != true {
		t.Fatalf("a timed ban schedules the unban: %v %v", res, err)
	}
}

func TestF2BUnbanAndSettings(t *testing.T) {
	f, m := f2bFake()
	ctx := context.Background()
	if _, err := m.f2bUnban(ctx, json.RawMessage(`{"ips":["203.0.113.9"]}`), false); !errors.Is(err, errDangerous) {
		t.Fatal(err)
	}
	if _, err := m.f2bUnban(ctx, json.RawMessage(`{"ips":["203.0.113.9"]}`), true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.f2bUnban(ctx, json.RawMessage(`{"ips":["x; y"]}`), true); err == nil {
		t.Fatal("an address is validated")
	}
	if _, err := m.f2bUnban(ctx, json.RawMessage(`{"all":true}`), true); err != nil {
		t.Fatal(err)
	}
	var written string
	m.writeConf = func(path string, data []byte) error {
		if path != f2bConfPath {
			t.Fatalf("path %s", path)
		}
		written = string(data)
		return nil
	}
	body := `{"maxretry":4,"findtime":600,"bantime":7200,"increment":true,"recidive":false,"ignore":["10.0.0.0/24"],"self":"198.51.100.20"}`
	if _, err := m.f2bSaveSettings(ctx, json.RawMessage(body), true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(written, "maxretry = 4") || !strings.Contains(written, "198.51.100.20") || !strings.Contains(written, "127.0.0.1/8") {
		t.Fatalf("%s", written)
	}
	reloaded := false
	for _, c := range f.ran {
		reloaded = reloaded || c == "fail2ban-client reload"
	}
	if !reloaded {
		t.Fatalf("fail2ban must reload: %v", f.ran)
	}
	f.absent = map[string]bool{"fail2ban-client": true}
	if _, err := m.f2bUnban(ctx, json.RawMessage(`{"all":true}`), true); err == nil {
		t.Fatal("without fail2ban there is nothing to do")
	}
}

func TestBanHistory(t *testing.T) {
	f, m := f2bFake()
	f.files[f2bLogPath+".1"] = "2026-11-14 22:40:00,001 fail2ban.actions        [812]: NOTICE  [recidive] Ban 198.51.100.55\n"
	f.files[f2bLogPath] = "2026-11-15 14:41:03,001 fail2ban.actions        [812]: NOTICE  [sshd] Ban 198.51.100.7\n2026-11-15 15:41:03,001 fail2ban.actions        [812]: NOTICE  [sshd] Unban 198.51.100.7\n"
	res, err := m.f2bBanHistory(context.Background(), json.RawMessage(`{"limit":2}`), false)
	if err != nil {
		t.Fatal(err)
	}
	r := res.(map[string]any)
	ev := r["events"].([]f2bEvent)
	if r["total"] != 3 || len(ev) != 2 || ev[0].Event != "unban" || ev[1].IP != "198.51.100.7" {
		t.Fatalf("%v", r)
	}
}

func TestKeyRemove(t *testing.T) {
	pub1, _, _ := ed25519.GenerateKey(rand.Reader)
	pub2, _, _ := ed25519.GenerateKey(rand.Reader)
	k1, _ := ssh.NewPublicKey(pub1)
	k2, _ := ssh.NewPublicKey(pub2)
	line1 := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k1))) + " one@host"
	line2 := `command="/bin/true" ` + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k2))) + " two@host"
	file := "# my keys\n" + line1 + "\n" + line2 + "\n"
	fp := ssh.FingerprintSHA256(k1)

	next, n := removeKeyByFingerprint(file, fp)
	if n != 1 || strings.Contains(next, "one@host") || !strings.Contains(next, "two@host") || !strings.Contains(next, "# my keys") {
		t.Fatalf("%d\n%s", n, next)
	}
	if _, n := removeKeyByFingerprint(file, "SHA256:"+strings.Repeat("A", 43)); n != 0 {
		t.Fatal("an unknown fingerprint removes nothing")
	}

	files := map[string]string{ //nolint:gosec // test data, not credentials
		"/etc/passwd":                      "alice:x:1000:1000::/home/alice:/bin/bash\n",
		"/home/alice/.ssh/authorized_keys": file,
	}
	f := &fakeSys{runs: map[string]string{"sshd -T": "authorizedkeysfile .ssh/authorized_keys\n"}, files: files}
	m := f.module()
	m.passwdPath = "/etc/passwd"
	written := map[string]string{}
	m.writeKeyFile = func(path string, data []byte, _ string) error { written[path] = string(data); return nil }
	ctx := context.Background()
	call := func(user, path, fingerprint string, dangerous bool) error {
		b, _ := json.Marshal(map[string]string{"user": user, "file": path, "fingerprint": fingerprint})
		_, err := m.keyRemove(ctx, b, dangerous)
		return err
	}
	if err := call("alice", "/home/alice/.ssh/authorized_keys", fp, false); !errors.Is(err, errDangerous) {
		t.Fatalf("needs a confirmation: %v", err)
	}
	for name, args := range map[string][3]string{
		"another file": {"alice", "/etc/shadow", fp},
		"another user": {"mallory", "/home/alice/.ssh/authorized_keys", fp},
		"bad print":    {"alice", "/home/alice/.ssh/authorized_keys", "SHA256:x"},
		"not in file":  {"alice", "/home/alice/.ssh/authorized_keys", "SHA256:" + strings.Repeat("B", 43)},
	} {
		if err := call(args[0], args[1], args[2], true); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if len(written) != 0 {
		t.Fatalf("a refused request writes nothing: %v", written)
	}
	if err := call("alice", "/home/alice/.ssh/authorized_keys", fp, true); err != nil {
		t.Fatal(err)
	}
	if len(written) != 2 {
		t.Fatalf("the copy and the new file: %v", written)
	}
	for path, content := range written {
		if strings.Contains(path, ".bak-") {
			if content != file {
				t.Fatal("the copy must be the old content")
			}
		} else if strings.Contains(content, "one@host") {
			t.Fatal("the key must be gone")
		}
	}
}
