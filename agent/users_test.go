//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	fixPwFile = "root:x:0:0:root:/root:/bin/bash\nwww-data:x:33:33:www-data:/var/www:/usr/sbin/nologin\nbolgov:x:1000:1000:Owner,,,:/home/bolgov:/bin/bash\ndeploy:x:1001:1001:Deploy:/home/deploy:/bin/bash\nbackup:x:1002:1002::/var/backups:/usr/sbin/nologin\n" //nolint:gosec // a fixture of /etc/passwd, not a credential
	fixGrFile = "root:x:0:\nsudo:x:27:bolgov\ndocker:x:998:bolgov,deploy\nwww-data:x:33:deploy\nbolgov:x:1000:\ndeploy:x:1001:\nops:x:1500:deploy\nbackup:x:1002:\n"
	fixShFile = "root:*:20000:0:99999:7:::\nbolgov:$6$abc:20100:0:99999:7:::\ndeploy:!:20100:0:99999:7::20300:\nbackup:!$6$x:20100:0:99999:7:::\n"
)

type fakeCmd struct {
	name, stdin string
	args        []string
}

// newTestUsers builds a module over fixed files and a recording command runner.
func newTestUsers(t *testing.T, failOn string) (*usersModule, *[]fakeCmd) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	var calls []fakeCmd
	m := &usersModule{
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, fakeCmd{name: name, args: args})
			if name == failOn {
				return []byte("boom"), errors.New("exit 1")
			}
			return nil, nil
		},
		runStdin: func(_ context.Context, stdin, name string, args ...string) ([]byte, error) {
			calls = append(calls, fakeCmd{name: name, stdin: stdin, args: args})
			return nil, nil
		},
		readFile:     os.ReadFile,
		lookPath:     func(string) (string, error) { return "", errors.New("none") },
		now:          func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) },
		writeSudoers: func(string, []byte) error { return nil },
		removeFile:   func(string) error { return nil },
		writeKeys:    func(string, string, string) error { return nil },
		procStatuses: func() []string { return []string{"Name: x\nUid:\t1001\t1001\t1001\t1001\n", "Uid:\t0\t0\t0\t0\n"} },
		passwdPath:   write("passwd", fixPwFile), groupPath: write("group", fixGrFile),
		shadowPath: write("shadow", fixShFile), shellsPath: write("shells", "# list\n/bin/sh\n/bin/bash\n/usr/sbin/nologin\n"),
		sudoersDir: filepath.Join(dir, "sudoers.d"),
	}
	return m, &calls
}

func TestParseShadowStates(t *testing.T) {
	today := int64(20400)
	sh := parseShadow(fixShFile, today)
	if !sh["bolgov"].HasPassword || sh["bolgov"].Locked {
		t.Fatalf("bolgov: %+v", sh["bolgov"])
	}
	if sh["deploy"].HasPassword || !sh["deploy"].Locked {
		t.Fatalf("deploy has no password and an expired account: %+v", sh["deploy"])
	}
	if !sh["backup"].HasPassword {
		t.Fatalf("a password hash behind ! still counts as set: %+v", sh["backup"])
	}
	if sh["root"].HasPassword {
		t.Fatalf("* is no password: %+v", sh["root"])
	}
}

func TestSudoNoPasswd(t *testing.T) {
	c := "# comment\n%sudo ALL=(ALL:ALL) ALL\ndeploy ALL=(ALL:ALL) NOPASSWD: ALL\nother ALL=(ALL) ALL\n"
	if !sudoNoPasswd(c, "deploy") || sudoNoPasswd(c, "other") || sudoNoPasswd(c, "dep") {
		t.Fatal("NOPASSWD must match the whole user name only")
	}
}

func TestParseLastlog(t *testing.T) {
	out := "Username         Port     From             Latest\nroot                                       **Never logged in**\nbolgov           pts/0    10.0.0.5         Mon Nov 16 09:12:01 +0000 2026\n"
	m := parseLastlog(out, time.UTC)
	if len(m) != 1 || m["bolgov"].From != "10.0.0.5" || m["bolgov"].TS == 0 {
		t.Fatalf("%+v", m)
	}
}

func TestUsersStateRows(t *testing.T) {
	m, _ := newTestUsers(t, "")
	st := m.collect(context.Background())
	got := map[string]userRow{}
	for _, u := range st.Users {
		got[u.Name] = u
	}
	if _, ok := got["www-data"]; ok {
		t.Fatal("a system account with no shell and no processes must be hidden")
	}
	if r := got["bolgov"]; r.Sudo != "pw" || !r.Human || len(r.Groups) != 2 {
		t.Fatalf("bolgov: %+v", r)
	}
	if r := got["deploy"]; !r.Locked || r.Procs != 1 || r.Sudo != "none" {
		t.Fatalf("deploy: %+v", r)
	}
	if r := got["root"]; !r.Protected || r.Sudo != "root" || r.Procs != 1 {
		t.Fatalf("root: %+v", r)
	}
	if st.RootLogin {
		t.Fatal("root has no password here")
	}
	for _, want := range []string{"docker", "ops", "www-data"} {
		if !containsStr(st.Groups, want) {
			t.Fatalf("group %s should be offered: %v", want, st.Groups)
		}
	}
	if containsStr(st.Groups, "sudo") || containsStr(st.Groups, "bolgov") || containsStr(st.Groups, "root") {
		t.Fatalf("sudo, root and private groups must not be offered: %v", st.Groups)
	}
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func TestUsersAddBuildsCommandsAndRefusesBadInput(t *testing.T) {
	m, calls := newTestUsers(t, "")
	ctx := context.Background()
	if _, err := m.add(ctx, raw(map[string]any{"name": "reports"}), false); !errors.Is(err, errDangerous) {
		t.Fatalf("needs confirmation: %v", err)
	}
	for name, req := range map[string]map[string]any{
		"upper case":    {"name": "Reports"},
		"existing":      {"name": "deploy"},
		"bad group":     {"name": "reports", "groups": []string{"root"}},
		"bad home":      {"name": "reports", "home": "/etc/x"},
		"bad shell":     {"name": "reports", "shell": "/tmp/evil"},
		"short pass":    {"name": "reports", "password": "abc"},
		"not a key":     {"name": "reports", "ssh_key": "hello"},
		"dotdot home":   {"name": "reports", "home": "/home/../etc"},
		"newline gecos": {"name": "reports", "gecos": "a\nb"},
	} {
		if _, err := m.add(ctx, raw(req), true); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("a refused request must not run anything: %v", *calls)
	}
	if _, err := m.add(ctx, raw(map[string]any{"name": "reports", "groups": []string{"docker"}, "sudo": "nopw", "password": "s3cret-pass-1"}), true); err != nil {
		t.Fatal(err)
	}
	c := *calls
	if c[0].name != "useradd" || !strings.Contains(strings.Join(c[0].args, " "), "--groups docker,sudo") || c[0].args[len(c[0].args)-1] != "reports" {
		t.Fatalf("useradd: %+v", c[0])
	}
	if c[1].name != "chpasswd" || c[1].stdin != "reports:s3cret-pass-1\n" || len(c[1].args) != 0 {
		t.Fatalf("the password must go through stdin only: %+v", c[1])
	}
	for _, cc := range c {
		if strings.Contains(strings.Join(cc.args, " "), "s3cret") {
			t.Fatal("the password leaked into a command line")
		}
	}
	if c[len(c)-1].name != "visudo" {
		t.Fatalf("the sudoers file must be checked: %+v", c)
	}
}

func TestUsersUpdateGuardsAndGroups(t *testing.T) {
	m, calls := newTestUsers(t, "")
	ctx := context.Background()
	// root and system accounts are never changed
	for _, name := range []string{"root", "www-data", "ghost", "../x"} {
		if _, err := m.update(ctx, raw(map[string]any{"name": name, "locked": true}), true); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	// bolgov is the only unlocked user with sudo and root has no password: sudo cannot be taken away
	if _, err := m.update(ctx, raw(map[string]any{"name": "bolgov", "sudo": "none"}), true); err == nil || !strings.Contains(err.Error(), "only unlocked user with sudo") {
		t.Fatalf("last sudo guard: %v", err)
	}
	if _, err := m.update(ctx, raw(map[string]any{"name": "bolgov", "locked": true}), true); err == nil {
		t.Fatal("the last administrator cannot be locked")
	}
	if len(*calls) != 0 {
		t.Fatalf("nothing may run: %v", *calls)
	}
	// deploy: a group the panel does not offer is refused; offered ones are replaced, unmanaged ones stay
	if _, err := m.update(ctx, raw(map[string]any{"name": "deploy", "groups": []string{"shadow"}}), true); err == nil {
		t.Fatal("a group that is not offered must be refused")
	}
	if _, err := m.update(ctx, raw(map[string]any{"name": "deploy", "groups": []string{"ops"}, "sudo": "pw"}), true); err != nil {
		t.Fatal(err)
	}
	c := *calls
	if c[0].name != "usermod" || strings.Join(c[0].args, " ") != "-G ops,sudo deploy" {
		t.Fatalf("usermod: %+v", c[0])
	}
}

func TestUsersDeleteRefusesSystemAccounts(t *testing.T) {
	m, calls := newTestUsers(t, "")
	ctx := context.Background()
	for _, name := range []string{"root", "www-data", "backup2"} {
		if _, err := m.del(ctx, raw(map[string]any{"name": name}), true); err == nil {
			t.Errorf("%s must not be deleted", name)
		}
	}
	if _, err := m.del(ctx, raw(map[string]any{"name": "deploy", "remove_home": true}), true); err != nil {
		t.Fatal(err)
	}
	var del fakeCmd
	for _, c := range *calls {
		if c.name == "userdel" {
			del = c
		}
	}
	if strings.Join(del.args, " ") != "--remove deploy" {
		t.Fatalf("userdel: %+v", del)
	}
}

func TestTermUserRefusesOtherAccounts(t *testing.T) {
	for _, name := range []string{"", "a b", "$(id)", "nonexistent-user-xyz"} {
		if _, err := termUser(name); err == nil {
			t.Errorf("%q must be refused", name)
		}
	}
}
