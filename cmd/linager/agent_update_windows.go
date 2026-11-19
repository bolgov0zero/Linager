//go:build windows

package main

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"github.com/bolgov0zero/linager/agent"
	"github.com/bolgov0zero/linager/internal/version"
)

// newAgentUpdateCmds are the update commands of the Windows agent. The panel updates the agent by itself; these
// are the same steps by hand, and the two hidden commands that the update starts.
func newAgentUpdateCmds(dataDir *string) []*cobra.Command {
	var ver string
	upd := &cobra.Command{
		Use:   "update",
		Short: "Update this agent from the Linager server (signed release, automatic rollback)",
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(c.Context(), os.Interrupt)
			defer stop()
			out := c.OutOrStdout()
			if err := agent.UpdateFromServer(ctx, *dataDir, version.Version, ver, func(s string) { _, _ = fmt.Fprintln(out, s) }); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(out, "Installed. The service restarts now and rolls back by itself if it cannot reconnect to the server in about a minute.")
			return nil
		},
	}
	upd.Flags().StringVar(&ver, "version", "", "install this exact version (default: the server's release)")

	status := &cobra.Command{
		Use:   "status",
		Short: "Show the agent version and its connection state",
		RunE: func(c *cobra.Command, _ []string) error {
			_, _ = fmt.Fprint(c.OutOrStdout(), agent.StatusText(*dataDir, version.Version))
			return nil
		},
	}

	var exe string
	rollback := &cobra.Command{
		Use:    "rollback-update",
		Short:  "Restore the previous binary of an unconfirmed update (run by the scheduled task)",
		Hidden: true,
		RunE:   func(*cobra.Command, []string) error { return agent.RollbackUpdate(*dataDir, exe) },
	}
	rollback.Flags().StringVar(&exe, "binary", `C:\Program Files\Linager\linager.exe`, "path of the installed binary")

	restart := &cobra.Command{
		Use:    "restart-service",
		Short:  "Restart the agent service (started by an update from inside the service)",
		Hidden: true,
		RunE: func(*cobra.Command, []string) error {
			time.Sleep(time.Second) // the service has answered the server by now
			return agent.RestartService()
		},
	}
	rollbackChange := &cobra.Command{
		Use:    "rollback-change",
		Short:  "Roll back an unconfirmed network change (run by the scheduled task)",
		Hidden: true,
		RunE:   func(*cobra.Command, []string) error { return agent.RollbackNetworkChange(*dataDir) },
	}
	var helperAddr string
	var helperSession int
	helper := &cobra.Command{
		Use:    "assistant-helper",
		Short:  "Serve the screen of a Windows session to the agent (started by the agent service in the session)",
		Hidden: true,
		Run: func(c *cobra.Command, _ []string) {
			ctx, stop := signal.NotifyContext(c.Context(), os.Interrupt)
			defer stop()
			code, err := agent.RunAssistHelper(ctx, helperAddr, helperSession)
			if err != nil {
				_, _ = fmt.Fprintln(c.ErrOrStderr(), err)
			}
			if code != 0 {
				stop()
				os.Exit(code)
			}
		},
	}
	helper.Flags().StringVar(&helperAddr, "addr", "", "address of the agent on the loopback")
	helper.Flags().IntVar(&helperSession, "session", 0, "number of the session")
	return []*cobra.Command{upd, status, rollback, restart, rollbackChange, helper}
}
