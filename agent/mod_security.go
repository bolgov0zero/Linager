package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The "Security" tab: a read-only look at SSH. Nothing here changes the system and the command takes
// no parameters, so there is nothing for the browser to inject.

const (
	secDays       = 7
	secCacheTTL   = time.Minute
	secMaxIPs     = 20000
	secTop        = 15
	secMaxLogins  = 200
	secMaxSession = 50
)

// secMaxLines bounds how many journal lines are read (a heavily attacked host writes a few a second);
// a variable so a test can lower it.
var secMaxLines = 2_000_000

type secModule struct {
	// access to the system; tests replace these
	run      func(ctx context.Context, name string, args ...string) ([]byte, error)
	stream   func(ctx context.Context, name string, args []string, onLine func(string) bool) error
	readFile func(path string) ([]byte, error)
	glob     func(pattern string) ([]string, error)
	mtime    func(path string) int64
	lookPath func(name string) (string, error)
	now      func() time.Time

	passwdPath string
	authLogs   []string // oldest first
	aptConfDir string

	mu      sync.Mutex
	cache   json.RawMessage
	cacheAt time.Time
}

func newSecModule() *secModule {
	return &secModule{
		run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // fixed commands, no user input
			cmd.Env = append(os.Environ(), "LC_ALL=C")
			return cmd.Output()
		},
		stream:   streamLines,
		readFile: os.ReadFile,
		glob:     filepath.Glob,
		mtime: func(path string) int64 {
			if st, err := os.Stat(path); err == nil {
				return st.ModTime().Unix()
			}
			return 0
		},
		lookPath:   exec.LookPath,
		now:        time.Now,
		passwdPath: "/etc/passwd",
		authLogs:   []string{"/var/log/auth.log.1", "/var/log/auth.log"},
		aptConfDir: "/etc/apt/apt.conf.d",
	}
}

// streamLines runs a command and hands its output lines to onLine until it returns false.
func streamLines(ctx context.Context, name string, args []string, onLine func(string) bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // fixed commands, no user input
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		if !onLine(sc.Text()) {
			cancel()
			break
		}
	}
	_ = cmd.Wait()
	return nil
}

// ---- the answer ----

type secSSH struct {
	Installed        bool              `json:"installed"`
	Source           string            `json:"source"` // "effective" (sshd -T), "files" or ""
	Ports            []string          `json:"ports"`
	PermitRootLogin  string            `json:"permit_root_login"`
	PasswordAuth     string            `json:"password_auth"`
	PubkeyAuth       string            `json:"pubkey_auth"`
	KbdInteractive   string            `json:"kbd_interactive"`
	PermitEmptyPass  string            `json:"permit_empty_passwords"`
	MaxAuthTries     string            `json:"max_auth_tries"`
	UserRestrictions bool              `json:"user_restrictions"` // AllowUsers / AllowGroups are set
	Raw              map[string]string `json:"-"`
}

type secF2B struct {
	Installed bool      `json:"installed"`
	Active    bool      `json:"active"`
	Jails     []f2bJail `json:"jails"`
}

type secLogin struct {
	TS            int64  `json:"time"`
	User          string `json:"user"`
	IP            string `json:"ip"`
	Method        string `json:"method"`
	PriorFailures int    `json:"prior_failures"` // failed attempts from this address before the login
}

type secTopIP struct {
	IP    string `json:"ip"`
	Count int    `json:"count"`
	Users int    `json:"users"` // distinct user names tried
	Last  int64  `json:"last"`
}

type secTopUser struct {
	User    string `json:"user"`
	Count   int    `json:"count"`
	Invalid bool   `json:"invalid"` // the account does not exist
}

type secFailures struct {
	Total        int          `json:"total"`
	InvalidUsers int          `json:"invalid_user_attempts"` // "Invalid user" lines: probes of accounts that do not exist
	Addresses    int          `json:"addresses"`
	ByDay        []int        `json:"by_day"` // oldest first, secDays buckets of 24 hours ending now
	TopIPs       []secTopIP   `json:"top_ips"`
	TopUsers     []secTopUser `json:"top_users"`
}

type secJournal struct {
	Source       string `json:"source"` // "journal", "authlog" or "none"
	Oldest       int64  `json:"oldest"` // time of the oldest record of the log (0: unknown)
	CoversPeriod bool   `json:"covers_period"`
	Truncated    bool   `json:"truncated"`
}

type secSession struct {
	ID    string `json:"id"`
	User  string `json:"user"`
	IP    string `json:"ip"`
	Since int64  `json:"since"`
}

type secState struct {
	GeneratedAt int64        `json:"generated_at"`
	PeriodDays  int          `json:"period_days"`
	SSH         secSSH       `json:"ssh"`
	Fail2ban    secF2B       `json:"fail2ban"`
	AutoUpdates bool         `json:"auto_updates"`
	Logins      []secLogin   `json:"logins"`
	Failures    secFailures  `json:"failures"`
	Keys        []authKey    `json:"keys"`
	Sessions    []secSession `json:"sessions"`
	Journal     secJournal   `json:"journal"`
}

// state answers "security.state"; the result is cached for a minute, so opening the tab twice is cheap.
func (m *secModule) state(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cache != nil && m.now().Sub(m.cacheAt) < secCacheTTL {
		return m.cache, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	st := m.collect(ctx)
	b, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	m.cache, m.cacheAt = b, m.now()
	return m.cache, nil
}

func (m *secModule) collect(ctx context.Context) *secState {
	now := m.now()
	st := &secState{GeneratedAt: now.Unix(), PeriodDays: secDays, Logins: []secLogin{}, Keys: []authKey{}, Sessions: []secSession{}}
	st.SSH = m.sshConfig(ctx)
	st.Fail2ban = m.fail2ban(ctx)
	st.AutoUpdates = m.autoUpdates()
	m.analyzeLogs(ctx, now, st)
	st.Keys = m.authorizedKeys(st.SSH.Raw)
	st.Sessions = m.sessions(ctx, now)
	return st
}

// ---- sshd ----

func (m *secModule) sshConfig(ctx context.Context) secSSH {
	s := secSSH{Ports: []string{}}
	var text string
	if out, err := m.run(ctx, "sshd", "-T"); err == nil {
		text, s.Source = string(out), "effective"
	} else {
		var sb strings.Builder
		files, _ := m.glob("/etc/ssh/sshd_config.d/*.conf") // sshd reads the drop-ins first
		sort.Strings(files)
		for _, f := range append(files, "/etc/ssh/sshd_config") {
			if b, err := m.readFile(f); err == nil {
				sb.Write(b)
				sb.WriteByte('\n')
			}
		}
		if sb.Len() > 1 {
			text, s.Source = sb.String(), "files"
		}
	}
	if s.Source == "" {
		_, err := m.lookPath("sshd")
		s.Installed = err == nil
		return s
	}
	conf := parseSSHDConfig(text)
	s.Installed = true
	s.Raw = conf
	ports := parseSSHPorts(text) // "Port" may repeat
	if len(ports) == 0 {
		ports = []int{22}
	}
	for _, p := range ports {
		s.Ports = append(s.Ports, strconv.Itoa(p))
	}
	s.PermitRootLogin = conf["permitrootlogin"]
	s.PasswordAuth = conf["passwordauthentication"]
	s.PubkeyAuth = conf["pubkeyauthentication"]
	s.KbdInteractive = firstNonEmpty(conf["kbdinteractiveauthentication"], conf["challengeresponseauthentication"])
	s.PermitEmptyPass = conf["permitemptypasswords"]
	s.MaxAuthTries = conf["maxauthtries"]
	s.UserRestrictions = conf["allowusers"] != "" || conf["allowgroups"] != ""
	return s
}

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

// ---- fail2ban ----

func (m *secModule) fail2ban(ctx context.Context) secF2B {
	f := secF2B{Jails: []f2bJail{}}
	if _, err := m.lookPath("fail2ban-client"); err != nil {
		return f
	}
	f.Installed = true
	out, err := m.run(ctx, "fail2ban-client", "status")
	if err != nil {
		return f // installed, but the service is not running
	}
	f.Active = true
	for _, name := range parseF2BJails(string(out)) {
		if b, err := m.run(ctx, "fail2ban-client", "status", name); err == nil {
			f.Jails = append(f.Jails, parseF2BJail(name, string(b)))
		}
	}
	return f
}

// ---- unattended upgrades ----

func (m *secModule) autoUpdates() bool {
	files, _ := m.glob(filepath.Join(m.aptConfDir, "*"))
	sort.Strings(files)
	var contents []string
	for _, f := range files {
		if b, err := m.readFile(f); err == nil {
			contents = append(contents, string(b))
		}
	}
	if !unattendedEnabled(contents) {
		return false
	}
	_, err := m.lookPath("unattended-upgrade")
	return err == nil
}

// ---- the log: logins and failed attempts ----

// sshStats accumulates the events of the period in time order.
type sshStats struct {
	since, now int64
	logins     []secLogin
	total      int
	invalidAll int
	byDay      []int
	ips        map[string]*ipStat
	users      map[string]*userStat
	otherIPs   int
}

type ipStat struct {
	count int
	users map[string]struct{}
	last  int64
}

type userStat struct {
	count   int
	invalid bool
}

func newSSHStats(now time.Time) *sshStats {
	return &sshStats{since: now.Add(-secDays * 24 * time.Hour).Unix(), now: now.Unix(), byDay: make([]int, secDays),
		ips: map[string]*ipStat{}, users: map[string]*userStat{}}
}

func (s *sshStats) add(ev sshEvent) {
	if ev.TS < s.since || ev.TS > s.now+3600 {
		return
	}
	switch ev.Kind {
	case "accepted":
		prior := 0
		if st := s.ips[ev.IP]; st != nil {
			prior = st.count
		}
		s.logins = append(s.logins, secLogin{TS: ev.TS, User: ev.User, IP: ev.IP, Method: ev.Method, PriorFailures: prior})
		if len(s.logins) > secMaxLogins*4 {
			s.logins = append([]secLogin(nil), s.logins[len(s.logins)-secMaxLogins:]...)
		}
	case "failed":
		s.total++
		day := secDays - 1 - int((s.now-ev.TS)/86400)
		if day >= 0 && day < secDays {
			s.byDay[day]++
		}
		st := s.ips[ev.IP]
		if st == nil {
			if len(s.ips) >= secMaxIPs {
				s.otherIPs++
			} else {
				st = &ipStat{users: map[string]struct{}{}}
				s.ips[ev.IP] = st
			}
		}
		if st != nil {
			st.count++
			st.last = ev.TS
			if len(st.users) < 64 {
				st.users[ev.User] = struct{}{}
			}
		}
		us := s.users[ev.User]
		if us == nil && len(s.users) < secMaxIPs {
			us = &userStat{}
			s.users[ev.User] = us
		}
		if us != nil {
			us.count++
			us.invalid = us.invalid || ev.Invalid
		}
	case "invalid":
		s.invalidAll++
	}
}

func (s *sshStats) fill(st *secState) {
	// logins: newest first
	l := s.logins
	if len(l) > secMaxLogins {
		l = l[len(l)-secMaxLogins:]
	}
	st.Logins = make([]secLogin, 0, len(l))
	for i := len(l) - 1; i >= 0; i-- {
		st.Logins = append(st.Logins, l[i])
	}
	f := secFailures{Total: s.total, InvalidUsers: s.invalidAll, Addresses: len(s.ips) + s.otherIPs, ByDay: s.byDay,
		TopIPs: []secTopIP{}, TopUsers: []secTopUser{}}
	for ip, x := range s.ips {
		f.TopIPs = append(f.TopIPs, secTopIP{IP: ip, Count: x.count, Users: len(x.users), Last: x.last})
	}
	sort.Slice(f.TopIPs, func(i, j int) bool {
		if f.TopIPs[i].Count != f.TopIPs[j].Count {
			return f.TopIPs[i].Count > f.TopIPs[j].Count
		}
		return f.TopIPs[i].IP < f.TopIPs[j].IP
	})
	if len(f.TopIPs) > secTop {
		f.TopIPs = f.TopIPs[:secTop]
	}
	for u, x := range s.users {
		f.TopUsers = append(f.TopUsers, secTopUser{User: u, Count: x.count, Invalid: x.invalid})
	}
	sort.Slice(f.TopUsers, func(i, j int) bool {
		if f.TopUsers[i].Count != f.TopUsers[j].Count {
			return f.TopUsers[i].Count > f.TopUsers[j].Count
		}
		return f.TopUsers[i].User < f.TopUsers[j].User
	})
	if len(f.TopUsers) > secTop {
		f.TopUsers = f.TopUsers[:secTop]
	}
	st.Failures = f
}

var sshIdent = []string{"-t", "sshd", "-t", "sshd-session"} // OpenSSH 9.8 and later log from sshd-session

func (m *secModule) analyzeLogs(ctx context.Context, now time.Time, st *secState) {
	stats := newSSHStats(now)
	st.Journal = secJournal{Source: "none"}

	// 1. the journal
	if _, err := m.lookPath("journalctl"); err == nil {
		lines, events := 0, 0
		args := append([]string{"-o", "short-unix", "-q", "--no-pager", "--since", "7 days ago"}, sshIdent...)
		err := m.stream(ctx, "journalctl", args, func(line string) bool {
			lines++
			if lines > secMaxLines {
				st.Journal.Truncated = true
				return false
			}
			if ev, ok := parseJournalLine(line); ok {
				stats.add(ev)
				events++
			}
			return true
		})
		if err == nil {
			st.Journal.Source = "journal"
			// the oldest record of the whole journal tells whether it reaches back seven days
			_ = m.stream(ctx, "journalctl", []string{"-o", "short-unix", "-q", "--no-pager"}, func(line string) bool {
				if sp := strings.IndexByte(line, ' '); sp > 0 {
					if ts, ok := parseUnixPrefix(line[:sp]); ok {
						st.Journal.Oldest = ts
						return false
					}
				}
				return true
			})
		}
		if events > 0 {
			stats.fill(st)
			st.Journal.CoversPeriod = st.Journal.Oldest > 0 && st.Journal.Oldest <= stats.since
			return
		}
	}

	// 2. /var/log/auth.log (hosts where sshd writes to a file)
	read := false
	for _, f := range m.authLogs {
		b, err := m.readFile(f)
		if err != nil {
			continue
		}
		read = true
		first := true
		for _, line := range strings.Split(string(b), "\n") {
			if first {
				if ts, ok := authLogTime(line, now); ok && (st.Journal.Oldest == 0 || ts < st.Journal.Oldest) {
					st.Journal.Oldest = ts
				}
				first = false
			}
			if ev, ok := parseAuthLogLine(line, now); ok {
				stats.add(ev)
			}
		}
	}
	if read {
		st.Journal.Source = "authlog"
	}
	stats.fill(st)
	st.Journal.CoversPeriod = st.Journal.Oldest > 0 && st.Journal.Oldest <= stats.since
}

func parseUnixPrefix(s string) (int64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return int64(f), true
}

// ---- authorized_keys ----

func (m *secModule) authorizedKeys(conf map[string]string) []authKey {
	patterns := strings.Fields(conf["authorizedkeysfile"])
	if len(patterns) == 0 {
		patterns = []string{".ssh/authorized_keys", ".ssh/authorized_keys2"}
	}
	b, err := m.readFile(m.passwdPath)
	if err != nil {
		return []authKey{}
	}
	keys := []authKey{}
	seen := map[string]bool{}
	users := parsePasswd(string(b))
	if len(users) > 100 {
		users = users[:100]
	}
	for _, u := range users {
		for _, pat := range patterns {
			path := expandKeyPath(pat, u.Home, u.Name)
			if seen[path] {
				continue
			}
			seen[path] = true
			content, err := m.readFile(path)
			if err != nil {
				continue
			}
			keys = append(keys, parseAuthorizedKeys(u.Name, path, string(content), m.mtime(path))...)
		}
	}
	return keys
}

// ---- who is logged in now ----

var sessionTimeRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)

func (m *secModule) sessions(ctx context.Context, now time.Time) []secSession {
	out := []secSession{}
	if _, err := m.lookPath("loginctl"); err != nil {
		return out
	}
	list, err := m.run(ctx, "loginctl", "list-sessions", "--no-legend")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(list), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || len(out) >= secMaxSession {
			continue
		}
		id := f[0]
		if !sessionIDRe.MatchString(id) {
			continue
		}
		b, err := m.run(ctx, "loginctl", "show-session", id, "-p", "Name", "-p", "RemoteHost", "-p", "Remote", "-p", "Timestamp")
		if err != nil {
			continue
		}
		kv := map[string]string{}
		for _, l := range strings.Split(string(b), "\n") {
			if k, v, ok := strings.Cut(l, "="); ok {
				kv[k] = strings.TrimSpace(v)
			}
		}
		if kv["Remote"] != "yes" {
			continue
		}
		s := secSession{ID: id, User: kv["Name"], IP: kv["RemoteHost"]}
		if t := sessionTimeRe.FindString(kv["Timestamp"]); t != "" {
			if tt, err := time.ParseInLocation("2006-01-02 15:04:05", t, now.Location()); err == nil {
				s.Since = tt.Unix()
			}
		}
		out = append(out, s)
	}
	return out
}

var sessionIDRe = regexp.MustCompile(`^[a-zA-Z0-9]{1,16}$`)

// userNameRe is the shape of a login name; it is checked before a name reaches a command.
var userNameRe = regexp.MustCompile(`^[a-zA-Z0-9._][a-zA-Z0-9._-]{0,63}$`)

type secTerminated struct {
	ID   string `json:"id"`
	User string `json:"user"`
	IP   string `json:"ip"`
}

// terminate ends remote (SSH) sessions through systemd-logind: one session by id, or all remote sessions of
// a user. Only sessions that are in the agent's own fresh list can be ended, so a name or an id coming from
// the browser never selects anything else (local console sessions are never touched). The processes of the
// session are stopped with it, so the command needs the confirmation of the administrator.
func (m *secModule) terminate(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p struct {
		Session string `json:"session"`
		User    string `json:"user"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || (p.Session == "") == (p.User == "") {
		return nil, errors.New("give either a session or a user")
	}
	if p.Session != "" && !sessionIDRe.MatchString(p.Session) {
		return nil, errors.New("invalid session")
	}
	if p.User != "" && !userNameRe.MatchString(p.User) {
		return nil, errors.New("invalid user")
	}
	if _, err := m.lookPath("loginctl"); err != nil {
		return nil, errors.New("systemd-logind is not available on this host")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var targets []secSession
	for _, s := range m.sessions(ctx, m.now()) {
		if (p.Session != "" && s.ID == p.Session) || (p.User != "" && s.User == p.User) {
			targets = append(targets, s)
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("no such remote session")
	}
	done := []secTerminated{}
	var firstErr error
	for _, s := range targets {
		if _, err := m.run(ctx, "loginctl", "terminate-session", s.ID); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("session %s: %w", s.ID, err)
			}
			continue
		}
		done = append(done, secTerminated{ID: s.ID, User: s.User, IP: s.IP})
	}
	m.mu.Lock()
	m.cache = nil // the next answer must not show the ended sessions
	m.mu.Unlock()
	if len(done) == 0 {
		return nil, firstErr
	}
	return map[string]any{"ok": true, "terminated": done}, nil
}
