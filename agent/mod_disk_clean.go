package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Freeing space: a short list of things that are safe to remove. The browser only names the kind; the agent
// finds out what there is to remove itself, so a package or a path never comes from the browser.

type cleanItem struct {
	Kind   string   `json:"kind"`  // journal, apt, docker-images, docker-build, kernels
	Size   uint64   `json:"size"`  // what the thing takes
	Frees  uint64   `json:"frees"` // what removing it should free (an estimate)
	Count  int      `json:"count"`
	Items  []string `json:"items,omitempty"`
	Keep   string   `json:"keep,omitempty"`
	Active bool     `json:"-"`
}

const journalKeep = 500 << 20

var journalSizeRe = regexp.MustCompile(`take up ([0-9.]+)([KMGT]?)`)

// parseJournalUsage reads the size from `journalctl --disk-usage`.
func parseJournalUsage(out string) uint64 {
	m := journalSizeRe.FindStringSubmatch(out)
	if m == nil {
		return 0
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	mult := map[string]float64{"": 1, "K": 1 << 10, "M": 1 << 20, "G": 1 << 30, "T": 1 << 40}[m[2]]
	return uint64(f * mult)
}

// kernelPkgRe matches versioned kernel image packages, not the meta packages.
var kernelPkgRe = regexp.MustCompile(`^linux-image-[0-9][0-9a-zA-Z.+~-]*$`)

type kernelPkg struct {
	name    string
	version string
	size    uint64 // installed size in bytes
}

// oldKernels picks the installed kernel packages that can go: everything except the running kernel and the
// newest installed one.
func oldKernels(pkgs []kernelPkg, running string) []kernelPkg {
	var all []kernelPkg
	for _, p := range pkgs {
		if kernelPkgRe.MatchString(p.name) && !strings.HasSuffix(p.name, "-dbg") {
			all = append(all, p)
		}
	}
	sort.Slice(all, func(i, j int) bool { return versionLess(all[j].name, all[i].name) }) // newest first
	var out []kernelPkg
	for i, p := range all {
		if i == 0 || strings.Contains(p.name, running) && running != "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// versionLess compares kernel package names piece by piece, numbers as numbers.
func versionLess(a, b string) bool {
	split := func(s string) []string {
		return regexp.MustCompile(`[0-9]+|[^0-9]+`).FindAllString(s, -1)
	}
	x, y := split(a), split(b)
	for i := 0; i < len(x) && i < len(y); i++ {
		nx, ex := strconv.Atoi(x[i])
		ny, ey := strconv.Atoi(y[i])
		if ex == nil && ey == nil {
			if nx != ny {
				return nx < ny
			}
			continue
		}
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return len(x) < len(y)
}

func (m *diskModule) installedKernels(ctx context.Context) []kernelPkg {
	out, err := m.run(ctx, "dpkg-query", "-W", "-f", "${db:Status-Abbrev}\t${Package}\t${Version}\t${Installed-Size}\n", "linux-image-*")
	if err != nil {
		return nil
	}
	var pkgs []kernelPkg
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 4 || !strings.HasPrefix(f[0], "ii") {
			continue
		}
		kb, _ := strconv.ParseUint(strings.TrimSpace(f[3]), 10, 64)
		pkgs = append(pkgs, kernelPkg{name: f[1], version: f[2], size: kb << 10})
	}
	return pkgs
}

func (m *diskModule) runningKernel(ctx context.Context) string {
	out, err := m.run(ctx, "uname", "-r")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// dockerDF is what /system/df tells about the images and the build cache.
type dockerDF struct {
	Images []struct {
		ID         string `json:"Id"`
		Size       int64  `json:"Size"`
		Containers int    `json:"Containers"`
		RepoTags   []string
	} `json:"Images"`
	BuildCache []struct {
		Size   int64 `json:"Size"`
		InUse  bool  `json:"InUse"`
		Shared bool  `json:"Shared"`
	} `json:"BuildCache"`
}

func dirSize(dir string, glob string) uint64 {
	files, _ := filepath.Glob(filepath.Join(dir, glob))
	var n uint64
	for _, f := range files {
		if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() {
			n += uint64(st.Size()) //nolint:gosec // sizes are not negative
		}
	}
	return n
}

// cleanCandidates lists what could be cleaned now.
func (m *diskModule) cleanCandidates(ctx context.Context) []cleanItem {
	var out []cleanItem
	if b, err := m.run(ctx, "journalctl", "--disk-usage"); err == nil {
		if size := parseJournalUsage(string(b)); size > journalKeep {
			out = append(out, cleanItem{Kind: "journal", Size: size, Frees: size - journalKeep, Keep: "500M"})
		}
	}
	if n := dirSize("/var/cache/apt/archives", "*.deb"); n > 0 {
		out = append(out, cleanItem{Kind: "apt", Size: n, Frees: n})
	}
	if dockerAvailable() {
		var df dockerDF
		dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if dockerJSON(dctx, http.MethodGet, "/system/df", &df) == nil {
			var size uint64
			var names []string
			for _, im := range df.Images {
				if im.Containers == 0 && im.Size > 0 {
					size += uint64(im.Size)
					name := shortImageID(im.ID)
					if len(im.RepoTags) > 0 {
						name = im.RepoTags[0]
					}
					names = append(names, name)
				}
			}
			if len(names) > 0 {
				sort.Strings(names)
				out = append(out, cleanItem{Kind: "docker-images", Size: size, Frees: size, Count: len(names), Items: names})
			}
			var bc uint64
			for _, c := range df.BuildCache {
				if !c.InUse && c.Size > 0 {
					bc += uint64(c.Size)
				}
			}
			if bc > 0 {
				out = append(out, cleanItem{Kind: "docker-build", Size: bc, Frees: bc})
			}
		}
		cancel()
	}
	if old := oldKernels(m.installedKernels(ctx), m.runningKernel(ctx)); len(old) > 0 {
		var size uint64
		var names []string
		for _, k := range old {
			size += k.size
			names = append(names, k.name)
		}
		out = append(out, cleanItem{Kind: "kernels", Size: size, Frees: size, Count: len(old), Items: names})
	}
	return out
}

func (m *diskModule) totalAvail() uint64 {
	var n uint64
	for _, f := range m.filesystems() {
		n += f.avail
	}
	return n
}

// cleanCmd is "disk.clean": {kind}. The agent works out the targets itself.
func (m *diskModule) cleanCmd(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p struct{ Kind string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	before := m.totalAvail()
	var output string
	var err error
	var reclaimed uint64
	switch p.Kind {
	case "journal":
		var b []byte
		b, err = m.run(ctx, "journalctl", "--vacuum-size=500M")
		output = tail(string(b))
	case "apt":
		var b []byte
		b, err = m.run(ctx, "apt-get", "clean")
		output = tail(string(b))
	case "docker-images":
		if !dockerAvailable() {
			return nil, errors.New("docker is not available")
		}
		var res struct{ SpaceReclaimed uint64 }
		q := url.Values{"filters": {`{"dangling":["false"]}`}}
		err = dockerSend(ctx, http.MethodPost, "/images/prune?"+q.Encode(), nil, &res)
		reclaimed = res.SpaceReclaimed
	case "docker-build":
		if !dockerAvailable() {
			return nil, errors.New("docker is not available")
		}
		var res struct{ SpaceReclaimed uint64 }
		err = dockerSend(ctx, http.MethodPost, "/build/prune?all=true", nil, &res)
		reclaimed = res.SpaceReclaimed
	case "kernels":
		old := oldKernels(m.installedKernels(ctx), m.runningKernel(ctx))
		if len(old) == 0 {
			return nil, errors.New("there are no old kernels to remove")
		}
		args := []string{"purge", "-y", "-o", "DPkg::Lock::Timeout=120"}
		for _, k := range old {
			args = append(args, k.name)
		}
		var b []byte
		b, err = m.run(ctx, "apt-get", args...)
		output = tail(string(b))
	default:
		return nil, errors.New("unknown kind")
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", cmdMessage(err), output)
	}
	m.forget()
	freed := reclaimed
	if after := m.totalAvail(); after > before && freed == 0 {
		freed = after - before
	}
	return map[string]any{"ok": true, "freed": freed, "output": output}, nil
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 2000 {
		s = s[len(s)-2000:]
	}
	return s
}
