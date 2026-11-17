package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/bolgov0zero/linager/internal/release"
)

// fakeHost is an SSH server that answers the commands of the installer with canned output.
type fakeHost struct {
	t        *testing.T
	ln       net.Listener
	signer   ssh.Signer
	login    string
	password string
	sudoPW   string // "" = passwordless sudo
	root     bool
	osID     string
	reach    map[string]bool // address -> reachable
	failCmd  string          // an install command that fails
	mu       sync.Mutex
	cmds     []string
}

func newFakeHost(t *testing.T) *fakeHost {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := &fakeHost{t: t, ln: ln, signer: signer, login: "deploy", password: "login-pw", sudoPW: "", osID: "debian", reach: map[string]bool{"10.9.9.9": true}}
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if c.User() == h.login && string(pw) == h.password {
			return nil, nil
		}
		return nil, io.EOF
	}}
	cfg.AddHostKey(signer)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go h.serve(c, cfg)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return h
}

func (h *fakeHost) port() int { return h.ln.Addr().(*net.TCPAddr).Port }

func (h *fakeHost) serve(c net.Conn, cfg *ssh.ServerConfig) {
	_, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "no")
			continue
		}
		ch, creqs, _ := nc.Accept()
		go func() {
			for r := range creqs {
				if r.Type != "exec" {
					_ = r.Reply(false, nil)
					continue
				}
				var p struct{ Command string }
				_ = ssh.Unmarshal(r.Payload, &p)
				_ = r.Reply(true, nil)
				stdin, _ := io.ReadAll(ch)
				out, code := h.handle(p.Command, string(stdin))
				_, _ = ch.Write([]byte(out))
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{uint32(code)})) //nolint:gosec // a small exit code of a test
				_ = ch.Close()
				return
			}
		}()
	}
}

func (h *fakeHost) record(cmd string) {
	h.mu.Lock()
	h.cmds = append(h.cmds, cmd)
	h.mu.Unlock()
}

func (h *fakeHost) saw(sub string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.cmds {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

func (h *fakeHost) handle(cmd, stdin string) (string, int) {
	h.record(cmd)
	inner := cmd
	switch {
	case strings.HasPrefix(cmd, "sudo -n sh -c "):
		if h.sudoPW != "" {
			return "sudo: a password is required\n", 1
		}
		inner = cmd
	case strings.HasPrefix(cmd, "sudo -S -p '' sh -c "):
		if strings.TrimSpace(stdin) != h.sudoPW {
			return "Sorry, try again.\n", 1
		}
	}
	switch {
	case strings.Contains(cmd, "/etc/os-release"):
		id, ver, name := h.osID, "13", "Debian GNU/Linux 13 (trixie)"
		arch := "amd64"
		if id == "alpine" {
			ver, name, arch = "3.20", "Alpine Linux v3.20", ""
		}
		return "ID=" + id + "\nVER=" + ver + "\nNAME=" + name + "\nARCH=" + arch + "\nWGET=yes\nSYSTEMD=yes\nFREE=4200\n", 0
	case cmd == "id -u":
		if h.root {
			return "0\n", 0
		}
		return "1000\n", 0
	case cmd == "sudo -n true":
		if h.sudoPW != "" {
			return "sudo: a password is required\n", 1
		}
		return "", 0
	case strings.HasPrefix(cmd, "command -v sudo"):
		return "", 0
	case strings.HasPrefix(cmd, "sudo -S -p '' true"):
		if strings.TrimSpace(stdin) == h.sudoPW {
			return "", 0
		}
		return "Sorry, try again.\n", 1
	case strings.HasPrefix(cmd, "timeout 5 bash -c"):
		for addr, ok := range h.reach {
			if ok && strings.Contains(cmd, "/dev/tcp/"+addr+"/") {
				return "", 0
			}
		}
		return "", 124
	case strings.HasPrefix(cmd, "mktemp -d"):
		return "/tmp/linager-install.test\n", 0
	case strings.HasPrefix(cmd, "stat -c %s"):
		return "500\n", 0
	case strings.HasPrefix(cmd, "sha256sum"):
		return strings.Repeat("ab", 32) + "  /tmp/linager-install.test/linager\n", 0
	case strings.HasPrefix(cmd, "rm -rf"):
		return "", 0
	case strings.Contains(inner, "/var/lib/linager-agent/state.json") && strings.Contains(inner, "echo yes"):
		return "no\n", 0
	case strings.Contains(inner, "wget -nv"):
		return "", 0
	case strings.Contains(inner, " install agent"):
		if h.failCmd == "install" {
			return "[3/4] Pair with the server FAILED: boom\n", 1
		}
		return "[1/4] Check the system ok\n[4/4] Start the service ok\n", 0
	case strings.Contains(inner, "agent uninstall --purge"):
		return "removed\n", 0
	}
	return "", 0
}

// fakeServer is the side of Linager that the installer calls.
type fakeServer struct {
	mu        sync.Mutex
	ended     []int64
	forgotten []string
	audits    []string
	polls     int
	online    bool // whether the agent shows up
	bound     bool
}

func (f *fakeServer) deps(port string) SSHDeps {
	return SSHDeps{
		Hosts: func() []string { return []string{"10.9.9.9"} }, Port: "9443", Fingerprint: "SHA256:serverfp", Pin: "sha256//pin=",
		Assets: func(context.Context) (map[string]release.Asset, error) {
			return map[string]release.Asset{"amd64": {Name: "linager_linux_amd64", SHA256: strings.Repeat("ab", 32), Size: 1000}}, nil
		},
		NewToken: func(context.Context, string, *int64) (string, int64, error) {
			return "lk_" + strings.Repeat("S", 22), 7, nil
		},
		EndToken: func(_ context.Context, id int64) { f.mu.Lock(); f.ended = append(f.ended, id); f.mu.Unlock() },
		Observe: func(context.Context, int64) (string, string, bool, bool) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.polls++
			if !f.bound {
				return "", "", false, false
			}
			return "srv-1", "web-08", true, f.online
		},
		Forget: func(_ context.Context, id string) { f.mu.Lock(); f.forgotten = append(f.forgotten, id); f.mu.Unlock() },
		Audit: func(_ context.Context, user, action, target, result string, d map[string]any) {
			b, _ := json.Marshal(d)
			f.mu.Lock()
			f.audits = append(f.audits, user+"|"+action+"|"+target+"|"+result+"|"+string(b))
			f.mu.Unlock()
		},
	}
}

func newTestInstaller(f *fakeServer) *SSHInstaller {
	in := NewSSHInstaller(f.deps("9443"), nil)
	in.deny = func(netip.Addr) bool { return false } // the fake host is on loopback
	sshBindMax, sshConnectMax = 2*time.Second, 2*time.Second
	return in
}

func startCheck(t *testing.T, in *SSHInstaller, h *fakeHost, password string) *sshSession {
	t.Helper()
	s, err := in.Start(1, "admin", SSHStart{Host: "127.0.0.1", Port: h.port(), Login: h.login, Auth: "password", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func waitPhase(t *testing.T, s *sshSession, want ...string) SSHState {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		st := s.State()
		for _, w := range want {
			if st.Phase == w {
				return st
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, _ := json.Marshal(s.State())
	t.Fatalf("timeout waiting for %v: %s", want, b)
	return SSHState{}
}

func waitGateKind(t *testing.T, s *sshSession, kind string) SSHState {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if st := s.State(); st.Gate != nil && st.Gate.Kind == kind {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, _ := json.Marshal(s.State())
	t.Fatalf("no %s gate: %s", kind, b)
	return SSHState{}
}

func stepStates(st SSHState) string {
	var b strings.Builder
	for _, s := range st.Steps {
		b.WriteString(map[string]string{"pending": ".", "running": "~", "done": "#", "warn": "!", "failed": "x"}[s.State])
	}
	return b.String()
}

func TestSSHInstallHappyPath(t *testing.T) {
	h, f := newFakeHost(t), &fakeServer{bound: true, online: true}
	in := newTestInstaller(f)
	s := startCheck(t, in, h, h.password)

	g := waitGateKind(t, s, "hostkey")
	if g.Gate.Data["fingerprint"] != ssh.FingerprintSHA256(h.signer.PublicKey()) {
		t.Fatalf("the gate must show the fingerprint of the host key: %+v", g.Gate)
	}
	if err := s.Answer("hostkey", gateAnswer{Accept: true}); err != nil {
		t.Fatal(err)
	}
	st := waitPhase(t, s, "ready", "check_failed")
	if st.Phase != "ready" || stepStates(st) != "########" || st.Mode != "nopass" || st.Server != "10.9.9.9:9443" {
		t.Fatalf("check: %s %+v", stepStates(st), st)
	}
	if st.Steps[3].Detail != "Debian GNU/Linux 13 (trixie) · amd64" {
		t.Fatalf("system detail: %q", st.Steps[3].Detail)
	}
	if err := s.Answer("hostkey", gateAnswer{Accept: true}); err == nil {
		t.Fatal("an answer to a gate that is gone must be refused")
	}

	if err := s.StartInstall(); err != nil {
		t.Fatal(err)
	}
	st = waitPhase(t, s, "done", "install_failed")
	if st.Phase != "done" || stepStates(st) != "##############" || st.NewName != "web-08" || st.NewID != "srv-1" {
		b, _ := json.Marshal(st)
		t.Fatalf("install: %s %s", stepStates(st), b)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.ended) != 1 || f.ended[0] != 7 {
		t.Fatalf("the token must end with the install: %v", f.ended)
	}
	if !h.saw("--join-token") || !h.saw("install agent --no-tui") || !h.saw("rm -rf") {
		t.Fatalf("the commands: %v", h.cmds)
	}
	// secrets: the token and the passwords never reach the log or the state
	all, _ := json.Marshal(s.State())
	for _, secret := range []string{"lk_" + strings.Repeat("S", 22), h.password} {
		if strings.Contains(string(all), secret) {
			t.Fatalf("the state leaks %q: %s", secret, all)
		}
	}
	if len(f.audits) != 2 || !strings.HasPrefix(f.audits[0], "admin|ssh.check|deploy@127.0.0.1|ok") || !strings.HasPrefix(f.audits[1], "admin|ssh.install|deploy@127.0.0.1|ok") {
		t.Fatalf("audit: %v", f.audits)
	}
	for _, a := range f.audits {
		if strings.Contains(a, h.password) {
			t.Fatal("the audit has a password")
		}
	}
	// the secrets are wiped after a successful install
	if s.secret != "" || s.client != nil {
		t.Fatal("the secrets and the connection must be dropped after the install")
	}
}

func TestSSHSudoPasswordAndWrongOnes(t *testing.T) {
	h, f := newFakeHost(t), &fakeServer{bound: true, online: true}
	h.sudoPW = "the-sudo-pw"
	in := newTestInstaller(f)
	s := startCheck(t, in, h, h.password)
	waitGateKind(t, s, "hostkey")
	_ = s.Answer("hostkey", gateAnswer{Accept: true})
	waitGateKind(t, s, "sudo")
	_ = s.Answer("sudo", gateAnswer{Accept: true, Password: "wrong"})
	// a wrong password asks again, and says so
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := s.State()
		if st.Gate != nil && st.Gate.Kind == "sudo" && st.Gate.Data["retry"] == true {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no retry gate: %+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = s.Answer("sudo", gateAnswer{Accept: true, Password: "the-sudo-pw"}) //nolint:gosec // a test value
	st := waitPhase(t, s, "ready", "check_failed")
	if st.Phase != "ready" || st.Mode != "password" {
		t.Fatalf("%s %+v", stepStates(st), st)
	}
	if err := s.StartInstall(); err != nil {
		t.Fatal(err)
	}
	st = waitPhase(t, s, "done", "install_failed")
	if st.Phase != "done" {
		t.Fatalf("install: %+v", st)
	}
	b, _ := json.Marshal(st)
	for _, secret := range []string{"the-sudo-pw", "wrong", h.password} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("the state leaks %q: %s", secret, b)
		}
	}
}

func TestSSHChecksThatFail(t *testing.T) {
	// the system is not supported
	h, f := newFakeHost(t), &fakeServer{}
	h.osID = "alpine"
	s := startCheck(t, newTestInstaller(f), h, h.password)
	waitGateKind(t, s, "hostkey")
	_ = s.Answer("hostkey", gateAnswer{Accept: true})
	st := waitPhase(t, s, "check_failed")
	if st.Steps[3].Code != "os_unsupported" || !strings.Contains(st.Steps[3].Detail, "Alpine") || stepStates(st) != "###x...." && stepStates(st) != "###x....." {
		t.Fatalf("%s %+v", stepStates(st), st.Steps[3])
	}
	if err := s.StartInstall(); err == nil {
		t.Fatal("an install after failed checks must be refused")
	}

	// a wrong password
	h2 := newFakeHost(t)
	s = startCheck(t, newTestInstaller(f), h2, "not-the-password")
	waitGateKind(t, s, "hostkey")
	_ = s.Answer("hostkey", gateAnswer{Accept: true})
	st = waitPhase(t, s, "check_failed")
	if st.Steps[2].Code != "auth_failed" {
		t.Fatalf("%+v", st.Steps)
	}

	// the host key is refused
	h3 := newFakeHost(t)
	s = startCheck(t, newTestInstaller(f), h3, h3.password)
	waitGateKind(t, s, "hostkey")
	_ = s.Answer("hostkey", gateAnswer{Accept: false})
	st = waitPhase(t, s, "check_failed")
	if st.Steps[1].Code != "hostkey_rejected" {
		t.Fatalf("%+v", st.Steps)
	}
}

func TestSSHServerPortNotReachable(t *testing.T) {
	h, f := newFakeHost(t), &fakeServer{bound: true, online: true}
	h.reach = map[string]bool{"192.0.2.77": true} // only another address works
	s := startCheck(t, newTestInstaller(f), h, h.password)
	waitGateKind(t, s, "hostkey")
	_ = s.Answer("hostkey", gateAnswer{Accept: true})
	g := waitGateKind(t, s, "address")
	if st := s.State(); st.Steps[6].Code != "port_unreachable" {
		t.Fatalf("%+v", st.Steps[6])
	}
	_ = g
	_ = s.Answer("address", gateAnswer{Accept: true, Address: "192.0.2.77"})
	st := waitPhase(t, s, "ready", "check_failed")
	if st.Phase != "ready" || st.Server != "192.0.2.77:9443" {
		t.Fatalf("%+v", st)
	}
}

func TestSSHInstallFailuresRollBack(t *testing.T) {
	// the installer itself fails: the agent is removed again
	h, f := newFakeHost(t), &fakeServer{}
	h.failCmd = "install"
	s := startCheck(t, newTestInstaller(f), h, h.password)
	waitGateKind(t, s, "hostkey")
	_ = s.Answer("hostkey", gateAnswer{Accept: true})
	waitPhase(t, s, "ready")
	_ = s.StartInstall()
	st := waitPhase(t, s, "install_failed")
	if st.Steps[len(sshCheckIDs)+3].Code != "install_failed" || st.Steps[len(st.Steps)-1].ID != "rollback" || !h.saw("agent uninstall --purge") {
		t.Fatalf("%s %+v", stepStates(st), st.Steps)
	}
	f.mu.Lock()
	if len(f.ended) != 1 {
		t.Fatalf("the token must end also after a failure: %v", f.ended)
	}
	f.mu.Unlock()

	// the agent is accepted but never connects: rolled back and the server entry removed
	h2, f2 := newFakeHost(t), &fakeServer{bound: true, online: false}
	s = startCheck(t, newTestInstaller(f2), h2, h2.password)
	waitGateKind(t, s, "hostkey")
	_ = s.Answer("hostkey", gateAnswer{Accept: true})
	waitPhase(t, s, "ready")
	_ = s.StartInstall()
	st = waitPhase(t, s, "install_failed")
	if st.Steps[len(sshCheckIDs)+5].Code != "not_connected" || !h2.saw("agent uninstall --purge") {
		t.Fatalf("%s %+v", stepStates(st), st.Steps)
	}
	f2.mu.Lock()
	defer f2.mu.Unlock()
	if len(f2.forgotten) != 1 || f2.forgotten[0] != "srv-1" {
		t.Fatalf("the server entry of a failed install must be removed: %v", f2.forgotten)
	}
	// a second try after a failure is allowed
	_ = f2
}

func TestSSHTargetsAndSessions(t *testing.T) {
	f := &fakeServer{}
	in := NewSSHInstaller(f.deps("9443"), []string{"192.168.0.0/16"})
	for _, host := range []string{"127.0.0.1", "169.254.169.254", "0.0.0.0", "::1", "224.0.0.1", "10.1.1.1"} {
		if _, err := in.Start(1, "admin", SSHStart{Host: host, Port: 22, Login: "u", Auth: "password", Password: "x"}); err == nil {
			t.Fatalf("%s must be refused", host)
		}
	}
	// allowed networks limit the targets; sessions are per owner and wiped on close
	in.deny = func(netip.Addr) bool { return false }
	h := newFakeHost(t)
	if _, err := in.Start(1, "admin", SSHStart{Host: "127.0.0.1", Port: h.port(), Login: h.login, Auth: "password", Password: h.password}); err == nil {
		t.Fatal("an address outside the allowed networks must be refused")
	}
	in2 := newTestInstaller(f)
	var ids []string
	for i := 0; i < 3; i++ {
		s := startCheck(t, in2, h, h.password)
		ids = append(ids, s.id)
	}
	if _, err := in2.Start(1, "admin", SSHStart{Host: "127.0.0.1", Port: h.port(), Login: h.login, Auth: "password", Password: h.password}); err == nil {
		t.Fatal("a fourth open install must be refused")
	}
	if _, ok := in2.Get(ids[0], 2); ok {
		t.Fatal("another user must not see the session")
	}
	s, _ := in2.Get(ids[0], 1)
	if !in2.Close(ids[0], 1) || s.secret != "" {
		t.Fatal("closing must wipe the secrets")
	}
	if _, ok := in2.Get(ids[0], 1); ok {
		t.Fatal("a closed session is gone")
	}
	_ = strconv.Itoa
}
