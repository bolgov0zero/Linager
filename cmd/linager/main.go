// Command linager is the single Linager binary: server, agent, installer and updater.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bolgov0zero/linager/internal/version"
)

func newRootCmd() *cobra.Command {
	var lang string
	root := &cobra.Command{
		Use:           "linager",
		Short:         rootShort,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Without arguments in a terminal: the interactive menu. In a script or a pipe: the usual help.
		RunE: func(c *cobra.Command, _ []string) error { return rootRun(c, lang) },
	}
	root.Flags().StringVar(&lang, "lang", "", "menu language: ru or en (default: from LANG)")
	root.AddCommand(newVersionCmd(), newAgentCmd())
	root.AddCommand(platformCommands()...)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "linager "+version.String())
			return err
		},
	}
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		var ee *exitError
		if errors.As(err, &ee) {
			os.Exit(ee.code)
		}
		os.Exit(1)
	}
}
