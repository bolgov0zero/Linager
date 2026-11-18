//go:build !windows

package main

import (
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bolgov0zero/linager/agent"
	"github.com/bolgov0zero/linager/internal/config"
	"github.com/bolgov0zero/linager/internal/version"
)

// runAgent is `linager agent run` on Linux: the systemd service.
func runAgent(c *cobra.Command, dataDir, cfgPath string) error {
	ctx, stop := signal.NotifyContext(c.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// a newer agent knows more modules: list them in the file too (they are enabled either way)
	if added, err := config.CompleteAgent(cfgPath); err != nil {
		log.Warn("could not add the new modules to the agent configuration", "err", err)
	} else if len(added) > 0 {
		log.Info("added the new modules to the agent configuration", "modules", added)
	}
	cfg, err := config.LoadAgent(cfgPath)
	if err != nil {
		return err
	}
	err = agent.Run(ctx, agent.RunOptions{DataDir: dataDir, AgentVersion: version.Version, Config: cfg, Log: log})
	switch {
	case errors.Is(err, agent.ErrRevoked), errors.Is(err, agent.ErrUninstalled), errors.Is(err, agent.ErrNotEnrolled):
		return &exitError{code: exitCodeConfig, err: err}
	case ctx.Err() != nil:
		return nil
	}
	return err
}
