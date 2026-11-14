package server

import (
	"testing"

	"github.com/bolgov0zero/linager/internal/protocol"
)

func TestEffectiveFailed(t *testing.T) {
	s := &protocol.Sample{FailedServices: 2, FailedUnits: []string{"a.service", "b.service"}}
	effectiveFailed(s, map[string]bool{"a.service": true})
	if s.FailedServices != 1 {
		t.Fatalf("ignored unit must not count, got %d", s.FailedServices)
	}
	old := &protocol.Sample{FailedServices: 3} // an agent that sends no unit names keeps its count
	effectiveFailed(old, map[string]bool{"a.service": true})
	if old.FailedServices != 3 {
		t.Fatalf("count without names must stay, got %d", old.FailedServices)
	}
}
