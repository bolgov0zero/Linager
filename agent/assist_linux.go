//go:build linux

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/bolgov0zero/linager/internal/config"
)

// registerAssistant adds the Assistant to the commands of a Linux computer: the console of a graphical session of
// GNOME on Wayland. A computer without a screen has no sessions to offer, and the panel hides the Assistant for it.
func registerAssistant(r *Registry, cfg *config.Agent) {
	if cfg.Enabled("assistant") {
		r.streams["assistant"] = assistantStream
		r.cmds["assistant.sessions"] = assistSessionsCmd
	}
}

// listAssistSessions lists the graphical sessions of people (the one in front on the first seat is the console).
func listAssistSessions() []AssistSession {
	var out []AssistSession
	for _, s := range readLogindSessions(logindDir) {
		if !s.graphicalUserSession() {
			continue
		}
		id, err := strconv.ParseUint(s.ID, 10, 32)
		if err != nil {
			continue
		}
		typ := "other"
		if s.Seat == "seat0" && s.Active {
			typ = "console"
		}
		st := s.State
		if s.Type != "wayland" {
			st = s.Type // x11: found, but not supported yet
		}
		out = append(out, AssistSession{ID: uint32(id), Type: typ, User: s.User, State: st, Console: typ == "console", Screen: s.supported() && s.Active})
	}
	return out
}

func assistSessionsCmd(context.Context, json.RawMessage, bool) (any, error) {
	return map[string]any{"sessions": listAssistSessions()}, nil
}

// resolveAssistSession turns what the panel asked for ("console" or a number) into a session that can be shown.
func resolveAssistSession(s string) (logindSession, error) {
	list := readLogindSessions(logindDir)
	if s == "" || s == "console" {
		c, ok := consoleSession(list)
		if !ok {
			return logindSession{}, errors.New("no graphical session at the console of this computer: nobody is signed in to the desktop")
		}
		return checkSupported(c)
	}
	for _, x := range list {
		if x.ID == s && x.graphicalUserSession() {
			return checkSupported(x)
		}
	}
	return logindSession{}, fmt.Errorf("no graphical session %s", s)
}

func checkSupported(s logindSession) (logindSession, error) {
	if !s.Active {
		return s, fmt.Errorf("the session of %s is in the background: it has no screen to show", s.User)
	}
	return s, nil
}

// sessionLocked asks systemd-logind whether the screen of the session is locked (GNOME says so when it locks): GNOME
// lets no screen cast see the lock screen, so it is read below the compositor.
func sessionLocked(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()
	var path dbus.ObjectPath
	if conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").CallWithContext(ctx, "org.freedesktop.login1.Manager.GetSession", 0, id).Store(&path) != nil {
		return false
	}
	v, err := conn.Object("org.freedesktop.login1", path).GetProperty("org.freedesktop.login1.Session.LockedHint")
	if err != nil {
		return false
	}
	locked, _ := v.Value().(bool)
	return locked
}

// assistantStream is the Assistant of one session: the helper is started as the person of the session and the frames
// are carried. Ctrl+Alt+Del does not exist here.
func assistantStream(ctx context.Context, params json.RawMessage, sio *StreamIO) error {
	var p struct {
		Session string `json:"session"`
	}
	_ = json.Unmarshal(params, &p)
	return runAssistRelay(ctx, sio, p.Session, launchAssistHelper, nil)
}

// linHelper is the helper process in a session.
type linHelper struct {
	cmd  *exec.Cmd
	done chan struct{}
	mu   sync.Mutex
	err  error
	kill bool
}

func (l *linHelper) Done() <-chan struct{} { return l.done }

func (l *linHelper) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// Kill ends the helper and what it started (GStreamer): they are one process group.
func (l *linHelper) Kill() {
	l.mu.Lock()
	l.kill = true
	l.mu.Unlock()
	if l.cmd.Process != nil {
		_ = syscall.Kill(-l.cmd.Process.Pid, syscall.SIGKILL)
	}
}

// launchAssistHelper starts `linager agent assistant-helper` with the rights of the person of the session (the
// compositor lets only that person into its remote-desktop interface) and the environment of the session: the bus
// and the runtime directory of the person.
func launchAssistHelper(_ context.Context, session, addr, token string) (assistHelper, error) {
	s, err := resolveAssistSession(session)
	if err != nil {
		return nil, err
	}
	// the sign-in screen, an X11 session and a locked screen are read below the compositor: that needs root
	kms := !s.viaCompositor() || sessionLocked(s.ID)
	uid, gid, groups, name, home := sessionCredentials(s)
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	run := fmt.Sprintf("/run/user/%d", s.UID)
	// the number the helper puts in its hello: the sessions of a person are numbered, those of the sign-in screen are "c1", "c2"
	sid := s.ID
	if _, err := strconv.Atoi(sid); err != nil {
		sid = "0"
	}
	args := []string{"agent", "assistant-helper", "--addr", addr, "--session", sid}
	if kms {
		args = append(args, "--mode", "kms")
	}
	cmd := exec.Command(exe, args...) //nolint:gosec // our own binary
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + home,
		"USER=" + name,
		"LOGNAME=" + name,
		"XDG_RUNTIME_DIR=" + run,
		"DBUS_SESSION_BUS_ADDRESS=unix:path=" + run + "/bus",
		assistTokenEnv + "=" + token,
	}
	cmd.Dir = "/"
	tail := &tailWriter{max: 400}
	cmd.Stderr = io.MultiWriter(os.Stderr, tail) // the journal of the agent shows what the helper says, and so does the panel
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid, Groups: groups}, Setpgid: true}
	if kms { // root, as this service is: the framebuffer and /dev/uinput are for root
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "USER=" + name, assistTokenEnv + "=" + token}
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start the helper as %s: %w", name, err)
	}
	l := &linHelper{cmd: cmd, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		l.mu.Lock()
		var ee *exec.ExitError
		if err != nil && !l.kill && errors.As(err, &ee) && ee.ExitCode() > 0 {
			msg := helperExitText(uint32(ee.ExitCode())) //nolint:gosec // a small exit code
			if t := tail.String(); t != "" {
				msg += ": " + t
			}
			l.err = errors.New(msg)
		} else if err != nil && !l.kill {
			l.err = err
		}
		l.mu.Unlock()
		close(l.done)
	}()
	return l, nil
}

// sessionCredentials are the rights the helper takes: those of the person of the session. A person in /etc/passwd is
// looked up; one that is not (the sign-in screen of GDM runs as a dynamic user of systemd) is read from the process that
// started the session.
func sessionCredentials(s logindSession) (uid, gid uint32, groups []uint32, name, home string) {
	uid = uint32(s.UID) //nolint:gosec // a user number
	name, home = s.User, "/tmp"
	if u, err := user.LookupId(strconv.Itoa(s.UID)); err == nil {
		name, home = u.Username, u.HomeDir
		if g, err := strconv.ParseUint(u.Gid, 10, 32); err == nil {
			gid = uint32(g)
		}
		if ids, err := u.GroupIds(); err == nil {
			for _, id := range ids {
				if n, err := strconv.ParseUint(id, 10, 32); err == nil {
					groups = append(groups, uint32(n))
				}
			}
		}
		return uid, gid, groups, name, home
	}
	gid = uid // a dynamic user has the group of its own number, unless the process says otherwise
	if s.Leader > 0 {
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", s.Leader)); err == nil { //nolint:gosec // a number
			if g, gs, ok := parseProcCreds(string(b)); ok {
				gid, groups = g, gs
			}
		}
	}
	if len(groups) == 0 {
		groups = []uint32{gid}
	}
	return uid, gid, groups, name, home
}

// tailWriter keeps the last bytes written to it: the end of what the helper said before it stopped.
type tailWriter struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tailWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailWriter) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(strings.Fields(string(t.buf)), " ")
}
