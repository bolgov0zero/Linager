package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// logindDir is where systemd-logind keeps one small file of key=value lines per session.
const logindDir = "/run/systemd/sessions"

// logindSession is what the Assistant needs to know about a session of systemd-logind.
type logindSession struct {
	ID     string // the number logind gave the session
	UID    int
	User   string
	Active bool   // the session is in front on its seat
	State  string // active, online, closing, ...
	Type   string // wayland, x11, tty, unspecified, ...
	Class  string // user, greeter, lock-screen, ...
	Seat   string // seat0 is the one with the screen and the keyboard
	Remote bool   // a session over the network (ssh, RDP): there is no screen of the computer in it
	Leader int    // the process that started the session: its credentials are the ones of the person
}

// readLogindSessions reads the session files of dir.
func readLogindSessions(dir string) []logindSession {
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return nil
	}
	var out []logindSession
	for _, f := range files {
		if strings.HasSuffix(f, ".ref") {
			continue
		}
		b, err := os.ReadFile(f) //nolint:gosec // a file of systemd-logind
		if err != nil {
			continue
		}
		if s, ok := parseLogindSession(filepath.Base(f), string(b)); ok {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// parseLogindSession reads one session file; id is the name of the file.
func parseLogindSession(id, text string) (logindSession, bool) {
	s := logindSession{ID: id}
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "UID":
			s.UID, _ = strconv.Atoi(v)
		case "USER":
			s.User = v
		case "ACTIVE":
			s.Active = v == "1"
		case "STATE":
			s.State = v
		case "TYPE":
			s.Type = v
		case "CLASS":
			s.Class = v
		case "SEAT":
			s.Seat = v
		case "REMOTE":
			s.Remote = v == "1"
		case "LEADER":
			s.Leader, _ = strconv.Atoi(v)
		}
	}
	return s, s.UID > 0 || s.User != ""
}

// graphicalUserSession tells whether the session is one whose screen the Assistant can show: the graphical session of a
// person on a seat or the sign-in screen (the greeter of GDM), not a login over the network. x11 is listed too so that
// the reason can be told.
func (s logindSession) graphicalUserSession() bool {
	return !s.Remote && (s.Class == "user" || s.Class == "greeter") && s.Seat != "" && (s.Type == "wayland" || s.Type == "x11")
}

// supported is true for the kind of session the Assistant can capture now (GNOME on Wayland).
func (s logindSession) supported() bool { return s.graphicalUserSession() && s.Type == "wayland" }

// consoleSession is the graphical session that is in front on seat0: the console of the computer (a person's session,
// or the sign-in screen while nobody is signed in).
func consoleSession(list []logindSession) (logindSession, bool) {
	var greeter *logindSession
	for i, s := range list {
		if !s.graphicalUserSession() || s.Seat != "seat0" || !s.Active {
			continue
		}
		if s.Class == "user" {
			return s, true
		}
		if greeter == nil {
			greeter = &list[i]
		}
	}
	if greeter != nil {
		return *greeter, true
	}
	return logindSession{}, false
}

// assistGraphical says whether the computer has a graphical console the Assistant can show (the panel then offers it).
func assistGraphical() bool {
	s, ok := consoleSession(readLogindSessions(logindDir))
	return ok && s.supported()
}

// parseProcCreds reads the group and the supplementary groups out of /proc/<pid>/status. A user that is not in
// /etc/passwd (the sign-in screen of GDM runs as a dynamic user of systemd) has no other way to be asked for them.
func parseProcCreds(status string) (gid uint32, groups []uint32, ok bool) {
	for _, line := range strings.Split(status, "\n") {
		k, v, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		f := strings.Fields(v)
		switch k {
		case "Gid":
			if len(f) > 0 {
				n, err := strconv.ParseUint(f[0], 10, 32)
				if err != nil {
					return 0, nil, false
				}
				gid, ok = uint32(n), true
			}
		case "Groups":
			for _, g := range f {
				if n, err := strconv.ParseUint(g, 10, 32); err == nil {
					groups = append(groups, uint32(n))
				}
			}
		}
	}
	return gid, groups, ok
}
