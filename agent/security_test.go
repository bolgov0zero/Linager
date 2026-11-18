//go:build !windows

package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestParseJournalLines(t *testing.T) {
	cases := []struct {
		line string
		want sshEvent
		ok   bool
	}{
		{"1731666000.123456 web-01 sshd[1234]: Accepted publickey for root from 203.0.113.5 port 51234 ssh2: ED25519 SHA256:abc",
			sshEvent{Kind: "accepted", TS: 1731666000, User: "root", IP: "203.0.113.5", Method: "publickey", Port: 51234}, true},
		{"1731666100.000000 web-01 sshd-session[1300]: Failed password for invalid user admin from 198.51.100.7 port 4444 ssh2",
			sshEvent{Kind: "failed", TS: 1731666100, User: "admin", IP: "198.51.100.7", Method: "password", Port: 4444, Invalid: true}, true},
		{"1731666101.5 web-01 sshd[1301]: Failed password for root from 2001:db8::1 port 4445 ssh2",
			sshEvent{Kind: "failed", TS: 1731666101, User: "root", IP: "2001:db8::1", Method: "password", Port: 4445}, true},
		{"1731666102 web-01 sshd[1302]: Invalid user test from 198.51.100.8 port 5555",
			sshEvent{Kind: "invalid", TS: 1731666102, User: "test", IP: "198.51.100.8", Invalid: true}, true},
		{"1731666103 web-01 sshd[1303]: Accepted keyboard-interactive/pam for deploy from 198.51.100.7 port 4446 ssh2",
			sshEvent{Kind: "accepted", TS: 1731666103, User: "deploy", IP: "198.51.100.7", Method: "keyboard-interactive", Port: 4446}, true},
		{"1731666104 web-01 sshd[1]: Server listening on 0.0.0.0 port 22.", sshEvent{}, false},
		{"garbage", sshEvent{}, false},
		{"abc web-01 sshd[1]: Accepted publickey for root from 1.2.3.4 port 1 ssh2", sshEvent{}, false},
	}
	for _, c := range cases {
		got, ok := parseJournalLine(c.line)
		if ok != c.ok || got != c.want {
			t.Errorf("%q:\n got %+v %v\nwant %+v %v", c.line, got, ok, c.want, c.ok)
		}
	}
}

func TestParseAuthLog(t *testing.T) {
	now := time.Date(2024, 11, 15, 12, 0, 0, 0, time.UTC)
	rfc := "2024-11-15T10:00:00.123456+00:00 web-01 sshd[1]: Failed password for root from 198.51.100.7 port 22 ssh2"
	ev, ok := parseAuthLogLine(rfc, now)
	if !ok || ev.TS != time.Date(2024, 11, 15, 10, 0, 0, 0, time.UTC).Unix() || ev.User != "root" {
		t.Fatalf("rfc3339: %+v %v", ev, ok)
	}
	old := "Nov  3 08:15:00 web-01 sshd[7]: Accepted password for bob from 192.0.2.9 port 40000 ssh2"
	ev, ok = parseAuthLogLine(old, now)
	if !ok || ev.TS != time.Date(2024, 11, 3, 8, 15, 0, 0, time.UTC).Unix() || ev.Kind != "accepted" {
		t.Fatalf("traditional: %+v %v", ev, ok)
	}
	// a December line read in January belongs to the previous year
	jan := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	if ev, ok := parseAuthLogLine("Dec 31 23:00:00 h sshd[1]: Failed password for x from 1.1.1.1 port 1 ssh2", jan); !ok || time.Unix(ev.TS, 0).UTC().Year() != 2024 {
		t.Fatalf("year rollover: %+v", ev)
	}
	if _, ok := parseAuthLogLine("Nov  3 08:15:00 web-01 cron[7]: something", now); ok {
		t.Fatal("other programs must be ignored")
	}
}

func TestParseSSHDConfig(t *testing.T) {
	conf := parseSSHDConfig(`# comment
Port 2222
PermitRootLogin prohibit-password
permitrootlogin yes
PasswordAuthentication=no
Match User bob
  PasswordAuthentication yes
`)
	if conf["port"] != "2222" || conf["permitrootlogin"] != "prohibit-password" || conf["passwordauthentication"] != "no" {
		t.Fatalf("first value must win, Match is ignored: %v", conf)
	}
	eff := parseSSHDConfig("port 22\nport 2200\npermitrootlogin without-password\nkbdinteractiveauthentication no\nauthorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2\n")
	if eff["authorizedkeysfile"] != ".ssh/authorized_keys .ssh/authorized_keys2" || eff["kbdinteractiveauthentication"] != "no" {
		t.Fatalf("effective listing: %v", eff)
	}
}

const f2bStatus = `Status
|- Number of jail:	2
` + "`" + `- Jail list:	recidive, sshd
`

const f2bJailOut = `Status for the jail: sshd
|- Filter
|  |- Currently failed:	3
|  |- Total failed:	120
|  ` + "`" + `- File list:	/var/log/auth.log
` + "`" + `- Actions
   |- Currently banned:	2
   |- Total banned:	9
   ` + "`" + `- Banned IP list:	198.51.100.7 203.0.113.9
`

func TestParseFail2ban(t *testing.T) {
	jails := parseF2BJails(f2bStatus)
	if len(jails) != 2 || jails[0] != "recidive" || jails[1] != "sshd" {
		t.Fatalf("jails: %v", jails)
	}
	j := parseF2BJail("sshd", f2bJailOut)
	if j.CurrentlyFailed != 3 || j.TotalFailed != 120 || j.CurrentlyBanned != 2 || j.TotalBanned != 9 || len(j.BannedIPs) != 2 || j.BannedIPs[1] != "203.0.113.9" {
		t.Fatalf("jail: %+v", j)
	}
	if empty := parseF2BJail("x", "Status for the jail: x\n|- Actions\n   |- Banned IP list:\t\n"); len(empty.BannedIPs) != 0 {
		t.Fatalf("empty ban list: %+v", empty)
	}
}

func TestParseAuthorizedKeys(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	edKey, _ := ssh.NewPublicKey(pub)
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 1024) //nolint:gosec // a deliberately weak key to test the weak-key flag
	if err != nil {
		t.Skip("cannot generate a small RSA key:", err)
	}
	rsaKey, _ := ssh.NewPublicKey(&rsaPriv.PublicKey)
	ed := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(edKey)))
	rs := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(rsaKey)))

	content := "# my keys\n\n" + ed + " alice@laptop\n" +
		`command="echo a b",no-pty ` + rs + " old backup key\n" +
		"ssh-ed25519 !!!notbase64 broken\n" +
		"not a key line\n"
	keys := parseAuthorizedKeys("alice", "/home/alice/.ssh/authorized_keys", content, 1700000000)
	if len(keys) != 2 {
		t.Fatalf("keys: %+v", keys)
	}
	if keys[0].Type != "ssh-ed25519" || keys[0].Fingerprint != ssh.FingerprintSHA256(edKey) || keys[0].Comment != "alice@laptop" || keys[0].Restricted || keys[0].Weak {
		t.Fatalf("ed25519 key: %+v", keys[0])
	}
	if keys[1].Fingerprint != ssh.FingerprintSHA256(rsaKey) || keys[1].Bits != 1024 || !keys[1].Weak || !keys[1].Restricted || keys[1].Comment != "old backup key" {
		t.Fatalf("rsa key with options: %+v", keys[1])
	}
	if keys[0].ModTime != 1700000000 || keys[0].User != "alice" {
		t.Fatalf("metadata: %+v", keys[0])
	}
}

func TestPasswdAndPaths(t *testing.T) {
	users := parsePasswd(`root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
alice:x:1000:1000:Alice:/home/alice:/bin/bash
svc:x:1001:1001::/var/lib/svc:/bin/false
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
bob:x:1002:1002::/home/bob:/bin/zsh
`)
	if len(users) != 3 || users[0].Name != "root" || users[1].Name != "alice" || users[2].Name != "bob" {
		t.Fatalf("users: %+v", users)
	}
	if p := expandKeyPath(".ssh/authorized_keys", "/home/alice", "alice"); p != "/home/alice/.ssh/authorized_keys" {
		t.Fatalf("relative: %s", p)
	}
	if p := expandKeyPath("/etc/ssh/keys/%u", "/home/alice", "alice"); p != "/etc/ssh/keys/alice" {
		t.Fatalf("%%u: %s", p)
	}
	if p := expandKeyPath("%h/.keys", "/home/alice", "alice"); p != "/home/alice/.keys" {
		t.Fatalf("%%h: %s", p)
	}
}

func TestUnattendedEnabled(t *testing.T) {
	on := `APT::Periodic::Update-Package-Lists "1";` + "\n" + `APT::Periodic::Unattended-Upgrade "1";`
	off := `APT::Periodic::Unattended-Upgrade "0";`
	if !unattendedEnabled([]string{on}) || unattendedEnabled([]string{off}) || unattendedEnabled(nil) {
		t.Fatal("basic cases")
	}
	if unattendedEnabled([]string{on, off}) || !unattendedEnabled([]string{off, on}) {
		t.Fatal("the last file wins")
	}
}

// ---- the module with a fake system ----

const secNow = 1731670000 // 2024-11-15 12:06 UTC

type fakeSys struct {
	runs   map[string]string // "name arg arg" -> output; missing = error
	files  map[string]string
	globs  map[string][]string
	lines  []string // journal output of the sshd query
	oldest string   // first line of the full journal
	absent map[string]bool
	calls  int
	ran    []string // every command that was run, as "name args"
}

func (f *fakeSys) module() *secModule {
	m := newSecModule()
	m.now = func() time.Time { return time.Unix(secNow, 0).UTC() }
	m.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		f.calls++
		f.ran = append(f.ran, name+" "+strings.Join(args, " "))
		if out, ok := f.runs[name+" "+strings.Join(args, " ")]; ok {
			return []byte(out), nil
		}
		return nil, errors.New("not available")
	}
	m.stream = func(_ context.Context, name string, args []string, onLine func(string) bool) error {
		f.calls++
		lines := f.lines
		if len(args) == 4 { // "-o short-unix -q --no-pager": the whole journal
			lines = []string{f.oldest}
		}
		for _, l := range lines {
			if !onLine(l) {
				break
			}
		}
		return nil
	}
	m.readFile = func(p string) ([]byte, error) {
		if c, ok := f.files[p]; ok {
			return []byte(c), nil
		}
		return nil, errors.New("no such file")
	}
	m.glob = func(p string) ([]string, error) { return f.globs[p], nil }
	m.mtime = func(string) int64 { return 1700000000 }
	m.lookPath = func(n string) (string, error) {
		if f.absent[n] {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + n, nil
	}
	return m
}

func decodeState(t *testing.T, v any, err error) secState {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	var st secState
	b, _ := json.Marshal(v)
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSecurityStateFromJournal(t *testing.T) {
	day := int64(86400)
	ts := func(back int64) string { return itoa(secNow - back) }
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	k, _ := ssh.NewPublicKey(pub)
	f := &fakeSys{
		runs: map[string]string{
			"sshd -T":                "port 22\nport 2200\npermitrootlogin yes\npasswordauthentication yes\npubkeyauthentication yes\nkbdinteractiveauthentication no\nmaxauthtries 6\nauthorizedkeysfile .ssh/authorized_keys\n",
			"fail2ban-client status": f2bStatus, "fail2ban-client status sshd": f2bJailOut, "fail2ban-client status recidive": "Status for the jail: recidive\n|- Actions\n   |- Currently banned:\t0\n",
			"loginctl list-sessions --no-legend":                                   "  3 1000 alice seat0 -\n  4 0 root - -\n",
			"loginctl show-session 3 -p Name -p RemoteHost -p Remote -p Timestamp": "Name=alice\nRemote=no\nRemoteHost=\nTimestamp=Fri 2024-11-15 10:00:00 UTC\n",
			"loginctl show-session 4 -p Name -p RemoteHost -p Remote -p Timestamp": "Name=root\nRemote=yes\nRemoteHost=203.0.113.5\nTimestamp=Fri 2024-11-15 11:30:00 UTC\n",
		},
		files: map[string]string{ //nolint:gosec // log lines of the test, not credentials
			"/etc/passwd":                         "root:x:0:0:root:/root:/bin/bash\nalice:x:1000:1000::/home/alice:/bin/bash\n",
			"/root/.ssh/authorized_keys":          strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k))) + " root@ops\n",
			"/etc/apt/apt.conf.d/20auto-upgrades": `APT::Periodic::Unattended-Upgrade "1";`,
		},
		globs: map[string][]string{"/etc/apt/apt.conf.d/*": {"/etc/apt/apt.conf.d/20auto-upgrades"}},
		lines: []string{
			ts(9*day) + " h sshd[1]: Failed password for root from 198.51.100.99 port 1 ssh2", // older than seven days: ignored
			ts(3*day) + " h sshd[2]: Failed password for invalid user admin from 198.51.100.7 port 4444 ssh2",
			ts(3*day-10) + " h sshd-session[3]: Failed password for invalid user admin from 198.51.100.7 port 4445 ssh2",
			ts(day/2) + " h sshd[4]: Failed password for root from 198.51.100.7 port 4446 ssh2",
			ts(1*day-5) + " h sshd[5]: Invalid user test from 198.51.100.8 port 5555",
			ts(3600) + " h sshd[6]: Accepted publickey for root from 203.0.113.5 port 51234 ssh2: ED25519 SHA256:abc",
			ts(1800) + " h sshd[7]: Accepted password for deploy from 198.51.100.7 port 4447 ssh2",
		},
		oldest: ts(30*day) + " h kernel: boot",
	}
	m := f.module()
	v, err := m.state(context.Background(), nil, false)
	st := decodeState(t, v, err)

	if !st.SSH.Installed || st.SSH.Source != "effective" || strings.Join(st.SSH.Ports, ",") != "22,2200" || st.SSH.PermitRootLogin != "yes" || st.SSH.PasswordAuth != "yes" || st.SSH.KbdInteractive != "no" {
		t.Fatalf("ssh: %+v", st.SSH)
	}
	if st.Journal.Source != "journal" || !st.Journal.CoversPeriod || st.Journal.Oldest != secNow-30*day {
		t.Fatalf("journal: %+v", st.Journal)
	}
	if st.Failures.Total != 3 || st.Failures.InvalidUsers != 1 || st.Failures.Addresses != 1 {
		t.Fatalf("failures: %+v", st.Failures)
	}
	if len(st.Failures.TopIPs) != 1 || st.Failures.TopIPs[0].IP != "198.51.100.7" || st.Failures.TopIPs[0].Count != 3 || st.Failures.TopIPs[0].Users != 2 {
		t.Fatalf("top ips: %+v", st.Failures.TopIPs)
	}
	if st.Failures.TopUsers[0].User != "admin" || st.Failures.TopUsers[0].Count != 2 || !st.Failures.TopUsers[0].Invalid || st.Failures.TopUsers[1].Invalid {
		t.Fatalf("top users: %+v", st.Failures.TopUsers)
	}
	sum := 0
	for _, n := range st.Failures.ByDay {
		sum += n
	}
	if len(st.Failures.ByDay) != 7 || sum != 3 || st.Failures.ByDay[6] != 1 {
		t.Fatalf("by day (the last bucket is the latest 24 hours): %v", st.Failures.ByDay)
	}
	if len(st.Logins) != 2 || st.Logins[0].User != "deploy" || st.Logins[0].PriorFailures != 3 || st.Logins[1].User != "root" || st.Logins[1].PriorFailures != 0 {
		t.Fatalf("logins (newest first, with the earlier failures of the address): %+v", st.Logins)
	}
	if !st.Fail2ban.Installed || !st.Fail2ban.Active || len(st.Fail2ban.Jails) != 2 || st.Fail2ban.Jails[1].Name != "sshd" || st.Fail2ban.Jails[1].CurrentlyBanned != 2 {
		t.Fatalf("fail2ban: %+v", st.Fail2ban)
	}
	if !st.AutoUpdates {
		t.Fatal("auto updates")
	}
	if len(st.Keys) != 1 || st.Keys[0].User != "root" || st.Keys[0].Fingerprint != ssh.FingerprintSHA256(k) || st.Keys[0].Comment != "root@ops" {
		t.Fatalf("keys: %+v", st.Keys)
	}
	if len(st.Sessions) != 1 || st.Sessions[0].User != "root" || st.Sessions[0].IP != "203.0.113.5" || st.Sessions[0].Since == 0 {
		t.Fatalf("sessions (only remote ones): %+v", st.Sessions)
	}

	// the second call within a minute comes from the cache
	before := f.calls
	if _, err := m.state(context.Background(), nil, false); err != nil || f.calls != before {
		t.Fatalf("cache: %d calls after, %d before, err %v", f.calls, before, err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestSecurityStateWithoutJournal(t *testing.T) {
	f := &fakeSys{
		files: map[string]string{ //nolint:gosec // log lines of the test, not credentials
			"/etc/passwd":          "root:x:0:0:root:/root:/bin/bash\n",
			"/etc/ssh/sshd_config": "Port 2222\nPasswordAuthentication no\n",
			"/var/log/auth.log": "2024-11-14T10:00:00+00:00 h sshd[1]: Failed password for root from 198.51.100.7 port 1 ssh2\n" +
				"2024-11-15T10:00:00+00:00 h sshd[1]: Accepted publickey for root from 203.0.113.5 port 2 ssh2\n",
		},
		globs:  map[string][]string{},
		absent: map[string]bool{"journalctl": true, "fail2ban-client": true, "loginctl": true, "unattended-upgrade": true},
	}
	st := decodeState(t, mustState(t, f.module()), nil)
	if st.Journal.Source != "authlog" || st.Failures.Total != 1 || len(st.Logins) != 1 {
		t.Fatalf("auth.log fallback: %+v %+v", st.Journal, st.Failures)
	}
	if st.SSH.Source != "files" || strings.Join(st.SSH.Ports, ",") != "2222" || st.SSH.PasswordAuth != "no" {
		t.Fatalf("config from files: %+v", st.SSH)
	}
	if st.Fail2ban.Installed || st.AutoUpdates || len(st.Sessions) != 0 || st.Keys == nil {
		t.Fatalf("absent tools: %+v", st)
	}
}

func mustState(t *testing.T, m *secModule) any {
	t.Helper()
	v, err := m.state(context.Background(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSecurityStateNoSSH(t *testing.T) {
	f := &fakeSys{files: map[string]string{}, globs: map[string][]string{}, absent: map[string]bool{"sshd": true, "journalctl": true}}
	st := decodeState(t, mustState(t, f.module()), nil)
	if st.SSH.Installed || st.Journal.Source != "none" || st.Failures.Total != 0 || st.Logins == nil || st.Failures.TopIPs == nil {
		t.Fatalf("a host without ssh must give empty, non-null lists: %+v", st)
	}
}

func TestSecurityTruncatesHugeJournals(t *testing.T) {
	// the module stops reading after secMaxLines lines
	old := secMaxLines
	secMaxLines = 1000
	defer func() { secMaxLines = old }()
	f := &fakeSys{files: map[string]string{}, globs: map[string][]string{}}
	m := f.module()
	read := 0
	m.stream = func(_ context.Context, _ string, args []string, onLine func(string) bool) error {
		if len(args) == 4 {
			return nil
		}
		for i := 0; i < secMaxLines+10; i++ {
			read++
			if !onLine(itoa(secNow-100) + " h sshd[1]: Failed password for root from 198.51.100.7 port 1 ssh2") {
				return nil
			}
		}
		return nil
	}
	st := decodeState(t, mustState(t, m), nil)
	if !st.Journal.Truncated || read != secMaxLines+1 {
		t.Fatalf("truncated=%v read=%d", st.Journal.Truncated, read)
	}
}

// ---- ending sessions ----

func sessionsFake() *fakeSys {
	show := func(name, remote, host string) string {
		return "Name=" + name + "\nRemote=" + remote + "\nRemoteHost=" + host + "\nTimestamp=Fri 2024-11-15 10:00:00 UTC\n"
	}
	return &fakeSys{
		runs: map[string]string{
			"loginctl list-sessions --no-legend":                                   "  3 1000 alice seat0 tty1\n  4 0 root - -\n  5 1000 alice - -\n  6 1000 alice - -\n",
			"loginctl show-session 3 -p Name -p RemoteHost -p Remote -p Timestamp": show("alice", "no", ""),
			"loginctl show-session 4 -p Name -p RemoteHost -p Remote -p Timestamp": show("root", "yes", "203.0.113.5"),
			"loginctl show-session 5 -p Name -p RemoteHost -p Remote -p Timestamp": show("alice", "yes", "192.0.2.44"),
			"loginctl show-session 6 -p Name -p RemoteHost -p Remote -p Timestamp": show("alice", "yes", "192.0.2.45"),
			"loginctl terminate-session 4":                                         "", "loginctl terminate-session 5": "", "loginctl terminate-session 6": "",
		},
		files: map[string]string{}, globs: map[string][]string{},
	}
}

func terminated(t *testing.T, v any) []secTerminated {
	t.Helper()
	b, _ := json.Marshal(v)
	var out struct {
		Terminated []secTerminated `json:"terminated"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out.Terminated
}

func terminateCalls(f *fakeSys) []string {
	var out []string
	for _, c := range f.ran {
		if strings.HasPrefix(c, "loginctl terminate-session") {
			out = append(out, c)
		}
	}
	return out
}

func TestSecurityTerminateNeedsConfirmation(t *testing.T) {
	f := sessionsFake()
	if _, err := f.module().terminate(context.Background(), json.RawMessage(`{"session":"4"}`), false); !errors.Is(err, errDangerous) {
		t.Fatalf("without the confirmation of the administrator it must be refused: %v", err)
	}
	if len(terminateCalls(f)) != 0 {
		t.Fatalf("nothing may be run: %v", f.ran)
	}
}

func TestSecurityTerminateOneSession(t *testing.T) {
	f := sessionsFake()
	m := f.module()
	v, err := m.terminate(context.Background(), json.RawMessage(`{"session":"4"}`), true)
	if err != nil {
		t.Fatal(err)
	}
	got := terminated(t, v)
	if len(got) != 1 || got[0].ID != "4" || got[0].User != "root" || got[0].IP != "203.0.113.5" {
		t.Fatalf("result: %+v", got)
	}
	if c := terminateCalls(f); len(c) != 1 || c[0] != "loginctl terminate-session 4" {
		t.Fatalf("only that session must be ended: %v", c)
	}
}

func TestSecurityTerminateRefusesWhatIsNotARemoteSession(t *testing.T) {
	for _, params := range []string{
		`{"session":"3"}`,               // a local console session
		`{"session":"99"}`,              // does not exist
		`{"session":"4;ls"}`,            // not an id
		`{"session":"../x"}`,            // not an id
		`{"user":"nobody"}`,             // no remote session
		`{"user":"a b"}`,                // not a login name
		`{"session":"4","user":"root"}`, // both
		`{}`, `not json`,
	} {
		f := sessionsFake()
		if _, err := f.module().terminate(context.Background(), json.RawMessage(params), true); err == nil {
			t.Errorf("%s must be refused", params)
		}
		if len(terminateCalls(f)) != 0 {
			t.Errorf("%s: nothing may be ended: %v", params, f.ran)
		}
	}
}

func TestSecurityTerminateUserEndsOnlyHisRemoteSessions(t *testing.T) {
	f := sessionsFake()
	m := f.module()
	if _, err := m.state(context.Background(), nil, false); err != nil { // fills the cache
		t.Fatal(err)
	}
	v, err := m.terminate(context.Background(), json.RawMessage(`{"user":"alice"}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if got := terminated(t, v); len(got) != 2 || got[0].ID != "5" || got[1].ID != "6" {
		t.Fatalf("result: %+v", got)
	}
	c := terminateCalls(f)
	if len(c) != 2 || c[0] != "loginctl terminate-session 5" || c[1] != "loginctl terminate-session 6" {
		t.Fatalf("the local session 3 of alice and the session 4 of root must stay: %v", c)
	}
	if m.cache != nil {
		t.Fatal("the cached answer must be dropped, it still shows the ended sessions")
	}
}

func TestSecurityTerminatePartialFailure(t *testing.T) {
	f := sessionsFake()
	delete(f.runs, "loginctl terminate-session 5") // this one fails
	v, err := f.module().terminate(context.Background(), json.RawMessage(`{"user":"alice"}`), true)
	if err != nil {
		t.Fatalf("one ended session is a success: %v", err)
	}
	if got := terminated(t, v); len(got) != 1 || got[0].ID != "6" {
		t.Fatalf("result: %+v", got)
	}
	delete(f.runs, "loginctl terminate-session 6")
	if _, err := f.module().terminate(context.Background(), json.RawMessage(`{"user":"alice"}`), true); err == nil {
		t.Fatal("when nothing could be ended it must be an error")
	}
}

func TestSecurityTerminateWithoutLogind(t *testing.T) {
	f := sessionsFake()
	f.absent = map[string]bool{"loginctl": true}
	if _, err := f.module().terminate(context.Background(), json.RawMessage(`{"session":"4"}`), true); err == nil || !strings.Contains(err.Error(), "logind") {
		t.Fatalf("a host without logind: %v", err)
	}
}
