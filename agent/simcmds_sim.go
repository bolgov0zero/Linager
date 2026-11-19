//go:build sim

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// This file only exists in the development simulator (go build -tags sim ./cmd/linager-sim): canned answers
// of the modules, built from the real types, so that the tabs of the panel have data to show.

func init() {
	ok := func(v any) CmdFunc {
		return func(context.Context, json.RawMessage, bool) (any, error) { return v, nil }
	}
	devCommands = map[string]CmdFunc{
		"services.list":   ok(simServices()),
		"services.action": ok(map[string]bool{"ok": true}),
		"processes.list":  ok(simProcesses()),
		"apt.list": func(context.Context, json.RawMessage, bool) (any, error) {
			pk := simPackages()
			for i := range pk {
				pk[i].Ignored = ignores.isApt(pk[i].Name)
			}
			return map[string]any{"packages": pk, "reboot_required": true, "last_check": time.Now().Add(-3 * time.Hour).Unix(), "busy": false}, nil
		},
		"network.state":  ok(simNetwork()),
		"firewall.state": ok(simFirewall()),
		"security.state": ok(simSecurity()),
		"docker.state": func(context.Context, json.RawMessage, bool) (any, error) {
			d := simDocker()
			for _, c := range d["containers"].([]map[string]any) {
				c["ignored"] = ignores.isDocker(c["name"].(string))
			}
			return d, nil
		},
		"disk.state":    ok(simDisk()),
		"disk.scan":     ok(simScan()),
		"users.state":   ok(simUsers()),
		"apt.ignore":    ignoreCmd("apt", nil),
		"docker.ignore": ignoreCmd("docker", nil),
		"docker.config": simDockerConfig,
	}
	devStreams = map[string]StreamFunc{"logs": simLogs, "docker.edit": simDockerEdit}
}

func simLogs(ctx context.Context, _ json.RawMessage, io *StreamIO) error {
	units := []string{"sshd[812]", "nginx[1204]", "systemd[1]", "kernel", "CRON[2210]", "postgres[933]", "dockerd[701]"}
	msgs := []string{"Accepted publickey for deploy from 203.0.113.5 port 51422 ssh2: ED25519 SHA256:abc", "Failed password for invalid user admin from 192.0.2.10 port 40122 ssh2",
		"2024/01/12 10:11:12 [warn] 1204#1204: *4 upstream response is buffered to a temporary file /var/cache/nginx/proxy_temp/3/00/0000000003 while reading upstream, client: 10.0.1.5, server: example.lan",
		"Started Daily apt download activities.", "pam_unix(cron:session): session opened for user root(uid=0) by (uid=0)", "checkpoint complete: wrote 412 buffers (2.5%); 0 WAL file(s) added", "ufw block IN=ens18 OUT= MAC=bc:24 SRC=192.0.2.66 DST=10.0.1.10 LEN=44 TTL=240 ID=54321 PROTO=TCP SPT=4444 DPT=23"}
	line := func(i int) []byte {
		t := time.Now().Add(-time.Duration(60-i) * time.Second).Format("2006-01-02T15:04:05-0700")
		return []byte(fmt.Sprintf("%s web-01 %s: %s\n", t, units[i%len(units)], msgs[i%len(msgs)]))
	}
	for i := 0; i < 40; i++ {
		if err := io.Out(line(i)); err != nil {
			return err
		}
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for i := 40; ; i++ {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if err := io.Out(line(i)); err != nil {
				return err
			}
		}
	}
}

func simServices() map[string]any {
	u := func(n, d, a, s, e string) Unit { return Unit{Name: n, Description: d, Active: a, Sub: s, Enabled: e} }
	return map[string]any{"services": []Unit{
		u("ssh.service", "OpenBSD Secure Shell server", "active", "running", "enabled"),
		u("cron.service", "Regular background program processing daemon", "active", "running", "enabled"),
		u("nginx.service", "A high performance web server and a reverse proxy server", "active", "running", "enabled"),
		u("postgresql@16-main.service", "PostgreSQL Cluster 16-main", "active", "running", "enabled"),
		u("docker.service", "Docker Application Container Engine", "active", "running", "enabled"),
		u("containerd.service", "containerd container runtime", "active", "running", "enabled"),
		u("linager-agent.service", "Linager agent", "active", "running", "enabled"),
		u("fail2ban.service", "Fail2Ban Service", "active", "running", "enabled"),
		u("rsyslog.service", "System Logging Service", "active", "running", "enabled"),
		u("systemd-journald.service", "Journal Service", "active", "running", "static"),
		u("systemd-logind.service", "User Login Management", "active", "running", "static"),
		u("systemd-networkd.service", "Network Configuration", "active", "running", "enabled"),
		u("systemd-resolved.service", "Network Name Resolution", "active", "running", "enabled"),
		u("redis-server.service", "Advanced key-value store", "failed", "failed", "enabled"),
		u("apache2.service", "The Apache HTTP Server", "inactive", "dead", "disabled"),
		u("certbot.service", "Certbot", "inactive", "dead", "static"),
		u("unattended-upgrades.service", "Unattended Upgrades Shutdown", "active", "exited", "enabled"),
		u("apt-daily.service", "Daily apt download activities", "inactive", "dead", "static"),
		u("ufw.service", "Uncomplicated firewall", "inactive", "dead", "disabled"),
		u("smartmontools.service", "Self Monitoring and Reporting Technology (SMART) Daemon", "active", "running", "enabled"),
		u("exim4.service", "LSB: exim Mail Transport Agent", "active", "exited", "enabled"),
		u("a-very-long-unit-name-for-layout-checks@instance-with-a-long-suffix.service", "A unit with a name and a description long enough to test how the table wraps", "active", "running", "enabled"),
	}}
}

func simProcesses() map[string]any {
	cmds := []string{"/usr/sbin/nginx -g daemon on; master_process on;", "nginx: worker process", "postgres: 16/main: checkpointer", "/usr/bin/dockerd -H fd:// --containerd=/run/containerd/containerd.sock",
		"/usr/local/bin/linager agent run", "/usr/sbin/sshd -D", "/usr/bin/python3 /usr/bin/fail2ban-server -xf start", "node /srv/app/server.js --port 3000 --env production --log-level debug --long-argument-for-wrapping-checks",
		"/usr/sbin/cron -f", "/lib/systemd/systemd --user", "redis-server *:6379", "/usr/lib/postgresql/16/bin/postgres -D /var/lib/postgresql/16/main"}
	users := []string{"root", "www-data", "postgres", "root", "root", "root", "root", "deploy", "root", "deploy", "redis", "postgres"}
	list := make([]ProcInfo, 0, 30)
	for i := 0; i < 30; i++ {
		list = append(list, ProcInfo{PID: 400 + i*37, User: users[i%len(users)], CPU: float64((i*7)%40) / 3, MemPct: float64((i*5)%30) / 4, RSS: uint64(20+i*9) << 20, State: []string{"S", "S", "R", "S", "I"}[i%5], Cmd: cmds[i%len(cmds)]})
	}
	return map[string]any{"processes": list, "total": 187}
}

func simPackages() []AptPackage {
	return []AptPackage{
		{Name: "openssl", Current: "3.0.11-1~deb12u1", Candidate: "3.0.11-1~deb12u2", Security: true},
		{Name: "libssl3", Current: "3.0.11-1~deb12u1", Candidate: "3.0.11-1~deb12u2", Security: true},
		{Name: "linux-image-6.1.0-13-amd64", Current: "6.1.55-1", Candidate: "6.1.55-2", Security: true, Reboot: true},
		{Name: "nginx", Current: "1.22.1-9", Candidate: "1.22.1-9+deb12u1"},
		{Name: "curl", Current: "7.88.1-10+deb12u3", Candidate: "7.88.1-10+deb12u4", Security: true},
		{Name: "systemd", Current: "252.17-1~deb12u1", Candidate: "252.19-1~deb12u1", Reboot: true},
		{Name: "postgresql-16", Current: "16.1-1.pgdg120+1", Candidate: "16.2-1.pgdg120+1"},
		{Name: "docker-ce", Current: "5:24.0.6-1~debian.12~bookworm", Candidate: "5:25.0.1-1~debian.12~bookworm"},
		{Name: "tzdata", Current: "2023c-5+deb12u1", Candidate: "2024a-0+deb12u1"},
		{Name: "a-package-with-a-really-long-name-for-layout-checks-dev", Current: "1.0.0-1", Candidate: "1.0.1-1"},
	}
}

func simNetwork() map[string]any {
	ifs := []NetIface{
		{Name: "ens18", State: "up", Kind: "ethernet", MAC: "bc:24:11:2a:5f:91", MTU: 1500, Mode: "static", Addresses: []string{"10.0.1.10/24", "fe80::be24:11ff:fe2a:5f91/64"}, Gateway: "10.0.1.1", SpeedMbps: 1000, RxBytes: 48 << 30, TxBytes: 22 << 30, RxPackets: 41000000, TxPackets: 30000000, Primary: true, Editable: true},
		{Name: "docker0", State: "up", Kind: "bridge", MAC: "02:42:6b:3e:aa:10", MTU: 1500, Mode: "static", Addresses: []string{"172.17.0.1/16"}, RxBytes: 1 << 30, TxBytes: 3 << 30},
		{Name: "lo", State: "unknown", Kind: "loopback", MTU: 65536, Mode: "static", Addresses: []string{"127.0.0.1/8", "::1/128"}, RxBytes: 5 << 30, TxBytes: 5 << 30},
	}
	routes := []map[string]any{{"dst": "default", "gateway": "10.0.1.1", "dev": "ens18", "metric": 100, "protocol": "static"}, {"dst": "10.0.1.0/24", "gateway": "", "dev": "ens18", "metric": 0, "protocol": "kernel"}, {"dst": "172.17.0.0/16", "gateway": "", "dev": "docker0", "metric": 0, "protocol": "kernel"}}
	return map[string]any{"backend": "ifupdown", "editable": true, "config_editable": true, "interfaces": ifs, "dns": map[string][]string{"nameservers": {"10.0.1.1", "1.1.1.1"}, "search": {"example.lan"}},
		"dns_editable": true, "dns_backend": "resolvconf", "routes": routes, "config_preview": "auto lo\niface lo inet loopback\n\nauto ens18\niface ens18 inet static\n    address 10.0.1.10/24\n    gateway 10.0.1.1\n"}
}

func simFirewall() map[string]any {
	r := func(t string, p, b uint64) FwRule { return FwRule{Text: t, Packets: p, Bytes: b} }
	chains := []FwChain{
		{Name: "input", Policy: "drop", Rules: []FwRule{r("ct state established,related accept", 120344, 99000000), r("iif lo accept", 4400, 220000), r("tcp dport 22 accept", 812, 64000), r("tcp dport { 80, 443 } accept", 55012, 31000000), r("ip saddr @office accept", 12, 900), r("icmp type echo-request limit rate 5/second accept", 45, 3800), r("tcp dport 5432 ip saddr 10.0.1.0/24 accept", 330, 25000)}},
		{Name: "forward", Policy: "drop", Rules: []FwRule{r("ct state established,related accept", 900, 80000), r("iifname \"docker0\" accept", 1200, 99000)}},
		{Name: "output", Policy: "accept", Rules: []FwRule{}},
	}
	sets := []FwSet{{Name: "office", Family: "ipv4", Elements: []string{"203.0.113.5", "198.51.100.0/24"}}, {Name: "blocked", Family: "ipv4", Elements: []string{"192.0.2.66", "192.0.2.67", "192.0.2.68"}}}
	return map[string]any{"available": true, "ufw_active": false, "chains": chains, "sets": sets, "ruleset": "table inet filter {\n\tchain input {\n\t\ttype filter hook input priority filter; policy drop;\n\t}\n}\n", "other_tables": []string{"ip nat"}, "ssh_ports": []int{22}}
}

func simSecurity() secState {
	now := time.Now()
	return secState{GeneratedAt: now.Unix(), PeriodDays: 14,
		SSH: secSSH{Installed: true, Source: "effective", Ports: []string{"22"}, PermitRootLogin: "prohibit-password", PasswordAuth: "no", PubkeyAuth: "yes", KbdInteractive: "no", PermitEmptyPass: "no", MaxAuthTries: "4"},
		Fail2ban: secF2B{Installed: true, Active: true, Managed: true,
			Jails:    []f2bJail{{Name: "sshd", CurrentlyFailed: 2, TotalFailed: 412, CurrentlyBanned: 3, TotalBanned: 57, BannedIPs: []string{"192.0.2.10", "192.0.2.11", "198.51.100.7"}}},
			Bans:     []f2bBan{{IP: "192.0.2.10", Jail: "sshd", BannedAt: now.Add(-40 * time.Minute).Unix(), UnbanAt: now.Add(20 * time.Minute).Unix(), Attempts: 6}, {IP: "198.51.100.7", Jail: "sshd", BannedAt: now.Add(-3 * time.Hour).Unix(), Attempts: 11}},
			Settings: &f2bSettings{MaxRetry: 5, FindTime: 600, BanTime: 3600, Increment: true, Recidive: true, Ignore: []string{"127.0.0.1/8", "10.0.1.0/24"}}},
		AutoUpdates: true,
		Logins:      []secLogin{{TS: now.Add(-2 * time.Hour).Unix(), User: "deploy", IP: "203.0.113.5", Method: "publickey"}, {TS: now.Add(-26 * time.Hour).Unix(), User: "root", IP: "198.51.100.9", Method: "publickey", PriorFailures: 3}},
		Failures: secFailures{Total: 1840, InvalidUsers: 920, Addresses: 61, ByDay: []int{40, 55, 120, 210, 90, 60, 75, 300, 410, 150, 80, 60, 55, 35},
			TopIPs:   []secTopIP{{IP: "192.0.2.10", Count: 310, Users: 14, Last: now.Add(-41 * time.Minute).Unix()}, {IP: "198.51.100.7", Count: 205, Users: 9, Last: now.Add(-3 * time.Hour).Unix()}},
			TopUsers: []secTopUser{{User: "admin", Count: 400, Invalid: true}, {User: "root", Count: 380}, {User: "ubuntu", Count: 120, Invalid: true}}},
		Keys:     []authKey{{User: "deploy", File: "/home/deploy/.ssh/authorized_keys", Type: "ssh-ed25519", Fingerprint: "SHA256:abcdEFGH1234abcdEFGH1234abcdEFGH1234abcdEFG", Comment: "deploy@ci", ModTime: now.Add(-90 * 24 * time.Hour).Unix()}, {User: "root", File: "/root/.ssh/authorized_keys", Type: "ssh-rsa", Bits: 1024, Fingerprint: "SHA256:zzzzEFGH1234abcdEFGH1234abcdEFGH1234abcdEFG", Comment: "old-laptop", Weak: true, ModTime: now.Add(-900 * 24 * time.Hour).Unix()}},
		Sessions: []secSession{{ID: "42", User: "deploy", IP: "203.0.113.5", Since: now.Add(-2 * time.Hour).Unix()}},
		Journal:  secJournal{Source: "journal", Oldest: now.Add(-20 * 24 * time.Hour).Unix(), CoversPeriod: true}}
}

func simDocker() map[string]any {
	c := func(id, name, image, state, status string, ports []string, cpu float64, used uint64) map[string]any {
		return map[string]any{"id": id, "name": name, "image": image, "state": state, "status": status, "ports": ports, "created": time.Now().Add(-72 * time.Hour).Unix(), "compose": false, "cpu": cpu, "mem_used": used << 20, "mem_limit": uint64(2048) << 20}
	}
	cont := []map[string]any{
		c("a1b2c3d4e5f6", "web", "nginx:1.25", "running", "Up 3 days", []string{"0.0.0.0:80->80/tcp", "0.0.0.0:443->443/tcp"}, 1.4, 64),
		c("d1b2c3d4e5f6", "api", "registry.example.com/shop/api:2.4.1", "running", "Up 3 days", []string{"127.0.0.1:3000->3000/tcp"}, 3.1, 212),
		c("b1b2c3d4e5f6", "db", "postgres:16", "running", "Up 12 days", []string{"127.0.0.1:5432->5432/tcp"}, 4.2, 512),
		c("c1b2c3d4e5f6", "cache", "redis:7", "exited", "Exited (1) 2 hours ago", []string{}, 0, 0),
		c("e1b2c3d4e5f6", "grafana", "grafana/grafana:10.4.2", "running", "Up 9 days", []string{"0.0.0.0:3001->3000/tcp"}, 0.8, 148),
	}
	for _, i := range []int{0, 1} {
		cont[i]["compose"], cont[i]["project"] = true, "shop"
	}
	cont[0]["service"], cont[1]["service"] = "web", "api"
	cont[0]["upd"], cont[0]["upd_local"], cont[0]["upd_remote"] = "new", "aaaa11112222", "bbbb33334444"
	cont[1]["upd"] = "current"
	cont[2]["upd"], cont[2]["upd_local"], cont[2]["upd_remote"] = "new", "cccc11112222", "dddd33334444"
	cont[3]["upd"], cont[4]["upd"] = "current", "current"
	imgs := []map[string]any{{"id": "111111111111", "repo_tags": []string{"nginx:1.25"}, "size": 190 << 20, "created": time.Now().Add(-100 * 24 * time.Hour).Unix(), "used": true}, {"id": "222222222222", "repo_tags": []string{"postgres:16"}, "size": 430 << 20, "created": time.Now().Add(-60 * 24 * time.Hour).Unix(), "used": true}, {"id": "444444444444", "repo_tags": []string{"redis:7"}, "size": 117 << 20, "created": time.Now().Add(-40 * 24 * time.Hour).Unix(), "used": true}, {"id": "555555555555", "repo_tags": []string{"grafana/grafana:10.4.2"}, "size": 398 << 20, "created": time.Now().Add(-30 * 24 * time.Hour).Unix(), "used": true}, {"id": "666666666666", "repo_tags": []string{"registry.example.com/shop/api:2.4.1"}, "size": 241 << 20, "created": time.Now().Add(-9 * 24 * time.Hour).Unix(), "used": true}, {"id": "333333333333", "repo_tags": []string{}, "size": 80 << 20, "created": time.Now().Add(-200 * 24 * time.Hour).Unix(), "used": false}}
	return map[string]any{"available": true, "containers": cont, "images": imgs, "checked_at": time.Now().Add(-time.Hour).Unix()}
}

func simDisk() diskState {
	return diskState{Docker: true, Filesystems: []DiskFS{
		{Mount: "/", Device: "/dev/sda1", FS: "ext4", Total: 100 << 30, Used: 41 << 30, Avail: 59 << 30, InodesTotal: 6500000, InodesUsed: 410000, Pct: 41, PerDay: 300 << 20, DaysLeft: 190, HistoryDays: 14, Trend: []uint64{38 << 30, 39 << 30, 39 << 30, 40 << 30, 40 << 30, 41 << 30}},
		{Mount: "/data", Device: "/dev/sdb1", FS: "xfs", Total: 500 << 30, Used: 410 << 30, Avail: 90 << 30, InodesTotal: 100000000, InodesUsed: 9000000, Pct: 82, PerDay: 2 << 30, DaysLeft: 9, HistoryDays: 14, Trend: []uint64{380 << 30, 390 << 30, 396 << 30, 402 << 30, 406 << 30, 410 << 30}},
		{Mount: "/boot", Device: "/dev/sda15", FS: "vfat", Total: 512 << 20, Used: 160 << 20, Avail: 352 << 20, Pct: 31, Trend: []uint64{}},
	}, Cleanup: []cleanItem{{Kind: "journal", Size: 900 << 20, Frees: 400 << 20, Count: 1}, {Kind: "apt", Size: 350 << 20, Frees: 350 << 20, Count: 1}, {Kind: "docker-images", Size: 80 << 20, Frees: 80 << 20, Count: 1, Items: []string{"<none>"}}, {Kind: "kernels", Size: 600 << 20, Frees: 600 << 20, Count: 2, Items: []string{"linux-image-6.1.0-10", "linux-image-6.1.0-11"}, Keep: "6.1.0-13"}}}
}

func simScan() diskScan {
	n := func(name string, size uint64, ch ...*DiskNode) *DiskNode {
		return &DiskNode{Name: name, Size: size << 20, Children: ch}
	}
	tree := n("/", 41000, n("var", 18000, n("lib", 14000, n("docker", 9000), n("postgresql", 4000)), n("log", 3000)), n("usr", 9000), n("home", 7000, n("deploy", 6500)), n("opt", 4000), n("srv", 2500))
	return diskScan{At: time.Now().Unix(), Mount: "/", Tree: tree, Categories: []DiskCategory{{ID: "system", Size: 9000 << 20}, {ID: "apps", Size: 4000 << 20}, {ID: "user", Size: 7000 << 20}, {ID: "logs", Size: 3000 << 20}, {ID: "other", Size: 18000 << 20}}}
}

func simUsers() usersState {
	now := time.Now()
	u := func(name string, uid int, gecos, sh string, groups []string, sudo string, human bool) userRow {
		return userRow{Name: name, UID: uid, GID: uid, Gecos: gecos, Home: "/home/" + name, Shell: sh, Groups: groups, Sudo: sudo, HasPassword: human, PwChanged: now.Add(-30 * 24 * time.Hour).Unix(), LastLogin: now.Add(-time.Duration(uid) * time.Hour).Unix(), LastFrom: "203.0.113.5", Human: human, NoShell: sh == "/usr/sbin/nologin", Protected: uid == 0, Keys: 1}
	}
	rows := []userRow{
		u("root", 0, "root", "/bin/bash", []string{"root"}, "root", true),
		u("deploy", 1000, "Deploy user", "/bin/bash", []string{"deploy", "sudo", "docker"}, "nopw", true),
		u("alice", 1001, "Alice Example, Room 12, +1 555 0100, a very long description of the account for wrapping", "/bin/bash", []string{"alice", "sudo"}, "pw", true),
		u("bob", 1002, "Bob", "/bin/zsh", []string{"bob", "developers"}, "none", true),
		u("www-data", 33, "www-data", "/usr/sbin/nologin", []string{"www-data"}, "none", false),
		u("postgres", 107, "PostgreSQL administrator", "/bin/bash", []string{"postgres", "ssl-cert"}, "none", false),
		u("redis", 108, "", "/usr/sbin/nologin", []string{"redis"}, "none", false),
	}
	rows[3].Locked = true
	for i := range rows {
		rows[i].Sessions, rows[i].Procs = i%2, 3+i*4
	}
	return usersState{Users: rows, Groups: []string{"sudo", "docker", "developers", "ssl-cert", "www-data"}, SudoGroup: "sudo", RootLogin: true, Shells: []string{"/bin/bash", "/bin/zsh", "/bin/sh", "/usr/sbin/nologin"}}
}

var _ = fmt.Sprint

// simDockerConfig answers the editor of a container: "web" is a compose container, the others are plain.
func simDockerConfig(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct{ ID string }
	_ = json.Unmarshal(raw, &p)
	cfg := editConfig{Running: true, editSpec: editSpec{Name: "db", Image: "postgres:16", Restart: "unless-stopped", MemMB: 1024, CPUs: 2,
		Cmd:    "postgres -c max_connections=200",
		Ports:  []editPort{{IP: "127.0.0.1", Host: 5432, Cont: 5432, Proto: "tcp"}},
		Mounts: []editMount{{Type: "volume", Src: "pgdata", Dst: "/var/lib/postgresql/data"}, {Type: "bind", Src: "/srv/backup", Dst: "/backup", RO: true}},
		Env:    []editEnv{{K: "POSTGRES_USER", V: "app"}, {K: "POSTGRES_PASSWORD", V: "s3cret-pass"}, {K: "TZ", V: "UTC"}}}}
	switch p.ID {
	case "web", "a1b2c3d4e5f6":
		cfg.Name, cfg.Image, cfg.Cmd, cfg.MemMB, cfg.CPUs = "web", "nginx:1.25", "", 0, 0
		cfg.Ports = []editPort{{IP: "0.0.0.0", Host: 8080, Cont: 80, Proto: "tcp"}}
		cfg.Mounts = []editMount{}
		cfg.Env = []editEnv{{K: "NGINX_HOST", V: "example.lan"}}
		cfg.Compose = &editCompose{Project: "shop", File: "/opt/shop/docker-compose.yml"}
	case "cache", "c1b2c3d4e5f6":
		cfg.Name, cfg.Image, cfg.Cmd, cfg.MemMB, cfg.CPUs, cfg.Running = "cache", "redis:7", "redis-server --save '' --appendonly no", 128, 0, false
		cfg.Ports, cfg.Mounts, cfg.Env = []editPort{}, []editMount{}, []editEnv{}
	}
	return cfg, nil
}

// simDockerEdit shows a log like the real agent does.
func simDockerEdit(ctx context.Context, raw json.RawMessage, io *StreamIO) error {
	var p struct{ Spec editSpec }
	_ = json.Unmarshal(raw, &p)
	for _, l := range []string{"Changes: (simulator)", "Stopping " + p.Spec.Name, "Creating the new container", "Starting the new container", "Done: the simulator changed nothing."} {
		if err := io.Out([]byte(l + "\n")); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(400 * time.Millisecond):
		}
	}
	return nil
}
