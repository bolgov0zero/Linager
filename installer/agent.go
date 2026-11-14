package installer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bolgov0zero/linager/agent"
	"github.com/bolgov0zero/linager/internal/config"
	"github.com/bolgov0zero/linager/internal/version"
)

// AgentOpts are values given by flags; zero values are asked interactively.
type AgentOpts struct {
	Server      string // host:port
	Fingerprint string
	DataDir     string
	LogPath     string
	Force       bool
}

// AgentFlow installs the agent on this host and pairs it with a server.
func AgentFlow(o AgentOpts) Flow {
	return func(ctx context.Context, c Console) (*Summary, error) {
		l := c.Lang()
		if o.DataDir == "" {
			o.DataDir = AgentDir
		}
		if o.LogPath == "" {
			o.LogPath = LogFile
		}
		if err := checkSystem(l); err != nil {
			return nil, err
		}
		if FileExists(filepath.Join(o.DataDir, "state.json")) && !o.Force {
			return nil, fmt.Errorf("%s", tr(l, "err.enrolled", o.DataDir))
		}
		if o.Server == "" {
			if !c.Interactive() {
				return nil, fmt.Errorf("%s", tr(l, "err.noninteractive", "--server"))
			}
			a, err := c.Ask(Form{Title: tr(l, "agent.title"), Fields: []Field{{
				Key: "server", Label: tr(l, "agent.server"), Hint: tr(l, "agent.server.h"), Kind: KindText, Required: true,
				Validate: func(v string, _ map[string]string) error {
					if _, _, ok := SplitHostPort(v); !ok {
						return errors.New(tr(l, "err.hostport"))
					}
					return nil
				}}}})
			if err != nil {
				return nil, err
			}
			o.Server = strings.TrimSpace(a["server"])
		}
		host, port, ok := SplitHostPort(o.Server)
		if !ok {
			return nil, errors.New(tr(l, "err.hostport"))
		}
		addr := net.JoinHostPort(host, fmt.Sprint(port))
		run, closeLog := NewRunner(o.LogPath)
		defer closeLog()
		run.Logf("=== linager install agent %s server=%s", time.Now().Format(time.RFC3339), addr)

		var fp string
		steps := []step{
			{title: tr(l, "st.check"), fn: func(context.Context) error { return checkSystem(l) }},
			{title: tr(l, "st.binary"), fn: func(context.Context) error { return installSelf() }},
			{title: tr(l, "st.connect"), info: tr(l, "info.wait"), fn: func(ctx context.Context) error {
				got, err := agent.ProbeFingerprint(ctx, addr)
				if err != nil {
					return err
				}
				fp = got
				switch {
				case o.Fingerprint != "":
					if o.Fingerprint != got {
						return fmt.Errorf("%s", tr(l, "err.fp", got, o.Fingerprint))
					}
				case !c.Interactive():
					return fmt.Errorf("%s", tr(l, "err.fpmissing"))
				}
				return nil
			}},
		}
		// Fingerprint confirmation needs the terminal, so it runs between the steps.
		pairing := func(ctx context.Context) error {
			return agent.Enroll(ctx, agent.EnrollOptions{
				Server: addr, Fingerprint: fp, DataDir: o.DataDir, AgentVersion: version.Version,
				Show: func(key, code, f string) { c.Wait(Waiting{Key: key, Code: code, Fingerprint: f, Server: addr}) },
			})
		}
		steps = append(steps,
			step{title: tr(l, "st.pair"), fn: func(ctx context.Context) error {
				if o.Fingerprint == "" { // interactive confirmation of an unknown server
					a, err := c.Ask(Form{Title: tr(l, "agent.fp"), Fields: []Field{{
						Key: "trust", Label: tr(l, "agent.fp.trust") + "  " + fp, Kind: KindChoice, Default: "no",
						Options: []Option{{"yes", tr(l, "agent.yes")}, {"no", tr(l, "agent.no")}}}}})
					if err != nil {
						return err
					}
					if a["trust"] != "yes" {
						return errors.New(tr(l, "err.notrust"))
					}
				}
				return pairing(ctx)
			}},
			step{title: tr(l, "st.service"), fn: func(ctx context.Context) error {
				if err := writeAgentConfig(); err != nil {
					return err
				}
				if err := os.WriteFile("/etc/systemd/system/linager-agent.service", []byte(AgentUnit), 0o644); err != nil { //nolint:gosec // unit files are world-readable
					return err
				}
				if err := run.Run(ctx, "", "systemctl", "daemon-reload"); err != nil {
					return err
				}
				if err := run.Run(ctx, "", "systemctl", "enable", "linager-agent"); err != nil {
					return err
				}
				return run.Run(ctx, "", "systemctl", "restart", "linager-agent")
			}},
		)
		if err := runSteps(ctx, c, steps); err != nil {
			return nil, err
		}
		return &Summary{Title: tr(l, "done.agent"), Success: true, Lines: []KV{
			{Label: tr(l, "sum.server"), Value: addr},
			{Label: tr(l, "agent.fp"), Value: fp, Mono: true},
			{Label: tr(l, "sum.state"), Value: tr(l, "sum.state.ok")},
			{Label: tr(l, "sum.log"), Value: o.LogPath},
		}}, nil
	}
}

// writeAgentConfig creates /etc/linager/agent.yaml with every module enabled; an existing file is kept.
func writeAgentConfig() error {
	const path = "/etc/linager/agent.yaml"
	if FileExists(path) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // config dir is traversable, the file itself is 0600
		return err
	}
	var b strings.Builder
	b.WriteString("# Linager agent: switch modules off by setting them to false, then restart linager-agent.\nmodules:\n")
	for _, m := range config.AgentModules {
		fmt.Fprintf(&b, "  %s: true\n", m)
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
