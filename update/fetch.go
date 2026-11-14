package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bolgov0zero/linager/internal/release"
)

const maxBinary = 200 << 20

// Cached is a release stored on disk.
type Cached struct {
	Version  string
	Dir      string
	Manifest release.Manifest
	Size     int64
	Time     time.Time
}

// BinaryPath is the cached executable.
func (c Cached) BinaryPath() string { return filepath.Join(c.Dir, release.BinaryAsset) }

func normVersion(v string) string { return "v" + strings.TrimPrefix(v, "v") }

func get(ctx context.Context, url string, limit int64) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "linager-updater")
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(resp.Body, limit), resp.Body}, nil
}

func readAll(ctx context.Context, url string, limit int64) ([]byte, error) {
	rc, err := get(ctx, url, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

// VerifyManifest checks the signature of manifest.json bytes and parses it.
func VerifyManifest(key *release.Key, manifest []byte, sig string) (*release.Manifest, error) {
	if err := key.Verify(manifest, sig); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	var m release.Manifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		return nil, err
	}
	if !release.ParseVersion(m.Version).Valid || len(m.SHA256) != 64 {
		return nil, errors.New("manifest is invalid")
	}
	return &m, nil
}

// Download fetches a release into cacheDir/<version>, verifying the manifest signature and the
// binary's SHA-256. The directory is only created once everything is verified.
func Download(ctx context.Context, rel Release, key *release.Key, cacheDir string) (*Cached, error) {
	mb, err := readAll(ctx, rel.Assets["manifest.json"], 1<<20)
	if err != nil {
		return nil, err
	}
	sb, err := readAll(ctx, rel.Assets["manifest.json.minisig"], 4096)
	if err != nil {
		return nil, err
	}
	m, err := VerifyManifest(key, mb, string(sb))
	if err != nil {
		return nil, err
	}
	if normVersion(m.Version) != normVersion(rel.Tag) {
		return nil, errors.New("manifest version does not match the release tag")
	}
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(cacheDir, ".download-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	bin := filepath.Join(tmp, release.BinaryAsset)
	f, err := os.OpenFile(bin, os.O_CREATE|os.O_WRONLY, 0o755) //nolint:gosec // executable
	if err != nil {
		return nil, err
	}
	rc, err := get(ctx, rel.Assets[release.BinaryAsset], maxBinary)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), rc)
	_ = rc.Close()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(m.SHA256) || (m.Size > 0 && n != m.Size) {
		return nil, errors.New("binary does not match the signed manifest (checksum mismatch)")
	}
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), mb, 0o644); err != nil { //nolint:gosec // not secret
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json.minisig"), sb, 0o644); err != nil { //nolint:gosec // not secret
		return nil, err
	}
	dst := filepath.Join(cacheDir, normVersion(m.Version))
	_ = os.RemoveAll(dst)
	if err := os.Rename(tmp, dst); err != nil {
		return nil, err
	}
	_ = os.Chmod(dst, 0o755) //nolint:gosec // the agent port serves this directory's files read-only
	return &Cached{Version: normVersion(m.Version), Dir: dst, Manifest: *m, Size: n, Time: time.Now()}, nil
}

// LoadCached re-verifies one cached release (signature and checksum) and returns it.
func LoadCached(key *release.Key, cacheDir, version string) (*Cached, error) {
	dir := filepath.Join(cacheDir, normVersion(version))
	mb, err := os.ReadFile(filepath.Join(dir, "manifest.json")) //nolint:gosec // cache path built from a validated version
	if err != nil {
		return nil, err
	}
	sb, err := os.ReadFile(filepath.Join(dir, "manifest.json.minisig")) //nolint:gosec // see above
	if err != nil {
		return nil, err
	}
	m, err := VerifyManifest(key, mb, string(sb))
	if err != nil {
		return nil, err
	}
	c := &Cached{Version: normVersion(m.Version), Dir: dir, Manifest: *m}
	f, err := os.Open(c.BinaryPath()) //nolint:gosec // see above
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return nil, err
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(m.SHA256) {
		return nil, errors.New("cached binary does not match its manifest")
	}
	c.Size = n
	if st, err := os.Stat(c.BinaryPath()); err == nil {
		c.Time = st.ModTime()
	}
	return c, nil
}

// ListCached lists verified cached releases, newest first; broken ones are skipped.
func ListCached(key *release.Key, cacheDir string) []Cached {
	ents, _ := os.ReadDir(cacheDir)
	var out []Cached
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if c, err := LoadCached(key, cacheDir, e.Name()); err == nil {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return release.Newer(out[i].Version, out[j].Version) })
	return out
}

// PruneCached removes cached releases: everything except the newest verified one and the versions in keep.
// Directories that do not verify are removed too; hidden ones (downloads in progress) are left alone.
// It returns the removed versions and the bytes freed.
func PruneCached(key *release.Key, cacheDir string, keep ...string) (removed []string, freed int64) {
	good := ListCached(key, cacheDir)
	keepSet := map[string]bool{}
	for _, k := range keep {
		keepSet[normVersion(k)] = true
	}
	if len(good) > 0 {
		keepSet[good[0].Version] = true
	}
	ents, _ := os.ReadDir(cacheDir)
	for _, e := range ents {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || keepSet[normVersion(e.Name())] {
			continue
		}
		dir := filepath.Join(cacheDir, e.Name())
		size := dirSize(dir)
		if err := os.RemoveAll(dir); err != nil {
			continue
		}
		removed = append(removed, e.Name())
		freed += size
	}
	return removed, freed
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, ierr := d.Info(); ierr == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}
