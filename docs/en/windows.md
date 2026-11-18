# Windows agent (beta)

The agent runs on Windows 10 / 11 and Windows Server 2016 and newer (x64) and connects to a Linager server the same way as the Linux one. The **server stays on Linux**. The Windows agent is a beta: it has the modules that map onto Windows directly, and nothing else.

## What works

| Tab | What it does on Windows |
|---|---|
| Metrics | CPU, memory, swap, disks (every volume with a file system), network adapters and their speed, uptime, processes, "restart required" (Windows Update). Windows has no load average: it is shown as 0. |
| Terminal | PowerShell (cmd.exe if it is missing) in a pseudo console (ConPTY, Windows 10 1809 / Server 2019 and newer). The shell runs as **SYSTEM**, the full access of the agent, like the root shell on Linux. Everything is written to the audit journal. |
| Services | the list from the Service Control Manager: start, stop, restart, startup type (enable = automatic, disable = disabled). The agent itself, remote desktop, WinRM, sshd, the RPC, DHCP, DNS and firewall services ask for confirmation. A service that stopped with an error and starts automatically counts as failed. |
| Processes | the list with CPU, memory, user and command line; "end process" (Windows has one way to end a process, so "terminate" and "kill" do the same). System processes (csrss, lsass, services, wininit, winlogon, smss, System) are refused. |
| Logs | Windows event logs, followed live: System and Application by default; as "unit" a classic log (`Security`, `Setup`), an operational log (`Microsoft-Windows-TaskScheduler/Operational`) or the name of a source (for example `Service Control Manager`). "Kernel" shows the kernel sources. The priority is mapped to the level (error, warning, information). |
| Files | the file manager. The top of the tree is the list of drives, paths look like `/C:/Users/x`. Owner and group are not shown, and permissions cannot be changed from the panel (Windows keeps ACLs). The drive roots and `C:\Windows`, `C:\Program Files`, `C:\ProgramData`, `C:\Users` cannot be renamed, deleted or replaced. |
| Reboot | `shutdown /r`, with confirmation. |

Not available on Windows (the tabs are hidden): updates (apt), disk cleanup, network, firewall, security (fail2ban, keys), users, Docker.

## Install

Download `linager_windows_amd64.msi` from the release (a beta release has the tag `vX.Y.Z-beta.N`; check it against `SHA256SUMS`) and install it in an elevated prompt:

```powershell
msiexec /i linager_windows_amd64.msi /qn SERVER=panel.example.com:9443
```

`SERVER` is the agent port of the server (`host:port`). `FINGERPRINT=SHA256:...` is optional: with it the agent trusts only that server certificate; without it the pairing is protected by the check code (see below). The package installs `C:\Program Files\Linager\linager.exe`, registers the service **Linager Agent** (`LinagerAgent`, automatic start, runs as SYSTEM, started again after a failure) and starts it.

### Pairing
1. In the panel open "Add server", tab "Confirmations": the host appears there a few seconds after the service starts.
2. On the Windows host read the key and the check code (administrators only):

   ```powershell
   Get-Content C:\ProgramData\Linager\agent\enroll.txt
   ```

3. Enter the key ("Confirmation key") and **make sure the check code is the same**: it ties the key to the certificate the agent saw, so it protects the pairing against a man in the middle when no fingerprint was given. Confirm.

The same works from a console: `& "C:\Program Files\Linager\linager.exe" agent enroll --server panel.example.com:9443`. The service notices the new identity within 10 seconds.

## Files, log and removal
- `C:\ProgramData\Linager\agent\` keys and state; `C:\ProgramData\Linager\agent.yaml` (modules, optional, the same format as on Linux: `modules:` with `true` / `false`); `C:\ProgramData\Linager\agent.log` the log (a file over 5 MB is moved to `agent.log.1`). The folder `C:\ProgramData\Linager` is open to SYSTEM and the administrators only; this is the counterpart of the mode 600 of the Linux agent.
- `linager.exe agent status` shows the version, the server and the state of the service.
- After a change in `agent.yaml` restart the service: `Restart-Service LinagerAgent`.
- Remove: `msiexec /x linager_windows_amd64.msi /qn`. The keys, the log and the address of the server stay; to remove them as well, delete `C:\ProgramData\Linager` and the key `HKLM\SOFTWARE\Linager`. "Delete server" in the panel makes the agent remove its identity and service.

## Updating
**The panel does not update a Windows agent**: it shows "new MSI X" in the table of agents on the Updates page. Install the new MSI over the old one (`msiexec /i ... /qn`): the service is stopped and started again, the pairing and the settings stay. The server of the same release should be updated first, as with Linux agents.

## Limits of the beta
- Only x64. The exe and the MSI are not signed with a code signing certificate, so SmartScreen or an antivirus may warn about them.
- Checked: the code is built and tested on a Windows runner of GitHub Actions (metrics, services, processes, files, terminal, install and removal of the MSI). Not checked on a real fleet.
- Windows reports one product name "Windows 10" for Windows 11; the agent tells them apart by the build number (22000 and up).
