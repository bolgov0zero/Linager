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

// BinaryPath is the cached executable for this machine's architecture.
func (c Cached) BinaryPath() string { return c.BinaryPathFor(release.HostArch) }

// BinaryPathFor is the cached executable for arch.
func (c Cached) BinaryPathFor(arch string) string {
	return filepath.Join(c.Dir, release.BinaryAsset(arch))
}

// Asset is the manifest entry of this machine's binary.
func (c Cached) Asset() release.Asset {
	a, _ := c.Manifest.Asset(release.HostArch)
	return a
}

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
	if err := m.Validate(); err != nil {
		return nil, err
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
	assets := m.AssetList()
	if _, ok := assets[release.HostArch]; !ok {
		return nil, errors.New("the release has no binary for this machine (" + release.HostArch + ")")
	}
	var hostSize int64
	// The Windows agent is optional: a release that failed to build it still serves Linux.
	if wa, ok := m.WindowsAsset(); ok && rel.Assets[wa.Name] != "" {
		if _, err := downloadAsset(ctx, rel, wa, filepath.Join(tmp, wa.Name)); err != nil {
			return nil, fmt.Errorf("%s: %w", wa.Name, err)
		}
	}
	for arch, a := range assets {
		n, err := downloadAsset(ctx, rel, a, filepath.Join(tmp, a.Name))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.Name, err)
		}
		if arch == release.HostArch {
			hostSize = n
		}
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
	return &Cached{Version: normVersion(m.Version), Dir: dst, Manifest: *m, Size: hostSize, Time: time.Now()}, nil
}

// downloadAsset fetches one binary into path and checks it against its manifest entry.
func downloadAsset(ctx context.Context, rel Release, a release.Asset, path string) (int64, error) {
	url := rel.Assets[a.Name]
	if url == "" {
		return 0, errors.New("the release does not contain this asset")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o755) //nolint:gosec // executable
	if err != nil {
		return 0, err
	}
	rc, err := get(ctx, url, maxBinary)
	if err != nil {
		_ = f.Close()
		return 0, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), rc)
	_ = rc.Close()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, err
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(a.SHA256) || (a.Size > 0 && n != a.Size) {
		return 0, errors.New("binary does not match the signed manifest (checksum mismatch)")
	}
	return n, nil
}

// checkFile hashes the file at path and compares it with the manifest entry.
func checkFile(path string, a release.Asset) (int64, error) {
	f, err := os.Open(path) //nolint:gosec // cache path built from a validated version and a fixed name
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, err
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(a.SHA256) || (a.Size > 0 && n != a.Size) {
		return 0, errors.New("cached binary does not match its manifest")
	}
	return n, nil
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
	assets := m.AssetList()
	if _, ok := assets[release.HostArch]; !ok {
		return nil, errors.New("the release has no binary for this machine (" + release.HostArch + ")")
	}
	for arch, a := range assets {
		n, err := checkFile(c.BinaryPathFor(arch), a)
		if err != nil {
			return nil, err
		}
		if arch == release.HostArch {
			c.Size = n
		}
	}
	if wa, ok := m.WindowsAsset(); ok {
		// optional: a release without the file is still good for Linux, a file that is there must match
		if _, err := os.Stat(filepath.Join(dir, wa.Name)); err == nil {
			if _, err := checkFile(filepath.Join(dir, wa.Name), wa); err != nil {
				return nil, err
			}
		}
	}
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
