# Agent modules and configuration

Agent configuration: `/etc/linager/agent.yaml` (created by the installer, permissions 600). Each module can be disabled; after a change run `sudo systemctl restart linager-agent`. The admin panel shows tabs only for enabled modules.

```yaml
modules:
  metrics: true      # metrics (always needed by the dashboard)
  terminal: true     # web terminal (root shell)
  services: true     # systemd services
  apt: true          # apt updates
  processes: true    # processes
  logs: true         # journald log
  files: true        # file manager
  network: true      # network (ifupdown; netplan is view-only)
  firewall: true     # nftables (inet filter table)
  docker: true       # Docker (enabled only if the socket exists)
```

Reboot (`reboot`) is always available and requires confirmation.

## Module notes
- **apt.** Installing updates is not tied to the browser tab: if you close the tab or the server connection drops, `apt-get` runs to the end.
  When the tab is opened again, the installation log is shown from the beginning. If the package database is locked by another process (for example `unattended-upgrades`), the agent waits up to two minutes.
- **Network and firewall.** An unconfirmed change is rolled back by the agent itself after 30 seconds. In case the agent process has crashed or hangs there is a second line of defence:
  a one-shot systemd timer (`linager-agent-change-rollback`) runs the rollback in a separate process after 35 seconds.
- **Firewall.** The "rules close SSH" warning uses the ports sshd really listens on (`sshd -T`), not only port 22.
- **The agent certificate** is issued for one year and renews itself: when less than a third of the lifetime is left, the agent asks for a new one over the protected channel.
  If a host was switched off for longer than the certificate lifetime, enroll it again (`linager install`).
  The lifetime is set on the server: `agents.cert_lifetime` in `server.yaml` (for example `2160h`, default `8760h`).

## Agent files and directories
| Path | Purpose |
|---|---|
| `/usr/local/bin/linager` | binary (server and agent) |
| `/etc/linager/agent.yaml` | enabled modules |
| `/var/lib/linager-agent/` | key, certificate, state, rollback journal (`pending.json`), `linager.prev` |
| `/etc/systemd/system/linager-agent.service` | agent service |

## Client commands
```bash
linager agent status          # version, server, state
sudo linager agent update     # update from the server
sudo linager agent uninstall --purge   # remove from the host (keys and config)
```

## Removing a client from the admin panel
Server card, "Delete server": enter the server name and select "Also remove the agent from the server itself" (if the server is online, the agent deletes its keys and service and stops). The record and its history are deleted, and the agent certificate stops working immediately.
