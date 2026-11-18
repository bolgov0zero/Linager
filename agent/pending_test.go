//go:build !windows

package agent

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func newTestPending(t *testing.T) (*Pending, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	var rolled, committed atomic.Int32
	p := NewPending(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.Register("x", PendingKind{
		Rollback: func(json.RawMessage) error { rolled.Add(1); return nil },
		Commit:   func(json.RawMessage) error { committed.Add(1); return nil },
	})
	return p, &rolled, &committed
}

func TestPendingConfirmCancelsRollback(t *testing.T) {
	p, rolled, committed := newTestPending(t)
	if _, err := p.Begin("a", "x", nil, 80*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := p.Confirm("a"); err != nil || committed.Load() != 1 {
		t.Fatalf("confirm: %v committed=%d", err, committed.Load())
	}
	time.Sleep(200 * time.Millisecond)
	if rolled.Load() != 0 || p.Current() != nil {
		t.Fatal("a confirmed change must never roll back")
	}
}

func TestPendingRollsBackAfterTimeout(t *testing.T) {
	p, rolled, committed := newTestPending(t)
	if _, err := p.Begin("a", "x", nil, 60*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	if rolled.Load() != 1 || committed.Load() != 0 || p.Current() != nil {
		t.Fatalf("expected one automatic rollback, got rolled=%d committed=%d", rolled.Load(), committed.Load())
	}
	if err := p.Confirm("a"); err == nil {
		t.Fatal("confirming after the rollback must fail")
	}
}

func TestPendingBusyAndExplicitRollback(t *testing.T) {
	p, rolled, _ := newTestPending(t)
	if _, err := p.Begin("a", "x", nil, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Begin("b", "x", nil, time.Minute); err == nil {
		t.Fatal("only one pending change at a time")
	}
	if err := p.Confirm("wrong"); err == nil {
		t.Fatal("wrong id must be rejected")
	}
	if err := p.Rollback("a"); err != nil || rolled.Load() != 1 {
		t.Fatal("explicit rollback must run")
	}
}

func TestPendingRecoverAfterRestart(t *testing.T) {
	p, _, _ := newTestPending(t)
	if _, err := p.Begin("a", "x", map[string]string{"k": "v"}, 40*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	// simulate an agent crash: a new manager on the same directory, after the deadline passed
	time.Sleep(1100 * time.Millisecond) // deadlines have second resolution
	var rolled atomic.Int32
	q := NewPending(p.dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	q.Register("x", PendingKind{Rollback: func(d json.RawMessage) error {
		if string(d) != `{"k":"v"}` {
			t.Errorf("backup data lost: %s", d)
		}
		rolled.Add(1)
		return nil
	}, Commit: func(json.RawMessage) error { return nil }})
	p.mu.Lock()
	p.clear() // the crashed process cannot clean up; keep only the journal below
	p.mu.Unlock()
	_ = q.save // journal was removed by clear above, rewrite it
	q.cur = &pendingRec{ID: "a", Kind: "x", Deadline: time.Now().Add(-time.Second).Unix(), Data: json.RawMessage(`{"k":"v"}`)}
	_ = q.save()
	q.cur = nil
	q.Recover()
	if rolled.Load() != 1 || q.Current() != nil {
		t.Fatal("an expired journal must be rolled back at start")
	}
}

func TestRollbackUpdateRestoresPreviousBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "linager")
	_ = os.WriteFile(exe, []byte("NEW-BROKEN"), 0o755)         //nolint:gosec // test fixture
	_ = os.WriteFile(prevPath(dir), []byte("OLD-GOOD"), 0o755) //nolint:gosec // test fixture
	// confirmed update (no marker): nothing happens
	if err := RollbackUpdate(dir, exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEW-BROKEN" { //nolint:gosec // test fixture
		t.Fatal("without a marker the binary must stay")
	}
	_ = os.WriteFile(markerPath(dir), []byte(`{"from":"v1","to":"v2"}`), 0o600)
	_ = RollbackUpdate(dir, exe)                           // systemctl restart fails in tests; the file restore must still have happened
	if b, _ := os.ReadFile(exe); string(b) != "OLD-GOOD" { //nolint:gosec // test fixture
		t.Fatalf("the previous binary must be restored, got %q", b)
	}
	if _, err := os.Stat(markerPath(dir)); err == nil {
		t.Fatal("the marker must be removed")
	}
}
