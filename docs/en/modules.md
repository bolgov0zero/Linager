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
