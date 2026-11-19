//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

// ProcInfo is one process row.
type ProcInfo struct {
	PID    int     `json:"pid"`
	User   string  `json:"user"`
	CPU    float64 `json:"cpu"`
	MemPct float64 `json:"mem_pct"`
	RSS    uint64  `json:"rss"`
	State  string  `json:"state"`
	Cmd    string  `json:"cmd"`
}

// procMaxRows caps the list; "all" in the UI asks for this many.
const procMaxRows = 2000

// clampProcRows returns the row limit: the default 10 for a missing value, the cap for a larger one.
func clampProcRows(n int) int {
	switch {
	case n < 1:
		return 10
	case n > procMaxRows:
		return procMaxRows
	}
	return n
}

// procName is the executable name of a command line: `"C:\Program Files\x\app.exe" -v` gives "app.exe".
func procName(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	exe := cmd
	if strings.HasPrefix(cmd, `"`) {
		if j := strings.Index(cmd[1:], `"`); j >= 0 {
			exe = cmd[1 : 1+j]
		}
	} else if i := strings.IndexAny(cmd, " \t"); i >= 0 {
		exe = cmd[:i]
	}
	exe = strings.ReplaceAll(exe, `\`, "/")
	if strings.Contains(exe, "/") {
		return path.Base(exe)
	}
	return exe
}

// sortProcesses orders the rows in place: by RAM, by command name (case-insensitive, then PID), or by CPU.
func sortProcesses(list []ProcInfo, by string) {
	switch by {
	case "mem":
		sort.Slice(list, func(i, j int) bool { return list[i].RSS > list[j].RSS })
	case "name":
		sort.Slice(list, func(i, j int) bool {
			a, b := strings.ToLower(procName(list[i].Cmd)), strings.ToLower(procName(list[j].Cmd))
			if a != b {
				return a < b
			}
			return list[i].PID < list[j].PID
		})
	default:
		sort.Slice(list, func(i, j int) bool { return list[i].CPU > list[j].CPU })
	}
}

// cpuSeconds reads the processor time of every process.
func cpuSeconds(procs []*process.Process) map[int32]float64 {
	out := make(map[int32]float64, len(procs))
	for _, p := range procs {
		if t, err := p.Times(); err == nil {
			out[p.Pid] = t.User + t.System
		}
	}
	return out
}

func procList(ctx context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct {
		Sort string
		N    int
	}
	_ = json.Unmarshal(raw, &p)
	p.N = clampProcRows(p.N)
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	t0 := time.Now()
	a := cpuSeconds(procs)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(400 * time.Millisecond):
	}
	b := cpuSeconds(procs)
	dt := time.Since(t0).Seconds()
	var total uint64
	if vm, err := mem.VirtualMemory(); err == nil {
		total = vm.Total
	}
	list := make([]ProcInfo, 0, len(procs))
	for _, pr := range procs {
		pi := ProcInfo{PID: int(pr.Pid), State: "R"}
		if name, err := pr.Name(); err == nil {
			pi.Cmd = name
		}
		if cl, err := pr.Cmdline(); err == nil && cl != "" {
			pi.Cmd = cl
		}
		if pi.Cmd == "" {
			pi.Cmd = "[" + pathBase(pr.Pid) + "]"
		}
		if u, err := pr.Username(); err == nil {
			pi.User = u
		}
		if mi, err := pr.MemoryInfo(); err == nil && mi != nil {
			pi.RSS = mi.RSS
			if total > 0 {
				pi.MemPct = float64(mi.RSS) / float64(total) * 100
			}
		}
		if x, ok := a[pr.Pid]; ok {
			if y, ok := b[pr.Pid]; ok && y >= x && dt > 0 {
				pi.CPU = (y - x) / dt * 100 // percent of one core, like top
			}
		}
		list = append(list, pi)
	}
	sortProcesses(list, p.Sort)
	n := len(list)
	if len(list) > p.N {
		list = list[:p.N]
	}
	return map[string]any{"processes": list, "total": n}, nil
}

func pathBase(pid int32) string {
	switch pid {
	case 0:
		return "System Idle Process"
	case 4:
		return "System"
	}
	return "unknown"
}

// criticalProcesses are the processes whose end stops Windows with a blue screen.
var criticalProcesses = map[string]bool{
	"csrss.exe": true, "wininit.exe": true, "winlogon.exe": true, "services.exe": true, "lsass.exe": true, "smss.exe": true, "system": true,
}

// procKill ends a process. "TERM" ends that process only; "KILL" ends it together with every process it started (the tree),
// like taskkill /F /T. Windows has no softer signal that a service can send into the session of the person.
func procKill(ctx context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct {
		PID    int
		Signal string
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	switch p.Signal {
	case "", "TERM", "KILL":
	default:
		return nil, errors.New("unsupported signal")
	}
	if p.PID <= 4 || p.PID == os.Getpid() {
		return nil, errors.New("refusing to signal this process")
	}
	pr, err := process.NewProcessWithContext(ctx, int32(p.PID)) //nolint:gosec // a PID fits int32
	if err != nil {
		return nil, errors.New("no such process")
	}
	if name, err := pr.NameWithContext(ctx); err == nil && criticalProcesses[strings.ToLower(name)] {
		return nil, errors.New("refusing to end a system process")
	}
	ended := 0
	if p.Signal == "KILL" {
		ended = killTree(ctx, pr.Pid)
	}
	if err := pr.KillWithContext(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "children": ended}, nil
}

// killTree ends the processes below root (the deepest first) and returns how many it ended; the ones it must not touch
// (the agent itself, the system processes) and the ones that refuse are skipped.
func killTree(ctx context.Context, root int32) int {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return 0
	}
	parent := make(map[int32]int32, len(procs))
	created := make(map[int32]int64, len(procs))
	byPID := make(map[int32]*process.Process, len(procs))
	for _, pr := range procs {
		byPID[pr.Pid] = pr
		if pp, err := pr.PpidWithContext(ctx); err == nil {
			parent[pr.Pid] = pp
		}
		if ct, err := pr.CreateTimeWithContext(ctx); err == nil {
			created[pr.Pid] = ct
		}
	}
	n := 0
	for _, pid := range processDescendants(parent, created, root) {
		if pid <= 4 || int(pid) == os.Getpid() {
			continue
		}
		pr := byPID[pid]
		if name, err := pr.NameWithContext(ctx); err == nil && criticalProcesses[strings.ToLower(name)] {
			continue
		}
		if pr.KillWithContext(ctx) == nil {
			n++
		}
	}
	return n
}
