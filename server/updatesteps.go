package server

import (
	"time"

	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/server/store"
)

// The stages of an agent update, in order. The first five are reported by the agent itself, the last three are
// seen by the server: the connection dropped (offline), the new version said hello (reconnect) and the new
// version confirmed itself (confirm).
var stageIDs = []string{"download", "verify", "backup", "replace", "restart", "offline", "reconnect", "confirm"}

const (
	stageRestart   = 4
	stageOffline   = 5
	stageReconnect = 6
	stageConfirm   = 7

	// confirmGrace is how long the server waits for the "confirmed" event of a reconnected agent before it
	// counts the update as done anyway (an agent that does not send it).
	confirmGrace = 10 * time.Second
)

func newTimeline() []store.UpdateStep {
	out := make([]store.UpdateStep, len(stageIDs))
	for i, id := range stageIDs {
		out[i] = store.UpdateStep{ID: id, State: "pending"}
	}
	return out
}

// normalize returns a timeline of the expected shape (a row written by an older server has none).
func normalize(st []store.UpdateStep) []store.UpdateStep {
	if len(st) != len(stageIDs) {
		return newTimeline()
	}
	return st
}

func stageIndex(id string) int {
	for i, s := range stageIDs {
		if s == id {
			return i
		}
	}
	return -1
}

func closeStep(s *store.UpdateStep, now int64) {
	if s.StartedMS == 0 {
		s.StartedMS = now
	}
	s.State, s.FinishedMS = "done", now
}

func startStep(s *store.UpdateStep, now int64) {
	if s.State == "pending" {
		s.State, s.StartedMS = "running", now
	}
}

// applyEvent folds an event of the agent into the timeline; now is in milliseconds. An event without a state
// comes from an agent that predates the stages and means "this step is reached": the earlier ones are over.
func applyEvent(st []store.UpdateStep, e protocol.UpdateEvent, now int64) []store.UpdateStep {
	st = normalize(st)
	i := stageIndex(e.ID)
	if i < 0 && e.Step >= 1 && e.Step <= stageRestart+1 {
		i = e.Step - 1
	}
	if i < 0 {
		return st
	}
	for j := 0; j < i; j++ {
		if st[j].State == "pending" || st[j].State == "running" {
			closeStep(&st[j], now)
		}
	}
	s := &st[i]
	switch {
	case e.Error != "":
		if s.StartedMS == 0 {
			s.StartedMS = now
		}
		s.State, s.FinishedMS, s.Detail = "failed", now, e.Error
	case e.State == protocol.UpdateDone:
		closeStep(s, now)
		if e.Detail != "" {
			s.Detail = e.Detail
		}
		if e.Size > 0 {
			s.Bytes, s.Size = e.Bytes, e.Size
		}
	default:
		startStep(s, now)
		if s.State == "running" {
			if e.Detail != "" {
				s.Detail = e.Detail
			}
			if e.Size > 0 || e.Bytes > 0 {
				s.Bytes, s.Size = e.Bytes, e.Size
			}
		}
	}
	return st
}

// restartReached tells that the agent got as far as restarting: it may be gone for a moment.
func restartReached(st []store.UpdateStep) bool {
	st = normalize(st)
	return st[stageRestart].State != "pending"
}

// markOffline records that the connection of an agent that is restarting dropped.
func markOffline(st []store.UpdateStep, now int64) ([]store.UpdateStep, bool) {
	st = normalize(st)
	if !restartReached(st) || st[stageOffline].State != "pending" {
		return st, false
	}
	for j := 0; j < stageOffline; j++ {
		if st[j].State == "pending" || st[j].State == "running" {
			closeStep(&st[j], now)
		}
	}
	startStep(&st[stageOffline], now)
	return st, true
}

// markReconnect records that the agent is back with the given version and waits for its confirmation.
func markReconnect(st []store.UpdateStep, now int64, version string) []store.UpdateStep {
	st = normalize(st)
	for j := 0; j < stageReconnect; j++ {
		if st[j].State == "pending" || st[j].State == "running" {
			closeStep(&st[j], now)
		}
	}
	closeStep(&st[stageReconnect], now)
	st[stageReconnect].Detail = version
	startStep(&st[stageConfirm], now)
	return st
}

// markOldVersionBack records that the agent came back with its old version: the new one did not start or was
// rolled back, so the reconnect stage is where the update failed.
func markOldVersionBack(st []store.UpdateStep, now int64, version string) []store.UpdateStep {
	st = normalize(st)
	for j := 0; j < stageReconnect; j++ {
		if st[j].State == "pending" || st[j].State == "running" {
			closeStep(&st[j], now)
		}
	}
	s := &st[stageReconnect]
	s.StartedMS, s.FinishedMS, s.State, s.Detail = st[stageOffline].FinishedMS, now, "failed", version
	if s.StartedMS == 0 {
		s.StartedMS = now
	}
	return st
}

// failRunning marks the stage that was going on (or else the next one) as failed, unless one failed already.
func failRunning(st []store.UpdateStep, now int64, detail string) []store.UpdateStep {
	st = normalize(st)
	for _, s := range st {
		if s.State == "failed" {
			return st
		}
	}
	for _, want := range []string{"running", "pending"} {
		for i := range st {
			if st[i].State == want {
				if st[i].StartedMS == 0 {
					st[i].StartedMS = now
				}
				st[i].State, st[i].FinishedMS, st[i].Detail = "failed", now, detail
				return st
			}
		}
	}
	return st
}

// currentStep is the 1-based number of the stage that is going on (or the next one to go).
func currentStep(st []store.UpdateStep) int {
	for i, s := range st {
		if s.State == "running" || s.State == "failed" {
			return i + 1
		}
	}
	for i, s := range st {
		if s.State == "pending" {
			return i + 1
		}
	}
	return len(st)
}

// rollbackAt is the Unix time at which the rollback timer of the agent fires: it is armed when the file has been
// replaced. Zero when that has not happened yet or the update is over.
func rollbackAt(st []store.UpdateStep) int64 {
	if len(st) != len(stageIDs) {
		return 0
	}
	r := st[3] // replace
	if r.State != "done" || r.FinishedMS == 0 || st[stageConfirm].State == "done" {
		return 0
	}
	return r.FinishedMS/1000 + int64(protocol.AgentRollbackDelay/time.Second)
}
