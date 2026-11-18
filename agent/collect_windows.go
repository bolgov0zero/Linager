//go:build windows

package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/bolgov0zero/linager/internal/protocol"
)

type winCollector struct {
	mu       sync.Mutex
	prevNet  map[string]ifCounters
	warned   map[string]bool
	prevTime time.Time

	slow struct { // values refreshed outside the 5 s loop
		sync.Mutex
		failed  int
		failedN []string
	}
}

// NewCollector returns the collector of a Windows host and starts the refresh of the slow values.
func NewCollector(ctx context.Context) Collector {
	c := &winCollector{}
	go c.refreshSlow(ctx)
	return c
}

func (c *winCollector) refreshSlow(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	upd := time.NewTimer(3 * time.Minute) // the first search of Windows Update waits, the start of the service is busy enough
	defer upd.Stop()
	c.updateFailed()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.updateFailed()
		case <-upd.C:
			refreshUpdateCounts(ctx)
			upd.Reset(time.Hour)
		}
	}
}

func (c *winCollector) updateFailed() {
	names := failedServices()
	c.slow.Lock()
	c.slow.failed, c.slow.failedN = len(names), names
	c.slow.Unlock()
}

// regString and regDWORD read one value of HKLM.
func regString(path, name string) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer func() { _ = k.Close() }()
	v, _, _ := k.GetStringValue(name)
	return v
}

func regDWORD(path, name string) (uint64, bool) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		return 0, false
	}
	defer func() { _ = k.Close() }()
	v, _, err := k.GetIntegerValue(name)
	return v, err == nil
}

const ntVersionKey = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`

// kernelVersion is the Windows version number, for example "10.0.20348.2113".
func kernelVersion() string {
	major, ok1 := regDWORD(ntVersionKey, "CurrentMajorVersionNumber")
	minor, ok2 := regDWORD(ntVersionKey, "CurrentMinorVersionNumber")
	build := regString(ntVersionKey, "CurrentBuild")
	if !ok1 || !ok2 || build == "" {
		return build
	}
	if ubr, ok := regDWORD(ntVersionKey, "UBR"); ok {
		return fmt.Sprintf("%d.%d.%s.%d", major, minor, build, ubr)
	}
	return fmt.Sprintf("%d.%d.%s", major, minor, build)
}

// rebootPending reports the markers Windows leaves when an update waits for a restart.
func rebootPending() bool {
	for _, path := range []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired`,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending`,
	} {
		if k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE); err == nil {
			_ = k.Close()
			return true
		}
	}
	return false
}

func (c *winCollector) System(agentVersion string) protocol.SystemInfo {
	var total uint64
	if vm, err := mem.VirtualMemory(); err == nil {
		total = vm.Total
	}
	model := strings.Join(strings.Fields(regString(`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "ProcessorNameString")), " ")
	return protocol.SystemInfo{
		Hostname: hostname(), OS: osName(), Kernel: kernelVersion(), Arch: runtime.GOARCH,
		CPUModel: model, CPUCores: runtime.NumCPU(), MemTotal: total, AgentVersion: agentVersion,
	}
}

func (c *winCollector) Collect(now time.Time) protocol.Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := protocol.Sample{TS: now.Unix(), Updates: -1, ImageUpdates: -1}

	if p, err := cpu.Percent(0, false); err == nil && len(p) > 0 {
		s.CPU = p[0] // the usage since the previous call; the first call (priming) returns 0
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		s.MemTotal, s.MemUsed, s.MemPct = vm.Total, vm.Used, vm.UsedPercent
	}
	if sw, err := mem.SwapMemory(); err == nil {
		s.SwapTotal, s.SwapUsed = sw.Total, sw.Used
	}

	s.Disks = winDisks()
	for _, d := range s.Disks {
		if d.Pct > s.DiskPct {
			s.DiskPct = d.Pct
		}
	}
	sort.Slice(s.Disks, func(i, j int) bool { return s.Disks[i].Mount < s.Disks[j].Mount })

	s.Net = c.collectNet(now)
	for _, n := range s.Net {
		if isPhysicalIface(n.Iface) {
			s.RxBps += n.RxBps
			s.TxBps += n.TxBps
		}
	}

	if up, err := host.Uptime(); err == nil {
		s.Uptime = int64(up) //nolint:gosec // seconds since boot fit easily
		s.BootTime = now.Unix() - s.Uptime
	}
	if pids, err := process.Pids(); err == nil {
		s.Procs = len(pids)
	}
	s.RebootRequired = rebootPending()
	s.Updates, s.SecurityUpdates = updateCounts()

	c.slow.Lock()
	s.FailedServices = c.slow.failed
	s.FailedUnits = append([]string(nil), c.slow.failedN...)
	c.slow.Unlock()
	return s
}

// winDisks lists the fixed and removable volumes that have a file system.
func winDisks() []protocol.Disk {
	parts, err := disk.Partitions(false)
	if err != nil {
		return nil
	}
	var out []protocol.Disk
	seen := map[string]bool{}
	for _, p := range parts {
		if p.Fstype == "" || seen[p.Mountpoint] || slices.Contains(p.Opts, "cdrom") {
			continue
		}
		seen[p.Mountpoint] = true
		u, err := disk.Usage(p.Mountpoint)
		if err != nil || u.Total == 0 {
			continue
		}
		mount := strings.TrimRight(p.Mountpoint, `\`)
		out = append(out, protocol.Disk{Mount: mount, Device: p.Device, FS: p.Fstype, Total: u.Total, Used: u.Used, Pct: u.UsedPercent})
	}
	return out
}

// virtualIfaceNames are adapters of virtual switches and tunnels: they do not count in the total traffic.
var virtualIfaceNames = []string{"loopback", "vethernet", "virtual", "vmware", "vmnet", "hyper-v", "bluetooth", "isatap", "teredo", "wan miniport", "wsl", "docker", "tunnel"}

func isPhysicalIface(name string) bool {
	l := strings.ToLower(name)
	for _, v := range virtualIfaceNames {
		if strings.Contains(l, v) {
			return false
		}
	}
	return true
}

// ifCounters are the byte counters and the speed of one adapter, as Windows keeps them.
type ifCounters struct {
	rx, tx    uint64
	speedMbps int
}

// octets picks the byte counters of an adapter row. Some drivers (for example the emulated Intel PRO/1000 of
// a hypervisor) leave the total in InOctets / OutOctets empty and report only the parts, so the larger of the total
// and the sum of the parts is taken.
func octets(r *windows.MibIfRow2) (rx, tx uint64) {
	rx = max(r.InOctets, r.InUcastOctets+r.InMulticastOctets+r.InBroadcastOctets)
	tx = max(r.OutOctets, r.OutUcastOctets+r.OutMulticastOctets+r.OutBroadcastOctets)
	return rx, tx
}

func rowCounters(r *windows.MibIfRow2) ifCounters {
	rx, tx := octets(r)
	c := ifCounters{rx: rx, tx: tx}
	if sp := r.ReceiveLinkSpeed; sp > 0 && sp != ^uint64(0) {
		c.speedMbps = int(sp / 1_000_000) //nolint:gosec // a link speed in Mbit/s fits an int
	}
	return c
}

// readIfTable reads the counters of every adapter in one call, by interface index and by name (the alias that
// net.Interfaces reports). One table is more robust than one call per adapter: an adapter that cannot be queried
// alone is still in the table. level is MibIfTableNormal or MibIfTableRaw (the numbers as the driver reports them).
func readIfTable(level uint32) (byIndex map[int]ifCounters, byName map[string]ifCounters, err error) {
	var t *windows.MibIfTable2
	if err := windows.GetIfTable2Ex(level, &t); err != nil || t == nil {
		if err == nil {
			err = errors.New("no table")
		}
		return nil, nil, err
	}
	defer windows.FreeMibTable(unsafe.Pointer(t))
	rows := unsafe.Slice(&t.Table[0], int(t.NumEntries))
	byIndex, byName = make(map[int]ifCounters, len(rows)), make(map[string]ifCounters, len(rows))
	for i := range rows {
		c := rowCounters(&rows[i])
		byIndex[int(rows[i].InterfaceIndex)] = c
		byName[windows.UTF16ToString(rows[i].Alias[:])] = c
	}
	return byIndex, byName, nil
}

// legacyCounters reads the old 32-bit counters (they wrap at 4 GB; used only when the others are empty).
func legacyCounters(index int) (ifCounters, bool) {
	row := windows.MibIfRow{Index: uint32(index)} //nolint:gosec // an interface index is small and positive
	if err := windows.GetIfEntry(&row); err != nil {
		return ifCounters{}, false
	}
	return ifCounters{rx: uint64(row.InOctets), tx: uint64(row.OutOctets)}, true
}

// bigger merges two readings of one adapter: the larger counter of each pair, the speed that is known.
func bigger(a, b ifCounters) ifCounters {
	a.rx, a.tx = max(a.rx, b.rx), max(a.tx, b.tx)
	if a.speedMbps == 0 {
		a.speedMbps = b.speedMbps
	}
	return a
}

// readIfCounters asks Windows for the counters of one adapter (used when the table is not available).
func readIfCounters(index int) (ifCounters, bool) {
	row := windows.MibIfRow2{InterfaceIndex: uint32(index)} //nolint:gosec // an interface index is small and positive
	if err := windows.GetIfEntry2Ex(windows.MibIfTableNormal, &row); err != nil {
		return ifCounters{}, false
	}
	return rowCounters(&row), true
}

func (c *winCollector) collectNet(now time.Time) []protocol.NetIface {
	dt := now.Sub(c.prevTime).Seconds()
	ifaces, _ := net.Interfaces()
	cur := make(map[string]ifCounters, len(ifaces))
	byIndex, byName, terr := readIfTable(windows.MibIfTableNormal)
	if byIndex == nil {
		byIndex, byName = map[int]ifCounters{}, map[string]ifCounters{}
	}
	if rawIdx, rawName, rerr := readIfTable(windows.MibIfTableRaw); rerr == nil {
		for k, v := range rawIdx {
			byIndex[k] = bigger(byIndex[k], v)
		}
		for k, v := range rawName {
			byName[k] = bigger(byName[k], v)
		}
	}
	if terr != nil {
		c.warnOnce("table", "network counters: the adapter table is not available, asking adapter by adapter", "err", terr)
	}
	var out []protocol.NetIface
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
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
		cc, ok := byIndex[ifc.Index]
		if !ok {
			cc, ok = byName[ifc.Name]
		}
		if !ok {
			cc, ok = readIfCounters(ifc.Index)
		}
		if ok && cc.rx == 0 && cc.tx == 0 {
			if lc, lok := legacyCounters(ifc.Index); lok {
				cc = bigger(cc, lc)
			}
		}
		if ok && n.Up && n.IP != "" && cc.rx == 0 && cc.tx == 0 {
			c.warnOnce(ifc.Name+"/zero", "network counters: the driver reports no bytes for an adapter that is up", "adapter", ifc.Name, "index", ifc.Index)
		}
		if !ok && n.Up && n.IP != "" {
			c.warnOnce(ifc.Name, "network counters: no counters for an adapter that is up", "adapter", ifc.Name, "index", ifc.Index)
		}
		if ok {
			cur[ifc.Name] = cc
			n.RxTotal, n.TxTotal, n.SpeedMbps = cc.rx, cc.tx, cc.speedMbps
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

// warnOnce writes a problem to the log of the agent the first time it is seen.
func (c *winCollector) warnOnce(key, msg string, args ...any) {
	if c.warned == nil {
		c.warned = map[string]bool{}
	}
	if !c.warned[key] {
		c.warned[key] = true
		slog.Warn(msg, args...)
	}
}
