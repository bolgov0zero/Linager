// Package update finds, downloads and verifies Linager releases.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bolgov0zero/linager/internal/release"
)

// DefaultRepo is the GitHub repository releases are read from.
const DefaultRepo = "bolgov0zero/linager"

// Release is one published release.
type Release struct {
	Tag        string
	Prerelease bool
	Notes      string
	Assets     map[string]string // asset name -> download URL
}

// Complete reports whether the release has everything needed for an update.
func (r Release) Complete() bool {
	for _, n := range []string{release.BinaryAsset, "manifest.json", "manifest.json.minisig"} {
		if r.Assets[n] == "" {
			return false
		}
	}
	return true
}

// Source lists releases.
type Source interface {
	Releases(ctx context.Context) ([]Release, error)
}

type ghRelease struct {
	Tag        string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
	Body       string `json:"body"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

type jsonSource struct {
	url    string
	client *http.Client
}

// GitHub returns the source for a "owner/repo" repository.
func GitHub(repo string) Source {
	if repo == "" {
		repo = DefaultRepo
	}
	return &jsonSource{url: "https://api.github.com/repos/" + repo + "/releases?per_page=30", client: &http.Client{Timeout: 30 * time.Second}}
}

// URL returns a source reading a GitHub-shaped releases.json from base (a mirror or an offline server).
func URL(base string) Source {
	return &jsonSource{url: strings.TrimRight(base, "/") + "/releases.json", client: &http.Client{Timeout: 30 * time.Second}}
}

// NewSource picks GitHub for an empty value or "owner/repo", otherwise treats it as a mirror URL.
func NewSource(spec string) Source {
	if strings.HasPrefix(spec, "http://") || strings.HasPrefix(spec, "https://") {
		return URL(spec)
	}
	return GitHub(spec)
}

func (s *jsonSource) Releases(ctx context.Context) ([]Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "linager-updater")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release list: %s", resp.Status)
	}
	var raw []ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Release, 0, len(raw))
	for _, g := range raw {
		if g.Draft {
			continue
		}
		r := Release{Tag: g.Tag, Prerelease: g.Prerelease, Notes: g.Body, Assets: map[string]string{}}
		for _, a := range g.Assets {
			r.Assets[a.Name] = a.URL
		}
		out = append(out, r)
	}
	return out, nil
}

// Channel names.
const (
	Stable = "stable"
	Beta   = "beta"
)

// Latest returns the highest complete release for the channel (stable skips pre-releases).
func Latest(rels []Release, channel string) (Release, bool) {
	var best Release
	found := false
	for _, r := range rels {
		v := release.ParseVersion(r.Tag)
		if !r.Complete() || !v.Valid {
			continue
		}
		// The pre-release flag comes from the (unsigned) release list; the tag is bound to the signed
		// manifest, so a version with a suffix never counts as stable whatever the list claims.
		if channel != Beta && (r.Prerelease || v.Pre != "") {
			continue
		}
		if !found || release.Newer(r.Tag, best.Tag) {
			best, found = r, true
		}
	}
	return best, found
}

// Find returns the release with the given tag (with or without a leading v).
func Find(rels []Release, version string) (Release, error) {
	want := strings.TrimPrefix(version, "v")
	for _, r := range rels {
		if strings.TrimPrefix(r.Tag, "v") == want {
			if !r.Complete() {
				return Release{}, errors.New("release " + r.Tag + " is incomplete")
			}
			return r, nil
		}
	}
	return Release{}, errors.New("release " + version + " not found")
}

// NotesLines turns release notes into short bullet lines.
func NotesLines(body string) []string {
	out := []string{} // never nil: the web reads notes.length
	full := false
	for _, raw := range strings.Split(body, "\n") {
		l := strings.TrimSpace(raw)
		item := strings.TrimSpace(strings.TrimLeft(l, "-*• "))
		if item == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(strings.ToLower(item), "full changelog") {
			continue
		}
		// An indented line that is not a new bullet continues the previous item (wrapped CHANGELOG lines).
		if len(out) > 0 && raw != l && !strings.ContainsAny(l[:1], "-*•") {
			if !full {
				out[len(out)-1] += " " + item
			}
			continue
		}
		if full = len(out) >= 12; !full {
			out = append(out, item)
		}
	}
	return out
}
