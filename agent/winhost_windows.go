//go:build windows

package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bolgov0zero/linager/internal/config"
)

// bootstrapKey is where the MSI package stores the address of the server (SERVER and FINGERPRINT properties).
const bootstrapKey = `SOFTWARE\Linager\Agent`

// bootstrap returns the server address and the fingerprint the package was installed with.
func bootstrap() (server, fingerprint string) {
	return strings.TrimSpace(regString(bootstrapKey, "Server")), strings.TrimSpace(regString(bootstrapKey, "Fingerprint"))
}

// HostOptions configure RunHost.
type HostOptions struct {
	DataDir      string
	ConfigPath   string
	AgentVersion string
	Log          *slog.Logger
}

func enrolled(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, stateFile))
	return err == nil
}

// RunHost is the life of the Windows agent: it pairs with the server when it has the address and no identity yet,
// then keeps the connection. It returns when ctx ends, or after the server removed this agent.
func RunHost(ctx context.Context, o HostOptions) error {
	pause := func(d time.Duration) bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(d):
			return true
		}
	}
	notified := false
	for ctx.Err() == nil {
		if !enrolled(o.DataDir) {
			server, fp := bootstrap()
			if server == "" {
				if !notified {
					o.Log.Info("this agent is not paired with a server: install the package with SERVER=host:port or run `linager agent enroll --server host:port`")
					notified = true
				}
				if !pause(10 * time.Second) {
					return nil
				}
				continue
			}
			if err := pairHost(ctx, o, server, fp); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				o.Log.Warn("pairing failed", "err", err, "retry_in", "30s")
				if !pause(30 * time.Second) {
					return nil
				}
			}
			continue
		}
		cfg, err := config.LoadAgent(o.ConfigPath)
		if err != nil {
			o.Log.Error("cannot read the agent configuration, all modules of Windows are enabled", "err", err)
			cfg = config.DefaultAgent()
		}
		err = Run(ctx, RunOptions{DataDir: o.DataDir, AgentVersion: o.AgentVersion, Config: cfg, Log: o.Log})
		switch {
		case errors.Is(err, ErrRevoked), errors.Is(err, ErrUninstalled):
			o.Log.Warn("the agent stops", "reason", err)
			return nil
		case ctx.Err() != nil:
			return nil
		case errors.Is(err, ErrNotEnrolled):
			continue
		}
		o.Log.Error("the agent stopped, starting again", "err", err)
		if !pause(5 * time.Second) {
			return nil
		}
	}
	return nil
}

// pairHost asks the server to accept this host. Nobody sits in front of a service, so the key and the check code
// go to a file that only administrators can read; the person in the panel compares the code, which also protects
// the pairing when no fingerprint was given.
func pairHost(ctx context.Context, o HostOptions, server, fp string) error {
	note := filepath.Join(o.DataDir, "enroll.txt")
	defer func() { _ = os.Remove(note) }()
	return Enroll(ctx, EnrollOptions{
		Server: server, Fingerprint: fp, DataDir: o.DataDir, AgentVersion: o.AgentVersion,
		Confirm: func(string) bool { return true },
		Show: func(key, code, f string) {
			text := fmt.Sprintf("Key:         %s\r\nCheck code:  %s\r\nFingerprint: %s\r\n\r\nEnter the key in the Linager panel and make sure the check code matches.\r\n", key, code, f)
			_ = os.WriteFile(note, []byte(text), 0o600)
			o.Log.Info("waiting for approval in the panel", "key", key, "check_code", code, "fingerprint", f, "file", note)
		},
	})
}

// OpenLog opens the log file of the service for appending. A file over 5 MB is moved aside first.
func OpenLog(path string) (io.WriteCloser, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	_ = secureDir(dir) // the folder of the log and the configuration belongs to SYSTEM and the administrators
	if st, err := os.Stat(path); err == nil && st.Size() > 5<<20 {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // our own log file
}
