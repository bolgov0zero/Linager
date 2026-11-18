//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The "Disk" tab on Windows speaks the same protocol as on Linux (disk.state, disk.scan, disk.clean), so the tab is the
// same: volumes with a forecast of when they fill up, what takes the space (a tree of folders and a pie), and a few
// safe ways to free space. The forecast comes from a history the agent keeps itself (a point every half hour).

const (
	diskSampleEvery = 30 * time.Minute
	diskKeepDays    = 31
	diskForecastDay = 14
	diskScanLimit   = 100 * time.Second
	diskScanCache   = 10 * time.Minute
	diskStateCache  = 30 * time.Second
	diskMaxChildren = 25
	diskMaxDepth    = 4
	diskMaxEntries  = 4_000_000
)

type histPoint struct {
	T int64  `json:"t"`
	U uint64 `json:"u"`
}

// DiskFS is one volume with its growth.
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
	PerDay      float64  `json:"per_day"`
	DaysLeft    float64  `json:"days_left"`
	HistoryDays float64  `json:"history_days"`
	Trend       []uint64 `json:"trend"`
}

// DiskNode is a folder with its size; only the biggest children are kept.
type DiskNode struct {
	Name     string      `json:"name"`
	Size     uint64      `json:"size"`
	Children []*DiskNode `json:"children,omitempty"`
}

// DiskCategory is a slice of the pie of one volume.
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

type cleanItem struct {
	Kind  string `json:"kind"`
	Size  uint64 `json:"size"`
	Frees uint64 `json:"frees"`
	Count int    `json:"count"`
}

type diskModule struct {
	dir     string
	mu      sync.Mutex
	hist    map[string][]histPoint
	state   json.RawMessage
	stateAt time.Time
	scanMu  sync.Mutex
	scans   map[string]*diskScan
	started bool
}

func newDiskModule(dir string) *diskModule {
	m := &diskModule{dir: dir, hist: map[string][]histPoint{}, scans: map[string]*diskScan{}}
	m.load()
	return m
}

func (m *diskModule) histPath() string { return filepath.Join(m.dir, "disk-history.json") }

func (m *diskModule) load() {
	if b, err := os.ReadFile(m.histPath()); err == nil {
		var h map[string][]histPoint
		if json.Unmarshal(b, &h) == nil {
			m.hist = h
		}
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

// volumes lists the fixed and removable volumes that have a file system.
func volumes() []fsInfo {
	var out []fsInfo
	for _, d := range winDisks() {
		out = append(out, fsInfo{mount: d.Mount, device: d.Device, fs: d.FS, total: d.Total, used: d.Used, avail: d.Total - min(d.Total, d.Used)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mount < out[j].mount })
	return out
}

type fsInfo struct {
	mount, device, fs  string
	total, used, avail uint64
}

func (m *diskModule) record() {
	now := time.Now().Unix()
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := false
	live := map[string]bool{}
	for _, f := range volumes() {
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
	for mount := range m.hist {
		if !live[mount] {
			delete(m.hist, mount)
			changed = true
		}
	}
	if changed {
		m.save()
	}
}

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

type diskState struct {
	Filesystems []DiskFS    `json:"filesystems"`
	Cleanup     []cleanItem `json:"cleanup"`
	Docker      bool        `json:"docker"`
}

func (m *diskModule) stateCmd(ctx context.Context, _ json.RawMessage, _ bool) (any, error) {
	m.mu.Lock()
	if m.state != nil && time.Since(m.stateAt) < diskStateCache {
		s := m.state
		m.mu.Unlock()
		return s, nil
	}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	st := diskState{Filesystems: []DiskFS{}, Cleanup: []cleanItem{}}
	now := time.Now().Unix()
	m.record()
	m.mu.Lock()
	for _, f := range volumes() {
		d := DiskFS{Mount: f.mount, Device: f.device, FS: f.fs, Total: f.total, Used: f.used, Avail: f.avail}
		if f.used+f.avail > 0 {
			d.Pct = float64(f.used) / float64(f.used+f.avail) * 100
		}
		pts := m.hist[f.mount]
		d.PerDay, d.DaysLeft, d.HistoryDays = forecast(pts, f.avail, now)
		d.Trend = trend(pts, 30)
		st.Filesystems = append(st.Filesystems, d)
	}
	m.mu.Unlock()
	st.Cleanup = winCleanCandidates(ctx)
	b, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.state, m.stateAt = b, time.Now()
	m.mu.Unlock()
	return json.RawMessage(b), nil
}

// ---- scan ----

// the places of the pie: system and programs, user profiles, temporary files; the rest is "other"
var diskCategories = []struct {
	id    string
	paths []string
}{
	{"sys", []string{"/Windows", "/Program Files", "/Program Files (x86)"}},
	{"home", []string{"/Users"}},
	{"tmp", []string{"/Windows/Temp", "/Windows/SoftwareDistribution", "/$Recycle.Bin"}},
}

type scanner struct {
	ctx     context.Context
	partial bool
	entries int
}

// walk returns the size of a folder. Links and mount points are not entered.
func (s *scanner) walk(path string, depth int) *DiskNode {
	n := &DiskNode{Name: filepath.Base(path)}
	ents, err := os.ReadDir(path)
	if err != nil {
		return n
	}
	var kids []*DiskNode
	for _, e := range ents {
		if s.ctx.Err() != nil || s.entries > diskMaxEntries {
			s.partial = true
			break
		}
		s.entries++
		switch {
		case e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0:
			continue
		case e.IsDir():
			k := s.walk(filepath.Join(path, e.Name()), depth+1)
			n.Size += k.Size
			if depth < diskMaxDepth {
				kids = append(kids, k)
			}
		default:
			if info, err := e.Info(); err == nil {
				n.Size += uint64(info.Size()) //nolint:gosec // a size is not negative
			}
		}
	}
	sort.Slice(kids, func(i, j int) bool { return kids[i].Size > kids[j].Size })
	for i, k := range kids {
		if i < diskMaxChildren {
			n.Children = append(n.Children, k)
		}
	}
	return n
}

func findNode(root *DiskNode, rel string) *DiskNode {
	cur := root
	for _, part := range strings.Split(strings.Trim(rel, "/"), "/") {
		if part == "" {
			continue
		}
		var next *DiskNode
		for _, c := range cur.Children {
			if strings.EqualFold(c.Name, part) {
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

func categorize(tree *DiskNode, f fsInfo) []DiskCategory {
	var out []DiskCategory
	var known uint64
	counted := map[string]bool{}
	for _, c := range diskCategories {
		var sum uint64
		for _, p := range c.paths {
			// a place that is inside a bigger one of an earlier slice is not counted twice
			if counted[p] {
				continue
			}
			if n := findNode(tree, p); n != nil {
				sum += n.Size
				counted[p] = true
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
	out = append(out, DiskCategory{ID: "other", Size: other}, DiskCategory{ID: "free", Size: f.avail})
	return out
}

func (m *diskModule) scanCmd(ctx context.Context, raw json.RawMessage, _ bool) (any, error) {
	var p struct {
		Mount string `json:"mount"`
		Force bool   `json:"force"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.Mount == "" {
		return nil, errors.New("invalid request")
	}
	var vol *fsInfo
	for _, f := range volumes() {
		if f.mount == p.Mount { // only volumes the agent itself lists
			f := f
			vol = &f
		}
	}
	if vol == nil {
		return nil, errors.New("no such volume")
	}
	m.mu.Lock()
	if c := m.scans[p.Mount]; c != nil && !p.Force && time.Since(time.Unix(c.At, 0)) < diskScanCache {
		m.mu.Unlock()
		return c, nil
	}
	m.mu.Unlock()
	if !m.scanMu.TryLock() {
		return nil, errors.New("another scan is running, try again in a minute")
	}
	defer m.scanMu.Unlock()
	sctx, cancel := context.WithTimeout(ctx, diskScanLimit)
	defer cancel()
	sc := &scanner{ctx: sctx}
	tree := sc.walk(p.Mount+`\`, 0)
	tree.Name = p.Mount
	res := &diskScan{At: time.Now().Unix(), Mount: p.Mount, Partial: sc.partial, Tree: tree, Categories: categorize(tree, *vol)}
	m.mu.Lock()
	m.scans[p.Mount] = res
	m.mu.Unlock()
	return res, nil
}

func (m *diskModule) forget() {
	m.mu.Lock()
	m.state = nil
	m.mu.Unlock()
}

// forecast fits a line through the points of the last two weeks: bytes per day and days until the free space is used
// up (0 when the volume does not grow or there is too little history).
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
	slope := (n*sxy - sx*sy) / den
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
