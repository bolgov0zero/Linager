//go:build !windows

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The "users" module: the local accounts of the host. The browser names an account and what to change; the
// agent checks every name against its own fresh view of /etc/passwd and /etc/group, so a name, a group or a
// path from the browser never selects anything else. Every change needs the confirmation of the administrator
// and is written to the audit log by the server. Passwords reach the agent over the mTLS link, go to chpasswd
// on its standard input and are never written to a log or put into a command line.

// Accounts the panel manages are the regular users (UIDs 1000..59999); root and system accounts are shown
// but never changed.
const (
	uidMinHuman = 1000
	uidMaxHuman = 59999
)

var (
	newUserRe  = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	homePathRe = regexp.MustCompile(`^/[A-Za-z0-9._/-]{1,200}$`)
	sshKeyRe   = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com) [A-Za-z0-9+/=]{20,1000}( [^\r\n]{0,200})?$`)
)

// homePrefixes are the places a home directory may be created in.
var homePrefixes = []string{"/home/", "/srv/", "/opt/", "/var/"}

// userGroupOffer lists the groups the panel offers, besides the groups with a GID of 1000 and more:
// membership in them is granted by name, never by a number from the browser.
var userGroupOffer = []string{"docker", "adm", "www-data", "systemd-journal", "backup", "users", "staff", "plugdev", "video", "audio", "dialout", "cdrom", "ssl-cert", "lxd", "libvirt", "kvm", "ssh-users", "ssh"}

type usersModule struct {
	run      func(ctx context.Context, name string, args ...string) ([]byte, error)
	runStdin func(ctx context.Context, stdin string, name string, args ...string) ([]byte, error)
	readFile func(path string) ([]byte, error)
	lookPath func(name string) (string, error)
	now      func() time.Time

	writeSudoers func(path string, data []byte) error
	removeFile   func(path string) error
	writeKeys    func(home, uidName, key string) error
	procStatuses func() []string

	passwdPath, groupPath, shadowPath, shellsPath, sudoersDir string
}

func newUsersModule() *usersModule {
	return &usersModule{
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // fixed commands; every argument is checked against a pattern first
			cmd.Env = append(os.Environ(), "LC_ALL=C")
			return cmd.CombinedOutput()
		},
		runStdin: func(ctx context.Context, stdin string, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // fixed commands
			cmd.Env = append(os.Environ(), "LC_ALL=C")
			cmd.Stdin = strings.NewReader(stdin)
			return cmd.CombinedOutput()
		},
		readFile:     os.ReadFile,
		lookPath:     exec.LookPath,
		now:          time.Now,
		writeSudoers: defaultWriteSudoers,
		removeFile:   os.Remove,
		writeKeys:    defaultWriteUserKey,
		procStatuses: readProcStatuses,
		passwdPath:   "/etc/passwd", groupPath: "/etc/group", shadowPath: "/etc/shadow", shellsPath: "/etc/shells",
		sudoersDir: "/etc/sudoers.d",
	}
}

// ---- state ----

type userRow struct {
	Name        string   `json:"name"`
	UID         int      `json:"uid"`
	GID         int      `json:"gid"`
	Gecos       string   `json:"gecos"`
	Home        string   `json:"home"`
	Shell       string   `json:"shell"`
	Groups      []string `json:"groups"`
	Sudo        string   `json:"sudo"` // none | pw | nopw | root
	Locked      bool     `json:"locked"`
	HasPassword bool     `json:"has_password"`
	PwChanged   int64    `json:"pw_changed"`
	LastLogin   int64    `json:"last_login"`
	LastFrom    string   `json:"last_from"`
	Sessions    int      `json:"sessions"`
	Procs       int      `json:"procs"`
	Keys        int      `json:"keys"`
	Human       bool     `json:"human"`
	Protected   bool     `json:"protected"`
	NoShell     bool     `json:"no_shell"`
}

type usersState struct {
	Users     []userRow `json:"users"`
	Groups    []string  `json:"groups"` // groups the panel offers
	SudoGroup string    `json:"sudo_group"`
	RootLogin bool      `json:"root_login"` // root has a password, so losing sudo does not lock the host
	Shells    []string  `json:"shells"`
}

func isNoShell(sh string) bool {
	return sh == "" || strings.HasSuffix(sh, "/nologin") || strings.HasSuffix(sh, "/false")
}

func (m *usersModule) read(path string) string {
	b, err := m.readFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// sudoGroupName is the group that grants sudo: "sudo" on Debian and Ubuntu, "wheel" elsewhere.
func sudoGroupName(groups []grEntry) string {
	for _, g := range groups {
		if g.Name == "sudo" {
			return "sudo"
		}
	}
	for _, g := range groups {
		if g.Name == "wheel" {
			return "wheel"
		}
	}
	return "sudo"
}

// offeredGroups are the groups the browser may put an account into.
func offeredGroups(groups []grEntry, users []pwEntry) []string {
	userNames := map[string]bool{}
	for _, u := range users {
		userNames[u.Name] = true
	}
	want := map[string]bool{}
	for _, n := range userGroupOffer {
		want[n] = true
	}
	var out []string
	for _, g := range groups {
		switch {
		case want[g.Name]:
			out = append(out, g.Name)
		case g.GID >= uidMinHuman && g.GID <= uidMaxHuman && !userNames[g.Name]: // a group made on purpose, not a private group of a user
			out = append(out, g.Name)
		}
	}
	sort.Strings(out)
	return out
}

func (m *usersModule) shells() []string {
	out := []string{}
	for _, line := range strings.Split(m.read(m.shellsPath), "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "/") && !strings.HasSuffix(line, "/nologin") {
			out = append(out, line)
		}
	}
	out = append(out, "/usr/sbin/nologin")
	return out
}

func (m *usersModule) state(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	return m.collect(ctx), nil
}

func (m *usersModule) collect(ctx context.Context) *usersState {
	pw := parsePasswdAll(m.read(m.passwdPath))
	gr := parseGroupFile(m.read(m.groupPath))
	now := m.now()
	shadow := parseShadow(m.read(m.shadowPath), now.Unix()/86400)
	sudoGroup := sudoGroupName(gr)
	sudoMembers := map[string]bool{}
	for _, g := range gr {
		if g.Name == sudoGroup {
			for _, n := range g.Members {
				sudoMembers[n] = true
			}
		}
	}
	var sudoers []string
	for _, p := range m.sudoersFiles() {
		sudoers = append(sudoers, m.read(p))
	}
	sessions := map[string]int{}
	if _, err := m.lookPath("loginctl"); err == nil {
		if out, err := m.run(ctx, "loginctl", "list-sessions", "--no-legend"); err == nil {
			sessions = sessionCounts(string(out))
		}
	}
	logins := m.lastlogs(ctx, now)
	procs := countProcs(m.procStatuses())

	st := &usersState{Users: []userRow{}, Groups: offeredGroups(gr, pw), SudoGroup: sudoGroup, Shells: m.shells()}
	for _, u := range pw {
		human := u.UID >= uidMinHuman && u.UID <= uidMaxHuman
		if !human && u.UID != 0 && isNoShell(u.Shell) && procs[u.UID] == 0 && sessions[u.Name] == 0 {
			// system accounts with no shell and no processes are noise; the ones that run something stay
			continue
		}
		sh := shadow[u.Name]
		row := userRow{
			Name: u.Name, UID: u.UID, GID: u.GID, Gecos: u.Gecos, Home: u.Home, Shell: u.Shell,
			Groups: userGroupNames(gr, u.Name), Locked: sh.Locked, HasPassword: sh.HasPassword, PwChanged: sh.Changed,
			Sessions: sessions[u.Name], Procs: procs[u.UID], Human: human, Protected: u.UID == 0, NoShell: isNoShell(u.Shell),
			Sudo: "none",
		}
		if l, ok := logins[u.Name]; ok {
			row.LastLogin, row.LastFrom = l.TS, l.From
		}
		if u.UID == 0 {
			row.Sudo = "root"
			st.RootLogin = sh.HasPassword && !sh.Locked
		} else {
			noPw := false
			for _, c := range sudoers {
				if sudoNoPasswd(c, u.Name) {
					noPw = true
					break
				}
			}
			switch {
			case noPw:
				row.Sudo = "nopw"
			case sudoMembers[u.Name]:
				row.Sudo = "pw"
			}
		}
		if human && u.Home != "" {
			if b, err := m.readFile(filepath.Join(u.Home, ".ssh", "authorized_keys")); err == nil {
				row.Keys = countKeyLines(string(b))
			}
		}
		st.Users = append(st.Users, row)
	}
	return st
}

func (m *usersModule) sudoersFiles() []string {
	files := []string{"/etc/sudoers"}
	entries, err := os.ReadDir(m.sudoersDir)
	if err != nil {
		return files
	}
	for _, e := range entries {
		if !e.IsDir() && !strings.ContainsAny(e.Name(), ".~") { // sudo ignores such names
			files = append(files, filepath.Join(m.sudoersDir, e.Name()))
		}
	}
	return files
}

func (m *usersModule) lastlogs(ctx context.Context, now time.Time) map[string]lastLogin {
	for _, cmd := range []string{"lastlog", "lastlog2"} {
		if _, err := m.lookPath(cmd); err != nil {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		out, err := m.run(cctx, cmd)
		cancel()
		if err == nil {
			return parseLastlog(string(out), now.Location())
		}
	}
	return map[string]lastLogin{}
}

// readProcStatuses reads /proc/<pid>/status of every process; unreadable ones are skipped.
func readProcStatuses() []string {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		if b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "status")); err == nil { //nolint:gosec // /proc/<pid>/status of the host
			out = append(out, string(b))
		}
	}
	return out
}

// ---- changes ----

// target finds the account of a request among the regular users; root and system accounts are refused.
func (m *usersModule) target(name string) (pwEntry, error) {
	if !userNameRe.MatchString(name) {
		return pwEntry{}, errors.New("invalid user")
	}
	pw := parsePasswdAll(m.read(m.passwdPath))
	for _, u := range pw {
		if u.Name != name {
			continue
		}
		if u.UID < uidMinHuman || u.UID > uidMaxHuman {
			return pwEntry{}, errors.New("root and system accounts cannot be changed here")
		}
		return u, nil
	}
	return pwEntry{}, errors.New("no such user")
}

func sudoersName(user string) string {
	return "linager-" + strings.ReplaceAll(user, ".", "_")
}

// setSudoFile writes (nopw) or removes the sudoers file the panel owns for the account; a file that fails
// visudo is not installed, so a mistake cannot break sudo for everyone.
func (m *usersModule) setSudoFile(ctx context.Context, name string, nopw bool) error {
	path := filepath.Join(m.sudoersDir, sudoersName(name))
	if !nopw {
		if err := m.removeFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	tmp := path + ".new" // names with a dot are ignored by sudo until they are renamed
	if err := m.writeSudoers(tmp, []byte(name+" ALL=(ALL:ALL) NOPASSWD: ALL\n")); err != nil {
		return err
	}
	if out, err := m.run(ctx, "visudo", "-c", "-f", tmp); err != nil {
		_ = m.removeFile(tmp)
		return fmt.Errorf("sudoers check failed: %s", strings.TrimSpace(string(out)))
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = m.removeFile(tmp)
		return err
	}
	return nil
}

func defaultWriteSudoers(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o440) //nolint:gosec // sudo wants its files to be mode 0440
}

func cmdErr(what string, out []byte, err error) error {
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return fmt.Errorf("%s: %s", what, msg)
}

func validPassword(p string) bool {
	return len(p) >= 8 && len(p) <= 128 && !strings.ContainsAny(p, "\r\n\x00")
}

type userAddReq struct {
	Name     string   `json:"name"`
	Gecos    string   `json:"gecos"`
	Shell    string   `json:"shell"`
	Home     string   `json:"home"`
	Groups   []string `json:"groups"`
	Sudo     string   `json:"sudo"`
	Password string   `json:"password"` // empty: the password login stays off
	SSHKey   string   `json:"ssh_key"`
}

// add is the command "users.add".
func (m *usersModule) add(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p userAddReq
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	if !newUserRe.MatchString(p.Name) {
		return nil, errors.New("the login must be lowercase letters, digits, _ or -, up to 32 characters")
	}
	if strings.ContainsAny(p.Gecos, ":\r\n\x00") || len(p.Gecos) > 100 {
		return nil, errors.New("invalid description")
	}
	if p.Sudo == "" {
		p.Sudo = "none"
	}
	if p.Sudo != "none" && p.Sudo != "pw" && p.Sudo != "nopw" {
		return nil, errors.New("invalid sudo mode")
	}
	if p.Password != "" && !validPassword(p.Password) {
		return nil, errors.New("the password must be 8 to 128 characters")
	}
	if p.SSHKey = strings.TrimSpace(p.SSHKey); p.SSHKey != "" && !sshKeyRe.MatchString(p.SSHKey) {
		return nil, errors.New("this is not a public SSH key")
	}
	home := p.Home
	if home == "" {
		home = "/home/" + p.Name
	}
	if !homePathRe.MatchString(home) || strings.Contains(home, "..") || strings.HasSuffix(home, "/") || !hasAnyPrefix(home, homePrefixes) {
		return nil, errors.New("the home directory must be a path in /home, /srv, /opt or /var")
	}
	shell := p.Shell
	if shell == "" {
		shell = "/bin/bash"
	}
	if !containsStr(m.shells(), shell) {
		return nil, errors.New("this shell is not in /etc/shells")
	}
	pw := parsePasswdAll(m.read(m.passwdPath))
	gr := parseGroupFile(m.read(m.groupPath))
	for _, u := range pw {
		if u.Name == p.Name {
			return nil, errors.New("a user with this login already exists")
		}
	}
	offered := offeredGroups(gr, pw)
	groups := []string{}
	for _, g := range p.Groups {
		if !containsStr(offered, g) {
			return nil, fmt.Errorf("group %q is not available", g)
		}
		groups = append(groups, g)
	}
	if p.Sudo != "none" {
		groups = append(groups, sudoGroupName(gr))
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	args := []string{"--create-home", "--home-dir", home, "--shell", shell, "--user-group"}
	if p.Gecos != "" {
		args = append(args, "--comment", p.Gecos)
	}
	if len(groups) > 0 {
		args = append(args, "--groups", strings.Join(groups, ","))
	}
	args = append(args, p.Name)
	if out, err := m.run(ctx, "useradd", args...); err != nil {
		return nil, cmdErr("useradd", out, err)
	}
	var warnings []string
	if p.Password != "" {
		if out, err := m.runStdin(ctx, p.Name+":"+p.Password+"\n", "chpasswd"); err != nil {
			warnings = append(warnings, cmdErr("password was not set", out, err).Error())
		}
	}
	if p.Sudo == "nopw" {
		if err := m.setSudoFile(ctx, p.Name, true); err != nil {
			warnings = append(warnings, "sudo without a password was not set: "+err.Error())
		}
	}
	if p.SSHKey != "" {
		if err := m.writeKeys(home, p.Name, p.SSHKey); err != nil {
			warnings = append(warnings, "the SSH key was not saved: "+err.Error())
		}
	}
	return map[string]any{"ok": true, "warnings": warnings}, nil
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// defaultWriteUserKey puts the key into ~/.ssh/authorized_keys of a new account with the right owner and mode.
func defaultWriteUserKey(home, name, key string) error {
	u, err := user.Lookup(name)
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chown(dir, uid, gid); err != nil {
		return err
	}
	file := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(file, []byte(key+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chown(file, uid, gid)
}

type userUpdateReq struct {
	Name   string    `json:"name"`
	Groups *[]string `json:"groups"`
	Sudo   *string   `json:"sudo"`
	Shell  *string   `json:"shell"`
	Locked *bool     `json:"locked"`
}

// lastSudoGuard refuses a change that would leave nobody who can administer the host through sudo
// while root cannot log in (the usual Ubuntu setup).
func (m *usersModule) lastSudoGuard(ctx context.Context, name string) error {
	st := m.collect(ctx)
	if st.RootLogin {
		return nil
	}
	for _, u := range st.Users {
		if u.Name != name && u.Human && u.Sudo != "none" && !u.Locked {
			return nil
		}
	}
	return errors.New("this is the only unlocked user with sudo and root cannot log in: pick another administrator first")
}

// update is the command "users.update": sudo mode, supplementary groups, shell and the lock of an account.
func (m *usersModule) update(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p userUpdateReq
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	u, err := m.target(p.Name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	pw := parsePasswdAll(m.read(m.passwdPath))
	gr := parseGroupFile(m.read(m.groupPath))
	offered := offeredGroups(gr, pw)
	sudoGroup := sudoGroupName(gr)
	cur := m.collect(ctx)
	var curRow userRow
	for _, r := range cur.Users {
		if r.Name == u.Name {
			curRow = r
		}
	}

	if p.Sudo != nil && *p.Sudo != "none" && *p.Sudo != "pw" && *p.Sudo != "nopw" {
		return nil, errors.New("invalid sudo mode")
	}
	removesAdmin := (p.Sudo != nil && *p.Sudo == "none" && curRow.Sudo != "none") || (p.Locked != nil && *p.Locked && !curRow.Locked && curRow.Sudo != "none")
	if removesAdmin {
		if err := m.lastSudoGuard(ctx, u.Name); err != nil {
			return nil, err
		}
	}

	// supplementary groups: what the panel offers is replaced by the request, what it does not offer stays
	if p.Groups != nil || p.Sudo != nil {
		newSudo := curRow.Sudo
		if p.Sudo != nil {
			newSudo = *p.Sudo
		}
		want := map[string]bool{}
		if p.Groups != nil {
			for _, g := range *p.Groups {
				if !containsStr(offered, g) {
					return nil, fmt.Errorf("group %q is not available", g)
				}
				want[g] = true
			}
		} else {
			for _, g := range curRow.Groups {
				if containsStr(offered, g) {
					want[g] = true
				}
			}
		}
		for _, g := range curRow.Groups {
			if g != sudoGroup && !containsStr(offered, g) {
				want[g] = true // a group the panel does not manage stays as it is
			}
		}
		if newSudo == "pw" || newSudo == "nopw" {
			want[sudoGroup] = true
		}
		var list []string
		for g := range want {
			list = append(list, g)
		}
		sort.Strings(list)
		if out, err := m.run(ctx, "usermod", "-G", strings.Join(list, ","), u.Name); err != nil {
			return nil, cmdErr("usermod", out, err)
		}
		if p.Sudo != nil {
			if err := m.setSudoFile(ctx, u.Name, newSudo == "nopw"); err != nil {
				return nil, err
			}
		}
	}
	if p.Shell != nil {
		if !containsStr(m.shells(), *p.Shell) {
			return nil, errors.New("this shell is not in /etc/shells")
		}
		if out, err := m.run(ctx, "usermod", "-s", *p.Shell, u.Name); err != nil {
			return nil, cmdErr("usermod", out, err)
		}
	}
	if p.Locked != nil {
		if *p.Locked {
			if out, err := m.run(ctx, "usermod", "-e", "1", u.Name); err != nil { // expired: no login of any kind
				return nil, cmdErr("usermod", out, err)
			}
			_, _ = m.run(ctx, "usermod", "-L", u.Name) // also the password; an account without one has nothing to lock
			_, _ = m.run(ctx, "loginctl", "terminate-user", u.Name)
		} else {
			if out, err := m.run(ctx, "usermod", "-e", "", u.Name); err != nil {
				return nil, cmdErr("usermod", out, err)
			}
			_, _ = m.run(ctx, "usermod", "-U", u.Name) // refuses an account that has no password; that is fine
		}
	}
	return map[string]any{"ok": true}, nil
}

type userDeleteReq struct {
	Name       string `json:"name"`
	RemoveHome bool   `json:"remove_home"`
}

// del is the command "users.delete": the sessions and processes of the account are ended first.
func (m *usersModule) del(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p userDeleteReq
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	u, err := m.target(p.Name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cur := m.collect(ctx)
	for _, r := range cur.Users {
		if r.Name == u.Name && r.Sudo != "none" {
			if err := m.lastSudoGuard(ctx, u.Name); err != nil {
				return nil, err
			}
		}
	}
	_, _ = m.run(ctx, "loginctl", "terminate-user", u.Name)
	_, _ = m.run(ctx, "pkill", "-KILL", "-u", u.Name)
	args := []string{}
	if p.RemoveHome {
		args = append(args, "--remove")
	}
	args = append(args, u.Name)
	if out, err := m.run(ctx, "userdel", args...); err != nil {
		return nil, cmdErr("userdel", out, err)
	}
	_ = m.setSudoFile(ctx, u.Name, false)
	return map[string]any{"ok": true}, nil
}

type userPasswdReq struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// passwd is the command "users.passwd": sets a new password (the account stays locked if it was locked).
func (m *usersModule) passwd(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p userPasswdReq
	if err := json.Unmarshal(raw, &p); err != nil || !validPassword(p.Password) {
		return nil, errors.New("the password must be 8 to 128 characters")
	}
	u, err := m.target(p.Name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if out, err := m.runStdin(ctx, u.Name+":"+p.Password+"\n", "chpasswd"); err != nil {
		return nil, cmdErr("chpasswd", bytes.TrimSpace(out), err)
	}
	return map[string]any{"ok": true}, nil
}

// kill is the command "users.kill": ends every session of the account (and so its processes).
func (m *usersModule) kill(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	u, err := m.target(p.Name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := m.lookPath("loginctl"); err != nil {
		return nil, errors.New("systemd-logind is not available on this host")
	}
	if out, err := m.run(ctx, "loginctl", "terminate-user", u.Name); err != nil {
		return nil, cmdErr("loginctl", out, err)
	}
	return map[string]any{"ok": true}, nil
}
