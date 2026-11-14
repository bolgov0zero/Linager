# Linager security

## Threat model
The Linager server manages servers with root privileges, so compromising the control panel is equivalent to compromising all clients. The protection is designed on the assumption that the admin panel may run on a local network without HTTPS, and that the agent channel may travel over an untrusted network.

## What is protected and how
- **Agent channel:** TLS 1.3. The client-port certificate is self-signed, and the agent pins its fingerprint. After enrollment, mutual TLS (mTLS) is used with a certificate issued by the server's internal CA. Every message is signed with Ed25519 (the agent key and a separate server command-signing key), and there is a sequence counter and a time window against replays.
- **Agent enrollment:** the agent verifies the server fingerprint (the `--fingerprint` flag or explicit confirmation), both ends show a **verification code**, and the administrator enters the **enrollment key** from the agent's console. Five wrong keys reject the request; requests are rate-limited and expire after 15 minutes.
- **Administrator login:** argon2id passwords, brute-force protection (per IP and per login, with growing delays), one-time TOTP codes cannot be reused, sessions are stored in the database (only a hash of the token is kept), `HttpOnly` cookie, `SameSite=Strict`, `Secure` over HTTPS, CSRF token on every state-changing request, `Origin` check for WebSocket.
- **2FA:** TOTP is enabled in the admin panel ("2FA"). The `admin.require_totp: true` parameter in `server.yaml` blocks the entire interface until the administrator enables 2FA.
- **Headers:** a strict Content Security Policy (scripts only from the same origin; `'unsafe-inline'` styles are required by the xterm.js terminal), `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, `Permissions-Policy`, COOP and CORP, HSTS over HTTPS.
- **Dangerous actions:** stopping `ssh` and `linager-agent`, rebooting, deleting files, killing processes, firewall and network rules, and updates all require explicit confirmation. The server sets the confirmation flag only after the administrator responds; an agent rejects such commands without the flag.
- **Rollback:** network and firewall changes are applied with a 30-second timer on the agent. Without confirmation (including when the connection is lost), the settings roll back automatically; the change journal survives an agent restart.
- **Audit log:** all administrator actions (login, enrollment, commands, edits, updates, opening and closing a terminal with the data volume) are recorded with the connection address and the address from `X-Forwarded-For`. Keystrokes in the terminal are not recorded.
- **Updates:** releases are signed (minisign, Ed25519), and the public key is embedded in the binary. The manifest, checksum, and signature are verified on the server and on the agent before replacement; a database copy is made before an update; the binary is restored if the update fails.
- **Permissions:** secrets (`server.yaml`, keys, backups) have permissions 600. The server service runs as the `linager` user with systemd restrictions (`ProtectSystem=strict`, `NoNewPrivileges`, address family filters, an empty capability set except for binding ports). Privileged operations (updates) are performed by a separate service.
- **Database:** only a local PostgreSQL, `scram-sha-256`, and a random role password. The installer refuses to continue if PostgreSQL listens on anything other than localhost or allows trust/md5 over TCP.

## What the administrator should do
1. **Enable 2FA** and, if possible, set `admin.require_totp: true`.
2. If the admin panel is reachable over plain HTTP on a closed network, **restrict the admin panel port with a firewall**. By default the server trusts `X-Forwarded-*` headers from any address (`admin.trusted_proxies: 0.0.0.0/0`), so the client address in the log can be spoofed when the port is accessed directly. For public installations, narrow `admin.trusted_proxies` to the address of your proxy and `admin.listen` to its network.
3. Open the client port (`agents.listen`) only for the networks that need it.
4. Store backups as secrets: they contain the CA keys.
5. Disable unneeded agent modules in `/etc/linager/agent.yaml` (the terminal and file manager give root access to the entire host).

## Known limitations of version 0.1
- A single administrator; roles are defined in the data schema but not implemented.
- The terminal and file manager run as the agent's root user.
- Network editing on Ubuntu (netplan) is disabled: view only.
- The "Let's Encrypt / own certificate" TLS modes for the admin panel without Caddy are not implemented; use Caddy or your own reverse proxy.

## Reporting a vulnerability
See [SECURITY.md](../../SECURITY.md).
