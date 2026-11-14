package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
// The state is journaled on disk, so it survives an agent restart and even a reboot. A transient
// systemd timer (see EnableWatchdog) repeats the rollback from outside when this process cannot do it.
type Pending struct {
	dir   string
	log   *slog.Logger
	mu    sync.Mutex
	cur   *pendingRec
	timer *time.Timer
	kinds map[string]PendingKind
	// armExt / disarmExt control the out-of-process safety net (a transient systemd timer that runs
	// `linager agent rollback-change`); nil when it is not available.
	armExt    func(delay time.Duration) error
	disarmExt func()
}

// watchdogGrace is how much later than the in-process timer the out-of-process rollback fires. It only
// matters when the agent process is dead or stuck at the deadline.
const watchdogGrace = 5 * time.Second

func (p *Pending) armWatchdog(d time.Duration) {
	if p.armExt == nil {
		return
	}
	if err := p.armExt(d + watchdogGrace); err != nil {
		p.log.Warn("cannot arm the backup rollback timer; only the agent's own timer protects this change", "err", err)
	}
}

func (p *Pending) disarmWatchdog() {
	if p.disarmExt != nil {
		p.disarmExt()
	}
}

// journalGone reports that another process (the backup rollback) has taken over the pending change.
func (p *Pending) journalGone() bool {
	_, err := os.Stat(p.journal())
	return errors.Is(err, os.ErrNotExist)
}

// forget drops the in-memory state without touching the system. Caller holds p.mu.
func (p *Pending) forget() {
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.cur = nil
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
	p.armWatchdog(window)
	return p.cur.Deadline, nil
}

// arm starts the auto-rollback timer. Caller holds p.mu.
func (p *Pending) arm(d time.Duration) {
	id := p.cur.ID
	p.timer = time.AfterFunc(d, func() { p.expire(id) })
}

func (p *Pending) clear() {
	p.forget()
	_ = os.Remove(p.journal())
	p.disarmWatchdog()
}

func (p *Pending) expire(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil || p.cur.ID != id {
		return
	}
	if p.journalGone() {
		p.forget() // the backup rollback already handled it
		return
	}
	p.disarmWatchdog() // this process is alive and does the rollback itself
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
	if p.journalGone() {
		p.forget()
		return errors.New(protocol.ErrNoPending)
	}
	p.disarmWatchdog()
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
	if p.journalGone() {
		p.forget()
		return nil // already rolled back by the backup timer
	}
	p.disarmWatchdog()
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
	p.armWatchdog(left) // a transient timer does not survive a reboot: arm it again
}

// changeRollbackUnit is the transient systemd unit of the backup rollback.
const changeRollbackUnit = "linager-agent-change-rollback"

// EnableWatchdog turns on the out-of-process safety net: a transient systemd timer that rolls the change
// back even when the agent process has crashed or hangs. It needs root and systemd.
func (p *Pending) EnableWatchdog() {
	if os.Geteuid() != 0 {
		return
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	p.disarmExt = func() {
		_ = exec.Command("systemctl", "stop", changeRollbackUnit+".timer").Run()
		_ = exec.Command("systemctl", "reset-failed", changeRollbackUnit+".service").Run()
	}
	p.armExt = func(delay time.Duration) error {
		p.disarmExt()
		secs := int((delay + time.Second - 1) / time.Second)
		out, err := exec.Command("systemd-run", "--unit="+changeRollbackUnit, "--on-active="+strconv.Itoa(secs), //nolint:gosec // fixed binary, our own paths
			"--description=Linager network/firewall change rollback", exe, "agent", "rollback-change", "--data-dir", p.dir).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
}

// RollbackPendingChange is `linager agent rollback-change`, run by the backup timer shortly after the
// confirmation window. A healthy agent has confirmed or rolled back by then and removed the journal, so
// normally there is nothing to do. Otherwise this process takes the journal over and restores the
// previous network or firewall configuration.
func RollbackPendingChange(dir string, log *slog.Logger) error {
	p := NewPending(dir, log)
	claimed := p.journal() + ".rollback"
	if err := os.Rename(p.journal(), claimed); err != nil {
		return nil
	}
	defer func() { _ = os.Remove(claimed) }()
	b, err := os.ReadFile(claimed) //nolint:gosec // fixed name inside the data dir
	if err != nil {
		return err
	}
	var rec pendingRec
	if err := json.Unmarshal(b, &rec); err != nil {
		return err
	}
	p.Register("network", newNetModule(p, dir).kind())
	p.Register("firewall", newFwModule(p, dir).kind())
	k, ok := p.kinds[rec.Kind]
	if !ok || k.Rollback == nil {
		return errors.New("unknown change kind " + rec.Kind)
	}
	log.Warn("the agent did not handle an unconfirmed change in time, rolling back", "id", rec.ID, "kind", rec.Kind)
	return k.Rollback(rec.Data)
}
