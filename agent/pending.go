package agent

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/internal/protocol"
)

// PendingWindow is how long an applied change waits for confirmation before it is rolled back.
var PendingWindow = protocol.ChangeWindow

// PendingKind supplies the actions for one kind of risky change.
type PendingKind struct {
	// Rollback restores the previous state; Commit makes the new state permanent.
	Rollback func(data json.RawMessage) error
	Commit   func(data json.RawMessage) error
}

type pendingRec struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Deadline int64           `json:"deadline"` // unix seconds
	Data     json.RawMessage `json:"data,omitempty"`
}

// Pending runs the "apply, then confirm within 30 seconds or roll back" protocol on the agent.
// The state is journaled on disk, so it survives an agent restart and even a reboot.
type Pending struct {
	dir   string
	log   *slog.Logger
	mu    sync.Mutex
	cur   *pendingRec
	timer *time.Timer
	kinds map[string]PendingKind
}

// NewPending creates the manager; dir holds the journal.
func NewPending(dir string, log *slog.Logger) *Pending {
	return &Pending{dir: dir, log: log, kinds: map[string]PendingKind{}}
}

// Register adds a kind.
func (p *Pending) Register(kind string, k PendingKind) { p.kinds[kind] = k }

func (p *Pending) journal() string { return filepath.Join(p.dir, "pending.json") }

func (p *Pending) save() error {
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(p.cur)
	return os.WriteFile(p.journal(), b, 0o600)
}

// Begin records a pending change and starts the timer. It must be called BEFORE the change is applied.
func (p *Pending) Begin(id, kind string, data any, window time.Duration) (int64, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return 0, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur != nil {
		return 0, errBusy
	}
	if _, ok := p.kinds[kind]; !ok {
		return 0, errors.New("unknown change kind")
	}
	p.cur = &pendingRec{ID: id, Kind: kind, Deadline: time.Now().Add(window).Unix(), Data: raw}
	if err := p.save(); err != nil {
		p.cur = nil
		return 0, err
	}
	p.arm(window)
	return p.cur.Deadline, nil
}

// arm starts the auto-rollback timer. Caller holds p.mu.
func (p *Pending) arm(d time.Duration) {
	id := p.cur.ID
	p.timer = time.AfterFunc(d, func() { p.expire(id) })
}

func (p *Pending) clear() {
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.cur = nil
	_ = os.Remove(p.journal())
}

func (p *Pending) expire(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil || p.cur.ID != id {
		return
	}
	p.log.Warn("change not confirmed in time, rolling back", "id", id, "kind", p.cur.Kind)
	if err := p.kinds[p.cur.Kind].Rollback(p.cur.Data); err != nil {
		p.log.Error("automatic rollback failed", "err", err)
	}
	p.clear()
}

// Confirm makes the pending change permanent.
func (p *Pending) Confirm(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil || p.cur.ID != id {
		return errors.New(protocol.ErrNoPending)
	}
	err := p.kinds[p.cur.Kind].Commit(p.cur.Data)
	p.clear()
	return err
}

// Rollback undoes the pending change immediately.
func (p *Pending) Rollback(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil || p.cur.ID != id {
		return errors.New(protocol.ErrNoPending)
	}
	err := p.kinds[p.cur.Kind].Rollback(p.cur.Data)
	p.clear()
	return err
}

// Abort drops a change whose apply step failed after Begin; it rolls back the partial state.
func (p *Pending) Abort(id string) { _ = p.Rollback(id) }

// Current returns the pending change, if any.
func (p *Pending) Current() *pendingRec {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil {
		return nil
	}
	c := *p.cur
	return &c
}

// Recover handles a journal left by a previous agent process: an expired change is rolled back at once,
// an open one resumes its timer.
func (p *Pending) Recover() {
	b, err := os.ReadFile(p.journal())
	if err != nil {
		return
	}
	var rec pendingRec
	if json.Unmarshal(b, &rec) != nil || p.kinds[rec.Kind].Rollback == nil {
		_ = os.Remove(p.journal())
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cur = &rec
	left := time.Until(time.Unix(rec.Deadline, 0))
	if left <= 0 {
		p.log.Warn("found an unconfirmed change from a previous run, rolling back", "id", rec.ID, "kind", rec.Kind)
		if err := p.kinds[rec.Kind].Rollback(rec.Data); err != nil {
			p.log.Error("rollback after restart failed", "err", err)
		}
		p.clear()
		return
	}
	p.arm(left)
}
