package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/server/store"
)

const (
	sparkLen       = 12
	persistEvery   = 30 * time.Second
	maxSampleAge   = 2 * time.Hour
	maxFutureSkew  = 5 * time.Minute
	bootChangeSecs = 60
)

// serverState is the in-memory view of one server.
type serverState struct {
	loaded    bool
	last      *protocol.Sample
	system    *protocol.SystemInfo
	spark     []float64
	persisted time.Time
}

// Metrics ingests samples, keeps the latest state and feeds the live stream.
type Metrics struct {
	DB   *store.Store
	Live *Live
	Log  *slog.Logger

	mu     sync.Mutex
	states map[string]*serverState
}

// NewMetrics creates the ingest service.
func NewMetrics(db *store.Store, live *Live, log *slog.Logger) *Metrics {
	return &Metrics{DB: db, Live: live, Log: log, states: map[string]*serverState{}}
}

func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return lo
	}
	return math.Min(hi, math.Max(lo, v))
}

func sanitize(s *protocol.Sample) {
	s.CPU = clamp(s.CPU, 0, 100)
	s.MemPct = clamp(s.MemPct, 0, 100)
	s.DiskPct = clamp(s.DiskPct, 0, 100)
	s.RxBps = clamp(s.RxBps, 0, 1e12)
	s.TxBps = clamp(s.TxBps, 0, 1e12)
	for i := range s.Load {
		s.Load[i] = clamp(s.Load[i], 0, 1e6)
	}
	if s.Disks == nil {
		s.Disks = []protocol.Disk{}
	}
	if s.Net == nil {
		s.Net = []protocol.NetIface{}
	}
}

// state returns the server state, loading the persisted copy on first use. Caller holds m.mu.
func (m *Metrics) state(ctx context.Context, id string) *serverState {
	st := m.states[id]
	if st == nil {
		st = &serverState{}
		m.states[id] = st
	}
	if !st.loaded {
		st.loaded = true
		if ps, err := m.DB.LoadLastState(ctx, id); err == nil {
			if len(ps.LastSample) > 0 {
				var s protocol.Sample
				if json.Unmarshal(ps.LastSample, &s) == nil {
					st.last = &s
				}
			}
			if len(ps.SystemInfo) > 0 {
				var si protocol.SystemInfo
				if json.Unmarshal(ps.SystemInfo, &si) == nil {
					st.system = &si
				}
			}
		}
		if v, err := m.DB.RecentCPU(ctx, id, sparkLen); err == nil {
			st.spark = v
		}
	}
	return st
}

// View is a copy of the state for API responses.
type View struct {
	Sample *protocol.Sample
	System *protocol.SystemInfo
	Spark  []float64
}

// Get returns the current view of a server.
func (m *Metrics) Get(ctx context.Context, id string) View {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.state(ctx, id)
	return View{Sample: st.last, System: st.system, Spark: append([]float64(nil), st.spark...)}
}

// Forget drops the state of a deleted server.
func (m *Metrics) Forget(id string) {
	m.mu.Lock()
	delete(m.states, id)
	m.mu.Unlock()
}

// Ingest stores a batch and publishes the newest sample live.
func (m *Metrics) Ingest(ctx context.Context, serverID string, b *protocol.MetricsBatch) {
	now := time.Now()
	var samples []protocol.Sample
	for _, s := range b.Samples {
		ts := time.Unix(s.TS, 0)
		if now.Sub(ts) > maxSampleAge || ts.Sub(now) > maxFutureSkew {
			continue
		}
		sanitize(&s)
		samples = append(samples, s)
	}
	if len(samples) == 0 && b.System == nil {
		return
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].TS < samples[j].TS })

	if len(samples) > 0 {
		raw := make([]store.RawPoint, 0, len(samples))
		for _, s := range samples {
			raw = append(raw, store.RawPoint{TS: time.Unix(s.TS, 0), CPU: float32(s.CPU), Mem: float32(s.MemPct), Disk: float32(s.DiskPct),
				Rx: float32(s.RxBps), Tx: float32(s.TxBps), L1: float32(s.Load[0])})
		}
		if err := m.DB.EnsureRange(ctx, store.Tiers[0], raw[0].TS, raw[len(raw)-1].TS); err != nil {
			m.Log.Error("ensure partitions", "err", err)
			return
		}
		if err := m.DB.InsertRaw(ctx, serverID, raw); err != nil {
			m.Log.Error("insert metrics", "err", err)
			return
		}
	}

	m.mu.Lock()
	st := m.state(ctx, serverID)
	if b.System != nil {
		st.system = b.System
	}
	var newest *protocol.Sample
	var events []store.Event
	for i := range samples {
		s := samples[i]
		if st.last != nil && s.TS <= st.last.TS {
			continue
		}
		if st.last != nil {
			if st.last.BootTime > 0 && s.BootTime-st.last.BootTime > bootChangeSecs {
				events = append(events, store.Event{TS: s.BootTime, Kind: "reboot", Message: "boot time changed"})
			}
			if s.FailedServices > st.last.FailedServices {
				events = append(events, store.Event{TS: s.TS, Kind: "service_failed", Message: "failed services: " + itoa(s.FailedServices)})
			}
		}
		cp := s
		st.last = &cp
		newest = &cp
		st.spark = append(st.spark, s.CPU)
		if len(st.spark) > sparkLen {
			st.spark = st.spark[len(st.spark)-sparkLen:]
		}
	}
	persist := newest != nil && time.Since(st.persisted) > persistEvery || (b.System != nil)
	var lastJSON, sysJSON []byte
	if persist && st.last != nil {
		st.persisted = time.Now()
		lastJSON, _ = json.Marshal(st.last)
	}
	if persist && st.system != nil {
		sysJSON, _ = json.Marshal(st.system)
	}
	m.mu.Unlock()

	for _, e := range events {
		_ = m.DB.AddEvent(ctx, serverID, time.Unix(e.TS, 0), e.Kind, e.Message)
	}
	if persist && lastJSON != nil {
		_ = m.DB.SaveLastState(ctx, serverID, lastJSON, sysJSON)
	}
	if newest != nil {
		m.Live.Publish("metrics", map[string]any{"server_id": serverID, "sample": newest})
	}
}

// Status records connect/disconnect transitions as events and announces them.
func (m *Metrics) Status(ctx context.Context, serverID string, online bool) {
	kind, msg := "offline", "agent disconnected"
	if online {
		kind, msg = "online", "agent connected"
	}
	_ = m.DB.AddEvent(context.WithoutCancel(ctx), serverID, time.Now(), kind, msg)
	m.Live.Publish("status", map[string]any{"server_id": serverID, "online": online, "last_seen_at": time.Now().UTC().Format(time.RFC3339)})
}

func itoa(n int) string { return itoa64(int64(n)) }

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	p := len(b)
	for n > 0 {
		p--
		b[p] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
