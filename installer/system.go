package installer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"strings"
)

// Default paths.
const (
	BinPath    = "/usr/local/bin/linager"
	ConfigDir  = "/etc/linager"
	ConfigFile = "/etc/linager/server.yaml"
	DataDir    = "/var/lib/linager"
	AgentDir   = "/var/lib/linager-agent"
	LogFile    = "/var/log/linager-install.log"
)

// OSInfo is parsed from /etc/os-release.
type OSInfo struct{ ID, VersionID, Pretty string }

// ParseOSRelease parses os-release content.
func ParseOSRelease(content string) OSInfo {
	var o OSInfo
	for _, line := range strings.Split(content, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			o.ID = v
		case "VERSION_ID":
			o.VersionID = v
		case "PRETTY_NAME":
			o.Pretty = v
		}
	}
	return o
}

// Supported reports whether the distribution is on the supported list.
func (o OSInfo) Supported() bool {
	switch o.ID + ":" + o.VersionID {
	case "debian:12", "debian:13", "ubuntu:22.04", "ubuntu:24.04", "ubuntu:26.04":
		return true
	}
	return false
}

// Runner executes system commands and records everything in the install log.
type Runner struct {
	log io.Writer
}

// NewRunner opens (appends to) the log file; on failure it logs to nowhere.
func NewRunner(path string) (*Runner, func()) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // fixed log path
	if err != nil {
		return &Runner{log: io.Discard}, func() {}
	}
	return &Runner{log: f}, func() { _ = f.Close() }
}

// Logf writes a line to the log.
func (r *Runner) Logf(format string, a ...any) { _, _ = fmt.Fprintf(r.log, format+"\n", a...) }

// CmdError carries the tail of a failed command's output.
type CmdError struct {
	Cmd  string
	Err  error
	Tail []string
}

func (e *CmdError) Error() string { return fmt.Sprintf("%s: %v", e.Cmd, e.Err) }
func (e *CmdError) Unwrap() error { return e.Err }

// Run executes a command, appending its output to the log. stdin may be nil.
func (r *Runner) Run(ctx context.Context, stdin string, name string, args ...string) error {
	_, err := r.Output(ctx, stdin, name, args...)
	return err
}

// Output is Run that also returns stdout.
func (r *Runner) Output(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // callers pass fixed commands
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C", "LANG=C")
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, all bytes.Buffer
	cmd.Stdout = io.MultiWriter(&out, &all)
	cmd.Stderr = &all
	r.Logf("$ %s %s", name, redact(args))
	err := cmd.Run()
	_, _ = r.log.Write(all.Bytes())
	if err != nil {
		return out.String(), &CmdError{Cmd: name + " " + strings.Join(args[:min(len(args), 2)], " "), Err: err, Tail: lastLines(all.String(), 10)}
	}
	return out.String(), nil
}

// redact hides SQL passwords in logged arguments.
func redact(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.Contains(strings.ToUpper(a), "PASSWORD") {
			a = "<sql with password hidden>"
		}
		out[i] = a
	}
	return strings.Join(out, " ")
}

func lastLines(s string, n int) []string {
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		if t := strings.TrimRight(sc.Text(), " \r"); t != "" {
			lines = append(lines, t)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// RandomString returns n random characters from [a-zA-Z0-9] (safe in SQL and URLs).
func RandomString(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		v, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[v.Int64()]
	}
	return string(b)
}

// PortFree reports whether a TCP port can be bound on all addresses.
func PortFree(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// FreeLoopbackPort asks the kernel for a free port on 127.0.0.1.
func FreeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// LocalHosts returns the host name and non-loopback addresses for the agent-port certificate.
func LocalHosts(extra []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if h, err := os.Hostname(); err == nil {
		add(h)
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && !ipn.IP.IsLinkLocalUnicast() {
				add(ipn.IP.String())
			}
		}
	}
	for _, e := range extra {
		add(e)
	}
	return out
}

var errNotRoot = errors.New("not root")

// RequireRoot returns errNotRoot unless running as uid 0.
func RequireRoot() error {
	if os.Geteuid() != 0 {
		return errNotRoot
	}
	return nil
}

// FileExists reports whether path exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
