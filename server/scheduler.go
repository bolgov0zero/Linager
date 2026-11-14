package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/bolgov0zero/linager/server/store"
)

// Scheduler keeps partitions, roll-ups and retention up to date.
type Scheduler struct {
	DB  *store.Store
	Log *slog.Logger
	// Retention overrides tier retention by name; missing entries use the tier default.
	Retention map[string]time.Duration
}

type lookbacks [4]time.Duration // raw->1m, 1m->5m, 5m->15m, 15m->1h

var (
	regularLookback = lookbacks{55 * time.Minute, 2 * time.Hour, 4 * time.Hour, 8 * time.Hour}
	startupLookback = lookbacks{55 * time.Minute, 8 * time.Hour, 24 * time.Hour, 48 * time.Hour}
)

func (s *Scheduler) retention(t store.Tier) time.Duration {
	if d, ok := s.Retention[t.Name]; ok && d > 0 {
		return d
	}
	return t.DefaultRetention
}

// Tick performs one maintenance pass.
func (s *Scheduler) Tick(ctx context.Context, now time.Time, lb lookbacks) {
	// raw rows are only kept for about an hour, so the first lookback must stay within it
	for i := 0; i < 4; i++ {
		src, dst := store.Tiers[i], store.Tiers[i+1]
		if err := s.DB.Aggregate(ctx, src, dst, now.Add(-lb[i]), now); err != nil {
			s.Log.Error("aggregate", "tier", dst.Name, "err", err)
		}
	}
}

// Maintain creates upcoming partitions and drops expired ones.
func (s *Scheduler) Maintain(ctx context.Context, now time.Time) {
	for _, t := range store.Tiers {
		if err := s.DB.EnsureRange(ctx, t, now, now.Add(2*time.Hour)); err != nil {
			s.Log.Error("ensure partitions", "tier", t.Name, "err", err)
		}
		if n, err := s.DB.DropExpired(ctx, t, s.retention(t), now); err != nil {
			s.Log.Error("drop partitions", "tier", t.Name, "err", err)
		} else if n > 0 {
			s.Log.Info("dropped metric partitions", "tier", t.Name, "count", n)
		}
	}
}

// RunOnce performs one regular roll-up pass (used by tests and manual triggers).
func (s *Scheduler) RunOnce(ctx context.Context) { s.Tick(ctx, time.Now(), regularLookback) }

// Run blocks until ctx ends.
func (s *Scheduler) Run(ctx context.Context) {
	s.Maintain(ctx, time.Now())
	s.Tick(ctx, time.Now(), startupLookback)
	minute := time.NewTicker(time.Minute)
	maint := time.NewTicker(10 * time.Minute)
	defer minute.Stop()
	defer maint.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-minute.C:
			s.Tick(ctx, now, regularLookback)
		case now := <-maint.C:
			s.Maintain(ctx, now)
		}
	}
}
