package installer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// setupCaddy installs Caddy (distribution package when available, otherwise the official
// repository) and adds a reverse-proxy block for the admin panel.
func setupCaddy(ctx context.Context, run *Runner, p *ServerParams) error {
	if _, err := exec.LookPath("caddy"); err != nil {
		if err := installCaddy(ctx, run); err != nil {
			return err
		}
	}
	return writeCaddyfile(ctx, run, p)
}

func installCaddy(ctx context.Context, run *Runner) error {
	apt := []string{"-o", "DPkg::Lock::Timeout=180", "-y", "-q"}
	if err := run.Run(ctx, "", "apt-get", append(apt, "update")...); err != nil {
		return err
	}
	pol, _ := run.Output(ctx, "", "apt-cache", "policy", "caddy")
	if strings.Contains(pol, "Candidate:") && !strings.Contains(pol, "Candidate: (none)") {
		return run.Run(ctx, "", "apt-get", append(apt, "install", "caddy")...)
	}
	// Official Caddy repository (Cloudsmith).
	if err := run.Run(ctx, "", "apt-get", append(apt, "install", "gpg", "ca-certificates", "apt-transport-https")...); err != nil {
		return err
	}
	key, err := httpGet(ctx, "https://dl.cloudsmith.io/public/caddy/stable/gpg.key")
	if err != nil {
		return err
	}
	dearmored, err := run.Output(ctx, string(key), "gpg", "--dearmor")
	if err != nil {
		return err
	}
	if err := os.WriteFile("/usr/share/keyrings/caddy-stable-archive-keyring.gpg", []byte(dearmored), 0o644); err != nil { //nolint:gosec // apt keyrings are world-readable
		return err
	}
	list, err := httpGet(ctx, "https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt")
	if err != nil {
		return err
	}
	if err := os.WriteFile("/etc/apt/sources.list.d/caddy-stable.list", list, 0o644); err != nil { //nolint:gosec // apt sources are world-readable
		return err
	}
	if err := run.Run(ctx, "", "apt-get", append(apt, "update")...); err != nil {
		return err
	}
	return run.Run(ctx, "", "apt-get", append(apt, "install", "caddy")...)
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(url + ": " + resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// mergeCaddyfile returns the new Caddyfile content: a stock file is replaced, a user file
// keeps its content and gets (or updates) the marked Linager block at the end.
func mergeCaddyfile(existing, block string) string {
	if IsDefaultCaddyfile(existing) {
		return block
	}
	if i := strings.Index(existing, CaddyMarker); i >= 0 {
		existing = existing[:i]
	}
	return strings.TrimRight(existing, "\n") + "\n\n" + block
}

func writeCaddyfile(ctx context.Context, run *Runner, p *ServerParams) error {
	path := p.CaddyCfgPath
	old, _ := os.ReadFile(path) //nolint:gosec // fixed Caddyfile path
	if len(bytes.TrimSpace(old)) > 0 {
		backup := path + ".linager-bak-" + time.Now().Format("20060102-150405")
		if err := os.WriteFile(backup, old, 0o644); err != nil { //nolint:gosec // Caddyfile is not secret
			return err
		}
	}
	block := RenderCaddyBlock(p.Domain, p.PublicPort, p.AdminPort)
	if err := os.MkdirAll("/etc/caddy", 0o755); err != nil { //nolint:gosec // standard Caddy config dir
		return err
	}
	if err := os.WriteFile(path, []byte(mergeCaddyfile(string(old), block)), 0o644); err != nil { //nolint:gosec // Caddyfile is not secret
		return err
	}
	if err := run.Run(ctx, "", "caddy", "validate", "--config", path, "--adapter", "caddyfile"); err != nil {
		_ = os.WriteFile(path, old, 0o644) //nolint:gosec // restoring the user's file
		return err
	}
	if err := run.Run(ctx, "", "systemctl", "enable", "caddy"); err != nil {
		return err
	}
	return run.Run(ctx, "", "systemctl", "restart", "caddy")
}
