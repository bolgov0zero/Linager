//go:build windows

package agent

import (
	"errors"

	"golang.org/x/sys/windows/svc"
)

// errNoInPlaceUpdate is why the panel cannot update a Windows agent: a new version is installed with its MSI.
var errNoInPlaceUpdate = errors.New("this agent is updated by installing the new MSI package")

// armRollback refuses: an update in place is not available on Windows, so nothing is replaced.
func armRollback(string, string) error { return errNoInPlaceUpdate }

func stopRollbackTimer() {}

func restartService() error { return errNoInPlaceUpdate }

// serviceState is the state of the agent service as the service control manager reports it.
func serviceState() string {
	m, err := connectSCM()
	if err != nil {
		return "unknown"
	}
	defer func() { _ = m.Disconnect() }()
	s, err := openLimited(m, winServiceName)
	if err != nil {
		return "not installed"
	}
	defer func() { _ = s.Close() }()
	st, err := s.Query()
	if err != nil {
		return "unknown"
	}
	_, sub := stateNames(st.State)
	if st.State == svc.Running {
		return "running"
	}
	return sub
}
