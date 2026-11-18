package agent

import (
	"time"

	"github.com/bolgov0zero/linager/internal/protocol"
)

// Collector produces host samples. Implementations must be safe for sequential use.
type Collector interface {
	Collect(now time.Time) protocol.Sample
	System(agentVersion string) protocol.SystemInfo
}
