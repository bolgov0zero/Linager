# Updates, backups, and rollback

## How releases are built
Each GitHub release contains the binary `linager_linux_amd64`, a `.deb` package, `SHA256SUMS`, and a signed `manifest.json` (version, channel, checksum, number of DB migrations, minimum agent version, changelog). The minisign signature (Ed25519) is verified with the public key **embedded in the binary**. A binary that does not match the signed manifest is not installed, neither on the server nor on an agent.

## Updating the server
From the admin panel: the "Updates" page (the header button with a counter), then "Update to X". From the terminal:

```bash
sudo linager update --check      # what is available
sudo linager update              # update (asks for confirmation; --yes to skip the prompt)
```

Order (visible step by step in the admin panel): signature check → backup (`pg_dump` plus configuration and keys) → service stop → binary replacement (the previous one is kept in `/var/lib/linager/prev/`) → database migrations (forward only) → start and check (the admin panel returns 200, the client port accepts connections, up to 60 s). **If the check fails, the previous binary is restored automatically.** Migrations cannot be rolled back, so a database copy is made before every update: `/var/backups/linager/linager-pre-update-*.tar.gz`.

When started from the admin panel, the update is run by the `linager-updater` service (a systemd path unit watches the request file and runs `linager update --apply-request` as root). The server service itself runs without root privileges and cannot replace the binary. The installer creates these units.

## Updating agents
Agents take the release **from the server** (client port, mTLS), so the server must be updated first. On the "Updates" page you can update one agent, selected agents, or all agents. Modes: all at once, one by one, or first one and then the rest; option "stop on error". An offline agent is queued and updated when it connects. An agent older than the minimum supported version is marked "not supported".

The agent verifies the signature and checksum, keeps the previous binary (`/var/lib/linager-agent/linager.prev`), replaces `/usr/local/bin/linager`, starts a **rollback timer** (`systemd-run`, runs the previous binary), and restarts. The new version confirms itself with its first successful connection to the server and cancels the timer. **If there is no connection, the previous binary is restored automatically after about 70 s.** Manually on the client host: `sudo linager agent update`, `linager agent status`.

## Channels and release source
The `stable` channel (tags `vX.Y.Z`) or `beta` channel (`vX.Y.Z-beta.N`) is selected in the admin panel. In `/etc/linager/server.yaml`:

```yaml
update:
  source: ""            # empty = GitHub bolgov0zero/linager; "owner/repo"; or a mirror URL serving releases.json
  channel: stable
```

Mirror (for closed networks): a directory with the release files and a `releases.json` in the GitHub API format (`tag_name`, `prerelease`, `body`, `assets[].name/browser_download_url`), served over HTTP(S).

## Backups
```bash
sudo linager backup --keep 7     # /var/backups/linager/linager-backup-<date>.tar.gz (permissions 600)
```
The archive contains: `db.dump` (pg_dump, custom format), `config/server.yaml`, and `data/` (CA keys, the command-signing certificate and key). **The archive contains secrets; store it as carefully as the server itself.** The installer enables a daily backup at 02:00 (`linager-backup.timer`; the 7 most recent are kept).

Manual database restore:
```bash
sudo systemctl stop linager-server
tar -xzf linager-backup-XXXX.tar.gz db.dump
sudo -u postgres pg_restore --clean --if-exists --no-owner -d linager db.dump
sudo systemctl start linager-server
```

## Signing key (for the repository owner)
The key is generated once: `minisign -G -W -p minisign.pub -s minisign.key`. The contents of `minisign.key` go into the repository secret `MINISIGN_KEY` (plus `MINISIGN_PASSWORD` if it is password-protected). The line from `minisign.pub` is written into `internal/release/release.go` (`PublicKey`) and `install.sh` (`PUBKEY`). A release without the secret is published as a pre-release without a signature; the installer and the updater reject such releases.
