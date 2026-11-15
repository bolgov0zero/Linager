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
  security: true     # security: SSH logins, password guessing, fail2ban, keys (read-only)
  docker: true       # Docker (enabled only if the socket exists)
```

Reboot (`reboot`) is always available and requires confirmation.

## Module notes
- **apt.** Installing updates is not tied to the browser tab: if you close the tab or the server connection drops, `apt-get` runs to the end. When you come back to the tab, the browser attaches to the running operation and receives all accumulated events and the log. Package statuses come from `APT::Status-Fd` (the `pmstatus`/`pmerror`/`dlstatus` channel), not from the output text: queued → updating → done or error; packages apt never reached after a failure are marked "not updated".
  When the tab is opened again, the installation log is shown from the beginning. If the package database is locked by another process (for example `unattended-upgrades`), the agent waits up to two minutes.
- **security.** The Security tab, read-only. The SSH state from `sshd -T` (from `sshd_config` if that fails): port, root login, password and key login, empty passwords, `AllowUsers`/`AllowGroups`; fail2ban (jails, banned addresses); automatic security updates. For 7 days: successful logins and failed attempts from the systemd journal (`journalctl -t sshd -t sshd-session`; `sshd-session` is needed for OpenSSH 9.8+, which Debian 13 ships), from `/var/log/auth.log` when there is no journal. Failed attempts are counted by address and login, and a login from an address that guessed passwords earlier is flagged. The keys of `authorized_keys` of every user with a shell (the path comes from `AuthorizedKeysFile`): type, RSA size, SHA256 fingerprint, comment, file change time; only fingerprints are sent, the keys never leave the host. Remote sessions from `loginctl`. The answer is cached for a minute and at most 2 million journal lines are read. Only SSH logins are visible (not console or `su`); if the host does not keep the journal across reboots, the history is shorter than 7 days and the tab says so.
- **docker.** "Update image" in the container menu: the image is pulled again and the container is recreated with the same settings (network, volumes, variables, ports). The old container is stopped and renamed, the new one is started and checked; if it does not start or exits at once, the old one is brought back. A docker compose container is updated with `docker compose pull` and `up -d --no-deps <service>` from the project labels (the `docker` command and the compose plugin are needed on the host). The previous image stays on the host.
- **Terminal.** Snippets (commands shared by all users) are managed in Settings → Snippets; in the terminal the Snippets list pastes a command and runs it, while the button at the right of the row only pastes it.
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
