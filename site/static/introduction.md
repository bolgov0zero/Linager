# Introduction

Linager is an open-source system for managing Debian and Ubuntu servers: a server with a web admin panel and lightweight agents on the managed hosts.

## What you get

- Live metrics every 5 seconds and history up to 360 days.
- A web terminal, a file manager, systemd services, processes and logs.
- apt updates, network and firewall editing with an automatic rollback, Docker.
- Roles, 2FA, passkeys and an audit log of every administrator action.
- Access control for the admin panel: a white or black list by real client address, trusted proxies and an access journal.

::: tip One binary
The same `linager` binary is the server or the agent. You pick the role during installation.
:::

## How it fits together

The server keeps its data in a local PostgreSQL and serves the panel. Each agent opens an outgoing mutual-TLS connection to the server, so managed hosts need no open inbound port. Network and firewall changes are applied with a 30-second rollback timer that lives on the agent.

![Server overview](/screenshots/en/server-overview.png)

### Supported platforms

| System | Server | Agent |
|---|---|---|
| Debian 12, 13 | amd64 | amd64, arm64 |
| Ubuntu 22.04, 24.04, 26.04 | amd64 | amd64, arm64 |

The agent also runs on a Raspberry Pi 4 with a 64-bit OS.

## Where to go next

Start with [Installation](install), then add your first server in one of three ways ("+ Server" in the panel): run the same installer on it, choose **Client**, and confirm the check code; or make an **install link** and run one command on the new host; or let the panel install the agent **over SSH**. When an install ends, the Linager menu opens by itself ([the terminal menu](menu)). To put the panel behind a reverse proxy, read [Reverse proxy](proxy); to restrict who may open it, read [Admin panel access](access).
