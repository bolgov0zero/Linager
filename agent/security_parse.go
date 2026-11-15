package agent

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Parsers for the "Security" tab: sshd log lines, sshd configuration, fail2ban output and
// authorized_keys. They work on text, so they are tested with real samples and no system calls.

var (
	sshAcceptedRe = regexp.MustCompile(`Accepted (\S+) for (\S+) from (\S+) port (\d+)`)
	sshFailedRe   = regexp.MustCompile(`Failed (\S+) for (invalid user )?(\S+) from (\S+) port (\d+)`)
	sshInvalidRe  = regexp.MustCompile(`Invalid user (\S*) from (\S+)`)
)

// sshEvent is one interesting line of the sshd log.
type sshEvent struct {
	Kind    string // "accepted", "failed", "invalid"
	TS      int64
	User    string
	IP      string
	Method  string
	Port    int
	Invalid bool // the failed attempt was for a user that does not exist
}

// parseSSHMessage recognizes an sshd message; ok is false for lines that do not matter.
func parseSSHMessage(msg string, ts int64) (sshEvent, bool) {
	if m := sshAcceptedRe.FindStringSubmatch(msg); m != nil {
		port, _ := strconv.Atoi(m[4])
		return sshEvent{Kind: "accepted", TS: ts, Method: normalizeMethod(m[1]), User: m[2], IP: m[3], Port: port}, true
	}
	if m := sshFailedRe.FindStringSubmatch(msg); m != nil {
		port, _ := strconv.Atoi(m[5])
		return sshEvent{Kind: "failed", TS: ts, Method: normalizeMethod(m[1]), Invalid: m[2] != "", User: m[3], IP: m[4], Port: port}, true
	}
	if m := sshInvalidRe.FindStringSubmatch(msg); m != nil {
		return sshEvent{Kind: "invalid", TS: ts, User: m[1], IP: m[2], Invalid: true}, true
	}
	return sshEvent{}, false
}

func normalizeMethod(m string) string {
	if strings.HasPrefix(m, "keyboard-interactive") {
		return "keyboard-interactive"
	}
	return m
}

// parseJournalLine reads a line of `journalctl -o short-unix`: "1731666000.123456 host sshd[1]: message".
func parseJournalLine(line string) (sshEvent, bool) {
	sp := strings.IndexByte(line, ' ')
	if sp < 1 {
		return sshEvent{}, false
	}
	f, err := strconv.ParseFloat(line[:sp], 64)
	if err != nil {
		return sshEvent{}, false
	}
	i := strings.Index(line, "]: ")
	if i < 0 {
		return sshEvent{}, false
	}
	return parseSSHMessage(line[i+3:], int64(f))
}

// authLogTime reads the time of a line of /var/log/auth.log in the RFC 3339 form that current rsyslog
// writes ("2024-11-15T12:34:56.123456+04:00 host ...") or the traditional "Nov 15 12:34:56 host ...".
func authLogTime(line string, now time.Time) (int64, bool) {
	if len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			return 0, false
		}
		t, err := time.Parse(time.RFC3339Nano, line[:sp])
		if err != nil {
			return 0, false
		}
		return t.Unix(), true
	}
	if len(line) < 16 {
		return 0, false
	}
	t, err := time.ParseInLocation("Jan _2 15:04:05", line[:15], now.Location())
	if err != nil {
		return 0, false
	}
	ts := time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, now.Location())
	if ts.After(now.Add(24 * time.Hour)) { // December lines read in January
		ts = ts.AddDate(-1, 0, 0)
	}
	return ts.Unix(), true
}

// parseAuthLogLine reads an sshd line of /var/log/auth.log.
func parseAuthLogLine(line string, now time.Time) (sshEvent, bool) {
	if !strings.Contains(line, "sshd[") && !strings.Contains(line, "sshd-session[") {
		return sshEvent{}, false
	}
	i := strings.Index(line, "]: ")
	if i < 0 {
		return sshEvent{}, false
	}
	ts, ok := authLogTime(line, now)
	if !ok {
		return sshEvent{}, false
	}
	return parseSSHMessage(line[i+3:], ts)
}

// ---- sshd configuration ----

// parseSSHDConfig reads `sshd -T` output or sshd_config text: lower-cased keywords, the first value wins
// (that is how sshd treats a repeated keyword) and the text after the first "Match" is ignored.
func parseSSHDConfig(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		key = strings.ToLower(key)
		if key == "match" {
			break
		}
		if eq := strings.IndexByte(key, '='); eq > 0 { // "Key=value"
			key, val = key[:eq], line[strings.IndexByte(line, '=')+1:]
		}
		val = strings.TrimSpace(strings.Trim(strings.TrimSpace(val), "="))
		if _, seen := out[key]; !seen {
			out[key] = val
		}
	}
	return out
}

// ---- fail2ban ----

type f2bJail struct {
	Name            string   `json:"name"`
	CurrentlyFailed int      `json:"currently_failed"`
	TotalFailed     int      `json:"total_failed"`
	CurrentlyBanned int      `json:"currently_banned"`
	TotalBanned     int      `json:"total_banned"`
	BannedIPs       []string `json:"banned_ips"`
}

var (
	f2bJailListRe = regexp.MustCompile(`(?m)Jail list:\s*(.*)$`)
	f2bNumRe      = func(label string) *regexp.Regexp { return regexp.MustCompile(`(?m)` + label + `:\s*(\d+)`) }
	f2bCurFailed  = f2bNumRe("Currently failed")
	f2bTotFailed  = f2bNumRe("Total failed")
	f2bCurBanned  = f2bNumRe("Currently banned")
	f2bTotBanned  = f2bNumRe("Total banned")
	f2bIPListRe   = regexp.MustCompile(`(?m)Banned IP list:\s*(.*)$`)
)

// parseF2BJails reads the jail names from `fail2ban-client status`.
func parseF2BJails(out string) []string {
	m := f2bJailListRe.FindStringSubmatch(out)
	if m == nil {
		return nil
	}
	var jails []string
	for _, j := range strings.Split(m[1], ",") {
		if j = strings.TrimSpace(j); j != "" {
			jails = append(jails, j)
		}
	}
	return jails
}

// parseF2BJail reads `fail2ban-client status <jail>`.
func parseF2BJail(name, out string) f2bJail {
	num := func(re *regexp.Regexp) int {
		if m := re.FindStringSubmatch(out); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
		return 0
	}
	j := f2bJail{Name: name, CurrentlyFailed: num(f2bCurFailed), TotalFailed: num(f2bTotFailed),
		CurrentlyBanned: num(f2bCurBanned), TotalBanned: num(f2bTotBanned), BannedIPs: []string{}}
	if m := f2bIPListRe.FindStringSubmatch(out); m != nil {
		j.BannedIPs = strings.Fields(m[1])
	}
	return j
}

// ---- authorized_keys ----

type authKey struct {
	User        string `json:"user"`
	File        string `json:"file"`
	Type        string `json:"type"`
	Bits        int    `json:"bits,omitempty"`
	Fingerprint string `json:"fingerprint"`
	Comment     string `json:"comment"`
	Restricted  bool   `json:"restricted"` // the key has options (command=, from=, ...)
	Weak        bool   `json:"weak"`       // DSA, or RSA below 2048 bits
	ModTime     int64  `json:"mtime"`
}

func isKeyType(s string) bool {
	return strings.HasPrefix(s, "ssh-") || strings.HasPrefix(s, "ecdsa-sha2-") || strings.HasPrefix(s, "sk-ssh-") || strings.HasPrefix(s, "sk-ecdsa-")
}

// splitKeyLine finds the key type in a line of authorized_keys: the options before it may contain
// quoted spaces ("command=\"a b\"").
func splitKeyLine(line string) (options, typ, blob, comment string, ok bool) {
	inQuote := false
	start := 0
	var fields []string
	var offsets []int
	for i := 0; i <= len(line); i++ {
		if i == len(line) || (!inQuote && (line[i] == ' ' || line[i] == '\t')) {
			if i > start {
				fields = append(fields, line[start:i])
				offsets = append(offsets, start)
			}
			start = i + 1
			continue
		}
		if line[i] == '"' && (i == 0 || line[i-1] != '\\') {
			inQuote = !inQuote
		}
	}
	for i, f := range fields {
		if !isKeyType(f) {
			continue
		}
		if i+1 >= len(fields) {
			return "", "", "", "", false
		}
		if i > 0 {
			options = strings.TrimSpace(line[:offsets[i]])
		}
		rest := ""
		if i+2 < len(fields) {
			rest = strings.TrimSpace(line[offsets[i+2]:])
		}
		return options, f, fields[i+1], rest, true
	}
	return "", "", "", "", false
}

// keyInfo computes the OpenSSH-style fingerprint of a key blob and, for RSA, its size.
func keyInfo(typ, b64 string) (fingerprint string, bits int, ok bool) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) < 8 {
		return "", 0, false
	}
	sum := sha256.Sum256(raw)
	fingerprint = "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	if typ == "ssh-rsa" {
		// wire format: string "ssh-rsa", mpint e, mpint n
		b := raw
		read := func() []byte {
			if len(b) < 4 {
				return nil
			}
			n := int(binary.BigEndian.Uint32(b))
			if n < 0 || len(b) < 4+n {
				return nil
			}
			v := b[4 : 4+n]
			b = b[4+n:]
			return v
		}
		read()
		read()
		if n := read(); n != nil {
			for len(n) > 0 && n[0] == 0 {
				n = n[1:]
			}
			bits = len(n) * 8
		}
	}
	return fingerprint, bits, true
}

// parseAuthorizedKeys lists the keys of one authorized_keys file.
func parseAuthorizedKeys(user, file, content string, mtime int64) []authKey {
	var keys []authKey
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		options, typ, blob, comment, ok := splitKeyLine(line)
		if !ok {
			continue
		}
		fp, bits, ok := keyInfo(typ, blob)
		if !ok {
			continue
		}
		keys = append(keys, authKey{User: user, File: file, Type: typ, Bits: bits, Fingerprint: fp, Comment: comment,
			Restricted: options != "", Weak: typ == "ssh-dss" || (typ == "ssh-rsa" && bits > 0 && bits < 2048), ModTime: mtime})
		if len(keys) >= 200 {
			break
		}
	}
	return keys
}

// expandKeyPath expands %h, %u and %% of an AuthorizedKeysFile value; relative paths are under the home.
func expandKeyPath(pattern, home, user string) string {
	var sb strings.Builder
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '%' && i+1 < len(pattern) {
			i++
			switch pattern[i] {
			case 'h':
				sb.WriteString(home)
			case 'u':
				sb.WriteString(user)
			case '%':
				sb.WriteByte('%')
			default:
				sb.WriteByte('%')
				sb.WriteByte(pattern[i])
			}
			continue
		}
		sb.WriteByte(pattern[i])
	}
	p := sb.String()
	if !strings.HasPrefix(p, "/") {
		p = strings.TrimRight(home, "/") + "/" + p
	}
	return p
}

// ---- users ----

type passwdUser struct{ Name, Home string }

// parsePasswd returns the accounts that can log in: root and the regular users with a real shell.
func parsePasswd(content string) []passwdUser {
	var out []passwdUser
	for _, line := range strings.Split(content, "\n") {
		f := strings.Split(line, ":")
		if len(f) < 7 {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		if err != nil || (uid != 0 && (uid < 1000 || uid == 65534)) {
			continue
		}
		shell := f[6]
		if shell == "" || strings.HasSuffix(shell, "/nologin") || strings.HasSuffix(shell, "/false") || f[5] == "" {
			continue
		}
		out = append(out, passwdUser{Name: f[0], Home: f[5]})
	}
	return out
}

// ---- unattended upgrades ----

var autoUpgradeRe = regexp.MustCompile(`APT::Periodic::Unattended-Upgrade\s+"(\d+)"`)

// unattendedEnabled reports whether the apt configuration enables the daily unattended upgrade
// (the last file in name order wins, like apt reads them).
func unattendedEnabled(files []string) bool {
	val := ""
	for _, content := range files {
		if m := autoUpgradeRe.FindAllStringSubmatch(content, -1); m != nil {
			val = m[len(m)-1][1]
		}
	}
	return val != "" && val != "0"
}
