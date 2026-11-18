//go:build windows

package main

import "github.com/spf13/cobra"

const rootShort = "Linager agent for Windows"

// rootRun is `linager` without arguments: the help.
func rootRun(c *cobra.Command, _ string) error { return c.Help() }

// platformCommands: the Windows build is the agent only, the server runs on Linux.
func platformCommands() []*cobra.Command { return nil }
