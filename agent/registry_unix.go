//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"

	"github.com/bolgov0zero/linager/internal/config"
	"github.com/bolgov0zero/linager/internal/version"
)

// registerPlatform registers the modules of a Linux host (apt, systemd, nftables, Docker and so on).
func (r *Registry) registerPlatform(cfg *config.Agent, col Collector, log *slog.Logger, dataDir string) {
	r.cmds["update.apply"] = r.updateApplyCmd(version.Version)
	if cfg.Enabled("terminal") {
		r.streams["terminal"] = termStream
	}
	if cfg.Enabled("services") {
		r.cmds["services.list"] = svcList
		r.cmds["services.action"] = svcAction
	}
	if cfg.Enabled("apt") {
		r.cmds["apt.list"] = aptList
		r.streams["apt"] = func(ctx context.Context, p json.RawMessage, io *StreamIO) error {
			return aptStream(ctx, p, io, func() { refreshUpdates(col) })
		}
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
		r.cmds["files.chmod"] = filesChmod
		r.cmds["files.chown"] = filesChown
		r.cmds["files.copy"] = transfer(false)
		r.cmds["files.move"] = transfer(true)
		r.streams["files.zip"] = filesZip
		r.streams["files.download"] = filesDownload
		r.streams["files.upload"] = filesUpload
	}
	pend := NewPending(dataDir, log)
	pend.EnableWatchdog()
	if cfg.Enabled("network") {
		nm := newNetModule(pend, dataDir)
		pend.Register("network", nm.kind())
		r.cmds["network.state"] = nm.state
		r.cmds["network.apply"] = nm.apply
		r.cmds["network.dns"] = nm.dns
	}
	if cfg.Enabled("firewall") {
		fm := newFwModule(pend, dataDir)
		pend.Register("firewall", fm.kind())
		r.cmds["firewall.state"] = fm.state
		r.cmds["firewall.ports"] = fm.ports
		r.cmds["firewall.log"] = fm.logCmd
		r.cmds["firewall.validate"] = fm.validate
		r.cmds["firewall.apply"] = fm.apply
	}
	if cfg.Enabled("network") || cfg.Enabled("firewall") {
		r.cmds["changes.confirm"] = func(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
			var p struct{ ID string }
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, errors.New("invalid request")
			}
			return map[string]bool{"ok": true}, pend.Confirm(p.ID)
		}
		r.cmds["changes.rollback"] = func(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
			var p struct{ ID string }
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, errors.New("invalid request")
			}
			return map[string]bool{"ok": true}, pend.Rollback(p.ID)
		}
		r.cmds["changes.status"] = func(context.Context, json.RawMessage, bool) (any, error) {
			if c := pend.Current(); c != nil {
				return map[string]any{"id": c.ID, "kind": c.Kind, "deadline": c.Deadline}, nil
			}
			return map[string]any{}, nil
		}
		pend.Recover() // an unconfirmed change from a previous run is rolled back right away
	}
	if cfg.Enabled("security") {
		sec := newSecModule()
		r.cmds["security.state"] = sec.state
		r.cmds["security.terminate"] = sec.terminate
		r.cmds["security.ban"] = sec.f2bBanIP
		r.cmds["security.unban"] = sec.f2bUnban
		r.cmds["security.banHistory"] = sec.f2bBanHistory
		r.cmds["security.f2bSettings"] = sec.f2bSaveSettings
		r.cmds["security.keyRemove"] = sec.keyRemove
		r.streams["security.f2bInstall"] = sec.f2bInstall
	}
	if cfg.Enabled("docker") && dockerAvailable() {
		r.cmds["docker.state"] = dockerState
		r.cmds["docker.container"] = dockerContainerAction
		r.cmds["docker.imageRemove"] = dockerImageRemove
		r.cmds["docker.check"] = dockerCheck
		r.streams["docker.composeLogs"] = dockerComposeLogs
		r.streams["docker.logs"] = dockerLogs
		r.streams["docker.pull"] = dockerPull
		r.streams["docker.update"] = dockerUpdate
	} else {
		r.modules = without(r.modules, "docker")
	}
	if cfg.Enabled("disk") {
		dm := newDiskModule(dataDir)
		dm.start()
		r.cmds["disk.state"] = dm.stateCmd
		r.cmds["disk.scan"] = dm.scanCmd
		r.cmds["disk.clean"] = dm.cleanCmd
	}
	if cfg.Enabled("users") {
		um := newUsersModule()
		r.cmds["users.state"] = um.state
		r.cmds["users.add"] = um.add
		r.cmds["users.update"] = um.update
		r.cmds["users.delete"] = um.del
		r.cmds["users.passwd"] = um.passwd
		r.cmds["users.kill"] = um.kill
	}
	r.cmds["system.reboot"] = sysReboot // always available: rebooting is not a toggleable module
}

// startBackground starts the work that runs next to the connection.
func (r *Registry) startBackground(ctx context.Context) {
	if slices.Contains(r.Modules(), "docker") {
		startImageChecks(ctx) // the dashboard shows the containers with a newer image
	}
}
