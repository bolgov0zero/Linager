package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type cmdRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // callers pass fixed commands and validated arguments
	return cmd.CombinedOutput()
}

type netModule struct {
	pend   *Pending
	dir    string // agent data dir (candidate files, backups)
	ifPath string
	npDir  string // netplan directory
	run    cmdRunner
	// DNS editing: the files and the stdin-capable runner, replaced by tests.
	resolvPath string // /etc/resolv.conf
	resolvedD  string // /etc/systemd/resolved.conf.d
	runStdin   stdinRunner
}

func newNetModule(p *Pending, dir string) *netModule {
	return &netModule{pend: p, dir: dir, ifPath: "/etc/network/interfaces", npDir: "/etc/netplan", run: execRunner,
		resolvPath: "/etc/resolv.conf", resolvedD: "/etc/systemd/resolved.conf.d", runStdin: execStdinRunner}
}

type ipAddrInfo struct {
	Family    string `json:"family"`
	Local     string `json:"local"`
	PrefixLen int    `json:"prefixlen"`
}
type ipLink struct {
	Ifname    string       `json:"ifname"`
	Flags     []string     `json:"flags"`
	MTU       int          `json:"mtu"`
	Operstate string       `json:"operstate"`
	Address   string       `json:"address"`
	AddrInfo  []ipAddrInfo `json:"addr_info"`
}
type ipRoute struct {
	Dst      string `json:"dst"`
	Gateway  string `json:"gateway"`
	Dev      string `json:"dev"`
	Protocol string `json:"protocol"`
	Metric   int    `json:"metric"`
}

// NetIface is one interface as shown in the panel.
type NetIface struct {
	Name      string   `json:"name"`
	State     string   `json:"state"`
	Kind      string   `json:"kind"`
	MAC       string   `json:"mac"`
	MTU       int      `json:"mtu"`
	Mode      string   `json:"mode"`
	Addresses []string `json:"addresses"`
	Gateway   string   `json:"gateway"`
	SpeedMbps int      `json:"speed_mbps"`
	RxBytes   uint64   `json:"rx_bytes"`
	TxBytes   uint64   `json:"tx_bytes"`
	RxPackets uint64   `json:"rx_packets"`
	TxPackets uint64   `json:"tx_packets"`
	RxErrors  uint64   `json:"rx_errors"`
	TxErrors  uint64   `json:"tx_errors"`
	RxDropped uint64   `json:"rx_dropped"`
	TxDropped uint64   `json:"tx_dropped"`
	Primary   bool     `json:"primary"`
	Editable  bool     `json:"editable"`
}

func (m *netModule) backend() string {
	if m, _ := filepath.Glob("/etc/netplan/*.yaml"); len(m) > 0 {
		if _, err := exec.LookPath("netplan"); err == nil {
			return "netplan"
		}
	}
	if _, err := os.Stat(m.ifPath); err == nil {
		if _, err := exec.LookPath("ifup"); err == nil {
			return "ifupdown"
		}
	}
	return "unknown"
}

func sysUint(path string) uint64 {
	n, _ := strconv.ParseUint(strings.TrimSpace(read(path)), 10, 64)
	return n
}

// includedFiles returns the files pulled in by `source`/`source-directory` lines.
func includedFiles(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "source":
			for _, g := range f[1:] {
				if !filepath.IsAbs(g) {
					g = filepath.Join("/etc/network", g)
				}
				m, _ := filepath.Glob(g)
				out = append(out, m...)
			}
		case "source-directory":
			if ents, err := os.ReadDir(f[1]); err == nil {
				for _, e := range ents {
					out = append(out, filepath.Join(f[1], e.Name()))
				}
			}
		}
	}
	return out
}

func definedInIncludes(text, iface string) bool {
	for _, p := range includedFiles(text) {
		for _, b := range parseIfaceBlocks(strings.Split(read(p), "\n")) {
			if b.name == iface {
				return true
			}
		}
	}
	return false
}

func (m *netModule) state(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	backend := m.backend()
	var links []ipLink
	if out, err := m.run(ctx, "ip", "-j", "addr", "show"); err == nil {
		_ = json.Unmarshal(out, &links)
	}
	var routes []ipRoute
	if out, err := m.run(ctx, "ip", "-j", "route", "show"); err == nil {
		_ = json.Unmarshal(out, &routes)
	}
	defDev, defGW := "", ""
	for _, r := range routes {
		if r.Dst == "default" && (defDev == "" || r.Metric < 0) {
			defDev, defGW = r.Dev, r.Gateway
		}
	}
	text := ""
	var blocks []*ifaceBlock
	if backend == "ifupdown" {
		text = read(m.ifPath)
		blocks = parseIfaceBlocks(strings.Split(text, "\n"))
	}
	ifs := make([]NetIface, 0, len(links))
	for _, l := range links {
		n := NetIface{Name: l.Ifname, MAC: l.Address, MTU: l.MTU, State: "DOWN", Mode: "unknown", Addresses: []string{}}
		up := false
		for _, f := range l.Flags {
			if f == "UP" {
				up = true
			}
		}
		if up && l.Operstate != "DOWN" {
			n.State = "UP"
		}
		for _, a := range l.AddrInfo {
			if a.Family == "inet" {
				n.Addresses = append(n.Addresses, fmt.Sprintf("%s/%d", a.Local, a.PrefixLen))
			}
		}
		base := filepath.Join("/sys/class/net", l.Ifname)
		if _, err := os.Stat(filepath.Join(base, "device")); err == nil {
			n.Kind = "physical"
		} else if l.Ifname == "lo" {
			n.Kind = "loopback"
		} else {
			n.Kind = "virtual"
		}
		if sp, err := strconv.Atoi(strings.TrimSpace(read(filepath.Join(base, "speed")))); err == nil && sp > 0 {
			n.SpeedMbps = sp
		}
		st := filepath.Join(base, "statistics")
		n.RxBytes, n.TxBytes = sysUint(st+"/rx_bytes"), sysUint(st+"/tx_bytes")
		n.RxPackets, n.TxPackets = sysUint(st+"/rx_packets"), sysUint(st+"/tx_packets")
		n.RxErrors, n.TxErrors = sysUint(st+"/rx_errors"), sysUint(st+"/tx_errors")
		n.RxDropped, n.TxDropped = sysUint(st+"/rx_dropped"), sysUint(st+"/tx_dropped")
		if l.Ifname == defDev {
			n.Primary, n.Gateway = true, defGW
		}
		for _, b := range blocks {
			if b.name == l.Ifname && b.family == "inet" {
				n.Mode = b.method
				for _, o := range b.opts {
					if f := strings.Fields(o); len(f) >= 2 && f[0] == "gateway" {
						n.Gateway = f[1]
					}
				}
			}
		}
		hasStanza := false
		for _, b := range blocks {
			hasStanza = hasStanza || (b.name == l.Ifname && b.family == "inet")
		}
		// physical NICs, and anything ifupdown already manages (containers have veth-based eth0)
		managed := n.Kind == "physical" || (hasStanza && !virtualIface(l.Ifname))
		n.Editable = backend == "ifupdown" && managed && l.Ifname != "lo" && !definedInIncludes(text, l.Ifname)
		ifs = append(ifs, n)
	}
	preview := text
	if backend == "netplan" {
		files, _ := filepath.Glob("/etc/netplan/*.yaml")
		sort.Strings(files)
		var sb strings.Builder
		for _, f := range files {
			sb.WriteString("# " + f + "\n" + read(f) + "\n")
		}
		preview = sb.String()
	}
	dnsBackend, dnsEditable := pickDNSBackend(m.dnsProbe(backend, defDev, text, blocks))
	dns := map[string][]string{"nameservers": {}, "search": {}}
	resolvText := read(m.resolvPath)
	if dnsBackend == "resolved" {
		// the stub file only names 127.0.0.53; the upstream list is what the administrator cares about
		if up := read(resolvedUpstreamPath); len(parseResolvConf(up).ns) > 0 {
			resolvText = up
		}
	}
	rc := parseResolvConf(resolvText)
	dns["nameservers"] = append(dns["nameservers"], rc.ns...)
	dns["search"] = append(dns["search"], rc.search...)
	rl := make([]map[string]any, 0, len(routes))
	for _, r := range routes {
		rl = append(rl, map[string]any{"dst": r.Dst, "gateway": r.Gateway, "dev": r.Dev, "metric": r.Metric, "protocol": r.Protocol})
	}
	st := map[string]any{"backend": backend, "editable": backend == "ifupdown", "config_editable": backend == "ifupdown" || backend == "netplan",
		"interfaces": ifs, "dns": dns, "dns_editable": dnsEditable, "dns_backend": dnsBackend, "routes": rl, "config_preview": preview}
	if backend == "netplan" {
		st["netplan_files"] = m.netplanFiles()
		st["netplan_effective"] = m.netplanEffective(ctx)
	}
	return st, nil
}

type netPendingData struct {
	Cand   string   `json:"cand"`
	Old    string   `json:"old"`
	Ifaces []string `json:"ifaces"`
	Up     []bool   `json:"up"`

	// netplan: one file replaced in place, the backup is restored on rollback
	Netplan   bool   `json:"netplan,omitempty"`
	NpPath    string `json:"np_path,omitempty"`
	NpBak     string `json:"np_bak,omitempty"`
	NpExisted bool   `json:"np_existed,omitempty"`

	// DNS change (network.dns): what to restore; Cand/Old are used only by the resolvconf backend
	DNS *dnsPending `json:"dns,omitempty"`
}

func validateNetChange(c NetChange) error {
	if c.Iface == "" || strings.ContainsAny(c.Iface, " /\t\n") || len(c.Iface) > 15 {
		return errors.New("invalid interface name")
	}
	if c.MTU != 0 && (c.MTU < 68 || c.MTU > 9000) {
		return errors.New("MTU must be between 68 and 9000")
	}
	switch c.Mode {
	case "dhcp":
	case "static":
		ip, nw, err := net.ParseCIDR(c.Address)
		if err != nil || ip.To4() == nil {
			return errors.New("address must be IPv4 in CIDR form")
		}
		if c.Gateway != "" {
			gw := net.ParseIP(c.Gateway)
			if gw == nil || gw.To4() == nil || !nw.Contains(gw) {
				return errors.New("gateway must be an IPv4 address inside the subnet")
			}
		}
	default:
		return errors.New("mode must be static or dhcp")
	}
	return nil
}

func (m *netModule) apply(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p struct {
		ID      string      `json:"id"`
		Changes []NetChange `json:"changes"`
		Raw     *string     `json:"raw"`  // the whole interfaces / netplan file, edited by hand
		File    string      `json:"file"` // netplan file name (netplan backend only)
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.ID == "" || (p.Raw == nil && len(p.Changes) == 0) {
		return nil, errors.New("invalid request")
	}
	backend := m.backend()
	if backend == "netplan" && p.Raw != nil && p.File != "" {
		return m.applyNetplan(ctx, p.ID, p.File, *p.Raw)
	}
	if backend != "ifupdown" {
		return nil, errors.New("network editing is not available on this system")
	}
	old := read(m.ifPath)
	if p.Raw != nil {
		return m.applyRaw(ctx, p.ID, old, *p.Raw)
	}
	for _, c := range p.Changes {
		if err := validateNetChange(c); err != nil {
			return nil, fmt.Errorf("%s: %w", c.Iface, err)
		}
		if definedInIncludes(old, c.Iface) {
			return nil, fmt.Errorf("%s is defined in an included file; edit it manually", c.Iface)
		}
	}
	cand, err := applyNetChanges(old, p.Changes)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return nil, err
	}
	candPath := filepath.Join(m.dir, "net-"+p.ID+".interfaces")
	if err := os.WriteFile(candPath, []byte(cand), 0o600); err != nil {
		return nil, err
	}
	data := netPendingData{Cand: candPath, Old: m.ifPath}
	for _, c := range p.Changes {
		data.Ifaces = append(data.Ifaces, c.Iface)
		data.Up = append(data.Up, c.Up)
	}
	deadline, err := m.pend.Begin(p.ID, "network", data, PendingWindow)
	if err != nil {
		_ = os.Remove(candPath)
		return nil, err
	}
	for i, ifc := range data.Ifaces {
		if err := m.reconfigure(ctx, data.Old, data.Cand, ifc, data.Up[i]); err != nil {
			m.pend.Abort(p.ID)
			return nil, err
		}
	}
	return map[string]any{"id": p.ID, "deadline": deadline}, nil
}

// applyRaw replaces the whole interfaces file with hand-edited text. Like the form-based change it goes through
// the pending flow: the changed interfaces are reconfigured with the candidate file and the old file comes back
// by itself unless the administrator confirms in time. The candidate is dry-run checked with ifup first.
func (m *netModule) applyRaw(ctx context.Context, id, old, text string) (any, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if strings.TrimSpace(text) == "" || len(text) > maxRawConfig || strings.ContainsRune(text, 0) {
		return nil, errors.New("the file is empty, too large or not text")
	}
	text = strings.TrimRight(text, "\n") + "\n"
	if text == strings.TrimRight(old, "\n")+"\n" {
		return nil, errors.New("nothing changed")
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return nil, err
	}
	candPath := filepath.Join(m.dir, "net-"+id+".interfaces")
	if err := os.WriteFile(candPath, []byte(text), 0o600); err != nil {
		return nil, err
	}
	ifaces, up := diffIfaces(old, text, func(n string) bool {
		return strings.TrimSpace(read("/sys/class/net/"+n+"/operstate")) == "up"
	})
	for i, ifc := range ifaces {
		if !up[i] {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		out, err := m.run(cctx, "ifup", "--no-act", "--force", "-i", candPath, ifc)
		cancel()
		if err != nil {
			_ = os.Remove(candPath)
			return nil, fmt.Errorf("%s: %s", ifc, lastLine(string(out), err.Error()))
		}
	}
	data := netPendingData{Cand: candPath, Old: m.ifPath, Ifaces: ifaces, Up: up}
	deadline, err := m.pend.Begin(id, "network", data, PendingWindow)
	if err != nil {
		_ = os.Remove(candPath)
		return nil, err
	}
	for i, ifc := range ifaces {
		if err := m.reconfigure(ctx, data.Old, data.Cand, ifc, up[i]); err != nil {
			m.pend.Abort(id)
			return nil, err
		}
	}
	return map[string]any{"id": id, "deadline": deadline}, nil
}

// reconfigure takes the interface down using the config it was brought up with and up with the new one.
func (m *netModule) reconfigure(ctx context.Context, from, to, iface string, up bool) error {
	cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	_, _ = m.run(cctx, "ifdown", "-i", from, iface)
	if !up {
		return nil
	}
	if out, err := m.run(cctx, "ifup", "-i", to, iface); err != nil {
		return fmt.Errorf("ifup %s failed: %s", iface, lastLine(string(out), err.Error()))
	}
	return nil
}

func (m *netModule) kind() PendingKind {
	return PendingKind{
		Rollback: func(raw json.RawMessage) error {
			var d netPendingData
			if err := json.Unmarshal(raw, &d); err != nil {
				return err
			}
			if d.Netplan {
				return m.netplanRollback(d)
			}
			if d.DNS != nil {
				return m.dnsRollback(d)
			}
			ctx := context.Background()
			var firstErr error
			for _, ifc := range d.Ifaces {
				cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
				_, _ = m.run(cctx, "ifdown", "-i", d.Cand, ifc)
				if out, err := m.run(cctx, "ifup", "-i", d.Old, ifc); err != nil && firstErr == nil {
					firstErr = fmt.Errorf("restoring %s: %s", ifc, lastLine(string(out), err.Error()))
				}
				cancel()
			}
			_ = os.Remove(d.Cand)
			return firstErr
		},
		Commit: func(raw json.RawMessage) error {
			var d netPendingData
			if err := json.Unmarshal(raw, &d); err != nil {
				return err
			}
			if d.Netplan {
				return m.netplanCommit(d)
			}
			if d.DNS != nil && d.DNS.Backend != dnsResolvconf {
				return nil // resolved and resolv.conf are already in place
			}
			cand, err := os.ReadFile(d.Cand) //nolint:gosec // our own candidate file
			if err != nil {
				return err
			}
			if old, err := os.ReadFile(d.Old); err == nil { //nolint:gosec // the interfaces file
				_ = os.WriteFile(filepath.Join(m.dir, "interfaces.bak-"+time.Now().Format("20060102-150405")), old, 0o600) //nolint:gosec // fixed configuration path
			}
			tmp := d.Old + ".linager-new"
			if err := os.WriteFile(tmp, cand, 0o644); err != nil { //nolint:gosec // interfaces(5) is world-readable
				return err
			}
			if err := os.Rename(tmp, d.Old); err != nil {
				return err
			}
			_ = os.Remove(d.Cand)
			return nil
		},
	}
}
