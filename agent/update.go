package agent

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
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/internal/protocol"
	"github.com/bolgov0zero/linager/internal/release"
	"github.com/bolgov0zero/linager/update"
)

// Agent update steps (the panel shows them as progress).
var updateSteps = []string{"download", "verify", "backup", "replace", "restart"}

// RollbackDelay is how long after the restart the old binary is restored if the new agent never confirmed.
// The agent must reconnect within 60 seconds; the extra time covers the restart itself.
var RollbackDelay = protocol.AgentRollbackDelay

var agentVersionRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[A-Za-z0-9.]+)?$`)

type updateMarker struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Exe      string `json:"exe"`
	Deadline int64  `json:"deadline"`
}

func markerPath(dir string) string { return filepath.Join(dir, "update.json") }
func prevPath(dir string) string   { return filepath.Join(dir, "linager.prev") }

type updater struct {
	dir     string
	version string // running version
	emit    func(protocol.UpdateEvent)
	mu      sync.Mutex
	busy    bool
}

func serverClient(dir string) (*http.Client, string, error) {
	st, cert, err := loadState(dir)
	if err != nil {
		return nil, "", err
	}
	return &http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{TLSClientConfig: pinnedTLS(st.ServerFP, &cert)}}, "https://" + st.Server, nil
}

func fetch(ctx context.Context, c *http.Client, url string, limit int64) (io.ReadCloser, error) {
	rc, _, err := fetchSized(ctx, c, url, limit)
	return rc, err
}

// fetchSized is fetch that also returns the announced size of the body (-1 when unknown).
func fetchSized(ctx context.Context, c *http.Client, url string, limit int64) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, 0, fmt.Errorf("server answered %s", resp.Status)
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(resp.Body, limit), resp.Body}, resp.ContentLength, nil
}

// LatestFromServer asks the server which release agents should run.
func LatestFromServer(ctx context.Context, dir string) (string, error) {
	c, base, err := serverClient(dir)
	if err != nil {
		return "", err
	}
	rc, err := fetch(ctx, c, base+"/v1/release/latest", 4096)
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()
	var out struct{ Version string }
	if err := json.NewDecoder(rc).Decode(&out); err != nil || !agentVersionRe.MatchString(out.Version) {
		return "", errors.New("the server did not name a release")
	}
	return out.Version, nil
}

// progressReader reports how much of a download has arrived, at most once a second.
type progressReader struct {
	r     io.Reader
	size  int64
	n     int64
	last  time.Time
	every func(n, size int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n += int64(n)
	if p.every != nil && time.Since(p.last) >= time.Second {
		p.last = time.Now()
		p.every(p.n, p.size)
	}
	return n, err
}

// Apply downloads, verifies and installs a release from the server, then arms the rollback timer.
// It does not restart: the caller restarts the service after reporting success. Every stage is reported twice,
// when it begins and when it ends, so the panel can show a timeline.
func (u *updater) Apply(ctx context.Context, version string, key *release.Key) error {
	if !agentVersionRe.MatchString(version) {
		return errors.New("invalid version")
	}
	// A signed but older release must not replace a newer agent: old versions may have known flaws.
	if release.Compare(release.ParseVersion(version), release.ParseVersion(u.version)) < 0 {
		return fmt.Errorf("refusing to downgrade from %s to %s", u.version, version)
	}
	u.mu.Lock()
	if u.busy {
		u.mu.Unlock()
		return errBusy
	}
	u.busy = true
	u.mu.Unlock()
	defer func() { u.mu.Lock(); u.busy = false; u.mu.Unlock() }()
	if _, err := os.Stat(markerPath(u.dir)); err == nil {
		return errors.New("a previous update is still waiting for confirmation")
	}

	send := func(i int, state string, err error, bytes, size int64) {
		if u.emit == nil {
			return
		}
		e := protocol.UpdateEvent{Version: version, Step: i + 1, Total: len(updateSteps), ID: updateSteps[i], State: state, Bytes: bytes, Size: size}
		if err != nil {
			e.Error, e.State = err.Error(), protocol.UpdateFailed
		}
		u.emit(e)
	}
	begin := func(i int) { send(i, protocol.UpdateRunning, nil, 0, 0) }
	end := func(i int) { send(i, protocol.UpdateDone, nil, 0, 0) }
	fail := func(i int, err error) error { send(i, protocol.UpdateFailed, err, 0, 0); return err }

	c, base, err := serverClient(u.dir)
	if err != nil {
		return fail(0, err)
	}
	exe, err := executablePath()
	if err != nil {
		return fail(0, err)
	}
	if r, e := filepath.EvalSymlinks(exe); e == nil {
		exe = r
	}
	root := base + "/v1/release/" + version

	// download: the signed manifest and the binary. The binary stays in a temporary file that is not
	// executable and is not touched until the next stage has checked it.
	begin(0)
	mrc, _, err := fetchSized(ctx, c, root+"/manifest.json", 1<<20)
	if err != nil {
		return fail(0, err)
	}
	mb, _ := io.ReadAll(mrc)
	_ = mrc.Close()
	src, _, err := fetchSized(ctx, c, root+"/manifest.json.minisig", 4096)
	if err != nil {
		return fail(0, err)
	}
	sb, _ := io.ReadAll(src)
	_ = src.Close()
	tmp := exe + ".linager-new"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // becomes executable only after the check
	if err != nil {
		return fail(0, err)
	}
	defer func() { _ = os.Remove(tmp) }()
	brc, size, err := fetchSized(ctx, c, root+"/binary?arch="+release.HostArch, 200<<20)
	if err != nil {
		_ = f.Close()
		return fail(0, err)
	}
	h := sha256.New()
	pr := &progressReader{r: brc, size: size, every: func(n, total int64) { send(0, protocol.UpdateRunning, nil, n, total) }}
	n, err := io.Copy(io.MultiWriter(f, h), pr)
	_ = brc.Close()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fail(0, err)
	}
	send(0, protocol.UpdateDone, nil, n, n)

	// verify: the signature of the manifest, then the checksum of the binary, before anything is replaced
	begin(1)
	m, err := update.VerifyManifest(key, mb, string(sb))
	if err != nil {
		return fail(1, err)
	}
	if strings.TrimPrefix(m.Version, "v") != strings.TrimPrefix(version, "v") {
		return fail(1, errors.New("manifest version mismatch"))
	}
	asset, ok := m.Asset(release.HostArch)
	if !ok {
		return fail(1, fmt.Errorf("the release has no binary for %s", release.HostArch))
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(asset.SHA256) || (asset.Size > 0 && n != asset.Size) {
		return fail(1, errors.New("binary does not match the signed manifest"))
	}
	if err := os.Chmod(tmp, 0o755); err != nil { //nolint:gosec // the executable of the agent
		return fail(1, err)
	}
	end(1)

	// backup the running binary
	begin(2)
	if err := copyExecutable(exe, prevPath(u.dir)); err != nil {
		return fail(2, err)
	}
	end(2)

	// replace atomically, journal the update and arm the rollback timer (run by the OLD binary)
	begin(3)
	mk := updateMarker{From: u.version, To: version, Exe: exe, Deadline: time.Now().Add(RollbackDelay).Unix()}
	mb2, _ := json.Marshal(mk)
	if err := os.WriteFile(markerPath(u.dir), mb2, 0o600); err != nil {
		return fail(3, err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Remove(markerPath(u.dir))
		return fail(3, err)
	}
	if err := armRollbackFn(u.dir, exe); err != nil {
		// without the safety net the update is not allowed to proceed
		_ = copyExecutable(prevPath(u.dir), exe)
		_ = os.Remove(markerPath(u.dir))
		return fail(3, fmt.Errorf("cannot arm the rollback timer: %w", err))
	}
	end(3)
	return nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // our own binary
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755) //nolint:gosec // executable
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

// Hooks that the tests replace: the path of the running binary and the arming of the systemd timer.
var (
	executablePath = os.Executable
	armRollbackFn  = armRollback
)

func armRollback(dir, exe string) error {
	args := []string{"--unit=linager-agent-rollback", "--on-active=" + fmt.Sprint(int(RollbackDelay.Seconds())), "--description=Linager agent update rollback",
		prevPath(dir), "agent", "rollback-update", "--data-dir", dir, "--binary", exe}
	_ = exec.Command("systemctl", "stop", "linager-agent-rollback.timer", "linager-agent-rollback.service").Run()
	_ = exec.Command("systemctl", "reset-failed", "linager-agent-rollback.service").Run()
	if out, err := exec.Command("systemd-run", args...).CombinedOutput(); err != nil { //nolint:gosec // fixed binary, arguments are paths we built
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ConfirmUpdate is called once the new agent is connected: it cancels the rollback timer. It reports whether
// there was an unconfirmed update to confirm.
func ConfirmUpdate(dir string, log func(string, ...any)) bool {
	b, err := os.ReadFile(markerPath(dir))
	if err != nil {
		return false
	}
	var mk updateMarker
	_ = json.Unmarshal(b, &mk)
	_ = os.Remove(markerPath(dir))
	_ = exec.Command("systemctl", "stop", "linager-agent-rollback.timer").Run()
	if log != nil {
		log("update confirmed", "from", mk.From, "to", mk.To)
	}
	return true
}

// RollbackUpdate restores the previous binary when an update was never confirmed. It is executed by the
// transient timer using the OLD binary, so it works even if the new one cannot start at all.
func RollbackUpdate(dir, exe string) error {
	if _, err := os.Stat(markerPath(dir)); err != nil {
		return nil // already confirmed
	}
	if err := copyExecutable(prevPath(dir), exe); err != nil {
		return err
	}
	_ = os.Remove(markerPath(dir))
	return exec.Command("systemctl", "restart", "linager-agent").Run()
}

// restartSoon restarts the service after the caller had time to answer the server.
func restartSoon() {
	go func() {
		time.Sleep(1500 * time.Millisecond)
		_ = exec.Command("systemctl", "restart", "linager-agent").Run()
	}()
}

// UpdateFromServer is `linager agent update`: fetch the server's release and install it.
func UpdateFromServer(ctx context.Context, dir, runningVersion, version string, progress func(string)) error {
	key, err := release.Embedded()
	if err != nil {
		return err
	}
	if version == "" {
		if version, err = LatestFromServer(ctx, dir); err != nil {
			return err
		}
	}
	if !release.Newer(version, runningVersion) {
		return errors.New("already up to date (" + runningVersion + ")")
	}
	u := &updater{dir: dir, version: runningVersion, emit: func(e protocol.UpdateEvent) {
		if progress != nil && e.Error == "" && e.State == protocol.UpdateRunning && e.Bytes == 0 {
			progress(fmt.Sprintf("[%d/%d] %s", e.Step, e.Total, e.ID))
		}
	}}
	if err := u.Apply(ctx, version, key); err != nil {
		return err
	}
	restartSoon()
	return nil
}

// updateApplyCmd handles the server's update.apply command.
func (r *Registry) updateApplyCmd(running string) CmdFunc {
	u := &updater{dir: r.dataDir, version: running}
	return func(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
		if !dangerous {
			return nil, errDangerous
		}
		var p struct{ Version string }
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, errors.New("invalid request")
		}
		key, err := release.Embedded()
		if err != nil {
			return nil, err
		}
		u.emit = r.emitUpdate
		if err := u.Apply(ctx, p.Version, key); err != nil {
			return nil, err
		}
		restartSoon()
		r.emitUpdate(protocol.UpdateEvent{Version: p.Version, Step: 5, Total: 5, ID: "restart", State: protocol.UpdateRunning})
		return map[string]any{"ok": true, "restarting": true}, nil
	}
}

// StatusText describes the agent for `linager agent status`.
func StatusText(dir, running string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Version:   %s\n", running)
	st, _, err := loadState(dir)
	if err != nil {
		fmt.Fprintf(&b, "Enrolled:  no (%v)\n", err)
		return b.String()
	}
	fmt.Fprintf(&b, "Server:    %s\nServer id: %s\n", st.Server, st.ServerID)
	out, _ := exec.Command("systemctl", "is-active", "linager-agent").Output()
	fmt.Fprintf(&b, "Service:   %s", strings.TrimSpace(string(out)))
	if strings.TrimSpace(string(out)) == "" {
		b.WriteString("unknown")
	}
	b.WriteString("\n")
	if mb, err := os.ReadFile(markerPath(dir)); err == nil {
		var mk updateMarker
		_ = json.Unmarshal(mb, &mk)
		fmt.Fprintf(&b, "Update:    %s -> %s is waiting for confirmation (rollback at %s)\n", mk.From, mk.To, time.Unix(mk.Deadline, 0).Format(time.RFC3339))
	}
	return b.String()
}
