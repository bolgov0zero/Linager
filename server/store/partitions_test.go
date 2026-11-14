package store

import (
	"testing"
	"time"
)

func TestPartitionBounds(t *testing.T) {
	ts := time.Date(2026, 11, 14, 7, 42, 11, 0, time.UTC) // Saturday
	cases := []struct {
		g          Granularity
		start, end string
	}{
		{Hourly, "2026-11-14T07:00:00Z", "2026-11-14T08:00:00Z"},
		{Daily, "2026-11-14T00:00:00Z", "2026-11-15T00:00:00Z"},
		{Weekly, "2026-11-09T00:00:00Z", "2026-11-16T00:00:00Z"}, // Monday start
		{Monthly, "2026-11-01T00:00:00Z", "2026-12-01T00:00:00Z"},
	}
	for _, c := range cases {
		s, e := c.g.Bounds(ts)
		if s.Format(time.RFC3339) != c.start || e.Format(time.RFC3339) != c.end {
			t.Errorf("gran %d: got %s..%s", c.g, s.Format(time.RFC3339), e.Format(time.RFC3339))
		}
	}
	if n := Tiers[1].PartitionName(ts); n != "metrics_1m_20261114" {
		t.Errorf("name %s", n)
	}
	if n := Tiers[0].PartitionName(ts); n != "metrics_raw_2026111407" {
		t.Errorf("name %s", n)
	}
}
