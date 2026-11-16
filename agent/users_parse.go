package agent

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Parsers for the "Users" tab: /etc/passwd, /etc/group, /etc/shadow, sudoers and lastlog. They work on text,
// so they are tested with samples and no system calls.

// pwEntry is one line of /etc/passwd.
type pwEntry struct {
	Name, Gecos, Home, Shell string
	UID, GID                 int
}

func parsePasswdAll(content string) []pwEntry {
	var out []pwEntry
	for _, line := range strings.Split(content, "\n") {
		f := strings.Split(line, ":")
		if len(f) < 7 || strings.HasPrefix(line, "#") {
			continue
		}
		uid, err1 := strconv.Atoi(f[2])
		gid, err2 := strconv.Atoi(f[3])
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, pwEntry{Name: f[0], UID: uid, GID: gid, Gecos: strings.SplitN(f[4], ",", 2)[0], Home: f[5], Shell: f[6]})
	}
	return out
}

// grEntry is one line of /etc/group.
type grEntry struct {
	Name    string
	GID     int
	Members []string
}

func parseGroupFile(content string) []grEntry {
	var out []grEntry
	for _, line := range strings.Split(content, "\n") {
		f := strings.Split(line, ":")
		if len(f) < 4 || strings.HasPrefix(line, "#") {
			continue
		}
		gid, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		g := grEntry{Name: f[0], GID: gid}
		for _, m := range strings.Split(f[3], ",") {
			if m = strings.TrimSpace(m); m != "" {
				g.Members = append(g.Members, m)
			}
		}
		out = append(out, g)
	}
	return out
}

// shadowInfo is what the panel reads from /etc/shadow: the state of the password and of the account.
type shadowInfo struct {
	HasPassword bool  // a password hash is set (locked or not)
	Locked      bool  // the account is expired (the way the panel blocks every kind of login)
	Changed     int64 // unix time of the last password change, 0 when unknown
}

// parseShadow reads the hashes only to tell whether one is set; they are never kept or sent anywhere.
func parseShadow(content string, today int64) map[string]shadowInfo {
	out := map[string]shadowInfo{}
	for _, line := range strings.Split(content, "\n") {
		f := strings.Split(line, ":")
		if len(f) < 8 || strings.HasPrefix(line, "#") {
			continue
		}
		hash := strings.TrimPrefix(f[1], "!")
		info := shadowInfo{HasPassword: len(hash) > 1 && hash != "*" && hash != "!"}
		if d, err := strconv.ParseInt(f[2], 10, 64); err == nil && d > 0 {
			info.Changed = d * 86400
		}
		if d, err := strconv.ParseInt(f[7], 10, 64); err == nil && d >= 0 && d <= today {
			info.Locked = true
		}
		out[f[0]] = info
	}
	return out
}

// sudoNoPasswd reports whether a sudoers file gives the user NOPASSWD (a line that starts with the user name).
func sudoNoPasswd(content, user string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == user && strings.Contains(line, "NOPASSWD") {
			return true
		}
	}
	return false
}

var (
	lastlogClassicRe = regexp.MustCompile(`[A-Z][a-z]{2} [A-Z][a-z]{2} +\d+ \d\d:\d\d:\d\d [+-]\d{4} \d{4}`)
	lastlogISORe     = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}`)
)

// lastLogin is the last login of a user and where it came from.
type lastLogin struct {
	TS   int64
	From string
}

// parseLastlog reads the output of lastlog (or lastlog2): "Username Port From Latest", with
// "**Never logged in**" for accounts that never logged in. Lines it cannot read are skipped.
func parseLastlog(content string, loc *time.Location) map[string]lastLogin {
	out := map[string]lastLogin{}
	for _, line := range strings.Split(content, "\n") {
		var ts time.Time
		var at int
		if m := lastlogClassicRe.FindStringIndex(line); m != nil {
			t, err := time.Parse("Mon Jan _2 15:04:05 -0700 2006", strings.Join(strings.Fields(line[m[0]:m[1]]), " "))
			if err != nil {
				t, err = time.Parse("Mon Jan 2 15:04:05 -0700 2006", strings.Join(strings.Fields(line[m[0]:m[1]]), " "))
			}
			if err != nil {
				continue
			}
			ts, at = t, m[0]
		} else if m := lastlogISORe.FindStringIndex(line); m != nil {
			t, err := time.ParseInLocation("2006-01-02 15:04:05", strings.Replace(line[m[0]:m[1]], "T", " ", 1), loc)
			if err != nil {
				continue
			}
			ts, at = t, m[0]
		} else {
			continue
		}
		head := strings.Fields(line[:at])
		if len(head) == 0 {
			continue
		}
		l := lastLogin{TS: ts.Unix()}
		if len(head) >= 3 {
			l.From = head[len(head)-1]
		}
		out[head[0]] = l
	}
	return out
}

// countProcs counts the processes of every real uid from the contents of /proc/<pid>/status files.
func countProcs(statuses []string) map[int]int {
	out := map[int]int{}
	for _, st := range statuses {
		for _, line := range strings.Split(st, "\n") {
			if rest, ok := strings.CutPrefix(line, "Uid:"); ok {
				if f := strings.Fields(rest); len(f) > 0 {
					if uid, err := strconv.Atoi(f[0]); err == nil {
						out[uid]++
					}
				}
				break
			}
		}
	}
	return out
}

// countKeyLines counts the keys of an authorized_keys file (comments and blank lines do not count).
func countKeyLines(content string) int {
	n := 0
	for _, line := range strings.Split(content, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			n++
		}
	}
	return n
}

// sessionCounts counts the logind sessions per user name from `loginctl list-sessions --no-legend`
// (SESSION UID USER SEAT ...).
func sessionCounts(out string) map[string]int {
	res := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) >= 3 {
			res[f[2]]++
		}
	}
	return res
}

// userGroupNames returns the names of the groups the user is a member of (not the primary one).
func userGroupNames(groups []grEntry, user string) []string {
	var out []string
	for _, g := range groups {
		for _, m := range g.Members {
			if m == user {
				out = append(out, g.Name)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}
