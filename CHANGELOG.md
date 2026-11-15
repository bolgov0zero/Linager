# Changelog

## 0.1.31

- Terminal key panel redone: no scrolling, a thin strip of five modes (Main, Ctrl+, Nav, F1-12, Symbols) and the arrows on the right like on a real keyboard (up above down), with Esc and Tab beside them. Ctrl and Alt still latch until the next key.

## 0.1.30

- File manager: "Properties" in the context menu (also for the current folder): path, type, size (folder content size on demand), creation, modification, change and access times, links and inode, link target; permissions (rwx checkboxes, setuid, setgid, sticky and the octal number) and owner and group can be changed, with a confirmation for recursion, special bits and owner changes. Needs agent 0.1.30.
- Terminal: a key panel for phones behind the keyboard button (Ctrl and Alt latch, Esc, Tab, arrows, Home, End, PgUp, PgDn, Ins, Del, F1-F12, symbols and common Ctrl shortcuts).

## 0.1.29

- Metrics: the "Events for the period" list scrolls inside its block (up to 320 px) instead of stretching it.

## 0.1.28

- Docker tab: the container and image actions moved into a drop-down "Actions" menu in each row.

## 0.1.27

- Phones: the agents, cache and history tables on the Updates page are shown as compact blocks, so there is no horizontal scrolling.

## 0.1.26

- Phones: the four metric cards on the server overview are smaller and sit two by two.
- Phones: the lists on the Services, Processes, Updates and Docker tabs are shown as compact blocks instead of wide tables, so there is no horizontal scrolling in them.

## 0.1.25

- Dashboard map: bigger tiles with the address, three metrics with mini bars (CPU, RAM, disk), network with coloured arrows, uptime and an event badge (also "no issues"); six tiles per row on wide screens; long press opens the menu on touch screens.
- Dashboard map menu: "Delete server" removes the server from the admin panel (with the usual confirmation); choosing the group is available only in edit mode.
- Dashboard cards: the buttons are pixel icons (terminal, open) plus a trash button that deletes the server from the admin panel (with confirmation).
- Updates: "Delete old" removes cached releases except the newest one and the version the server runs (refused while an update or rollout is running).

## 0.1.24

- Dashboard: only the server list scrolls, the summary and the filters stay in place (screens wider than 720 px).
- Dashboard: the "Rows" view is replaced by the "Map": small tiles with a status-coloured edge, name and CPU/RAM. Left click opens the server; right click (long press on touch screens) opens a menu: Terminal, Open, Group, Remove from the dashboard.

## 0.1.23

- File manager: the number of selected items is shown to the right of the path instead of a separate line above the list.

## 0.1.22

- File manager: right-click context menu (open, download, cut, copy, paste, rename, delete; on the empty area: paste, new folder, new file, refresh). The row buttons are gone; touch screens get a "⋯" button in the row.
- Multiple selection with Ctrl/⌘+click and Shift+click (also Shift+arrows, Ctrl/⌘+A), and the keys Ctrl/⌘+C, X, V, Delete, F2. Several files and folders download as one zip.
- Paste asks "Overwrite" or "Keep both" when a name already exists, with a "For all" checkbox when there are several conflicts.
- Agents must be updated to 0.1.22 for rename, copy, move and zip.

## 0.1.21

- Files: uploading a file no longer fails with "target is a directory" (the panel sent the folder instead of the full target path). Only the panel changed, agents need no update.

## 0.1.20

- Network tab on netplan systems (Ubuntu): the configuration can be edited by hand. Pick a file from `/etc/netplan` (or create a new one); it is checked with `netplan generate` and applied with `netplan apply` under the same 30-second automatic rollback. cloud-init generated files get a warning, and "Effective configuration" shows the merged result (`netplan get`).
- Services: the actions moved into a menu. New "Ignore" item: an ignored failed service no longer counts towards the server status and the dashboard counters.
- Terminal tab: the "In a window" button moves the terminal into a floating window.
- File editor: opens in floating windows like the terminals (move, resize, minimize, several at once) and uses CodeMirror 6 with syntax highlighting (PHP, HTML, CSS, JavaScript, JSON, YAML, XML, SQL, Markdown, Python, shell, nginx, Dockerfile, TOML, ini and more), line numbers and search.
- Agents must be updated to 0.1.20 for netplan editing and for ignoring services. Database migration 00006 adds the ignore list.

## 0.1.19

- Network tab: the configuration block can be edited by hand (ifupdown systems): the whole `/etc/network/interfaces` is edited, checked with `ifup --no-act`, applied with the same 30-second rollback as form changes, and written to the audit log. Netplan systems stay read-only. Agents must be updated to this version to use it.

## 0.1.18

- Server groups: create groups ("Groups…" next to the dashboard filters), put a server into a group in edit mode, and pick a group in the selector to see only its servers (the summary follows the selected group). Reordering works inside the selected group. Group changes are written to the audit log. Database migration 00005 adds the groups table.

## 0.1.17

- Server overview: the interfaces table takes the full width with aligned columns; the system block moved below it and shows its rows in several columns.

## 0.1.16

- The pairing ("Adding a server") dialog has a Back button that returns to the "Add server" dialog.

## 0.1.15

- Docker tab: the container buttons stand in one line (start and stop are one button that follows the state), so rows are much lower; on screens up to 1000 px the buttons wrap into two lines and ports and RAM are hidden.

## 0.1.14

- "Add server" dialog: client connection details (address, port, certificate fingerprint) with a button that copies `address:port`. The address is the one the panel was opened with, so it is the domain when the panel is behind a reverse proxy.
- Docs: Nginx Proxy Manager settings; the live-updates (SSE) note rewritten in plain words.

## 0.1.13

- Server page: no more gap between the active tab and the panel below it.

## 0.1.12

- Phones: the top bar is compact (logo, live indicator and a menu button) and stays pinned while the page scrolls; the buttons open as a panel under it.
- Checkboxes are restyled to match the rest of the interface (square with a hard edge and offset shade, pixel tick).
- The logo in the top bar is now the same `>_` pixel icon as the home screen icon, and the icon no longer has the extra bottom square.

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
