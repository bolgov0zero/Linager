# Changelog

## 0.2.2

- Docs: the README (en, ru), the landing page (en, ru) and the introduction of the site tell about the new ways to add a server (confirm a key, an install link, an install over SSH) and about the Linager menu that opens after an install. The landing got a "Three ways to add a server" block in the install section and a line in the install animation; the README got an "Adding servers" row in the table of features and a rewritten "Add a managed host" paragraph with the `wget … | sh` command.
- Fix: the note button next to "Reboot" (and the pencil next to the cross in the note window) could be a little taller or shorter than the text buttons, depending on the font metrics of the browser: the icon buttons had a fixed height while the height of the text buttons follows the font. The rows now stretch their buttons to one height (`align-items: stretch`), so the note button, "Reboot" and "Delete server", and the pencil and the cross, always have the same height and the same top edge. Checked in the browser with line heights from 14 px to 28 px.

## 0.2.1

- After an install the Linager menu opens, as if the person had typed `linager`: `install.sh` (the install from GitHub) and the script of an install by a link end with "Press Enter to open the Linager menu" (in Russian when `LANG` is Russian) and then run `linager` on the terminal. The prompt comes after the wizard's summary, so the addresses and the generated administrator password can be read first. The menu is not opened after a failed install, without a terminal (automation, `ssh` runs) or with `--no-tui`. `install.sh` no longer `exec`s the wizard; the link script removes its temporary files before it hands the terminal to the menu. Tests: the generated link script is valid shell, uses wget without sudo and ends with the menu; `install.sh` is valid shell and has the same ending; the ending was also run in a pseudo-terminal (prompt in both languages, Enter opens the menu, no terminal ends quietly, `--no-tui` skips it); `shellcheck install.sh` is clean.

- Install by a link: the command no longer has `sudo` (some systems do not have it; run it as root) and uses `wget` instead of `curl` (wget is in the base system of Debian and Ubuntu, curl is not): `wget -nv -O- --no-check-certificate --pinnedpubkey 'sha256//…' 'https://host:port/v1/join/lk_…' | sh`. `-nv` keeps the errors visible (an empty script piped into `sh` would end silently). The script of the link and the install over SSH download the binary with `wget` too (and install `wget` on the host when it is missing). Not verified on a real host: that `wget --pinnedpubkey` works together with `--no-check-certificate` (wget 1.21 on Debian 12/13 and Ubuntu 22.04+; wget refuses an unknown option, so an old wget fails instead of skipping the check).
- The note button on the server page and the pencil in the note window are the size of the other buttons of the project (36 px squares), and the empty note text has the missing space.

## 0.1.78

- New: **a note on a server.** On the server page, left of the "Reboot" button, an icon button opens the "Note" window with free text (up to 20,000 characters) in Markdown. The pencil next to the cross starts editing; "Cancel" and "Save" end the edit without closing the window (`Ctrl+Enter` saves, `Esc` in the editor cancels, the cross and a click outside refuse to close over an unsaved text). A small square on the button says that a note exists. Everyone who sees the server reads the note (on a phone the button is in the header), an administrator changes it. The Markdown renderer (`web/src/lib/markdown.ts`) escapes everything first and adds the markup afterwards: headings, bold, italic, strike, code and code blocks, nested lists with tasks, quotes, rules, links limited to `http`, `https` and `mailto`; HTML in a note is never run. Migration `00013` (`servers.note`, `note_updated_at`, `note_updated_by`); `GET /api/servers/{id}` carries the note, `PUT /api/servers/{id}/note` saves it (administrators; the audit gets `server.note` with the length, not the text). `Modal` got header actions and a separate `Esc` handler. Tests: the API against a real database (save, normalizing line ends, the length limit in characters, an unknown server, no text in the audit, operator and auditor read but cannot write) and `node --test web/src/lib/markdown.test.mjs` (escaping, unsafe links, quotes in attributes, bare addresses, code spans, blocks, odd input). Docs: `docs/ru/modules.md`, `docs/en/modules.md`.

- New: the "Add server" window has three tabs: **Confirmations** (the pending requests, the connected servers that are not on the dashboard, the connection details of the client), **Install by a link** and **Install over SSH**. Mockup: `docs/design/next/add-server.html`.
- **Install by a link.** The panel makes a link (lifetime, number of servers, an optional group, optional allowed networks, optional automatic approval) and shows two commands, an external one and a local one: `curl -fsSk --pinnedpubkey sha256//… 'https://host:port/v1/join/lk_…' | sudo sh`. The agent port serves the script and the binary of the cached, verified release to whoever has a valid token (`GET /v1/join/{token}` and `.../binary`), the key of the server is pinned in the command, and the script checks the binary against the signed manifest. `linager install agent` and `linager agent enroll` take `--join-token`. A request that came by a link needs no key: one click in "Confirmations", or none with automatic approval; the name of the server is the one the agent reports, the group is the one of the link. Only the hash of a token is stored (migration `00012`); a token can be limited to networks, to a number of uses and in time, and can be revoked; every step is in the audit journal (`join.create`, `join.revoke`, `enroll.join`, `enroll.approve`). API: `GET/POST /api/join-tokens`, `DELETE /api/join-tokens/{id}`; `GET /api/enrollments` has `via` and `needs_key`.
- **Install over SSH.** The panel connects to a new host with a password or a key, shows the host key to confirm, checks the system, systemd, disk, rights (asks for the `sudo` password when needed), that the host can reach the client port of the server, and an existing agent; then it installs the agent with a one-shot token that approves itself, shows the stages and the output, and rolls the host back (`agent uninstall --purge`, the server entry removed) when the agent does not connect. Secrets live only in memory for the length of the install and are never in the log or the audit; loopback, link-local and multicast targets are refused, `ssh_install.allowed_networks` in `server.yaml` narrows the range; a password is accepted over HTTPS only. API: `POST /api/ssh-install`, `GET/DELETE /api/ssh-install/{id}`, `POST .../answer`, `POST .../install`.
- Tests: the install links end to end against a real database and a real agent (valid, spent, revoked, limited-network and self-approving links), and the SSH installer against an in-process SSH server (host key, sudo with a wrong and a right password, an unreachable port, an unsupported system, a wrong password, a failed install with the rollback, targets and sessions, no secret in the state, the log or the audit). Docs: `docs/ru/install.md`, `docs/en/install.md`.

## 0.1.77

- Agent update: the panel shows the stages with their times instead of two states ("download" and "restart"). Why only two were seen: the server kept only the last step of the run and the panel asked every 2 seconds, so the stages that take milliseconds (verify, backup, replace) were never caught, and everything after the restart was one "restart" until the agent came back. Now the server keeps the timeline of the run (migration `00011`, `agent_updates.steps`) and the panel draws it as a list with marks, durations and details: the download with its size (`7.2 of 18.4 MB`), the signature check, the backup, the replacement with the rollback timer, the restart, and then the stages that the server sees by itself: **agent offline** (with the seconds left before the rollback), **the new version connected** and **confirmation**. The list is on the server page (the banner) and on the Updates page (the "Stages" button in the row of the agent); after an error or a rollback it stays and marks where it stopped. Only the current run is kept. In the agent: every stage sends an event when it begins and when it ends (`state`, `bytes`, `size` in `update.event`; agents of older versions keep working: their events are turned into the same list), the download reports its progress once a second, the new version sends a "confirmed" event after cancelling the rollback timer, and the binary is checked after it has been downloaded into a file that is not executable yet (it becomes executable only after the signature and the checksum are verified). The rollback delay (70 s) moved to `protocol.AgentRollbackDelay`. Tests: the stages of an old and of a new agent, the drop, hello, confirmation, rollback and the watchdog (database), and `Apply` end to end against a signed release, including a tampered binary.

## 0.1.76

- New: `linager` without arguments in a terminal opens an interactive menu (the logo, the state and numbered actions). The mode is found on its own: the server menu, the agent menu, a picker with two cards when the host has both, or an offer to install when there is neither. The state refreshes every 5 seconds. Server items: update, backup, restore (a list of archives), the administrator password, turning off the access restriction, the server fingerprint with the enrollment command, restart, logs. Agent items: enroll, update, restart, logs, remove. The items run the usual subcommands (through `sudo` for a user that is not root), dangerous ones need a confirmation, and the server actions are written to the audit journal as "CLI". Without a terminal `linager` still prints the help. Docs: `docs/ru/menu.md`, `docs/en/menu.md` (also on the site). Tests: detecting the roles, navigation, the confirmation, the password form, the dictionaries.
- New: `linager server reset-password --login L --password-file F` sets a new password for an existing user, lifts a sign-in lock-out and ends the user's sessions (until now `create-admin` worked only on an empty database). It writes an audit entry.

## 0.1.75

- Fix: on the dashboard the server group picker was lower than the filter buttons next to it. The items of the toolbar groups now stretch to the height of their row.

## 0.1.74

- Fix: the last line of the terminal window (the prompt) was cut off by the bottom edge. The terminal fits its rows to the height of the element it sits in, and that element had padding and a frame that were counted as free room. The frame and padding are now on an outer block and xterm sits in an inner one without them.

## 0.1.73

- Fix: a server update rolled back with "the new version did not become healthy: admin panel answered 403 Forbidden" whenever the admin access restriction was on and did not list 127.0.0.1 (or "only through trusted proxies" was ticked). The health probe of the updater (a plain GET of the panel root from the server itself, without `X-Forwarded-For` and without `text/html` in `Accept`) is now let through the restriction by the server; every other request is judged as before. The fix is on the server side because the update is run by the updater of the version being replaced, so it works for the update to this version too. Test: the guard lets the local probe in and refuses a browser, a proxied request, other paths, writes and other hosts.

## 0.1.72

- Admin access: the "Trusted proxies" and "Access journal" blocks are collapsed by default; "Access restriction" stays open.

## 0.1.71

- Server update, progress dialog: it no longer freezes on the first step and then ticks everything at once. The panel is served by the service that the update stops (from "Stopping the service" to "Start and check"), so for a few seconds it cannot learn the progress. The dialog now says so: the steps that had not finished at its last answer are shown as going on, with a seconds counter and an explanation, and when the server is back it shows the real result. The updater records when every step began and ended (`started_ms`, `finished_ms` in `update-status.json`), and the dialog shows how long each step took.

## 0.1.70

- Admin access: the white and black lists accept host names next to addresses, for a connection without a fixed public address (DDNS, for example `home.example.duckdns.org`). The server resolves the names once a minute and judges by the addresses they point to (IPv4 and IPv6); the list shows them with the time of the last check and a "Refresh" button. A name is added only when DNS knows it. When DNS stops answering, the last known address stays in force (it is stored and survives a restart) for 24 hours. A name that has no address blocks saving, like locking yourself out. Names are not allowed among the trusted proxies. New `POST /api/access/resolve`; `GET /api/access` and `POST /api/access/check` carry `hosts`.
- Tests: the restriction is in force from the first request after a start (a regression test).

## 0.1.69

- New settings section "Admin access" (Settings → Admin access): restricts who may open the admin panel. Three modes: off, white list (whatever is not listed is refused) and black list (whatever is not listed is allowed), a separate list for each, judged by the real client address. A checkbox "Only through trusted proxies" refuses direct connections to the port. "Check an address" shows what the server would do with an address, and the panel tells what happens to you before anything is saved. The restriction applies to the admin panel port only; the agent port is not affected.
- The trusted proxies (the addresses whose `X-Forwarded-For` is believed) are edited in the same section. The panel shows how the current request arrived and offers to add that proxy with one click. A connection from any other address is judged by its own address, so the local network can still connect directly while the internet comes through the reverse proxy. `admin.trusted_proxies` in `server.yaml` is used until something is saved in the section.
- Saving is refused when it would lock you out, when everybody is trusted (`0.0.0.0/0`), when a proxy that sends `X-Forwarded-For` is not in the list (otherwise the whole internet would look like the proxy) or when the white list is empty. If access is lost anyway, `linager access reset` on the server turns the restriction off (the lists stay); a running server applies it within seconds.
- Access journal: page opens, sign-ins (good and failed) and refused requests with the client address and the proxy it came through; filters, "to the white/black list" in one click, retention 7, 30 or 90 days. Changes of the settings are written to the audit log (`access.update`, `access.retention`, `access.reset`). New migration 00010.
- Settings: the sections are in a new order: Profile, Users, Server groups, Snippets, Admin access, Updates, Log.

## 0.1.68

- Updates, "Linager server": after a check that finds a newer release nothing pops up any more, the block just shows the button "Update to ...". The release notes open only by the button "Release notes" (without an update button in the dialog).
- The progress of a server update is a dialog now (it is gone from the block): it opens when the update is confirmed, lists the steps and the result, and after a good result it closes by itself in 5 seconds or by the OK button. After an error or a rollback it stays until OK. A dialog is also opened if a running update is seen after a page reload.

## 0.1.67

- Security: the "State" block is folded by default (its header shows the verdict, for example "4 things need attention"), and fail2ban moved to the first place, before "State" and "Now in the system". Same order on a phone.

## 0.1.66

- Updates, "Linager server": the notes of a release are shown in a dialog instead of a list inside the block. After a check that finds a newer release the dialog opens by itself (with a button to update right away), and the new button "Release notes" opens it any time. When the server is up to date the button shows the notes of the latest release. `GET /api/updates` now fills `server.notes` for the latest known release also when no update is available, and adds `server.notes_version`.

## 0.1.65

- Updates: the blocks "Agents" and "Same from the terminal" are folded by default, like "Update history". The header of "Agents" tells what needs doing: "to update: N" (the agents that can be updated, including failed and rolled-back ones), "all up to date", or "update running" while a rollout is in progress.

## 0.1.64

- Updates: the block "Update history" is folded by default (the header shows the number of records), like the other folded blocks.

## 0.1.63

- Phone, Firewall: "Ports" is no longer a tab of the segmented control but a folded block under the rules (closed by default, the header shows the number of listening ports), like on a desktop. The control keeps Rules, Lists and Log.
- Network (wide screen): the primary interface is a column of its own on the left, the other interfaces are a column of folded rows on the right (closed by default, a click unfolds a row in place). The badges (state, kind, primary) stand under the interface name. Under 900 px the columns stack.
- Side menu: the arrow of the fold button was redrawn as a clean symmetric pixel arrow (it was lopsided).

## 0.1.62

- Metrics: the blocks "Events in period" and "Metric retention" are folded by default (the header shows the number of events).
- Processes: new sort "By name" (all processes, alphabetically; "Show" gets 100 and "all", the agent caps a list at 2000). The rows show CPU and RAM as bars, the command as the executable name plus muted arguments, the state as a tag with a hint, and the end button is compact.
- Network: the primary interface comes first and is open, the others are one-line rows (name, state, kind, address, traffic) that unfold on a click, folded by default. DNS can be edited (new `POST /api/servers/{id}/network/dns`, audit entry `network.dns`): 1-3 name servers and up to 6 search domains. The change goes through the same confirm-or-roll-back window as the interfaces. The agent writes where the host keeps DNS: `/etc/resolv.conf` (atomic rewrite), a drop-in `/etc/systemd/resolved.conf.d/linager.conf` under systemd-resolved, or the `dns-*` lines of the primary stanza with resolvconf; netplan hosts stay view-only. `network.state` gets `dns_editable` and `dns_backend`.
- Firewall: two blocks like on the Metrics tab. "Rules" is always open, "Ports" (ports and connections, top remote addresses, how to read) is folded by default and loads only when opened.
- Security: "Now in the system", fail2ban and "State" stay open, the other blocks (failed attempts, logins, keys) are folded by default. "State" became a checklist: a verdict ("Everything is fine" / "N things need attention"), one row per check with a mark, the value and, for every check that is not fine, a hint what to do. Phone: the accordions follow the same order.
- Docker: the agent checks the images of its containers by itself, 60 seconds after start and then every 6 hours (the button uses the same code). The number of containers with a newer image (`image_updates` in the sample, -1 = unknown) is a tag "images: N" on the server card (warn from 5) and a part of the summary hint under the updates figure. A new shared component `Fold` is used for the folded blocks.

## 0.1.61

- Overview: the four bars (CPU, RAM, network) are filled at once when the page opens. `GET /api/servers/{id}` now returns `recent`, the last 60 raw samples (CPU, RAM, receive and send rate) from the stored metrics, and the page seeds its bars from them instead of starting empty and collecting samples live. RAM and network were empty before, CPU had only 12 values.

## 0.1.60

- Phone: the line with the number of servers above the summary is gone (the summary strip already shows it).

## 0.1.59

- Phone: the buttons in the header of a terminal window (user, snippets, keys, copy, paste) stand next to each other on the left instead of being spread over the whole width.
- Phone: with the keyboard open, the terminal (the tab and the floating windows) no longer shows the page through the gap between its key row and the keyboard. That strip, where the translucent keyboard and its accessory bar of newer iOS sit, is filled with a solid background behind the terminal.

## 0.1.58

- Phone: the lightning bolt of the snippets button was redrawn (a clean pixel bolt). Everything at the bottom edge (the tab bar, the button bars of dialogs, the dock of terminal windows) is lifted by at least 16 px above the rounded screen corners and the bars keep a margin at their sides; the fill of the bars still reaches the edge.
- Server groups moved to the settings (new section "Server groups", administrator only): create, rename, delete and, new, arrange the groups with the up and down buttons. The "Groups..." button of the dashboard is gone, on a phone and on a desktop. The order of the groups is kept on the server (migration 00009, `PUT /api/groups/order`, audit entry `group.reorder`) and is the order of the group list on the dashboard and in the access settings.

## 0.1.57

- Phone: while a layer is open (the terminal, a floating window, a dialog, a sheet) a finger move that cannot scroll anything inside it is cancelled, so the page no longer moves or bounces behind it, and the visual area does not pan while the keyboard is up. The content of a dialog and the list of a sheet still scroll.

## 0.1.56

- Server page: a square icon button at the right end of the "Terminal" item of the side menu opens the terminal in a floating window (shown while the server is online and the terminal module is allowed).
- Phone layout, fixes after the first real use: the page behind a full-screen layer (floating terminal window, dialog, sheet, drop-down list) no longer scrolls with the finger; the Terminal tab has no "open in window" button on a phone (a window stacked a second terminal over the tab); the button bar at the end of a dialog sits exactly on the bottom edge of the sheet; no gaps under the pinned bars (firewall and network draft/rollback, apt install, selection bar of Files, search bars of Metrics and Services, the toolbar of the dashboard); the dock of minimized windows no longer hides the last row of a page.
- Phone: "Terminal as user" in the action sheet of a user works again; the group picker moved out of the pinned row of the dashboard; map tiles lay their meters out as rows; the sign-in page no longer scrolls by 1 px; chart labels no longer overlap in English; fields of the terminal use 16 px.

## 0.1.55

- Phone layout, redone from scratch (up to 720 px, designed for an iPhone 14 Pro Max, mockup `docs/design/next/mobile.html`): a pinned top bar and a bottom bar instead of the drop-down menu (Servers, Linager updates, Log, More; on a server page Overview, Terminal, Files, Services and "Sections" with every tab by group). Drop-down lists, action menus, context menus and all dialogs open as sheets from the bottom edge, the buttons at the end of a dialog stay in reach, and the large dialogs fill the screen. No swipes; controls are 44 px high, fields use 16 px so iOS does not zoom.
- The viewport no longer scales the page to 0.9 and covers the notch and the home indicator (`viewport-fit=cover`, safe-area insets).
- Dashboard on a phone: the four tiles are one strip, the filters, search and the card/map switch are one pinned row, a card is shorter (no chart) and opens the server by a tap, its terminal button opens the Terminal tab. Edit mode has 44 px buttons.
- Server page on a phone: one header (back, section, server and state, actions), the frame around the tab is gone; every tab was adapted: tables became rows with an action sheet, the terminal and the logs fill the whole height with a row of special keys above the keyboard, the folder tree of Files is a sheet, the draft/apply/rollback bar of the firewall and the network is pinned above the bottom bar, the settings window starts with a list of sections.
- Terminal: the floating window opens 20% larger by default (864 x 528 instead of 720 x 440); a size you set yourself is kept.

## 0.1.54

- Theme: the panel no longer follows the light or dark setting of the system. The theme you pick with the button in the top bar is kept in the browser and shown after every reload; until you pick one it is dark. The colour of the browser bar on phones follows the chosen theme as well. Server only, agents need no update.
- Users: the tab shows all accounts by default (the "Regular" and "System" buttons narrow the list when pressed). The "Add user" window has two columns, so it fits a window of 720 px in height even with many groups, and the hint of a password option is shown only for the chosen one.
- Terminal: a "User" drop-down in the toolbar of every terminal window, like the one of the snippets: it lists root and the accounts that have a shell (read from the Users module, the chosen one is ticked) and starts the shell again as the picked account. The title of the window follows (`user@server`). The button is shown to administrators and to operators who were given the Users module. Needs agent 0.1.53 or newer.

## 0.1.53

- Users (new agent module `users`, tab "Users" in the group "Network and security"): the local accounts of a server. A table of the regular accounts (UID 1000 and up) and, on request, the system ones: rights (sudo, sudo without a password, superuser), groups (`sudo` and `docker` are marked as almost root), shell, last login with the address, sessions now. A row opens the properties window: sudo (none / with a password / without a password), the groups the panel offers, the shell, a lock of the account (an expired account, so neither a password nor a key works, and the sessions are ended). Add: login, description, shell, home, a generated or typed password (the browser makes it, it is shown once and goes to `chpasswd` through stdin), sudo, groups, an SSH key, with the commands shown before they run. Menu: change the password, end the sessions, delete (with the home directory or without, a typed name to confirm, the sessions and processes are ended first). Root and system accounts are never changed; the only unlocked user with sudo cannot lose the rights or be locked or deleted while root cannot log in. Every change asks for confirmation and goes to the audit log (`hostuser.*`, the password is not logged). It is a root module: an operator sees the tab only when the administrator gave them "Users".
- Terminal as a user: the "Terminal" button of a row opens a floating terminal window as that account (`runuser -l`, no password needed; the agent checks the name against `/etc/passwd`). The window has the same frame as the others (drag, resize, minimize, maximize), one window per user. It needs the Terminal module too. Needs agent 0.1.53; the tab is hidden for older agents.
## 0.1.52

- Server page: the tabs moved from the top row to a sidebar on the left, split into groups: General (Overview, Metrics, Terminal), System (Services, Processes, Logs, Updates), File system (Files, Disk), Network and security (Network, Firewall, Security) and Applications (Docker). Every tab has a pixel icon; a group is folded with a click on its title (the group of the open tab stays open). Counters: the number of apt updates on "Updates" (red when some of them are security updates) and of failed services on "Services". The button at the bottom folds the menu into a narrow column of icons (a hint shows the name on hover). The state is kept in the browser. A group whose tabs are all hidden (a disabled agent module, an operator without the module) disappears. Up and Down move between the tabs. On a narrow screen the menu is a drop-down with the current section on top. The page shows the group and the name of the open tab above the panel. Addresses `#srv-<name>/<tab>` are unchanged. Server only, agents need no update.
## 0.1.51

- Releases: `manifest.json` now has an `assets` map with the binary of every architecture (name, SHA-256, size); the top-level `sha256` and `size` still describe the amd64 binary, so agents of earlier releases keep updating. The server downloads, verifies and caches every binary of a release and gives an agent the one for its own architecture (`/v1/release/<ver>/binary?arch=`; an agent that sends none gets amd64). The agent checks the downloaded file against its own entry of the signed manifest. First step of arm64 support for agents.
- Build and release: `make build` produces `linager_linux_amd64` and `linager_linux_arm64` (`ARCHES=amd64` builds one), a release carries both binaries and one `.deb` per architecture (`linager_amd64.deb`, `linager_arm64.deb`), `SHA256SUMS` lists all of them and `manifest.json` has a checksum for each. CI runs the tests on an arm64 runner as well. 
- Installer: `install.sh` now accepts arm64 (a Raspberry Pi 4 with Raspberry Pi OS 64-bit, Debian or Ubuntu for arm64) and downloads the binary and the checksum of its own architecture. It refuses other CPUs and a 32-bit system on a 64-bit kernel (Raspberry Pi OS 32-bit) with a clear message. The "CPU" line of an arm64 agent shows the board ("Raspberry Pi 4 Model B Rev 1.5") instead of staying empty.

## 0.1.50

- Roles: besides the administrator there are now an Operator and an Auditor. When the administrator creates a user (or changes one in "Settings → Users → Role and access") they pick the role and, with two buttons, the servers and the modules. In the "Servers" window the servers are grouped; the switch of a group gives access to all its servers (they turn on and grey out) and to the servers that join the group later. The "Modules" window has a switch per module; Terminal, Files, Network, Firewall, Security and Docker are marked "root access", because they let a user do almost anything on the host.
- An operator works only on the servers and with the modules that were given, on every server of theirs; a closed module is closed as a whole. An operator cannot change the settings of the panel (users, updates, audit log, snippets, groups), add or delete agents, reboot servers or edit the dashboard. An auditor only sees the dashboard, the overview and the metrics of the chosen servers and changes nothing. Both get a "Profile" button instead of "Settings" (password, 2FA, passkey).
- The server checks every request: each route has an access rule (a test makes sure none is missing), a server the user may not see answers as if it did not exist, and the live events are filtered the same way. A change of rights works at once. The administrator cannot change their own role or demote the last administrator; creating a user and changing the access are written to the audit log. Existing users stay administrators. Server only, agents need no update.

## 0.1.49

- Agent: `/etc/linager/agent.yaml` is completed when the agent starts (so after every update): the modules a newer agent knows and the file does not list are appended to the `modules:` block as `true`, the rest of the file (comments, your `false` values) stays as it is. A module missing from the file was enabled before too, so nothing changes in behaviour; the file now lists everything. Needs agent 0.1.49.

## 0.1.48

- Firewall: a "Journal" button next to "Address lists" opens the journal of rule matches. A rule is marked for the journal with a switch in the table of rules (or "Switch on for all drop rules" in the window): the rule gets `limit rate 5/second log prefix "lg:..."` and the kernel writes the matched packets to its journal, which the agent reads (7 days). The window shows the matches over time and a table (time, chain, action, protocol, source, port, rule, packets) with filters by chain, action and protocol in any combination, by port (`22`, `80, 443`, `1000-2000`) and by source (an address, a network or `@list`), identical events can be grouped, a row expands to the details, and the whole list can be exported to CSV. From a row: add the source to an address list, block the address (a rule first in input, in the draft) or open the rule. Needs agent 0.1.48 and `journalctl`.

## 0.1.47

- Firewall: the rule `ct state established,related accept` is now written by the agent first in every chain (input, forward, output) and cannot be removed or disabled, so the replies of the connections that already exist (the agent link, an open SSH session, the replies to what the host opened itself) are never cut by a drop policy or a drop rule. The rule is not shown in the list (a note under the table says so) and is skipped when it comes from an older draft or from the nft syntax form. The warning before applying no longer asks for it, but still catches a rule that drops all TCP before the SSH port.
- Firewall: the rules are shown as a table with the columns Action (Allow, Drop, Reject), Protocol, Port, Source (an address, a network, `@list` or an interface) and Comment (with the conditions that have no column, such as the connection state or a rate limit); the new "All" tab shows the rules of all chains with a Chain column and the "As nft" button switches to the raw rule text. The automatic rule needs agent 0.1.47; with an older agent the list is already shown as a table.

## 0.1.46

- Firewall: the Source field of a rule (and of the "Restrict" form for a port) offers the address lists in a drop-down when it gets the focus: it narrows as you type, a click or the arrow keys and Enter put `@name` into the field. Server only, agents need no update.

## 0.1.45

- Disk tab (new agent module `disk`, right after Files): every partition as a pie chart with the free and the used space, the growth per day and a forecast "fills up in ~N days" (the agent records the used space every half hour); a measurement on request shows what takes the space (system, Docker, logs, databases, cache, home directories) and the biggest directories with drill-down; cleanup: journald, apt cache, unused Docker images, Docker build cache, old kernels, each with a confirmation and an audit entry. The pie charts share one look: a sector lifts on hover, a tooltip shows the size and the legend row is highlighted.
- Security: fail2ban can be installed from the panel (three strictness modes, the address of the administrator is never blocked) and configured (threshold, ban time, repeat offenders, "Do not block" list); a block list with the history of bans, unban of one, selected or all addresses, manual blocking for a chosen time and a "Ban" button in the table of failed attempts. A key can be removed from `authorized_keys` (a copy of the file is saved first, a warning for the last key). Needs agent 0.1.45.
- Firewall: the chains are tabs; a "Ports and connections" block with the listening ports, processes, connections and the state of each port in the firewall (with "Close the port" and "Restrict" that add a rule to the draft); "Address lists": named lists of addresses (nftables sets) that rules use as `@name`. Needs agent 0.1.45.
- Docker: "Check images" compares the digest of the tag in the registry with the image on the host (no download) and marks containers with a newer image; "Update marked"; compose containers are grouped by project (collapsed by default) with "Update project", "Restart", "Start project" and "Project logs". Needs agent 0.1.45.
- Heights of buttons, inputs and selects in a row are the same everywhere (the header "Settings" button with a counter, the dashboard search, the add-passkey row, the add-rule row of the firewall).

## 0.1.44

- Security tab: the "Logged in now" and fail2ban blocks moved right under the "State" block. Server only, agents need no update.

## 0.1.43

- Security: remote SSH sessions can be ended from the "Logged in now" block: the "End" button of a session and "End all sessions" for a user with several. A confirmation warns that the processes of the session stop too (tmux, screen, long operations) and that this is not a block. The agent ends only sessions from its own list of remote sessions (`loginctl terminate-session`), local console sessions are never touched, and every action goes to the audit log. Needs agent 0.1.43 and systemd-logind.

## 0.1.42

- Security tab (new agent module `security`, read-only): the SSH state (port, root login, password and key login, empty passwords, user restrictions), failed sign-in attempts of the last 7 days (total, by day, top addresses and logins, probes of users that do not exist), SSH logins (a login from an address that guessed passwords earlier is flagged), fail2ban jails and banned addresses, automatic security updates, the keys of every `authorized_keys` (fingerprints only, weak RSA/DSA marked) and the remote sessions now. Data comes from the systemd journal (`sshd` and `sshd-session`, so Debian 13 works), with `/var/log/auth.log` as a fallback. Needs agent 0.1.42; the tab is hidden for older agents.

## 0.1.41

- Settings: the window is wider (up to 1360 px). Server only, agents need no update.

## 0.1.40

- Settings: a "Settings" button in the top bar opens a large window with a side panel: Profile, Users, Updates, Audit log, Snippets. The old pages #updates and #audit moved into it (the old links still work); the 2FA button is now part of the profile.
- Sign-in: a "Passkey" button next to "Sign in" signs in without a login and a password (WebAuthn, needs HTTPS or localhost). Passkeys are added in the profile. "Sign in" goes to the 2FA step as before; the code field also takes a recovery code.
- Recovery codes: enabling 2FA shows 10 one-time codes (copy or download, shown once); the profile shows how many are left and can generate new ones (password required).
- Users: add users, change your own password (the other sessions end), reset the password or the 2FA of another user, delete users (not yourself). All actions go to the audit log. New `admin.login_rate` in server.yaml (sign-in attempts per minute per address, default 10).
- Snippets: commands shared by all users, managed in Settings. In the terminal (tab and floating window) a "Snippets" drop-down: a click pastes and runs the command, the small button at the right only pastes it.
- Docker: "Update image" in the container menu pulls the image again and recreates the container with the same settings, with a rollback to the old container if the new one does not start; a docker compose container is updated with `docker compose pull` and `up -d --no-deps`. Needs agent 0.1.40.

## 0.1.39

- Sign-in page: the card is centered under the top bar. Two-step sign-in: login and password first, then the 2FA code on a second step (only for users with 2FA; no more "I have 2FA" checkbox). The right password without a code creates no session and is not counted as a failed attempt. The code signs you in by itself when the sixth digit is typed or filled in. The fields are marked for password managers (`username`, `current-password`, `one-time-code`). Needs the server only; agents need no update.

## 0.1.38

- Metrics: a "Synchronize" checkbox next to the period chips shows the hover highlight (crosshair, dots and tooltip) on all four charts at once; the choice is remembered in the browser, off by default.
- Terminal: the keyboard icon in the key panel button is centered.

## 0.1.37

- Updates: after a successful install the progress bar is filled completely (apt's last reported percentage stays below 100, so the last segment stayed empty). Server only, agents need no update.

## 0.1.36

- Dashboard: the number of updates on a server card (and the security count) is now counted the same way as on the Updates tab. Before, the dashboard missed packages that need a new package to be installed, such as kernel updates, so a server could show 2 on the card and 4 on the tab. Needs agent 0.1.36; the card refreshes within an hour or after an apt run from the panel.

## 0.1.35

- Updates: installing packages now shows the state of every package (queued, updating, done, error; "not updated" for packages apt never reached after a failure) and an overall progress bar (download, then install). "Check" opens a progress dialog that closes by itself three seconds after a successful check. The apt console output is hidden by default behind a "Show console output" button. The statuses come from `APT::Status-Fd` on the agent, so they survive closing the tab: the browser attaches to the running operation and gets all events and the log. Needs agent 0.1.35.

## 0.1.34

- Terminal: the shell now gets HOME, USER, LOGNAME and SHELL (the agent service had no HOME, so `cd` said "HOME not set" and bash did not save its history). Commands are appended to the history right after they run, and a closed session gets three seconds to exit by itself before it is killed. Needs agent 0.1.34; open terminal sessions pick it up after the agent restarts.

## 0.1.33

- File editor: a "Wrap lines" switch in the window header wraps long lines instead of scrolling sideways. The choice is remembered; it is on by default on touch screens.

## 0.1.32

- Phones: the terminal buttons (keys, copy, paste, in a window, close session) are pixel icons instead of text; the floating terminal window shows copy and paste as icons.

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
