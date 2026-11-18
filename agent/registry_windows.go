//go:build windows

package agent

import (
	"context"
	"log/slog"

	"github.com/bolgov0zero/linager/internal/config"
)

// registerPlatform registers the modules of a Windows host: terminal, services, processes, event log and files.
// The other modules of the Linux agent (apt, firewall, network, users, docker, disk, security) have no Windows
// counterpart yet, and updates of the agent itself go through the MSI package.
func (r *Registry) registerPlatform(cfg *config.Agent, _ Collector, _ *slog.Logger, _ string) {
	if cfg.Enabled("terminal") {
		r.streams["terminal"] = termStream
	}
	if cfg.Enabled("services") {
		r.cmds["services.list"] = svcList
		r.cmds["services.action"] = svcAction
	}
	if cfg.Enabled("processes") {
		r.cmds["processes.list"] = procList
		r.cmds["processes.kill"] = procKill
	}
	if cfg.Enabled("logs") {
		r.streams["logs"] = logsStream
	}
	if cfg.Enabled("files") {
		r.cmds["files.list"] = filesList
		r.cmds["files.read"] = filesRead
		r.cmds["files.write"] = filesWrite
		r.cmds["files.mkdir"] = filesMkdir
		r.cmds["files.create"] = filesCreate
		r.cmds["files.delete"] = filesDelete
		r.cmds["files.rename"] = filesRename
		r.cmds["files.stat"] = filesStat
		r.cmds["files.du"] = filesDu
		r.cmds["files.copy"] = transfer(false)
		r.cmds["files.move"] = transfer(true)
		r.streams["files.zip"] = filesZip
		r.streams["files.download"] = filesDownload
		r.streams["files.upload"] = filesUpload
	}
	r.cmds["system.reboot"] = sysReboot // always available: rebooting is not a toggleable module
}

// startBackground starts the work that runs next to the connection: nothing on Windows.
func (r *Registry) startBackground(context.Context) {}
