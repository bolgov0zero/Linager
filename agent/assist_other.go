//go:build !linux && !windows

package agent

import "github.com/bolgov0zero/linager/internal/config"

// registerAssistant: the Assistant exists on Windows and on Linux only.
func registerAssistant(*Registry, *config.Agent) {}
