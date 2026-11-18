//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bolgov0zero/linager/internal/pki"
)

func TestReconnectPauseResetsAfterHealthySession(t *testing.T) {
	if got := reconnectPause(maxBackoff, 2*time.Second); got != maxBackoff {
		t.Fatalf("a failed attempt keeps the long pause, got %s", got)
	}
	if got := reconnectPause(maxBackoff, time.Hour); got != minBackoff {
		t.Fatalf("a long session must reset the pause, got %s", got)
	}
}

func TestCertRenewal(t *testing.T) {
	ca, err := pki.NewCA("test")
	if err != nil {
		t.Fatal(err)
	}
	pub, key, _ := pki.GenerateKey()
	issue := func(pubKey []byte, id string, d time.Duration) []byte {
		p, err := ca.IssueAgentCert(pubKey, id, d)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	cur, _ := pki.ParseCertPEM(issue(pub, "srv1", 90*24*time.Hour))
	if due := time.Until(renewDue(cur)); due < 59*24*time.Hour || due > 61*24*time.Hour {
		t.Fatalf("renewal is due when a third of the lifetime is left, got %s", due)
	}
	dir := t.TempDir()
	otherPub, _, _ := pki.GenerateKey()
	for name, bad := range map[string][]byte{
		"other key":   issue(otherPub, "srv1", 365*24*time.Hour),
		"other agent": issue(pub, "srv2", 365*24*time.Hour),
		"not longer":  issue(pub, "srv1", 24*time.Hour),
	} {
		if _, err := installRenewedCert(dir, key, cur, bad, time.Now()); err == nil {
			t.Fatalf("%s: certificate must be rejected", name)
		}
	}
	good := issue(pub, "srv1", 365*24*time.Hour)
	if _, err := installRenewedCert(dir, key, cur, good, time.Now()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, certFile)); string(b) != string(good) { //nolint:gosec // test path
		t.Fatal("agent.crt was not replaced")
	}
}

// An apt operation must survive the stream that started it, and a second stream must attach to it.
func TestAptJobOutlivesItsStream(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "go")
	old := aptCommand
	aptCommand = func(...string) *exec.Cmd {
		return exec.Command("sh", "-c", "echo one; while [ ! -e "+gate+" ]; do sleep 0.05; done; echo two") //nolint:gosec // test command
	}
	defer func() { aptCommand = old }()

	var mu sync.Mutex
	var first, second strings.Builder
	finished := make(chan struct{})
	ctx1, cancel1 := context.WithCancel(context.Background())
	done1 := make(chan error, 1)
	go func() {
		done1 <- aptStream(ctx1, []byte(`{"mode":"update"}`), &StreamIO{Out: func(b []byte) error {
			mu.Lock()
			first.Write(b)
			mu.Unlock()
			cancel1() // the browser tab closes right after the first output
			return nil
		}}, func() { close(finished) })
	}()
	if err := <-done1; err != nil {
		t.Fatal(err)
	}
	if !aptBusy() {
		t.Fatal("the operation must keep running after its stream is gone")
	}
	done2 := make(chan error, 1)
	go func() {
		done2 <- aptStream(context.Background(), []byte(`{"mode":"upgrade"}`), &StreamIO{Out: func(b []byte) error {
			mu.Lock()
			second.Write(b)
			mu.Unlock()
			return nil
		}}, nil)
	}()
	_ = os.WriteFile(gate, nil, 0o600)
	select {
	case err := <-done2:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the attached stream did not finish")
	}
	<-finished
	if got := second.String(); got != "one\ntwo\n" {
		t.Fatalf("the attached stream must replay the whole log, got %q", got)
	}
}

func TestBackupRollbackTakesOverTheJournal(t *testing.T) {
	dir := t.TempDir()
	p := NewPending(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.Register("x", PendingKind{Rollback: func(json.RawMessage) error { return nil }, Commit: func(json.RawMessage) error { return nil }})
	if _, err := p.Begin("c1", "x", nil, time.Hour); err != nil {
		t.Fatal(err)
	}
	// the backup timer fired while this process was stuck: it claims the journal
	if err := os.Rename(p.journal(), p.journal()+".rollback"); err != nil {
		t.Fatal(err)
	}
	if err := p.Confirm("c1"); err == nil {
		t.Fatal("a change the backup rollback took over must not be confirmed")
	}
	if p.Current() != nil {
		t.Fatal("the stale in-memory change must be dropped")
	}
	if got := parseSSHPorts("port 2222\nPort 22\naddressfamily any\n"); len(got) != 2 || got[0] != 2222 {
		t.Fatalf("ssh ports: %v", got)
	}
}
