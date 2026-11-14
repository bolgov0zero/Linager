# Changelog

## 0.1.11

- Phones: the admin panel is shown at 90% scale.
- Home screen icon (pixel console `>_`) and a web app manifest: a page added to the phone's home screen gets the icon and opens without the browser address bar.

## 0.1.10

- Phones: the admin panel no longer zooms (pinch, double tap and focusing an input field).

## 0.1.9

- The top bar stays pinned to the top of the window while the page scrolls (on screens wider than 720 px).

## 0.1.8

- Dashboard rows view: the "Terminal" and "Open" buttons stand one above the other to the right of the metrics.

## 0.1.7

- Files tab: the tree and the list fill the window height and scroll inside (the table header stays visible) instead of
  making the page grow.

## 0.1.6

- The "Linager updates" page no longer freezes after "Check" finds a release without notes: the server always sends a
  list (never null) and the page tolerates missing fields.
- Release notes on GitHub are taken from this file, so the updates page shows the real list of changes; wrapped lines
  of one item are joined.

## 0.1.5

- Metrics charts are redrawn: the time axis covers the whole selected period, averages are lines with a light fill and
  peaks a dashed line, gaps break the line, round y-axis steps, events as markers under the axis, legend and a hover
  tooltip with values and events. Network receive and send use two colour-blind-safe colours.
- Services table: rows keep one height and the action buttons stay on one line.
- Server page: the Processes tab follows Services; the page is centred.

## 0.1.4

- The "Linager updates" page no longer hangs on "Loading…" while a release check runs: the check talks to GitHub
  without holding the updates lock, and repeated clicks on "Check" share one request.

## 0.1.3

- Files tab works like a file explorer: a folder tree on the left (lazy loading, resizable), the folder contents on the right;
  click selects, double click or Enter opens a folder or the file editor; binary and large files offer a download.
- The server name follows the hostname: the agent reports it on every connection (also after a reboot) and right after it
  changes; a rename is recorded as a server event. Names that were not derived from the hostname stay as they are.
- All drop-down lists use a project-styled control instead of the native one.

## 0.1.2

- Dashboard: the "Terminal" button opens a floating terminal window over the dashboard. Windows can be moved, resized,
  minimized to a bar at the bottom, maximized and closed; several can be open at once (up to 3 per server).
- When the per-server terminal limit is reached, the interface shows a notice (the server now reports it as WebSocket
  close code 4029).

## 0.1.1

Fixes after the 0.1 review.

- New server setting `agents.cert_lifetime` (default one year).

- Agent certificates renew themselves over the channel before they expire (previously every agent would have dropped off after one year).
- Network and firewall changes get a second, out-of-process rollback timer for the case when the agent itself crashes or hangs.
- apt operations keep running when the browser tab closes or the connection drops; reopening the tab shows the log from the start.
- The SSH lock-out warning uses the ports sshd really listens on, not only port 22.
- The agent reconnects quickly again after a long healthy session (the pause used to stay at its maximum).
- `curl … | sudo sh` now starts the interactive wizard.
- Server update: the database is restored automatically when an update fails after its migrations; the root updater no longer trusts
  files the server user can change; downgrades are refused; new `linager restore` command.
- Agent rollouts survive a server restart; a failed first agent no longer stalls a "first, then the rest" rollout.
- Interface: dotted shape of large headings as in the mockup, working "Terminal" button on dashboard cards, agent update tags and banner,
  tabs of disabled modules are hidden.

## 0.1.0

First release.

- Server and agent in one Go binary; PostgreSQL; embedded Svelte web interface (ru/en, light/dark).
- Agent pairing with fingerprint pinning, check code and enrollment key; mTLS and signed messages afterwards.
- Dashboard (cards and rows, drag and drop order), live metrics (5 s) and history up to 360 days with roll-ups, events and CSV export.
- Web terminal, systemd services, apt updates, processes, journal, files, Docker.
- Network (ifupdown/ifupdown2) and nftables firewall with drafts and a 30-second automatic rollback enforced by the agent. Netplan: view only.
- Installer (interactive TUI and non-interactive): PostgreSQL, Caddy (Let's Encrypt) or own reverse proxy, systemd units.
- Signed self-update of server and agents with backup, health check and automatic rollback; rollout modes and offline queue.
- Backups (database, configuration, keys), audit log, optional mandatory 2FA, security hardening.
