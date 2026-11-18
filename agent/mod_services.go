//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// Unit is one systemd service as shown in the panel.
type Unit struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Enabled     string `json:"enabled"`
}

var unitRe = regexp.MustCompile(`^[a-zA-Z0-9:_.@-]{1,120}\.service$`)

// criticalUnits are services whose stop/restart/disable can cut the host or the agent off.
var criticalUnits = map[string]bool{"ssh.service": true, "sshd.service": true, "linager-agent.service": true}

var svcVerbs = map[string]bool{"start": true, "stop": true, "restart": true, "enable": true, "disable": true}

// IsDangerousService reports whether the action needs an explicit confirmation.
func IsDangerousService(unit, action string) bool {
	return criticalUnits[unit] && (action == "stop" || action == "restart" || action == "disable")
}

// parseUnits merges `systemctl list-units` and `list-unit-files` output.
func parseUnits(listUnits, listFiles string) []Unit {
	byName := map[string]*Unit{}
	for _, line := range strings.Split(listUnits, "\n") {
		f := strings.Fields(strings.TrimLeft(line, "●* "))
		if len(f) < 4 || !strings.HasSuffix(f[0], ".service") || strings.Contains(f[0], "@.") || f[1] == "not-found" {
			continue
		}
		byName[f[0]] = &Unit{Name: f[0], Active: f[2], Sub: f[3], Description: strings.Join(f[4:], " ")}
	}
	for _, line := range strings.Split(listFiles, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || !strings.HasSuffix(f[0], ".service") || strings.Contains(f[0], "@.") {
			continue
		}
		u := byName[f[0]]
		if u == nil {
			u = &Unit{Name: f[0], Active: "inactive", Sub: "dead"}
			byName[f[0]] = u
		}
		u.Enabled = f[1]
	}
	out := make([]Unit, 0, len(byName))
	for _, u := range byName {
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func systemctl(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "systemctl", append([]string{"--no-pager", "--no-legend", "--plain", "--full"}, args...)...) //nolint:gosec // arguments are validated by callers
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func svcList(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	units, err := systemctl(ctx, "list-units", "--type=service", "--all")
	if err != nil {
		return nil, errors.New("systemctl list-units failed")
	}
	files, _ := systemctl(ctx, "list-unit-files", "--type=service")
	return map[string]any{"services": parseUnits(units, files)}, nil
}

func svcAction(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	var p struct{ Unit, Action string }
	if err := json.Unmarshal(raw, &p); err != nil || !unitRe.MatchString(p.Unit) || !svcVerbs[p.Action] {
		return nil, errors.New("invalid unit or action")
	}
	if IsDangerousService(p.Unit, p.Action) && !dangerous {
		return nil, errDangerous
	}
	args := []string{p.Action}
	if p.Unit == "linager-agent.service" {
		args = append(args, "--no-block") // this process is the unit: do not wait for ourselves
	}
	if out, err := systemctl(ctx, append(args, p.Unit)...); err != nil {
		return nil, errors.New(strings.TrimSpace(lastLine(out, "systemctl failed")))
	}
	return map[string]bool{"ok": true}, nil
}

func lastLine(s, def string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return def
}
