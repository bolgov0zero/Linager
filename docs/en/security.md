# Linager security

## Threat model
The Linager server manages servers with root privileges, so compromising the control panel is equivalent to compromising all clients. The protection is designed on the assumption that the admin panel may run on a local network without HTTPS, and that the agent channel may travel over an untrusted network.

## What is protected and how
- **Agent channel:** TLS 1.3. The client-port certificate is self-signed, and the agent pins its fingerprint. After enrollment, mutual TLS (mTLS) is used with a certificate issued by the server's internal CA (valid for one year; the agent renews it over the channel by itself). Every message is signed with Ed25519 (the agent key and a separate server command-signing key), and there is a sequence counter and a time window against replays.
- **Agent enrollment:** the agent verifies the server fingerprint (the `--fingerprint` flag or explicit confirmation), both ends show a **verification code**, and the administrator enters the **enrollment key** from the agent's console. Five wrong keys reject the request; requests are rate-limited and expire after 15 minutes.
- **Administrator login:** argon2id passwords, brute-force protection (per IP and per login, with growing delays), one-time TOTP codes cannot be reused, sessions are stored in the database (only a hash of the token is kept), `HttpOnly` cookie, `SameSite=Strict`, `Secure` over HTTPS, CSRF token on every state-changing request, `Origin` check for WebSocket.
- **2FA:** TOTP is enabled in the admin panel ("2FA"). The `admin.require_totp: true` parameter in `server.yaml` blocks the entire interface until the administrator enables 2FA. Sign-in has two steps: login and password first, and only if the user has 2FA enabled does the page ask for the code. A right password without a code creates no session and is not counted as a failed attempt; a wrong code is counted.
- **Recovery codes:** enabling 2FA issues 10 one-time codes (only hashes are stored, shown once). A recovery code is entered instead of the code from the app and is burned; new ones are generated in Settings → Profile (the password is required) and the old ones stop working.
- **Passkeys (WebAuthn):** a separate way to sign in with the "Passkey" button on the sign-in page, no login and no password. The key asks for user verification (PIN or biometrics), so no separate 2FA is needed. It works only over HTTPS (or `http://localhost`) with a domain name; the relying party id is the host the panel is opened with, so if the domain changes the keys must be added again. Every call is rate-limited, the challenge is single-use and lives 5 minutes, and the request origin is checked.
- **Users:** you can add users, change your own password (the other sessions end), reset the password and 2FA of others. You cannot delete yourself. All of this is written to the audit log.
- **Sign-in rate:** `admin.login_rate` in `server.yaml` (attempts per minute per address, default 10).
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
- All users have one role, "administrator", with full access (including the terminal and files as root); roles such as "observer" do not exist yet, the data schema is ready for them.
- The terminal and file manager run as the agent's root user.
- Network editing on Ubuntu (netplan) is disabled: view only.
- The "Let's Encrypt / own certificate" TLS modes for the admin panel without Caddy are not implemented; use Caddy or your own reverse proxy.

## Reporting a vulnerability
See [SECURITY.md](../../SECURITY.md).
