// Command linager is the single Linager binary: server, agent, installer and updater.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bolgov0zero/linager/internal/version"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "linager",
		Short:         "Linager: server management for Debian and Ubuntu",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newVersionCmd(),
		stub("server", "Run the Linager server"),
		stub("agent", "Run the Linager agent"),
		stub("install", "Interactive installer (server or agent)"),
		stub("update", "Update Linager"),
		stub("backup", "Back up the database and configuration"),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "linager "+version.String())
		},
	}
}

// stub registers a command that is not implemented yet.
func stub(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("%q is not implemented yet", use)
		},
	}
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
