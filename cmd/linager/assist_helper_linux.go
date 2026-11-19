//go:build linux

package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bolgov0zero/linager/agent"
)

// assistHelperCmds is `linager agent assistant-helper`: the process the agent service starts as the person of a graphical
// session to serve its screen to the Assistant.
func assistHelperCmds() []*cobra.Command {
	var addr, mode string
	var session int
	helper := &cobra.Command{
		Use:    "assistant-helper",
		Short:  "Serve the screen of a graphical session to the agent (started by the agent service as the person of the session)",
		Hidden: true,
		Run: func(c *cobra.Command, _ []string) {
			ctx, stop := signal.NotifyContext(c.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			code, err := agent.RunAssistHelper(ctx, addr, session, mode)
			if err != nil {
				_, _ = fmt.Fprintln(c.ErrOrStderr(), err)
			}
			if code != 0 {
				stop()
				os.Exit(code)
			}
		},
	}
	helper.Flags().StringVar(&addr, "addr", "", "address of the agent on the loopback")
	helper.Flags().IntVar(&session, "session", 0, "number of the session")
	helper.Flags().StringVar(&mode, "mode", "", `"kms": read the screen below the compositor (the sign-in screen, X11, a locked screen)`)
	return []*cobra.Command{helper}
}
