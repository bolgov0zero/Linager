package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
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

type procStat struct {
	comm  string
	state string
	ticks uint64 // utime + stime
	rss   uint64 // pages
}

// parseProcStat parses /proc/<pid>/stat; the command name may contain spaces and parentheses.
func parseProcStat(s string) (procStat, bool) {
	i, j := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if i < 0 || j < i {
		return procStat{}, false
	}
	f := strings.Fields(s[j+1:])
	if len(f) < 22 {
		return procStat{}, false
	}
	ut, e1 := strconv.ParseUint(f[11], 10, 64)
	st, e2 := strconv.ParseUint(f[12], 10, 64)
	rss, e3 := strconv.ParseInt(f[21], 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || rss < 0 {
		return procStat{}, false
	}
	return procStat{comm: s[i+1 : j], state: f[0], ticks: ut + st, rss: uint64(rss)}, true
}

func readTicks() map[int]procStat {
	out := map[int]procStat{}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if ps, ok := parseProcStat(read("/proc/" + e.Name() + "/stat")); ok {
			out[pid] = ps
		}
	}
	return out
}

var (
	userMu    sync.Mutex
	userCache = map[uint32]string{}
)

func userName(uid uint32) string {
	userMu.Lock()
	defer userMu.Unlock()
	if n, ok := userCache[uid]; ok {
		return n
	}
	n := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(n); err == nil {
		n = u.Username
	}
	userCache[uid] = n
	return n
}

func procList(ctx context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct {
		Sort string
		N    int
	}
	_ = json.Unmarshal(raw, &p)
	if p.N < 1 || p.N > 200 {
		p.N = 10
	}
	t0 := time.Now()
	a := readTicks()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(400 * time.Millisecond):
	}
	b := readTicks()
	dt := time.Since(t0).Seconds()
	mem := parseMeminfo(read("/proc/meminfo")).total
	page := uint64(os.Getpagesize()) //nolint:gosec // page size is positive
	list := make([]ProcInfo, 0, len(b))
	for pid, cur := range b {
		pi := ProcInfo{PID: pid, State: cur.state, RSS: cur.rss * page, Cmd: cur.comm}
		if prev, ok := a[pid]; ok && cur.ticks >= prev.ticks && dt > 0 {
			pi.CPU = float64(cur.ticks-prev.ticks) / 100 / dt * 100 // CLK_TCK is 100 on Linux
		}
		if mem > 0 {
			pi.MemPct = float64(pi.RSS) / float64(mem) * 100
		}
		if st, err := os.Stat("/proc/" + strconv.Itoa(pid)); err == nil {
			if sys, ok := st.Sys().(*syscall.Stat_t); ok {
				pi.User = userName(sys.Uid)
			}
		}
		if cl := strings.ReplaceAll(strings.TrimRight(read("/proc/"+strconv.Itoa(pid)+"/cmdline"), "\x00"), "\x00", " "); cl != "" {
			pi.Cmd = cl
		}
		list = append(list, pi)
	}
	if p.Sort == "mem" {
		sort.Slice(list, func(i, j int) bool { return list[i].RSS > list[j].RSS })
	} else {
		sort.Slice(list, func(i, j int) bool { return list[i].CPU > list[j].CPU })
	}
	total := len(list)
	if len(list) > p.N {
		list = list[:p.N]
	}
	return map[string]any{"processes": list, "total": total}, nil
}

func procKill(_ context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct {
		PID    int
		Signal string
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	sig := syscall.SIGTERM
	switch p.Signal {
	case "", "TERM":
	case "KILL":
		sig = syscall.SIGKILL
	default:
		return nil, errors.New("unsupported signal")
	}
	if p.PID <= 1 || p.PID == os.Getpid() {
		return nil, errors.New("refusing to signal this process")
	}
	if err := syscall.Kill(p.PID, sig); err != nil {
		return nil, err
	}
	return map[string]bool{"ok": true}, nil
}
