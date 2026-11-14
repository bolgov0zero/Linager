# Installing Linager

Supported: Debian 12/13 and Ubuntu 22.04/24.04/26.04 (amd64, systemd).

## Quick install

```bash
curl -fsSL https://raw.githubusercontent.com/bolgov0zero/linager/main/install.sh | sudo sh
```

## Safe option: download, verify, run

```bash
curl -fsSLO https://raw.githubusercontent.com/bolgov0zero/linager/main/install.sh
less install.sh                      # read the script
sudo sh install.sh
```

The script clears the screen, checks the system, downloads the release, verifies the checksum and the minisign signature (the key is embedded in the script), and starts the `linager install` wizard.

## Wizard

The first question: what to install, **Server** or **Client**.

### Server
Questions (the ports are required, there are no default values):

| Question | Description |
|---|---|
| Port for clients | clients connect here directly over TLS |
| Admin panel access | `Caddy on this server` or `Own reverse proxy` |
| Domain, HTTPS port | Caddy only: Let's Encrypt, requires open ports 80 and 443 and a DNS record |
| Admin panel port (HTTP) | own proxy only: your proxy connects here |
| Login and password | a password of at least 12 characters; leave empty to generate one and show it once |

Stages: system check, PostgreSQL (from the OS repository, localhost only, `scram-sha-256`), database and role with a random password, configuration `/etc/linager/server.yaml` (permissions 600, owner `linager`) and keys, administrator account, Caddy (if selected), `linager-server` service, startup check. The installation log is written to `/var/log/linager-install.log`.

In "Own reverse proxy" mode the wizard installs and configures nothing; it only asks for the ports. The admin panel listens on `0.0.0.0:<port>`, and `X-Forwarded-*` headers are accepted from any address (the list can be narrowed in `server.yaml`: `admin.trusted_proxies`). The admin panel port is exposed directly, so restrict it with a firewall. Proxy examples: [proxy.md](proxy.md).

At the end, the wizard shows the admin panel address, the fingerprint of the client-port certificate, and a ready-to-use client installation command.

### Client
Questions: the server address `host:client-port` and confirmation of the fingerprint (or the `--fingerprint` flag). Then the wizard shows the **key** `XXXX-XXXX` and the **verification code**. Enter the key in the admin panel ("+ Server", the "Awaiting approval" block) and make sure the codes match. After confirmation, the `linager-agent` service starts.

## Unattended (automation)

```bash
sudo linager install server --no-tui --agent-port 9443 --admin-mode proxy --admin-port 8080 --admin-user admin
sudo linager install server --no-tui --agent-port 9443 --admin-mode caddy --domain linager.example.com
sudo linager install agent  --no-tui --server 10.0.0.5:9443 --fingerprint SHA256:...
```

Without a terminal, or with `--no-tui`, no questions are asked: all required parameters are taken from flags. `--lang ru|en` selects the language, `--force` allows a repeated installation, and `--admin-password-file` sets the password from a file.

## Removing a client
In the admin panel: open the server card and click "Delete server". You can remove the agent from the host at the same time. Manually on the host: `sudo linager agent uninstall --purge`.
