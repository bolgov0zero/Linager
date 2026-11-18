//go:build windows

package agent

import (
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Uninstall removes the agent identity and its service from this host. With purge it also deletes the data
// directory and the configuration. The files of the program stay: remove the MSI package to delete them.
func Uninstall(purge bool, dataDir string) error {
	// Always drop the identity so a stray restart cannot reconnect.
	for _, f := range []string{certFile, stateFile} {
		_ = os.Remove(filepath.Join(dataDir, f))
	}
	if purge {
		_ = os.RemoveAll(dataDir)
		_ = os.Remove(DefaultConfigPath)
	}
	m, err := mgr.Connect()
	if err != nil {
		return nil
	}
	defer func() { _ = m.Disconnect() }()
	s, err := m.OpenService(winServiceName)
	if err != nil {
		return nil // already removed
	}
	defer func() { _ = s.Close() }()
	_ = s.Delete() // the service disappears when it stops
	// This process is the service: the stop must not wait for itself, so it follows from another connection.
	go func() {
		time.Sleep(time.Second)
		m2, err := mgr.Connect()
		if err != nil {
			return
		}
		defer func() { _ = m2.Disconnect() }()
		if s2, err := m2.OpenService(winServiceName); err == nil {
			_, _ = s2.Control(svc.Stop)
			_ = s2.Close()
		}
	}()
	return nil
}
