package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// AptPackage is one upgradable package.
type AptPackage struct {
	Name      string `json:"name"`
	Current   string `json:"current"`
	Candidate string `json:"candidate"`
	Security  bool   `json:"security"`
	Reboot    bool   `json:"reboot"`
}

var aptMu sync.Mutex // one apt operation at a time

var instRe = regexp.MustCompile(`^Inst (\S+)(?: \[([^\]]+)\])? \((\S+) ([^)]*?)(?: \[[^\]]*\])?\)`)

// parseAptInst parses `apt-get -s upgrade` output.
func parseAptInst(out string) []AptPackage {
	var pk []AptPackage
	for _, line := range strings.Split(out, "\n") {
		m := instRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pk = append(pk, AptPackage{Name: m[1], Current: m[2], Candidate: m[3],
			Security: strings.Contains(strings.ToLower(m[4]), "security"), Reboot: needsReboot(m[1])})
	}
	return pk
}

// needsReboot is a heuristic: packages that usually require a reboot after the upgrade.
func needsReboot(name string) bool {
	return name == "libc6" || name == "systemd" || name == "dbus" || strings.HasPrefix(name, "linux-image") || strings.HasPrefix(name, "linux-base")
}

func aptSimulate(ctx context.Context) ([]AptPackage, error) {
	cmd := exec.CommandContext(ctx, "apt-get", "-s", "-o", "Debug::NoLocking=1", "upgrade", "--with-new-pkgs") //nolint:gosec // fixed command
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("apt-get simulation failed")
	}
	return parseAptInst(string(out)), nil
}

// lastAptCheck returns the unix time of the last successful `apt-get update`, or 0.
func lastAptCheck() int64 {
	var best int64
	for _, p := range []string{"/var/lib/apt/periodic/update-success-stamp", "/var/lib/apt/lists"} {
		if st, err := os.Stat(p); err == nil && st.ModTime().Unix() > best {
			best = st.ModTime().Unix()
		}
	}
	if m, _ := filepath.Glob("/var/lib/apt/lists/*Release"); len(m) > 0 {
		for _, p := range m {
			if st, err := os.Stat(p); err == nil && st.ModTime().Unix() > best {
				best = st.ModTime().Unix()
			}
		}
	}
	return best
}

func aptList(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	pk, err := aptSimulate(ctx)
	if err != nil {
		return nil, err
	}
	if pk == nil {
		pk = []AptPackage{}
	}
	_, rerr := os.Stat("/var/run/reboot-required")
	return map[string]any{"packages": pk, "reboot_required": rerr == nil, "last_check": lastAptCheck()}, nil
}

// aptStream runs update / upgrade / security-only upgrade and streams the output.
func aptStream(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p struct{ Mode string }
	_ = json.Unmarshal(raw, &p)
	if !aptMu.TryLock() {
		return errBusy
	}
	defer aptMu.Unlock()

	common := []string{"-y", "-q", "-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold", "-o", "Dpkg::Progress-Fancy=0"}
	var args []string
	switch p.Mode {
	case "update":
		args = []string{"update", "-q"}
	case "upgrade":
		args = append([]string{"upgrade", "--with-new-pkgs"}, common...)
	case "security":
		pk, err := aptSimulate(ctx)
		if err != nil {
			return err
		}
		names := []string{}
		for _, x := range pk {
			if x.Security {
				names = append(names, x.Name)
			}
		}
		if len(names) == 0 {
			return sio.Out([]byte("No security updates available.\n"))
		}
		args = append(append([]string{"install", "--only-upgrade"}, common...), names...)
	default:
		return errors.New("unknown mode")
	}
	cmd := exec.CommandContext(ctx, "apt-get", args...) //nolint:gosec // fixed command, package names come from apt itself
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C")
	return pipeCommand(cmd, sio)
}

// pipeCommand streams stdout and stderr of cmd into the stream until it exits.
func pipeCommand(cmd *exec.Cmd, sio *StreamIO) error {
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait(); _ = pw.Close() }()
	rd := bufio.NewReaderSize(pr, 16<<10)
	buf := make([]byte, 8<<10)
	for {
		n, err := rd.Read(buf)
		if n > 0 {
			if oerr := sio.Out(append([]byte(nil), buf[:n]...)); oerr != nil {
				_ = pr.Close()
				return oerr
			}
		}
		if err != nil {
			return nil
		}
	}
}
