//go:build windows

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bolgov0zero/linager/agent"
	"github.com/bolgov0zero/linager/internal/version"
)

// newAgentUpdateCmds: the Windows agent is updated by installing the new MSI, so only the status is here.
func newAgentUpdateCmds(dataDir *string) []*cobra.Command {
	return []*cobra.Command{{
		Use:   "status",
		Short: "Show the agent version and its connection state",
		RunE: func(c *cobra.Command, _ []string) error {
			_, _ = fmt.Fprint(c.OutOrStdout(), agent.StatusText(*dataDir, version.Version))
			return nil
		},
	}}
}
