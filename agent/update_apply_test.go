//go:build !windows

package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bolgov0zero/linager/internal/pki"
	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/internal/release"
)

func signManifest(priv ed25519.PrivateKey, id [8]byte, msg []byte) string {
	sig := ed25519.Sign(priv, msg)
	raw := append(append([]byte("Ed"), id[:]...), sig...)
	global := ed25519.Sign(priv, append(append([]byte(nil), sig...), "t"...))
	return "untrusted comment: test\n" + base64.StdEncoding.EncodeToString(raw) + "\ntrusted comment: t\n" + base64.StdEncoding.EncodeToString(global) + "\n"
}

// applyFixture is a data directory of an enrolled agent, a server that serves one signed release and a fake
// executable that the update replaces.
type applyFixture struct {
	dir, exe string
	key      *release.Key
	events   []protocol.UpdateEvent
}

func newApplyFixture(t *testing.T, newBinary, servedBinary string) *applyFixture {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	key := &release.Key{Pub: pub}
	copy(key.ID[:], "applykey")

	sum := sha256.Sum256([]byte(newBinary))
	asset := release.Asset{Name: release.BinaryAsset(release.HostArch), SHA256: hex.EncodeToString(sum[:]), Size: int64(len(newBinary))}
	m := release.Manifest{Version: "0.2.0", Channel: "stable", SHA256: asset.SHA256, Size: asset.Size, Assets: map[string]release.Asset{release.HostArch: asset}}
	mb, _ := json.Marshal(m)
	if release.HostArch != release.ArchAMD64 { // the top-level fields describe the amd64 binary
		a := release.Asset{Name: release.BinaryAsset(release.ArchAMD64), SHA256: asset.SHA256, Size: asset.Size}
		m.Assets[release.ArchAMD64] = a
		mb, _ = json.Marshal(m)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/release/0.2.0/manifest.json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(mb) })
	mux.HandleFunc("/v1/release/0.2.0/manifest.json.minisig", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(signManifest(priv, key.ID, mb)))
	})
	mux.HandleFunc("/v1/release/0.2.0/binary", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(servedBinary)) })
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	// the agent identity: a key and a self-signed certificate (the test server asks for none)
	_, agentPriv, _ := pki.GenerateKey()
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "agent"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, agentPriv.Public(), agentPriv)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, keyFile), pki.MarshalPrivateKey(agentPriv))
	mustWrite(t, filepath.Join(dir, certFile), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	st := State{Server: strings.TrimPrefix(srv.URL, "https://"), ServerFP: pki.Fingerprint(srv.Certificate().Raw)}
	sb, _ := json.Marshal(st)
	mustWrite(t, filepath.Join(dir, stateFile), sb)
	if _, _, err := loadState(dir); err != nil {
		t.Fatal(err)
	}

	f := &applyFixture{dir: dir, exe: filepath.Join(dir, "linager"), key: key}
	mustWrite(t, f.exe, []byte("old binary"))
	oldExe, oldArm := executablePath, armRollbackFn
	executablePath = func() (string, error) { return f.exe, nil }
	armRollbackFn = func(string, string) error { return nil }
	t.Cleanup(func() { executablePath, armRollbackFn = oldExe, oldArm })
	return f
}

func mustWrite(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *applyFixture) apply() error {
	u := &updater{dir: f.dir, version: "0.1.0", emit: func(e protocol.UpdateEvent) { f.events = append(f.events, e) }}
	return u.Apply(context.Background(), "0.2.0", f.key)
}

func (f *applyFixture) timeline() string {
	var b strings.Builder
	for _, e := range f.events {
		if e.Bytes > 0 && e.State == protocol.UpdateRunning {
			continue // progress ticks are timing dependent
		}
		b.WriteString(e.ID + ":" + e.State + " ")
	}
	return strings.TrimSpace(b.String())
}

func TestApplyReportsEveryStage(t *testing.T) {
	f := newApplyFixture(t, "new binary", "new binary")
	if err := f.apply(); err != nil {
		t.Fatal(err)
	}
	want := "download:running download:done verify:running verify:done backup:running backup:done replace:running replace:done"
	if got := f.timeline(); got != want {
		t.Fatalf("events:\n got %s\nwant %s", got, want)
	}
	if b, _ := os.ReadFile(f.exe); string(b) != "new binary" {
		t.Fatalf("the binary was not replaced: %q", b)
	}
	if fi, _ := os.Stat(f.exe); fi.Mode().Perm()&0o100 == 0 {
		t.Fatalf("the new binary is not executable: %v", fi.Mode())
	}
	if b, _ := os.ReadFile(prevPath(f.dir)); string(b) != "old binary" {
		t.Fatalf("the old binary was not kept: %q", b)
	}
	if _, err := os.Stat(markerPath(f.dir)); err != nil {
		t.Fatal("the update marker must exist until the new agent confirms")
	}
	if _, err := os.Stat(f.exe + ".linager-new"); err == nil {
		t.Fatal("the temporary file was left behind")
	}
	// the download ends with the byte count, so the panel can show the size
	var last protocol.UpdateEvent
	for _, e := range f.events {
		if e.ID == "download" && e.State == protocol.UpdateDone {
			last = e
		}
	}
	if last.Bytes != int64(len("new binary")) || last.Size != last.Bytes {
		t.Fatalf("download end: %+v", last)
	}
	if !ConfirmUpdate(f.dir, nil) || ConfirmUpdate(f.dir, nil) {
		t.Fatal("ConfirmUpdate must report the first confirmation only")
	}
}

func TestApplyRefusesABinaryThatIsNotTheSignedOne(t *testing.T) {
	f := newApplyFixture(t, "new binary", "tampered binary!")
	if err := f.apply(); err == nil || !strings.Contains(err.Error(), "signed manifest") {
		t.Fatalf("a tampered binary must be refused, got %v", err)
	}
	if got := f.timeline(); !strings.HasSuffix(got, "verify:running verify:failed") || strings.Contains(got, "backup") {
		t.Fatalf("events: %s", got)
	}
	if e := f.events[len(f.events)-1]; e.Error == "" || e.State != protocol.UpdateFailed {
		t.Fatalf("the failed event must carry the error: %+v", e)
	}
	if b, _ := os.ReadFile(f.exe); string(b) != "old binary" {
		t.Fatalf("the running binary must stay untouched: %q", b)
	}
	if _, err := os.Stat(f.exe + ".linager-new"); err == nil {
		t.Fatal("the unchecked file must be removed")
	}
}
