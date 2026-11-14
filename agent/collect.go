package agent

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/bolgov0zero/linager/internal/protocol"
)

// Collector produces host samples. Implementations must be safe for sequential use.
type Collector interface {
	Collect(now time.Time) protocol.Sample
	System(agentVersion string) protocol.SystemInfo
}

// ---- parsing helpers (pure, unit-tested) ----

// parseCPU returns busy and total jiffies from the aggregate "cpu " line of /proc/stat.
func parseCPU(stat string) (busy, total uint64, ok bool) {
	for _, line := range strings.Split(stat, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var v []uint64
		for _, s := range f[1:] {
			n, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			v = append(v, n)
		}
		for _, n := range v[:min(len(v), 8)] { // user nice system idle iowait irq softirq steal
			total += n
		}
		idle := v[3]
		if len(v) > 4 {
			idle += v[4]
		}
		return total - idle, total, true
	}
	return 0, 0, false
}

type memInfo struct{ total, avail, swapTotal, swapFree uint64 }

func parseMeminfo(s string) memInfo {
	var m memInfo
	for _, line := range strings.Split(s, "\n") {
		k, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		n, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		n *= 1024
		switch k {
		case "MemTotal":
			m.total = n
		case "MemAvailable":
			m.avail = n
		case "SwapTotal":
			m.swapTotal = n
		case "SwapFree":
			m.swapFree = n
		}
	}
	return m
}

type netCounters struct{ rx, tx uint64 }

func parseNetDev(s string) map[string]netCounters {
	out := map[string]netCounters{}
	for _, line := range strings.Split(s, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		rx, e1 := strconv.ParseUint(f[0], 10, 64)
		tx, e2 := strconv.ParseUint(f[8], 10, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		out[strings.TrimSpace(name)] = netCounters{rx, tx}
	}
	return out
}

type mount struct{ device, point, fs string }

var realFS = map[string]bool{"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "zfs": true, "f2fs": true, "jfs": true, "reiserfs": true, "ntfs": true, "ntfs3": true, "exfat": true}

func parseMounts(s string) []mount {
	var out []mount
	seen := map[string]bool{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !realFS[f[2]] || seen[f[0]] {
			continue
		}
		seen[f[0]] = true
		out = append(out, mount{device: f[0], point: strings.ReplaceAll(f[1], `\040`, " "), fs: f[2]})
	}
	return out
}

func virtualIface(name string) bool {
	for _, p := range []string{"lo", "veth", "docker", "br-", "virbr", "tun", "tap", "wg", "flannel", "cni", "cali"} {
		if name == p || strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// countAptInst counts upgradable packages from `apt-get -s upgrade` output.
func countAptInst(out string) (total, security int) {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Inst ") {
			total++
			if strings.Contains(line, "-security") {
				security++
			}
		}
	}
	return
}

// ---- Linux collector ----

type linuxCollector struct {
	mu        sync.Mutex
	prevBusy  uint64
	prevTotal uint64
	prevNet   map[string]netCounters
	prevTime  time.Time

	slow struct { // values refreshed outside the 5 s loop
		sync.Mutex
		failed   int
		updates  int
		security int
	}
}

// NewCollector returns the /proc-based collector and starts background refreshers
// for the slow-changing values (failed services, apt updates).
func NewCollector(ctx context.Context) Collector {
	c := &linuxCollector{}
	c.slow.updates = -1
	go c.refreshSlow(ctx)
	return c
}

func read(path string) string {
	b, err := os.ReadFile(path) //nolint:gosec // fixed /proc and /sys paths
	if err != nil {
		return ""
	}
	return string(b)
}

func (c *linuxCollector) refreshSlow(ctx context.Context) {
	failedT := time.NewTicker(30 * time.Second)
	updatesT := time.NewTicker(time.Hour)
	defer failedT.Stop()
	defer updatesT.Stop()
	c.updateFailed(ctx)
	c.updateAptCount(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-failedT.C:
			c.updateFailed(ctx)
		case <-updatesT.C:
			c.updateAptCount(ctx)
		}
	}
}

func (c *linuxCollector) updateFailed(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "systemctl", "list-units", "--state=failed", "--no-legend", "--plain").Output() //nolint:gosec // fixed command
	if err != nil {
		return
	}
	n := 0
	for _, l := range bytes.Split(out, []byte("\n")) {
		if len(bytes.TrimSpace(l)) > 0 {
			n++
		}
	}
	c.slow.Lock()
	c.slow.failed = n
	c.slow.Unlock()
}

func (c *linuxCollector) updateAptCount(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, "apt-get", "-s", "-o", "Debug::NoLocking=1", "upgrade") //nolint:gosec // fixed command
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return
	}
	t, s := countAptInst(string(out))
	c.slow.Lock()
	c.slow.updates, c.slow.security = t, s
	c.slow.Unlock()
}

func (c *linuxCollector) System(agentVersion string) protocol.SystemInfo {
	model := ""
	for _, l := range strings.Split(read("/proc/cpuinfo"), "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "model name" {
			model = strings.TrimSpace(v)
			break
		}
	}
	return protocol.SystemInfo{
		Hostname: hostname(), OS: osName(), Kernel: strings.TrimSpace(read("/proc/sys/kernel/osrelease")), Arch: runtime.GOARCH,
		CPUModel: model, CPUCores: runtime.NumCPU(), MemTotal: parseMeminfo(read("/proc/meminfo")).total, AgentVersion: agentVersion,
	}
}

func (c *linuxCollector) Collect(now time.Time) protocol.Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := protocol.Sample{TS: now.Unix(), Updates: -1}

	if busy, total, ok := parseCPU(read("/proc/stat")); ok {
		if c.prevTotal > 0 && total > c.prevTotal {
			s.CPU = float64(busy-c.prevBusy) / float64(total-c.prevTotal) * 100
		}
		c.prevBusy, c.prevTotal = busy, total
	}

	m := parseMeminfo(read("/proc/meminfo"))
	s.MemTotal, s.SwapTotal = m.total, m.swapTotal
	if m.total > 0 {
		s.MemUsed = m.total - min(m.avail, m.total)
		s.MemPct = float64(s.MemUsed) / float64(m.total) * 100
	}
	s.SwapUsed = m.swapTotal - min(m.swapFree, m.swapTotal)

	for _, mt := range parseMounts(read("/proc/mounts")) {
		var st unix.Statfs_t
		if unix.Statfs(mt.point, &st) != nil || st.Blocks == 0 {
			continue
		}
		bs := uint64(st.Bsize) //nolint:gosec // block size is positive
		total := st.Blocks * bs
		used := (st.Blocks - st.Bfree) * bs
		avail := st.Bavail * bs
		pct := 0.0
		if used+avail > 0 {
			pct = float64(used) / float64(used+avail) * 100 // same basis as df
		}
		s.Disks = append(s.Disks, protocol.Disk{Mount: mt.point, Device: mt.device, FS: mt.fs, Total: total, Used: used, Pct: pct})
		if pct > s.DiskPct {
			s.DiskPct = pct
		}
	}
	sort.Slice(s.Disks, func(i, j int) bool { return len(s.Disks[i].Mount) < len(s.Disks[j].Mount) })

	s.Net = c.collectNet(now)
	for _, n := range s.Net {
		if isPhysical(n.Iface) {
			s.RxBps += n.RxBps
			s.TxBps += n.TxBps
		}
	}

	if f := strings.Fields(read("/proc/loadavg")); len(f) >= 4 {
		for i := 0; i < 3; i++ {
			s.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
		if _, tot, ok := strings.Cut(f[3], "/"); ok {
			s.Procs, _ = strconv.Atoi(tot)
		}
	}
	if f := strings.Fields(read("/proc/uptime")); len(f) >= 1 {
		up, _ := strconv.ParseFloat(f[0], 64)
		s.Uptime = int64(up)
		s.BootTime = now.Unix() - s.Uptime
	}
	_, err := os.Stat("/var/run/reboot-required")
	s.RebootRequired = err == nil

	c.slow.Lock()
	s.FailedServices, s.Updates, s.SecurityUpdates = c.slow.failed, c.slow.updates, c.slow.security
	c.slow.Unlock()
	return s
}

func isPhysical(name string) bool {
	if virtualIface(name) {
		return false
	}
	_, err := os.Stat(filepath.Join("/sys/class/net", name, "device"))
	return err == nil
}

func (c *linuxCollector) collectNet(now time.Time) []protocol.NetIface {
	cur := parseNetDev(read("/proc/net/dev"))
	dt := now.Sub(c.prevTime).Seconds()
	ifaces, _ := net.Interfaces()
	var out []protocol.NetIface
	for _, ifc := range ifaces {
		if ifc.Name == "lo" {
			continue
		}
		n := protocol.NetIface{Iface: ifc.Name, Up: ifc.Flags&net.FlagUp != 0, MAC: ifc.HardwareAddr.String()}
		if addrs, err := ifc.Addrs(); err == nil {
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
					n.IP = ipn.IP.String()
					break
				}
			}
		}
		if sp, err := strconv.Atoi(strings.TrimSpace(read(filepath.Join("/sys/class/net", ifc.Name, "speed")))); err == nil && sp > 0 {
			n.SpeedMbps = sp
		}
		if cc, ok := cur[ifc.Name]; ok {
			n.RxTotal, n.TxTotal = cc.rx, cc.tx
			if p, ok := c.prevNet[ifc.Name]; ok && dt > 0 && cc.rx >= p.rx && cc.tx >= p.tx {
				n.RxBps = float64(cc.rx-p.rx) / dt
				n.TxBps = float64(cc.tx-p.tx) / dt
			}
		}
		out = append(out, n)
	}
	c.prevNet, c.prevTime = cur, now
	return out
}

// RefreshUpdates recomputes the upgradable package count right away (after an apt run).
func (c *linuxCollector) RefreshUpdates() { c.updateAptCount(context.Background()) }
