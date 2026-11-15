package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// fail2ban from the panel: install, settings, manual bans, unbans and the history of bans. Everything
// goes through fail2ban-client and one file of ours, /etc/fail2ban/jail.d/linager.conf, so the files
// of the administrator are never rewritten. The names and addresses that reach a command are validated here.

const (
	f2bConfPath = "/etc/fail2ban/jail.d/linager.conf"
	f2bLogPath  = "/var/log/fail2ban.log"
	sshJailName = "sshd"
)

// f2bPresets are the ready-made strictness modes: failures, window (s), ban (s).
var f2bPresets = map[string][3]int{
	"soft": {10, 600, 600},
	"norm": {5, 600, 3600},
	"hard": {3, 600, 86400},
}

// f2bSettings are the parameters of the sshd jail the panel manages.
type f2bSettings struct {
	MaxRetry  int      `json:"maxretry"`
	FindTime  int      `json:"findtime"` // seconds
	BanTime   int      `json:"bantime"`  // seconds, -1: forever
	Increment bool     `json:"increment"`
	Recidive  bool     `json:"recidive"`
	Ignore    []string `json:"ignore"`
}

// f2bBan is one banned address with its times.
type f2bBan struct {
	IP       string `json:"ip"`
	Jail     string `json:"jail"`
	BannedAt int64  `json:"banned_at"`
	UnbanAt  int64  `json:"unban_at"` // 0: no end known
	Attempts int    `json:"attempts"`
}

// f2bEvent is a ban or an unban from the log of fail2ban.
type f2bEvent struct {
	TS    int64  `json:"time"`
	Event string `json:"event"` // ban or unban
	IP    string `json:"ip"`
	Jail  string `json:"jail"`
}

var (
	f2bBanTimeRe  = regexp.MustCompile(`^(\S+)\s+(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) \+ (-?\d+) = (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`)
	f2bLogRe      = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})[,.]\d+ fail2ban\.actions\s*(?:\[\d+\])?: \w+\s+\[([^\]]+)\] (Ban|Unban|Restore Ban) (\S+)`)
	f2bJailNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,40}$`)
)

// parseBanTimes reads `fail2ban-client get <jail> banip --with-time`.
func parseBanTimes(out, jail string, loc *time.Location) []f2bBan {
	var bans []f2bBan
	for _, line := range strings.Split(out, "\n") {
		m := f2bBanTimeRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		b := f2bBan{IP: m[1], Jail: jail}
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", m[2], loc); err == nil {
			b.BannedAt = t.Unix()
		}
		if secs, _ := strconv.Atoi(m[3]); secs >= 0 {
			if t, err := time.ParseInLocation("2006-01-02 15:04:05", m[4], loc); err == nil {
				b.UnbanAt = t.Unix()
			}
		}
		bans = append(bans, b)
	}
	return bans
}

// parseF2BIgnore reads `fail2ban-client get <jail> ignoreip`.
func parseF2BIgnore(out string) []string {
	list := []string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "|- ") || strings.HasPrefix(line, "`- ") {
			if v := strings.TrimSpace(line[3:]); v != "" {
				list = append(list, v)
			}
		}
	}
	return list
}

// parseF2BLog reads the ban and unban lines of fail2ban.log, oldest first.
func parseF2BLog(text string, loc *time.Location) []f2bEvent {
	var ev []f2bEvent
	for _, line := range strings.Split(text, "\n") {
		m := f2bLogRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		t, err := time.ParseInLocation("2006-01-02 15:04:05", m[1], loc)
		if err != nil {
			continue
		}
		kind := "ban"
		if m[3] == "Unban" {
			kind = "unban"
		}
		ev = append(ev, f2bEvent{TS: t.Unix(), Event: kind, IP: m[4], Jail: m[2]})
	}
	return ev
}

// validIPOrNet accepts an address or a network of either family.
func validIPOrNet(s string) bool {
	if _, err := netip.ParsePrefix(s); err == nil {
		return true
	}
	_, err := netip.ParseAddr(s)
	return err == nil
}

// ipInList reports whether the address is covered by an address or a network of the list.
func ipInList(ip string, list []string) bool {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, e := range list {
		if p, err := netip.ParsePrefix(e); err == nil {
			if p.Contains(a) {
				return true
			}
		} else if b, err := netip.ParseAddr(e); err == nil && b == a {
			return true
		}
	}
	return false
}

// f2bConfOptions are the facts about the host that decide how the file is written.
type f2bConfOptions struct {
	Ports       []string
	BanAction   string // "", "nftables" or "nftables-multiport"
	AllPorts    string
	SystemdLogs bool // sshd writes to the journal, not to /var/log/auth.log
}

func formatSeconds(s int) string { return strconv.Itoa(s) }

// renderF2BConf writes the file of the panel.
func renderF2BConf(s f2bSettings, o f2bConfOptions) string {
	var b strings.Builder
	b.WriteString("# Managed by Linager (Security > fail2ban > Settings). Changes made here are overwritten.\n[DEFAULT]\n")
	ign := append([]string{}, s.Ignore...)
	fmt.Fprintf(&b, "ignoreip = %s\n", strings.Join(ign, " "))
	if o.BanAction != "" {
		fmt.Fprintf(&b, "banaction = %s\nbanaction_allports = %s\n", o.BanAction, o.AllPorts)
	}
	if s.Increment {
		b.WriteString("bantime.increment = true\nbantime.factor = 2\nbantime.maxtime = 7d\n")
	}
	b.WriteString("\n[" + sshJailName + "]\nenabled = true\n")
	if len(o.Ports) > 0 {
		fmt.Fprintf(&b, "port = %s\n", strings.Join(o.Ports, ","))
	}
	if o.SystemdLogs {
		b.WriteString("backend = systemd\n")
	}
	fmt.Fprintf(&b, "maxretry = %d\nfindtime = %s\nbantime = %s\n", s.MaxRetry, formatSeconds(s.FindTime), formatSeconds(s.BanTime))
	if s.Recidive {
		b.WriteString("\n[recidive]\nenabled = true\nbantime = 7d\nfindtime = 1d\nmaxretry = 3\n")
	}
	return b.String()
}

var confKeyRe = regexp.MustCompile(`^\s*([a-z.]+)\s*=\s*(.*?)\s*$`)

// parseF2BConf reads back the file of the panel: section by section, only the keys it writes.
func parseF2BConf(text string) (s f2bSettings, ok bool) {
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line[1 : len(line)-1]
			continue
		}
		m := confKeyRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		k, v := m[1], m[2]
		switch {
		case section == "DEFAULT" && k == "ignoreip":
			s.Ignore = strings.Fields(v)
		case section == "DEFAULT" && k == "bantime.increment":
			s.Increment = v == "true"
		case section == sshJailName && k == "maxretry":
			s.MaxRetry, _ = strconv.Atoi(v)
			ok = true
		case section == sshJailName && k == "findtime":
			s.FindTime, _ = strconv.Atoi(v)
		case section == sshJailName && k == "bantime":
			s.BanTime, _ = strconv.Atoi(v)
		case section == "recidive" && k == "enabled":
			s.Recidive = v == "true"
		}
	}
	return s, ok
}

// validateF2BSettings checks values that come from the browser before they reach a file.
func validateF2BSettings(s *f2bSettings) error {
	if s.MaxRetry < 1 || s.MaxRetry > 100 {
		return errors.New("failures before a ban must be between 1 and 100")
	}
	if s.FindTime < 60 || s.FindTime > 86400 {
		return errors.New("the window must be between 1 minute and 24 hours")
	}
	if s.BanTime != -1 && (s.BanTime < 60 || s.BanTime > 365*86400) {
		return errors.New("the ban time must be at least a minute (or forever)")
	}
	if len(s.Ignore) > 200 {
		return errors.New("too many addresses in the ignore list")
	}
	seen := map[string]bool{}
	list := s.Ignore[:0:0]
	for _, e := range s.Ignore {
		e = strings.TrimSpace(e)
		if !validIPOrNet(e) {
			return fmt.Errorf("%q is not an address or a network", e)
		}
		if !seen[e] {
			seen[e] = true
			list = append(list, e)
		}
	}
	s.Ignore = list
	return nil
}

// withLoopback makes sure the host itself and the administrator's address are never banned.
func withLoopback(ignore []string, self string) []string {
	var out []string
	for _, must := range []string{"127.0.0.1/8", "::1"} {
		if !containsStr(ignore, must) && !containsStr(ignore, strings.TrimSuffix(must, "/8")) {
			out = append(out, must)
		}
	}
	out = append(out, ignore...)
	if self != "" && !ipInList(self, out) {
		out = append(out, self)
	}
	return out
}

func containsStr(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// cmdMessage turns an exec error into text with what the command wrote to stderr.
func cmdMessage(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		return errors.New(strings.TrimSpace(string(ee.Stderr)))
	}
	return err
}

// ---- state ----

// fillF2B adds the settings and the banned addresses with their times to the short overview.
func (m *secModule) fillF2B(ctx context.Context, f *secF2B, attempts map[string]int) {
	f.Bans = []f2bBan{}
	if !f.Active {
		return
	}
	for _, j := range f.Jails {
		out, err := m.run(ctx, "fail2ban-client", "get", j.Name, "banip", "--with-time")
		bans := []f2bBan(nil)
		if err == nil {
			bans = parseBanTimes(string(out), j.Name, m.now().Location())
		}
		if len(bans) == 0 { // older versions or no times: the plain list
			for _, ip := range j.BannedIPs {
				bans = append(bans, f2bBan{IP: ip, Jail: j.Name})
			}
		}
		for i := range bans {
			bans[i].Attempts = attempts[bans[i].IP]
		}
		f.Bans = append(f.Bans, bans...)
	}
	sort.SliceStable(f.Bans, func(i, j int) bool { return f.Bans[i].BannedAt > f.Bans[j].BannedAt })
	for _, j := range f.Jails {
		if j.Name != sshJailName {
			continue
		}
		s := f2bSettings{Ignore: []string{}}
		num := func(key string) int {
			out, err := m.run(ctx, "fail2ban-client", "get", sshJailName, key)
			if err != nil {
				return 0
			}
			n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
			return n
		}
		s.MaxRetry, s.FindTime, s.BanTime = num("maxretry"), num("findtime"), num("bantime")
		if out, err := m.run(ctx, "fail2ban-client", "get", sshJailName, "ignoreip"); err == nil {
			s.Ignore = parseF2BIgnore(string(out))
		}
		if b, err := m.readFile(f2bConfPath); err == nil {
			f.Managed = true
			if mine, ok := parseF2BConf(string(b)); ok {
				s.Increment, s.Recidive = mine.Increment, mine.Recidive
			}
		}
		for _, other := range f.Jails {
			if other.Name == "recidive" {
				s.Recidive = true
			}
		}
		f.Settings = &s
	}
}

// ---- commands ----

type f2bCall struct {
	IP       string   `json:"ip"`
	IPs      []string `json:"ips"`
	All      bool     `json:"all"`
	Jail     string   `json:"jail"`
	Duration int      `json:"duration"` // seconds, 0: the time of the jail
	Self     string   `json:"self"`     // address of the administrator, never banned
}

func (m *secModule) forget() {
	m.mu.Lock()
	m.cache = nil
	m.mu.Unlock()
}

func (m *secModule) f2bReady() error {
	if _, err := m.lookPath("fail2ban-client"); err != nil {
		return errors.New("fail2ban is not installed")
	}
	return nil
}

func (m *secModule) currentIgnore(ctx context.Context) []string {
	out, err := m.run(ctx, "fail2ban-client", "get", sshJailName, "ignoreip")
	if err != nil {
		return nil
	}
	return parseF2BIgnore(string(out))
}

// f2bBanIP is "security.ban": blocks an address now, optionally only for some time.
func (m *secModule) f2bBanIP(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p f2bCall
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	if p.Jail == "" {
		p.Jail = sshJailName
	}
	a, err := netip.ParseAddr(p.IP)
	if err != nil || !f2bJailNameRe.MatchString(p.Jail) {
		return nil, errors.New("invalid address")
	}
	if a.IsLoopback() || a.IsUnspecified() {
		return nil, errors.New("this address cannot be blocked")
	}
	if p.Self != "" && ipInList(p.IP, []string{p.Self}) {
		return nil, errors.New("this is your own address, the panel will not block it")
	}
	if p.Duration != 0 && p.Duration != 3600 && p.Duration != 86400 && p.Duration != 604800 {
		return nil, errors.New("invalid duration")
	}
	if err := m.f2bReady(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if ipInList(p.IP, m.currentIgnore(ctx)) {
		return nil, errors.New("the address is in the list of addresses that are not blocked")
	}
	if _, err := m.run(ctx, "fail2ban-client", "set", p.Jail, "banip", p.IP); err != nil {
		return nil, cmdMessage(err)
	}
	timed := false
	if p.Duration > 0 {
		// fail2ban bans for the time of the jail: a transient systemd timer lifts a shorter manual ban
		if path, err := m.lookPath("systemd-run"); err == nil {
			cli, _ := m.lookPath("fail2ban-client")
			unit := fmt.Sprintf("linager-unban-%d", m.now().UnixNano())
			if _, err := m.run(ctx, path, "--quiet", "--unit", unit, "--on-active="+strconv.Itoa(p.Duration), cli, "set", p.Jail, "unbanip", p.IP); err == nil {
				timed = true
			}
		}
	}
	m.forget()
	return map[string]any{"ok": true, "timed": timed}, nil
}

// f2bUnban is "security.unban": some addresses, or all of them.
func (m *secModule) f2bUnban(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p f2bCall
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	if err := m.f2bReady(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if p.All {
		if _, err := m.run(ctx, "fail2ban-client", "unban", "--all"); err != nil {
			return nil, cmdMessage(err)
		}
		m.forget()
		return map[string]any{"ok": true, "all": true}, nil
	}
	if len(p.IPs) == 0 || len(p.IPs) > 500 {
		return nil, errors.New("give the addresses to unblock")
	}
	jail := p.Jail
	if jail == "" {
		jail = sshJailName
	}
	if !f2bJailNameRe.MatchString(jail) {
		return nil, errors.New("invalid jail")
	}
	done := 0
	var firstErr error
	for _, ip := range p.IPs {
		if _, err := netip.ParseAddr(ip); err != nil {
			return nil, fmt.Errorf("%q is not an address", ip)
		}
		if _, err := m.run(ctx, "fail2ban-client", "set", jail, "unbanip", ip); err != nil {
			if firstErr == nil {
				firstErr = cmdMessage(err)
			}
			continue
		}
		done++
	}
	m.forget()
	if done == 0 {
		return nil, firstErr
	}
	return map[string]any{"ok": true, "unbanned": done}, nil
}

// f2bBanHistory is "security.banHistory": the ban and unban events of the log, newest first.
func (m *secModule) f2bBanHistory(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(raw, &p)
	if p.Limit <= 0 || p.Limit > 1000 {
		p.Limit = 200
	}
	var events []f2bEvent
	read := false
	for _, f := range []string{f2bLogPath + ".1", f2bLogPath} {
		b, err := m.readFile(f)
		if err != nil {
			continue
		}
		read = true
		if len(b) > 16<<20 {
			b = b[len(b)-16<<20:]
		}
		events = append(events, parseF2BLog(string(b), m.now().Location())...)
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].TS > events[j].TS })
	total := len(events)
	if len(events) > p.Limit {
		events = events[:p.Limit]
	}
	if events == nil {
		events = []f2bEvent{}
	}
	return map[string]any{"events": events, "total": total, "log": f2bLogPath, "available": read}, nil
}

// confOptions looks at the host to choose the firewall action and the log backend.
func (m *secModule) confOptions(ctx context.Context) f2bConfOptions {
	var o f2bConfOptions
	if out, err := m.run(ctx, "sshd", "-T"); err == nil {
		for _, p := range parseSSHPorts(string(out)) {
			o.Ports = append(o.Ports, strconv.Itoa(p))
		}
	}
	if _, err := m.lookPath("nft"); err == nil {
		switch {
		case m.exists("/etc/fail2ban/action.d/nftables.conf"):
			o.BanAction, o.AllPorts = "nftables", "nftables[type=allports]"
		case m.exists("/etc/fail2ban/action.d/nftables-multiport.conf"):
			o.BanAction, o.AllPorts = "nftables-multiport", "nftables-allports"
		}
	}
	o.SystemdLogs = !m.exists("/var/log/auth.log")
	return o
}

func (m *secModule) exists(path string) bool {
	_, err := m.readFile(path)
	return err == nil
}

// f2bSaveSettings is "security.f2bSettings": writes the file of the panel and reloads fail2ban.
func (m *secModule) f2bSaveSettings(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p struct {
		f2bSettings
		Self string `json:"self"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	if err := validateF2BSettings(&p.f2bSettings); err != nil {
		return nil, err
	}
	if err := m.f2bReady(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	p.Ignore = withLoopback(p.Ignore, p.Self)
	text := renderF2BConf(p.f2bSettings, m.confOptions(ctx))
	if err := m.writeConf(f2bConfPath, []byte(text)); err != nil {
		return nil, err
	}
	if _, err := m.run(ctx, "fail2ban-client", "reload"); err != nil {
		return nil, fmt.Errorf("the settings were written, but fail2ban did not accept them: %w", cmdMessage(err))
	}
	m.forget()
	return map[string]any{"ok": true}, nil
}

func defaultWriteConf(path string, data []byte) error {
	tmp := path + ".linager-new"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { //nolint:gosec // a fail2ban configuration file is world-readable
		return err
	}
	return os.Rename(tmp, path)
}

// f2bInstall is the stream "security.f2bInstall": installs the package, writes the settings and starts the service.
func (m *secModule) f2bInstall(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p struct {
		Preset string `json:"preset"`
		Self   string `json:"self"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return errors.New("invalid request")
	}
	preset, ok := f2bPresets[p.Preset]
	if !ok {
		return errors.New("unknown preset")
	}
	if p.Self != "" && !validIPOrNet(p.Self) {
		return errors.New("invalid address")
	}
	if err := m.f2bReady(); err == nil {
		return errors.New("fail2ban is already installed")
	}
	say := func(format string, args ...any) { _ = sio.Out([]byte(fmt.Sprintf(format, args...) + "\n")) }
	run := func(name string, args ...string) error {
		say("$ %s %s", name, strings.Join(args, " "))
		cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // fixed commands, validated arguments
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C")
		w := outWriter{sio}
		cmd.Stdout, cmd.Stderr = w, w
		return cmd.Run()
	}
	if err := run("apt-get", "install", "-y", "-o", "DPkg::Lock::Timeout=120", "fail2ban", "python3-systemd"); err != nil {
		return fmt.Errorf("the package was not installed: %w", err)
	}
	s := f2bSettings{MaxRetry: preset[0], FindTime: preset[1], BanTime: preset[2], Ignore: withLoopback(nil, p.Self)}
	opts := m.confOptions(ctx)
	say("Writing %s", f2bConfPath)
	if err := m.writeConf(f2bConfPath, []byte(renderF2BConf(s, opts))); err != nil {
		return err
	}
	if err := run("systemctl", "enable", "fail2ban"); err != nil {
		return err
	}
	if err := run("systemctl", "restart", "fail2ban"); err != nil {
		return err
	}
	// the service needs a moment before the client can talk to it
	for i := 0; i < 10; i++ {
		if _, err := m.run(ctx, "fail2ban-client", "status", sshJailName); err == nil {
			m.forget()
			say("fail2ban protects SSH: %d failures in %d minutes, a ban for %s.", s.MaxRetry, s.FindTime/60, humanSeconds(s.BanTime))
			return nil
		}
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return errors.New("fail2ban was installed but the sshd jail did not start, see journalctl -u fail2ban")
}

func humanSeconds(s int) string {
	switch {
	case s < 0:
		return "ever"
	case s%86400 == 0:
		return fmt.Sprintf("%d d", s/86400)
	case s%3600 == 0:
		return fmt.Sprintf("%d h", s/3600)
	default:
		return fmt.Sprintf("%d min", s/60)
	}
}
