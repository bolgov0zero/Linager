package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bolgov0zero/linager/internal/release"
	"github.com/bolgov0zero/linager/server/store"
	"github.com/bolgov0zero/linager/update"
)

type noSource struct{}

func (noSource) Releases(context.Context) ([]update.Release, error) {
	return nil, errors.New("offline")
}

type env struct {
	d     Deps
	key   *release.Key
	priv  ed25519.PrivateKey
	calls []string
	runs  []store.UpdateRun
	dir   string
}

func sign(priv ed25519.PrivateKey, id [8]byte, msg []byte) string {
	sig := ed25519.Sign(priv, msg)
	raw := append(append([]byte("Ed"), id[:]...), sig...)
	g := ed25519.Sign(priv, append(append([]byte(nil), sig...), "t"...))
	return "untrusted comment: x\n" + base64.StdEncoding.EncodeToString(raw) + "\ntrusted comment: t\n" + base64.StdEncoding.EncodeToString(g) + "\n"
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	k := &release.Key{Pub: pub}
	copy(k.ID[:], "updtest1")
	e := &env{key: k, priv: priv, dir: t.TempDir()}
	bin := filepath.Join(e.dir, "linager")
	_ = os.WriteFile(bin, []byte("OLD"), 0o755) //nolint:gosec // test fixture
	e.d = Deps{
		BinPath: bin, CacheDir: filepath.Join(e.dir, "cache"), PrevDir: filepath.Join(e.dir, "prev"), StatusPath: filepath.Join(e.dir, "status.json"),
		ConfigPath: "/etc/linager/server.yaml", Source: noSource{}, Key: k, CurrentVersion: "v0.1.0",
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			e.calls = append(e.calls, name+" "+strings.Join(args, " "))
			return nil, nil
		},
		Backup: func(context.Context) (string, error) { return "backup.tar.gz", nil },
		Health: func(context.Context) error { return nil },
		Record: func(_ context.Context, r store.UpdateRun) { e.runs = append(e.runs, r) },
	}
	return e
}

// cache stores a signed release the way update.Download would.
func (e *env) cache(version, binary string) {
	dir := filepath.Join(e.d.CacheDir, version)
	_ = os.MkdirAll(dir, 0o755) //nolint:gosec // test fixture
	sum := sha256.Sum256([]byte(binary))
	m, _ := json.Marshal(release.Manifest{Version: version, Channel: "stable", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(binary))})
	_ = os.WriteFile(filepath.Join(dir, release.BinaryAsset(release.ArchAMD64)), []byte(binary), 0o755)     //nolint:gosec // test fixture
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), m, 0o644)                                         //nolint:gosec // test fixture
	_ = os.WriteFile(filepath.Join(dir, "manifest.json.minisig"), []byte(sign(e.priv, e.key.ID, m)), 0o644) //nolint:gosec // test fixture
}

func (e *env) binary() string { b, _ := os.ReadFile(e.d.BinPath); return string(b) }

func TestUpdateSucceeds(t *testing.T) {
	e := newEnv(t)
	clock := time.Unix(1_700_000_000, 0)
	e.d.Now = func() time.Time { clock = clock.Add(250 * time.Millisecond); return clock }
	e.cache("v0.2.0", "NEW")
	if err := ServerUpdate(context.Background(), e.d, "v0.2.0", "stable"); err != nil {
		t.Fatal(err)
	}
	if e.binary() != "NEW" {
		t.Fatal("the binary must be replaced")
	}
	if prev, _ := os.ReadFile(filepath.Join(e.d.PrevDir, "linager-0.1.0")); string(prev) != "OLD" {
		t.Fatal("the previous binary must be kept")
	}
	want := []string{"systemctl stop linager-server", e.d.BinPath + " server migrate --config /etc/linager/server.yaml", "systemctl start linager-server"}
	if strings.Join(e.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected command order: %v", e.calls)
	}
	st := ReadStatus(e.d.StatusPath)
	if st.State != "done" || st.To != "v0.2.0" {
		t.Fatalf("%+v", st)
	}
	var prevEnd int64
	for _, s := range st.Steps {
		if s.State != "done" {
			t.Fatalf("step %s is %s", s.ID, s.State)
		}
		// every step carries when it began and ended, and the steps follow one another
		if s.StartedMs == 0 || s.FinishedMs <= s.StartedMs || s.StartedMs < prevEnd {
			t.Fatalf("step %s has odd times: %+v (previous ended %d)", s.ID, s, prevEnd)
		}
		prevEnd = s.FinishedMs
	}
	if len(e.runs) != 1 || e.runs[0].Status != "success" {
		t.Fatalf("history: %+v", e.runs)
	}
}

func TestUnhealthyUpdateRollsBack(t *testing.T) {
	e := newEnv(t)
	e.cache("v0.2.0", "NEW")
	e.d.HealthTimeout = 1
	e.d.Health = func(context.Context) error { return errors.New("no answer") }
	err := ServerUpdate(context.Background(), e.d, "v0.2.0", "stable")
	if err == nil {
		t.Fatal("an unhealthy update must fail")
	}
	if e.binary() != "OLD" {
		t.Fatalf("the old binary must be restored, got %q", e.binary())
	}
	if st := ReadStatus(e.d.StatusPath); st.State != "rolled_back" {
		t.Fatalf("state %s", st.State)
	}
	if n := strings.Count(strings.Join(e.calls, "|"), "systemctl start linager-server"); n != 2 {
		t.Fatalf("the service must be started again after the rollback: %v", e.calls)
	}
	if len(e.runs) != 1 || e.runs[0].Status != "rolled_back" {
		t.Fatalf("history: %+v", e.runs)
	}
}

func TestMigrationFailureRollsBack(t *testing.T) {
	e := newEnv(t)
	e.cache("v0.2.0", "NEW")
	run := e.d.Run
	e.d.Run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "server" {
			return []byte("migration 7 failed"), errors.New("exit 1")
		}
		return run(ctx, name, args...)
	}
	if err := ServerUpdate(context.Background(), e.d, "v0.2.0", "stable"); err == nil || !strings.Contains(err.Error(), "migration 7 failed") {
		t.Fatalf("%v", err)
	}
	if e.binary() != "OLD" {
		t.Fatal("rollback must restore the old binary")
	}
}

func TestBackupFailureTouchesNothing(t *testing.T) {
	e := newEnv(t)
	e.cache("v0.2.0", "NEW")
	e.d.Backup = func(context.Context) (string, error) { return "", errors.New("disk full") }
	if err := ServerUpdate(context.Background(), e.d, "v0.2.0", "stable"); err == nil {
		t.Fatal("must fail")
	}
	if e.binary() != "OLD" || len(e.calls) != 0 {
		t.Fatalf("nothing may change when the backup fails: bin=%s calls=%v", e.binary(), e.calls)
	}
	// the failed step is closed with its time, the steps after it were never started
	for _, s := range ReadStatus(e.d.StatusPath).Steps {
		switch s.ID {
		case StepBackup:
			if s.State != "failed" || s.StartedMs == 0 || s.FinishedMs < s.StartedMs {
				t.Fatalf("failed step: %+v", s)
			}
		case StepStop, StepReplace, StepMigrate, StepStart:
			if s.State != "pending" || s.StartedMs != 0 {
				t.Fatalf("step %s must stay untouched: %+v", s.ID, s)
			}
		}
	}
}

func TestUnverifiedReleaseIsRefused(t *testing.T) {
	e := newEnv(t)
	e.cache("v0.2.0", "NEW")
	_ = os.WriteFile(filepath.Join(e.d.CacheDir, "v0.2.0", release.BinaryAsset(release.ArchAMD64)), []byte("EVIL"), 0o755) //nolint:gosec // test fixture: binary swapped after signing
	if err := ServerUpdate(context.Background(), e.d, "v0.2.0", "stable"); err == nil {
		t.Fatal("a binary that does not match the signed manifest must be refused")
	}
	if e.binary() != "OLD" || len(e.calls) != 0 {
		t.Fatal("nothing may change for an unverified release")
	}
	if err := ServerUpdate(context.Background(), e.d, "../../etc", "stable"); err == nil {
		t.Fatal("a path-like version must be refused")
	}
}

func TestFailureAfterMigrationsRestoresDatabase(t *testing.T) {
	e := newEnv(t)
	e.cache("v0.2.0", "NEW")
	e.d.HealthTimeout = 1
	e.d.Health = func(context.Context) error { return errors.New("no answer") }
	restored := ""
	e.d.Restore = func(_ context.Context, archive string) error { restored = archive; return nil }
	if err := ServerUpdate(context.Background(), e.d, "v0.2.0", "stable"); err == nil {
		t.Fatal("an unhealthy update must fail")
	}
	if restored != "backup.tar.gz" || e.binary() != "OLD" {
		t.Fatalf("binary %q, restored from %q", e.binary(), restored)
	}
}

func TestDowngradeIsRefused(t *testing.T) {
	e := newEnv(t)
	e.cache("v0.0.9", "ANCIENT")
	if err := ServerUpdate(context.Background(), e.d, "v0.0.9", "stable"); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("a signed older release must be refused, got %v", err)
	}
	if e.binary() != "OLD" || len(e.calls) != 0 {
		t.Fatalf("nothing may be touched: %q %v", e.binary(), e.calls)
	}
}
