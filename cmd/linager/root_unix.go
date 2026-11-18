//go:build !windows

package main

import (
	"github.com/spf13/cobra"

	"github.com/bolgov0zero/linager/console"
	"github.com/bolgov0zero/linager/installer"
	"github.com/bolgov0zero/linager/internal/version"
)

const rootShort = "Linager: server management for Debian and Ubuntu"

// rootRun is `linager` without arguments: in a terminal the interactive menu, in a script or a pipe the usual help.
func rootRun(c *cobra.Command, lang string) error {
	if !console.Interactive() {
		return c.Help()
	}
	return console.Run(c.Context(), console.Options{Lang: installer.ParseLang(lang), Version: version.Version})
}

// platformCommands are the commands of the Linux build: the server, the installer and the updater.
func platformCommands() []*cobra.Command {
	return []*cobra.Command{
		newServerCmd(),
		newInstallCmd(),
		newUpdateCmd(),
		newBackupCmd(),
		newRestoreCmd(),
		newAccessCmd(),
	}
}
