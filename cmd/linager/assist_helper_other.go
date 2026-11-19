//go:build !linux && !windows

package main

import "github.com/spf13/cobra"

// assistHelperCmds: the helper of the Assistant exists on Linux and Windows only.
func assistHelperCmds() []*cobra.Command { return nil }
