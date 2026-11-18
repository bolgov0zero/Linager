//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// The "Disk" tab: filesystems with a forecast of when they fill up, what takes the space, the biggest
// directories and a few safe ways to free space. The forecast comes from a history the agent keeps itself
// (a point every half hour), the directory sizes from a scan of one filesystem on request.

const (
	diskSampleEvery = 30 * time.Minute
	diskKeepDays    = 31
	diskForecastDay = 14
	diskScanLimit   = 100 * time.Second
	diskScanCache   = 10 * time.Minute
	diskStateCache  = 30 * time.Second
	diskMaxChildren = 25
	diskMaxDepth    = 4
)

// DiskFS is one mounted filesystem with its growth.
type DiskFS struct {
	Mount       string   `json:"mount"`
	Device      string   `json:"device"`
	FS          string   `json:"fs"`
	Total       uint64   `json:"total"`
	Used        uint64   `json:"used"`
	Avail       uint64   `json:"avail"`
	InodesTotal uint64   `json:"inodes_total"`
	InodesUsed  uint64   `json:"inodes_used"`
	Pct         float64  `json:"pct"`
	PerDay      float64  `json:"per_day"`   // growth in bytes per day (negative: shrinking)
	DaysLeft    float64  `json:"days_left"` // 0: unknown or not growing
	HistoryDays float64  `json:"history_days"`
	Trend       []uint64 `json:"trend"` // used bytes at the end of each of the last days
}

type histPoint struct {
	T int64  `json:"t"`
	U uint64 `json:"u"`
}

// DiskNode is a directory with its size on disk; only the biggest children are kept.
type DiskNode struct {
	Name     string      `json:"name"`
	Size     uint64      `json:"size"`
	Children []*DiskNode `json:"children,omitempty"`
}

// DiskCategory is a slice of the pie of one filesystem.
type DiskCategory struct {
	ID   string `json:"id"`
	Size uint64 `json:"size"`
}

type diskScan struct {
	At         int64          `json:"scanned_at"`
	Mount      string         `json:"mount"`
	Partial    bool           `json:"partial"`
	Tree       *DiskNode      `json:"tree"`
	Categories []DiskCategory `json:"categories"`
}

type fsInfo struct {
	mount, device, fs               string
	total, used, avail, files, free uint64
}

type diskModule struct {
	dir string

	// access to the system; tests replace these
	filesystems func() []fsInfo
	now         func() time.Time
	run         func(ctx context.Context, name string, args ...string) ([]byte, error)
	readFile    func(string) ([]byte, error)

	mu      sync.Mutex
	hist    map[string][]histPoint
	state   json.RawMessage
	stateAt time.Time
	scanMu  sync.Mutex
	scans   map[string]*diskScan
	started bool
}

func newDiskModule(dir string) *diskModule {
	m := &diskModule{dir: dir, filesystems: listFilesystems, now: time.Now, hist: map[string][]histPoint{}, scans: map[string]*diskScan{},
		readFile: os.ReadFile}
	m.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return execRunner(ctx, name, args...)
	}
	m.load()
	return m
}

// listFilesystems reads the mounted real filesystems and their usage (the numbers are the same as df shows).
func listFilesystems() []fsInfo {
	b, _ := os.ReadFile("/proc/mounts")
	var out []fsInfo
	for _, mt := range parseMounts(string(b)) {
		var st unix.Statfs_t
		if unix.Statfs(mt.point, &st) != nil || st.Blocks == 0 {
			continue
		}
		bs := uint64(st.Bsize) //nolint:gosec // block size is positive
		out = append(out, fsInfo{mount: mt.point, device: mt.device, fs: mt.fs, total: st.Blocks * bs, used: (st.Blocks - st.Bfree) * bs,
			avail: st.Bavail * bs, files: st.Files, free: st.Ffree})
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i].mount) < len(out[j].mount) })
	return out
}

// ---- history ----

func (m *diskModule) histPath() string { return filepath.Join(m.dir, "disk-history.json") }

func (m *diskModule) load() {
	b, err := os.ReadFile(m.histPath())
	if err != nil {
		return
	}
	var h map[string][]histPoint
	if json.Unmarshal(b, &h) == nil {
		m.hist = h
	}
}

func (m *diskModule) save() {
	b, err := json.Marshal(m.hist)
	if err != nil {
		return
	}
	_ = os.MkdirAll(m.dir, 0o700)
	tmp := m.histPath() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, m.histPath())
	}
}

// record adds a point per filesystem when the last one is old enough.
func (m *diskModule) record() {
	now := m.now().Unix()
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := false
	live := map[string]bool{}
	for _, f := range m.filesystems() {
		live[f.mount] = true
		pts := m.hist[f.mount]
		if n := len(pts); n > 0 && now-pts[n-1].T < int64(diskSampleEvery/time.Second)*5/6 {
			continue
		}
		pts = append(pts, histPoint{T: now, U: f.used})
		cut := now - diskKeepDays*86400
		i := 0
		for i < len(pts) && pts[i].T < cut {
			i++
		}
		m.hist[f.mount] = pts[i:]
		changed = true
	}
	for mount := range m.hist { // a filesystem that is gone does not keep its history
		if !live[mount] {
			delete(m.hist, mount)
			changed = true
		}
	}
	if changed {
		m.save()
	}
}

// start samples the filesystems in the background for the life of the agent.
func (m *diskModule) start() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()
	go func() {
		time.Sleep(20 * time.Second)
		for {
			m.record()
			time.Sleep(diskSampleEvery)
		}
	}()
}

// forecast fits a line through the points of the last two weeks: bytes per day and days until the free
// space is used up (0 when the filesystem does not grow or there is too little history).
func forecast(pts []histPoint, avail uint64, now int64) (perDay, daysLeft, spanDays float64) {
	var use []histPoint
	for _, p := range pts {
		if p.T >= now-diskForecastDay*86400 {
			use = append(use, p)
		}
	}
	if len(use) < 6 {
		return 0, 0, 0
	}
	span := float64(use[len(use)-1].T - use[0].T)
	if span < 12*3600 {
		return 0, 0, span / 86400
	}
	var sx, sy, sxx, sxy float64
	n := float64(len(use))
	t0 := use[0].T
	for _, p := range use {
		x, y := float64(p.T-t0), float64(p.U)
		sx += x
		sy += y
		sxx += x * x
		sxy += x * y
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0, 0, span / 86400
	}
	slope := (n*sxy - sx*sy) / den // bytes per second
	perDay = slope * 86400
	if slope > 0 && avail > 0 {
		daysLeft = float64(avail) / slope / 86400
	}
	return perDay, daysLeft, span / 86400
}

// trend is the used bytes at the end of each of the last days, oldest first.
func trend(pts []histPoint, days int) []uint64 {
	byDay := map[int64]uint64{}
	var order []int64
	for _, p := range pts {
		d := p.T / 86400
		if _, ok := byDay[d]; !ok {
			order = append(order, d)
		}
		byDay[d] = p.U
	}
	if len(order) > days {
		order = order[len(order)-days:]
	}
	out := make([]uint64, 0, len(order))
	for _, d := range order {
		out = append(out, byDay[d])
	}
	return out
}

// ---- state ----

type diskState struct {
	Filesystems []DiskFS    `json:"filesystems"`
	Cleanup     []cleanItem `json:"cleanup"`
	Docker      bool        `json:"docker"`
}

// stateCmd answers "disk.state" from a short cache.
func (m *diskModule) stateCmd(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	m.mu.Lock()
	if m.state != nil && m.now().Sub(m.stateAt) < diskStateCache {
		s := m.state
		m.mu.Unlock()
		return s, nil
	}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	st := diskState{Filesystems: []DiskFS{}, Cleanup: []cleanItem{}, Docker: dockerAvailable()}
	now := m.now().Unix()
	m.record() // make sure the numbers on the screen are in the history
	m.mu.Lock()
	for _, f := range m.filesystems() {
		d := DiskFS{Mount: f.mount, Device: f.device, FS: f.fs, Total: f.total, Used: f.used, Avail: f.avail, InodesTotal: f.files, InodesUsed: f.files - min(f.free, f.files)}
		if f.used+f.avail > 0 {
			d.Pct = float64(f.used) / float64(f.used+f.avail) * 100
		}
		pts := m.hist[f.mount]
		d.PerDay, d.DaysLeft, d.HistoryDays = forecast(pts, f.avail, now)
		d.Trend = trend(pts, 30)
		st.Filesystems = append(st.Filesystems, d)
	}
	m.mu.Unlock()
	st.Cleanup = m.cleanCandidates(ctx)
	b, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.state, m.stateAt = b, m.now()
	m.mu.Unlock()
	return json.RawMessage(b), nil
}

func (m *diskModule) forget() {
	m.mu.Lock()
	m.state = nil
	m.mu.Unlock()
}

// ---- scan ----

// Paths of the categories of the pie, relative to the root of the filesystem tree.
var diskCategories = []struct {
	id    string
	paths []string
}{
	{"sys", []string{"/usr", "/opt", "/boot", "/etc", "/snap", "/bin", "/sbin", "/lib", "/lib64", "/var/lib/apt", "/var/lib/dpkg"}},
	{"docker", []string{"/var/lib/docker", "/var/lib/containerd"}},
	{"logs", []string{"/var/log"}},
	{"db", []string{"/var/lib/postgresql", "/var/lib/mysql", "/var/lib/mongodb", "/var/lib/redis", "/var/lib/clickhouse", "/var/lib/elasticsearch"}},
	{"tmp", []string{"/var/cache", "/tmp", "/var/tmp"}},
	{"home", []string{"/home", "/root"}},
}

func isKnownDiskPath(p string) bool {
	for _, c := range diskCategories {
		for _, k := range c.paths {
			if p == k {
				return true
			}
		}
	}
	return false
}

type scanner struct {
	ctx     context.Context
	dev     uint64
	root    string
	partial bool
	seen    map[uint64]struct{}
	entries int
}

// walk returns the size on disk of a directory. Other filesystems and symbolic links are not entered.
func (s *scanner) walk(path string, depth int) *DiskNode {
	n := &DiskNode{Name: filepath.Base(path)}
	ents, err := os.ReadDir(path)
	if err != nil {
		return n
	}
	var kids []*DiskNode
	for _, e := range ents {
		if s.partial {
			break
		}
		if s.entries++; s.entries%2000 == 0 && s.ctx.Err() != nil {
			s.partial = true
			break
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}
		if info.IsDir() {
			if uint64(st.Dev) != s.dev { //nolint:gosec,unconvert // device ids are not negative; the type differs per platform
				continue
			}
			n.Size += uint64(st.Blocks) * 512 //nolint:gosec,unconvert // block counts are not negative
			child := s.walk(filepath.Join(path, e.Name()), depth+1)
			n.Size += child.Size
			kids = append(kids, child)
			continue
		}
		if uint64(st.Nlink) > 1 { //nolint:unconvert // a hard-linked file counts once; the type differs per platform
			ino := uint64(st.Ino) //nolint:unconvert // the type differs per platform
			if _, dup := s.seen[ino]; dup {
				continue
			}
			if len(s.seen) < 1_000_000 {
				s.seen[ino] = struct{}{}
			}
		}
		n.Size += uint64(st.Blocks) * 512 //nolint:gosec,unconvert // block counts are not negative
	}
	if depth >= diskMaxDepth {
		return n
	}
	sort.Slice(kids, func(i, j int) bool { return kids[i].Size > kids[j].Size })
	for i, k := range kids {
		abs := filepath.Join(path, k.Name)
		if i < diskMaxChildren || isKnownDiskPath(strings.TrimPrefix(abs, s.root)) {
			n.Children = append(n.Children, k)
		}
	}
	return n
}

// findNode looks up a path (relative to the scanned root, with a leading slash) in the retained tree.
func findNode(root *DiskNode, rel string) *DiskNode {
	cur := root
	for _, part := range strings.Split(strings.Trim(rel, "/"), "/") {
		if part == "" {
			continue
		}
		var next *DiskNode
		for _, c := range cur.Children {
			if c.Name == part {
				next = c
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

// categorize turns the scanned tree into the slices of the pie: known places, the rest of the used space,
// the space reserved for root and the free space.
func categorize(tree *DiskNode, f fsInfo) []DiskCategory {
	var out []DiskCategory
	var known uint64
	for _, c := range diskCategories {
		var sum uint64
		for _, p := range c.paths {
			if n := findNode(tree, p); n != nil && p != "/" {
				sum += n.Size
			}
		}
		if sum > 0 {
			out = append(out, DiskCategory{ID: c.id, Size: sum})
			known += sum
		}
	}
	other := uint64(0)
	if f.used > known {
		other = f.used - known
	}
	out = append(out, DiskCategory{ID: "other", Size: other})
	if reserved := f.total - min(f.total, f.used+f.avail); reserved*100 >= f.total {
		out = append(out, DiskCategory{ID: "reserved", Size: reserved})
	}
	out = append(out, DiskCategory{ID: "free", Size: f.avail})
	return out
}

// scanCmd is "disk.scan": {mount, force}. One scan at a time; the answer is kept for a few minutes.
func (m *diskModule) scanCmd(ctx context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct {
		Mount string `json:"mount"`
		Force bool   `json:"force"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.Mount == "" {
		return nil, errors.New("invalid request")
	}
	var fs *fsInfo
	for _, f := range m.filesystems() {
		if f.mount == p.Mount { // only mounts the agent itself lists
			f := f
			fs = &f
			break
		}
	}
	if fs == nil {
		return nil, errors.New("no such filesystem")
	}
	m.mu.Lock()
	if c := m.scans[p.Mount]; c != nil && !p.Force && m.now().Sub(time.Unix(c.At, 0)) < diskScanCache {
		m.mu.Unlock()
		return c, nil
	}
	m.mu.Unlock()
	if !m.scanMu.TryLock() {
		return nil, errors.New("another scan is running, try again in a minute")
	}
	defer m.scanMu.Unlock()
	var st unix.Stat_t
	if err := unix.Stat(p.Mount, &st); err != nil {
		return nil, err
	}
	sctx, cancel := context.WithTimeout(ctx, diskScanLimit)
	defer cancel()
	sc := &scanner{ctx: sctx, dev: uint64(st.Dev), root: strings.TrimSuffix(p.Mount, "/"), seen: map[uint64]struct{}{}} //nolint:gosec,unconvert // device ids are not negative
	tree := sc.walk(p.Mount, 0)
	tree.Name = p.Mount
	res := &diskScan{At: m.now().Unix(), Mount: p.Mount, Partial: sc.partial, Tree: tree, Categories: categorize(tree, *fs)}
	m.mu.Lock()
	m.scans[p.Mount] = res
	m.mu.Unlock()
	return res, nil
}
