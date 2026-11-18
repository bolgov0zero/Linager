//go:build windows

package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/bolgov0zero/linager/agent"
	"github.com/bolgov0zero/linager/internal/version"
)

// runAgent is `linager agent run` on Windows. The service control manager starts it with this command; started by
// hand it runs in the console until Ctrl+C.
func runAgent(c *cobra.Command, dataDir, cfgPath string) error {
	var out io.Writer = os.Stderr
	if f, err := agent.OpenLog(agent.LogPath); err == nil {
		defer func() { _ = f.Close() }()
		if agent.IsService() {
			out = f // a service has no console
		} else {
			out = io.MultiWriter(os.Stderr, f)
		}
	}
	log := slog.New(slog.NewTextHandler(out, nil))
	run := func(ctx context.Context) error {
		return agent.RunHost(ctx, agent.HostOptions{DataDir: dataDir, ConfigPath: cfgPath, AgentVersion: version.Version, Log: log})
	}
	if agent.IsService() {
		return agent.RunService(run)
	}
	ctx, stop := signal.NotifyContext(c.Context(), os.Interrupt)
	defer stop()
	return run(ctx)
}
