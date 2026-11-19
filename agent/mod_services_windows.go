//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// winServiceName is the name of the agent's own service.
const winServiceName = "LinagerAgent"

// Unit is one Windows service as shown in the panel. The fields follow the Linux ones: Active is
// active/inactive/activating/deactivating/failed, Sub is the Windows state, Enabled is enabled/static/disabled.
type Unit struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Enabled     string `json:"enabled"`
}

var unitRe = regexp.MustCompile(`^[a-zA-Z0-9:_.@$()-]{1,256}$`)

var svcVerbs = map[string]bool{"start": true, "stop": true, "restart": true, "enable": true, "disable": true}

// stateNames maps the Windows service states to the words of the panel.
func stateNames(s svc.State) (active, sub string) {
	switch s {
	case svc.Running:
		return "active", "running"
	case svc.Stopped:
		return "inactive", "dead"
	case svc.StartPending:
		return "activating", "start-pending"
	case svc.StopPending:
		return "deactivating", "stop-pending"
	case svc.ContinuePending:
		return "activating", "continue-pending"
	case svc.PausePending:
		return "deactivating", "pause-pending"
	case svc.Paused:
		return "inactive", "paused"
	}
	return "inactive", "unknown"
}

// serviceFailed tells whether an automatic service is stopped with an error. Both the counter of the dashboard and the list of
// the panel use it, so that a service the counter names is marked "failed" in the list.
func serviceFailed(startType uint32, st svc.Status) bool {
	if startType != mgr.StartAutomatic || st.State != svc.Stopped {
		return false
	}
	// 1077: never started since boot; 0: stopped by itself without an error (triggered or delayed services)
	return st.Win32ExitCode != 0 && st.Win32ExitCode != uint32(windows.ERROR_SERVICE_NEVER_STARTED)
}

func startTypeName(t uint32) string {
	switch t {
	case mgr.StartAutomatic:
		return "enabled"
	case mgr.StartManual:
		return "static"
	case mgr.StartDisabled:
		return "disabled"
	}
	return "static"
}

// openLimited opens a service for reading only, so that protected services can still be listed.
func openLimited(m *mgr.Mgr, name string) (*mgr.Service, error) {
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.OpenService(m.Handle, n, windows.SERVICE_QUERY_STATUS|windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		return nil, err
	}
	return &mgr.Service{Name: name, Handle: h}, nil
}

// connectSCM opens the service control manager for reading.
func connectSCM() (*mgr.Mgr, error) {
	h, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT|windows.SC_MANAGER_ENUMERATE_SERVICE)
	if err != nil {
		return nil, err
	}
	return &mgr.Mgr{Handle: h}, nil
}

func listServices() ([]Unit, error) {
	m, err := connectSCM()
	if err != nil {
		return nil, err
	}
	defer func() { _ = m.Disconnect() }()
	names, err := m.ListServices()
	if err != nil {
		return nil, err
	}
	out := make([]Unit, 0, len(names))
	for _, n := range names {
		s, err := openLimited(m, n)
		if err != nil {
			continue
		}
		u := Unit{Name: n}
		st, qerr := s.Query()
		if qerr == nil {
			u.Active, u.Sub = stateNames(st.State)
		}
		if cfg, err := s.Config(); err == nil {
			u.Enabled = startTypeName(cfg.StartType)
			u.Description = cfg.DisplayName
			if qerr == nil && serviceFailed(cfg.StartType, st) {
				u.Active, u.Sub = "failed", "failed"
			}
		}
		_ = s.Close()
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// failedServices names the automatic services that are stopped with an error.
func failedServices() []string {
	m, err := connectSCM()
	if err != nil {
		return nil
	}
	defer func() { _ = m.Disconnect() }()
	names, err := m.ListServices()
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range names {
		s, err := openLimited(m, n)
		if err != nil {
			continue
		}
		cfg, cerr := s.Config()
		st, qerr := s.Query()
		_ = s.Close()
		if cerr == nil && qerr == nil && serviceFailed(cfg.StartType, st) {
			out = append(out, n)
		}
	}
	return out
}

func svcList(_ context.Context, _ json.RawMessage, _ bool) (any, error) {
	units, err := listServices()
	if err != nil {
		return nil, errors.New("cannot read the services: " + err.Error())
	}
	return map[string]any{"services": units}, nil
}

func svcAction(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	var p struct{ Unit, Action string }
	if err := json.Unmarshal(raw, &p); err != nil || !unitRe.MatchString(p.Unit) || !svcVerbs[p.Action] {
		return nil, errors.New("invalid unit or action")
	}
	if IsDangerousWinService(p.Unit, p.Action) && !dangerous {
		return nil, errDangerous
	}
	m, err := mgr.Connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = m.Disconnect() }()
	s, err := m.OpenService(p.Unit)
	if err != nil {
		return nil, errors.New("no such service")
	}
	defer func() { _ = s.Close() }()
	if p.Unit == winServiceName && (p.Action == "stop" || p.Action == "restart") {
		// This process is the service: answer first, the stop follows a moment later.
		go func() {
			time.Sleep(1500 * time.Millisecond)
			_ = restartOrStop(s, p.Action == "restart")
		}()
		return map[string]bool{"ok": true}, nil
	}
	switch p.Action {
	case "start":
		err = s.Start()
	case "stop":
		_, err = s.Control(svc.Stop)
	case "restart":
		err = restartOrStop(s, true)
	case "enable":
		err = setStartType(s, mgr.StartAutomatic)
	case "disable":
		err = setStartType(s, mgr.StartDisabled)
	}
	if err != nil {
		return nil, errors.New(err.Error())
	}
	return map[string]bool{"ok": true}, nil
}

func setStartType(s *mgr.Service, t uint32) error {
	return windows.ChangeServiceConfig(s.Handle, windows.SERVICE_NO_CHANGE, t, windows.SERVICE_NO_CHANGE, nil, nil, nil, nil, nil, nil, nil)
}

// restartOrStop stops a service, waits until it is stopped and starts it again when restart is set.
func restartOrStop(s *mgr.Service, restart bool) error {
	st, err := s.Query()
	if err != nil {
		return err
	}
	if st.State != svc.Stopped {
		if _, err := s.Control(svc.Stop); err != nil {
			return err
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			if st, err = s.Query(); err != nil {
				return err
			}
			if st.State == svc.Stopped {
				break
			}
			if time.Now().After(deadline) {
				return errors.New("the service did not stop in 30 seconds")
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
	if !restart {
		return nil
	}
	return s.Start()
}
