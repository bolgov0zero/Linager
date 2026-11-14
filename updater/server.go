// Package updater performs the privileged server update: verify, back up, stop, replace,
// migrate, start, health-check and roll back on failure.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/bolgov0zero/linager/internal/config"
	"github.com/bolgov0zero/linager/internal/release"
	"github.com/bolgov0zero/linager/server/store"
	"github.com/bolgov0zero/linager/update"
)

// Step ids shown in the admin panel.
const (
	StepVerify  = "verify"
	StepBackup  = "backup"
	StepStop    = "stop"
	StepReplace = "replace"
	StepMigrate = "migrate"
	StepStart   = "start"
)

var stepOrder = []string{StepVerify, StepBackup, StepStop, StepReplace, StepMigrate, StepStart}

// Step is one entry of the progress list.
type Step struct {
	ID    string `json:"id"`
	State string `json:"state"` // pending | running | done | failed
}

// Status is written to the status file after every step; the panel reads it.
type Status struct {
	RunID      string `json:"run_id"`
	State      string `json:"state"` // idle | running | done | failed | rolled_back
	Steps      []Step `json:"steps"`
	From       string `json:"from"`
	To         string `json:"to"`
	Error      string `json:"error,omitempty"`
	StartedAt  int64  `json:"started_at"`
	FinishedAt int64  `json:"finished_at"`
}

// Request is what the panel asks the root updater to do (file written by the server user).
type Request struct {
	Version string `json:"version"`
	Channel string `json:"channel"`
	By      string `json:"by"`
}

var versionRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[A-Za-z0-9.]+)?$`)

// Deps are the collaborators of an update run; tests replace them.
type Deps struct {
	Cfg            *config.Server
	ConfigPath     string
	BinPath        string
	CacheDir       string
	PrevDir        string
	StatusPath     string
	RequestPath    string
	Source         update.Source
	Key            *release.Key
	CurrentVersion string
	Run            func(ctx context.Context, name string, args ...string) ([]byte, error)
	Backup         func(ctx context.Context) (string, error)
	Health         func(ctx context.Context) error
	HealthTimeout  time.Duration
	Record         func(ctx context.Context, r store.UpdateRun)
	Now            func() time.Time
	OnStatus       func(Status)
}

type runner struct {
	d  Deps
	st Status
}

func (r *runner) now() time.Time {
	if r.d.Now != nil {
		return r.d.Now()
	}
	return time.Now()
}

func (r *runner) save() {
	b, _ := json.Marshal(r.st)
	tmp := r.d.StatusPath + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil { //nolint:gosec // the unprivileged server reads this file
		_ = os.Rename(tmp, r.d.StatusPath)
	}
	if r.d.OnStatus != nil {
		r.d.OnStatus(r.st)
	}
}

func (r *runner) set(id, state string) {
	for i := range r.st.Steps {
		if r.st.Steps[i].ID == id {
			r.st.Steps[i].State = state
		}
	}
	r.save()
}

// ReadStatus loads the status file (idle when missing).
func ReadStatus(path string) Status {
	b, err := os.ReadFile(path) //nolint:gosec // fixed status path
	if err != nil {
		return Status{State: "idle", Steps: []Step{}}
	}
	var s Status
	if json.Unmarshal(b, &s) != nil {
		return Status{State: "idle", Steps: []Step{}}
	}
	return s
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src) //nolint:gosec // paths are built by the updater itself
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) //nolint:gosec // see above
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// ServerUpdate runs one update to version ("" = latest of the channel). It never touches the system
// before the release is verified and the backup exists.
func ServerUpdate(ctx context.Context, d Deps, version, channel string) (err error) {
	r := &runner{d: d}
	r.st = Status{RunID: fmt.Sprintf("%d", r.now().UnixNano()), State: "running", From: d.CurrentVersion, StartedAt: r.now().Unix()}
	for _, id := range stepOrder {
		r.st.Steps = append(r.st.Steps, Step{ID: id, State: "pending"})
	}
	r.save()
	defer func() {
		r.st.FinishedAt = r.now().Unix()
		switch {
		case err == nil:
			r.st.State = "done"
		case r.st.State != "rolled_back":
			r.st.State = "failed"
		}
		if err != nil {
			r.st.Error = err.Error()
		}
		r.save()
		if d.Record != nil {
			status := map[string]string{"done": "success", "rolled_back": "rolled_back"}[r.st.State]
			if status == "" {
				status = "failed"
			}
			d.Record(context.WithoutCancel(ctx), store.UpdateRun{Kind: "server", FromVersion: d.CurrentVersion, ToVersion: r.st.To, Status: status, Detail: r.st.Error})
		}
	}()

	// 1. verify
	r.set(StepVerify, "running")
	cached, verr := r.prepare(ctx, version, channel)
	if verr != nil {
		r.set(StepVerify, "failed")
		return verr
	}
	r.st.To = cached.Version
	if !release.Newer(cached.Version, d.CurrentVersion) && version == "" {
		r.set(StepVerify, "failed")
		return errors.New("already up to date")
	}
	r.set(StepVerify, "done")

	// 2. backup (nothing has been changed yet; a failure here aborts safely)
	r.set(StepBackup, "running")
	if d.Backup != nil {
		if _, berr := d.Backup(ctx); berr != nil {
			r.set(StepBackup, "failed")
			return fmt.Errorf("backup: %w", berr)
		}
	}
	r.set(StepBackup, "done")

	// 3. stop
	r.set(StepStop, "running")
	if out, serr := d.Run(ctx, "systemctl", "stop", "linager-server"); serr != nil {
		r.set(StepStop, "failed")
		return fmt.Errorf("stop service: %s", lastLine(out, serr))
	}
	r.set(StepStop, "done")

	// 4. replace the binary, keeping the previous one for rollback
	r.set(StepReplace, "running")
	prev := filepath.Join(d.PrevDir, "linager-"+strings.TrimPrefix(d.CurrentVersion, "v"))
	_ = os.MkdirAll(d.PrevDir, 0o750)
	if cerr := copyFile(d.BinPath, prev, 0o755); cerr != nil {
		r.set(StepReplace, "failed")
		_, _ = d.Run(ctx, "systemctl", "start", "linager-server")
		return fmt.Errorf("keep previous binary: %w", cerr)
	}
	if cerr := copyFile(cached.BinaryPath(), d.BinPath, 0o755); cerr != nil {
		r.set(StepReplace, "failed")
		r.rollback(ctx, prev)
		return fmt.Errorf("install binary: %w", cerr)
	}
	r.set(StepReplace, "done")

	// 5. migrations (forward only)
	r.set(StepMigrate, "running")
	if out, merr := d.Run(ctx, d.BinPath, "server", "migrate", "--config", d.ConfigPath); merr != nil {
		r.set(StepMigrate, "failed")
		r.rollback(ctx, prev)
		return fmt.Errorf("migrations: %s", lastLine(out, merr))
	}
	r.set(StepMigrate, "done")

	// 6. start and check
	r.set(StepStart, "running")
	if out, serr := d.Run(ctx, "systemctl", "start", "linager-server"); serr != nil {
		r.set(StepStart, "failed")
		r.rollback(ctx, prev)
		return fmt.Errorf("start service: %s", lastLine(out, serr))
	}
	if herr := r.waitHealthy(ctx); herr != nil {
		r.set(StepStart, "failed")
		r.rollback(ctx, prev)
		return fmt.Errorf("the new version did not become healthy: %w", herr)
	}
	r.set(StepStart, "done")
	return nil
}

func lastLine(out []byte, err error) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}

// prepare returns a verified cached release, downloading it when needed.
func (r *runner) prepare(ctx context.Context, version, channel string) (*update.Cached, error) {
	if version != "" && !versionRe.MatchString(version) {
		return nil, errors.New("invalid version")
	}
	if version != "" {
		if c, err := update.LoadCached(r.d.Key, r.d.CacheDir, version); err == nil {
			return c, nil
		}
	}
	rels, err := r.d.Source.Releases(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot read releases: %w", err)
	}
	var rel update.Release
	if version == "" {
		l, ok := update.Latest(rels, channel)
		if !ok {
			return nil, errors.New("no release available")
		}
		rel = l
		if c, err := update.LoadCached(r.d.Key, r.d.CacheDir, rel.Tag); err == nil {
			return c, nil
		}
	} else if rel, err = update.Find(rels, version); err != nil {
		return nil, err
	}
	c, err := update.Download(ctx, rel, r.d.Key, r.d.CacheDir)
	if err == nil {
		r.shareCache()
	}
	return c, err
}

// shareCache gives the release cache to the owner of the data directory: the updater runs as root, but
// the unprivileged server serves the cached releases to agents.
func (r *runner) shareCache() {
	if r.d.Cfg == nil {
		return
	}
	st, err := os.Stat(r.d.Cfg.DataDir)
	if err != nil {
		return
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	_ = filepath.Walk(r.d.CacheDir, func(p string, _ os.FileInfo, err error) error {
		if err == nil {
			_ = os.Lchown(p, int(sys.Uid), int(sys.Gid)) //nolint:gosec // best-effort ownership change of our own cache directory
		}
		return nil
	})
}

func (r *runner) rollback(ctx context.Context, prev string) {
	bg := context.WithoutCancel(ctx)
	_, _ = r.d.Run(bg, "systemctl", "stop", "linager-server")
	_ = copyFile(prev, r.d.BinPath, 0o755)
	_, _ = r.d.Run(bg, "systemctl", "start", "linager-server")
	r.st.State = "rolled_back"
}

func (r *runner) waitHealthy(ctx context.Context) error {
	timeout := r.d.HealthTimeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for { // always check at least once
		last := r.d.Health(ctx)
		if last == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return last
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// ApplyRequest reads and removes the request file, then runs the update.
func ApplyRequest(ctx context.Context, d Deps) error {
	b, err := os.ReadFile(d.RequestPath) //nolint:gosec // fixed request path
	if err != nil {
		return err
	}
	_ = os.Remove(d.RequestPath)
	var req Request
	if err := json.Unmarshal(b, &req); err != nil {
		return err
	}
	if req.Channel != update.Beta {
		req.Channel = update.Stable
	}
	return ServerUpdate(ctx, d, req.Version, req.Channel)
}

// HealthCheck builds the default check: the admin panel answers 200 and the agent port accepts TCP.
func HealthCheck(cfg *config.Server) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		host, port, err := net.SplitHostPort(cfg.Admin.Listen)
		if err != nil {
			return err
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/", nil)
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("admin panel answered %s", resp.Status)
		}
		_, ap, err := net.SplitHostPort(cfg.Agents.Listen)
		if err != nil {
			return err
		}
		c, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", ap))
		if err != nil {
			return fmt.Errorf("agent port: %w", err)
		}
		_ = c.Close()
		return nil
	}
}
