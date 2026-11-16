package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bolgov0zero/linager/internal/release"
)

type fakeRepo struct {
	key  *release.Key
	priv ed25519.PrivateKey
	srv  *httptest.Server
	rels []map[string]any
	data map[string][]byte
}

func signFile(priv ed25519.PrivateKey, id [8]byte, msg []byte) string {
	sig := ed25519.Sign(priv, msg)
	raw := append(append([]byte("Ed"), id[:]...), sig...)
	global := ed25519.Sign(priv, append(append([]byte(nil), sig...), "t"...))
	return "untrusted comment: test\n" + base64.StdEncoding.EncodeToString(raw) + "\ntrusted comment: t\n" + base64.StdEncoding.EncodeToString(global) + "\n"
}

func newFakeRepo(t *testing.T) *fakeRepo {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	k := &release.Key{Pub: pub}
	copy(k.ID[:], "fakekey1")
	r := &fakeRepo{key: k, priv: priv, data: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases.json", func(w http.ResponseWriter, _ *http.Request) { _ = json.NewEncoder(w).Encode(r.rels) })
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, req *http.Request) {
		b, ok := r.data[strings.TrimPrefix(req.URL.Path, "/dl/")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(b)
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

// publish adds a release; tamper changes the served binary after signing.
func (r *fakeRepo) publish(tag string, pre bool, binary string, tamper bool) {
	sum := sha256.Sum256([]byte(binary))
	m, _ := json.Marshal(release.Manifest{Version: tag, Channel: "stable", MinAgent: "v0.1.0", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(binary)), Notes: []string{"a", "b"}})
	served := binary
	if tamper {
		served = binary + "evil"
	}
	r.data[tag+"/bin"], r.data[tag+"/m"], r.data[tag+"/s"] = []byte(served), m, []byte(signFile(r.priv, r.key.ID, m))
	u := func(n string) string { return r.srv.URL + "/dl/" + tag + "/" + n }
	r.rels = append(r.rels, map[string]any{"tag_name": tag, "prerelease": pre, "body": "- first\n- second\n\nFull Changelog: x",
		"assets": []map[string]string{{"name": release.BinaryAsset(release.ArchAMD64), "browser_download_url": u("bin")}, {"name": "manifest.json", "browser_download_url": u("m")}, {"name": "manifest.json.minisig", "browser_download_url": u("s")}}})
}

func TestLatestByChannel(t *testing.T) {
	r := newFakeRepo(t)
	r.publish("v0.1.0", false, "one", false)
	r.publish("v0.2.0-beta.1", true, "two", false)
	r.rels = append(r.rels, map[string]any{"tag_name": "v0.3.0", "prerelease": false, "assets": []any{}}) // incomplete, must be ignored
	rels, err := URL(r.srv.URL).Releases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := Latest(rels, Stable); !ok || s.Tag != "v0.1.0" {
		t.Fatalf("stable: %+v", s)
	}
	if b, ok := Latest(rels, Beta); !ok || b.Tag != "v0.2.0-beta.1" {
		t.Fatalf("beta: %+v", b)
	}
	if l := NotesLines(rels[0].Notes); len(l) != 2 || l[0] != "first" {
		t.Fatalf("notes: %v", l)
	}
}

func TestDownloadVerifiesAndCaches(t *testing.T) {
	r := newFakeRepo(t)
	r.publish("v0.1.0", false, "#!/bin/sh\necho hi\n", false)
	rels, _ := URL(r.srv.URL).Releases(context.Background())
	dir := t.TempDir()
	c, err := Download(context.Background(), rels[0], r.key, dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != "v0.1.0" || c.Size != 18 {
		t.Fatalf("%+v", c)
	}
	if st, _ := os.Stat(c.BinaryPath()); st.Mode().Perm()&0o100 == 0 {
		t.Fatal("cached binary must be executable")
	}
	if got := ListCached(r.key, dir); len(got) != 1 {
		t.Fatalf("ListCached: %+v", got)
	}
	_ = os.WriteFile(c.BinaryPath(), []byte("evil"), 0o755) //nolint:gosec // test fixture
	if _, err := LoadCached(r.key, dir, "v0.1.0"); err == nil {
		t.Fatal("a modified cached binary must be rejected")
	}
	if len(ListCached(r.key, dir)) != 0 {
		t.Fatal("broken cache entries must be hidden")
	}
}

func TestDownloadRejectsTamperingAndForeignKeys(t *testing.T) {
	r := newFakeRepo(t)
	r.publish("v0.1.0", false, "good", true) // served binary differs from the signed checksum
	rels, _ := URL(r.srv.URL).Releases(context.Background())
	if _, err := Download(context.Background(), rels[0], r.key, t.TempDir()); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered binary must be rejected, got %v", err)
	}
	r2 := newFakeRepo(t)
	r2.publish("v0.1.0", false, "good", false)
	rels2, _ := URL(r2.srv.URL).Releases(context.Background())
	if _, err := Download(context.Background(), rels2[0], r.key, t.TempDir()); err == nil {
		t.Fatal("a manifest signed by another key must be rejected")
	}
	empty := t.TempDir()
	r3 := newFakeRepo(t)
	r3.publish("v0.2.0", false, "x", false)
	r3.rels[0]["tag_name"] = "v0.9.9" // tag does not match the signed manifest version
	rels3, _ := URL(r3.srv.URL).Releases(context.Background())
	if _, err := Download(context.Background(), rels3[0], r3.key, empty); err == nil || !strings.Contains(fmt.Sprint(err), "does not match") {
		t.Fatalf("version mismatch must be rejected, got %v", err)
	}
	if ents, _ := os.ReadDir(empty); len(ents) != 0 {
		t.Fatal("nothing may be left in the cache after a failed download")
	}
}

func TestPruneCachedKeepsNewestAndKept(t *testing.T) {
	r := newFakeRepo(t)
	for _, v := range []string{"v0.1.0", "v0.1.1", "v0.1.2", "v0.1.3"} {
		r.publish(v, false, "#!/bin/sh\necho "+v+"\n", false)
	}
	rels, _ := URL(r.srv.URL).Releases(context.Background())
	dir := t.TempDir()
	for _, rel := range rels {
		if _, err := Download(context.Background(), rel, r.key, dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, ".download-x"), 0o750); err != nil { // a download in progress
		t.Fatal(err)
	}
	removed, freed := PruneCached(r.key, dir, "v0.1.1") // v0.1.1 is the running version
	if len(removed) != 2 || freed == 0 {
		t.Fatalf("removed %v, freed %d", removed, freed)
	}
	left := ListCached(r.key, dir)
	if len(left) != 2 || left[0].Version != "v0.1.3" || left[1].Version != "v0.1.1" {
		t.Fatalf("left: %+v", left)
	}
	if _, err := os.Stat(filepath.Join(dir, ".download-x")); err != nil {
		t.Fatal("a download in progress must not be touched")
	}
}
