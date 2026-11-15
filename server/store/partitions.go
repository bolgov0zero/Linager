package store

import (
	"context"
	"fmt"
	"time"
)

// Granularity of a partitioned table.
type Granularity int

// Partition granularities.
const (
	Hourly Granularity = iota
	Daily
	Weekly
	Monthly
)

// Tier describes one metrics table.
type Tier struct {
	Name  string // "raw", "1m", ...
	Table string
	Gran  Granularity
	Step  time.Duration // sample interval of the tier
	// DefaultRetention is used when no setting overrides it.
	DefaultRetention time.Duration
}

// Tiers in ascending order of coarseness.
var Tiers = []Tier{
	{"raw", "metrics_raw", Hourly, 5 * time.Second, time.Hour},
	{"1m", "metrics_1m", Daily, time.Minute, 7 * 24 * time.Hour},
	{"5m", "metrics_5m", Weekly, 5 * time.Minute, 90 * 24 * time.Hour},
	{"15m", "metrics_15m", Monthly, 15 * time.Minute, 180 * 24 * time.Hour},
	{"1h", "metrics_1h", Monthly, time.Hour, 360 * 24 * time.Hour},
}

// TierByName returns a tier definition.
func TierByName(name string) (Tier, bool) {
	for _, t := range Tiers {
		if t.Name == name {
			return t, true
		}
	}
	return Tier{}, false
}

var layouts = map[Granularity]string{Hourly: "2006010215", Daily: "20060102", Weekly: "20060102", Monthly: "200601"}

// Bounds returns the [start, end) of the partition containing t (UTC).
func (g Granularity) Bounds(t time.Time) (time.Time, time.Time) {
	t = t.UTC()
	switch g {
	case Hourly:
		s := t.Truncate(time.Hour)
		return s, s.Add(time.Hour)
	case Daily:
		s := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return s, s.AddDate(0, 0, 1)
	case Weekly:
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		off := (int(d.Weekday()) + 6) % 7 // Monday = 0
		s := d.AddDate(0, 0, -off)
		return s, s.AddDate(0, 0, 7)
	default:
		s := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
		return s, s.AddDate(0, 1, 0)
	}
}

// PartitionName is the child table name for the partition holding t.
func (tr Tier) PartitionName(t time.Time) string {
	s, _ := tr.Gran.Bounds(t)
	return tr.Table + "_" + s.Format(layouts[tr.Gran])
}

// EnsurePartition creates the partition for t if it does not exist yet.
func (s *Store) EnsurePartition(ctx context.Context, tr Tier, t time.Time) error {
	name := tr.PartitionName(t)
	if _, ok := s.ensured.Load(name); ok {
		return nil
	}
	start, end := tr.Gran.Bounds(t)
	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')",
		name, tr.Table, start.Format(time.RFC3339), end.Format(time.RFC3339))
	if _, err := s.Pool.Exec(ctx, sql); err != nil {
		// Concurrent creators may race on the catalog; verify the partition exists.
		var n int
		if e2 := s.Pool.QueryRow(ctx, `SELECT count(*) FROM pg_class WHERE relname=$1`, name).Scan(&n); e2 != nil || n == 0 {
			return err
		}
	}
	s.ensured.Store(name, struct{}{})
	return nil
}

// EnsureRange creates all partitions covering [from, to].
func (s *Store) EnsureRange(ctx context.Context, tr Tier, from, to time.Time) error {
	for t := from; ; {
		if err := s.EnsurePartition(ctx, tr, t); err != nil {
			return err
		}
		_, end := tr.Gran.Bounds(t)
		if !end.Before(to) {
			return nil
		}
		t = end
	}
}

// DropExpired drops partitions that lie completely before now-retention.
func (s *Store) DropExpired(ctx context.Context, tr Tier, retention time.Duration, now time.Time) (int, error) {
	rows, err := s.Pool.Query(ctx, `SELECT c.relname FROM pg_inherits i
		JOIN pg_class c ON c.oid=i.inhrelid JOIN pg_class p ON p.oid=i.inhparent WHERE p.relname=$1`, tr.Table)
	if err != nil {
		return 0, err
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return 0, err
		}
		names = append(names, n)
	}
	rows.Close()
	cutoff := now.Add(-retention)
	dropped := 0
	for _, n := range names {
		suffix := n[len(tr.Table)+1:]
		start, err := time.Parse(layouts[tr.Gran], suffix)
		if err != nil {
			continue
		}
		_, end := tr.Gran.Bounds(start)
		if end.Before(cutoff) || end.Equal(cutoff) {
			if _, err := s.Pool.Exec(ctx, "DROP TABLE IF EXISTS "+n); err != nil {
				return dropped, err
			}
			s.ensured.Delete(n)
			dropped++
		}
	}
	return dropped, nil
}
