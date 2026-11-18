//go:build windows

package agent

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// prevFileName is the saved copy of the binary before an update, inside the data directory. It keeps the .exe
// extension so that the task scheduler runs it like any program.
const prevFileName = "linager.prev.exe"

// rollbackTask is the one-shot scheduled task that restores the old binary when the new agent never confirmed.
const rollbackTask = "LinagerAgentRollback"

// replaceExecutable puts the new file in place of the binary. A running program cannot be overwritten on Windows,
// but it can be renamed: the old file is moved aside (it is deleted at the next start) and the new one takes its name.
func replaceExecutable(tmp, exe string) error {
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe)
		return err
	}
	return nil
}

// restoreExecutable puts the saved binary back in place of exe.
func restoreExecutable(prev, exe string) error {
	tmp := exe + ".linager-restore"
	if err := copyExecutable(prev, tmp); err != nil {
		return err
	}
	if err := replaceExecutable(tmp, exe); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// CleanupOldBinary removes the file an earlier update moved aside; it could not be deleted while it ran.
func CleanupOldBinary() {
	if exe, err := os.Executable(); err == nil {
		_ = os.Remove(exe + ".old")
	}
}

// prepareRollback stops the service, so that the file of the broken version is not in use any more.
func prepareRollback() { _ = stopService() }

// rollbackDone removes the scheduled task of the rollback (it is the process that runs it).
func rollbackDone() { deleteTask(rollbackTask) }

// stopRollbackTimer cancels the rollback after the new version has confirmed itself.
func stopRollbackTimer() { deleteTask(rollbackTask) }

func deleteTask(name string) {
	_ = exec.Command("schtasks.exe", "/Delete", "/TN", name, "/F").Run() //nolint:gosec // fixed command, the name is a constant
}

// taskXML describes a task that runs once, at the given time, as SYSTEM.
func taskXML(start time.Time, command, args string) ([]byte, error) {
	esc := func(s string) string {
		var b bytes.Buffer
		if err := xml.EscapeText(&b, []byte(s)); err != nil {
			return ""
		}
		return b.String()
	}
	x := `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>Linager agent update rollback</Description></RegistrationInfo>
  <Triggers><TimeTrigger><StartBoundary>` + start.Format("2006-01-02T15:04:05") + `</StartBoundary><Enabled>true</Enabled></TimeTrigger></Triggers>
  <Principals><Principal id="Author"><UserId>S-1-5-18</UserId><RunLevel>HighestAvailable</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <ExecutionTimeLimit>PT10M</ExecutionTimeLimit>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author"><Exec><Command>` + esc(command) + `</Command><Arguments>` + esc(args) + `</Arguments></Exec></Actions>
</Task>`
	// schtasks reads the file as UTF-16 with a byte order mark
	u := utf16.Encode([]rune(x))
	out := []byte{0xFF, 0xFE}
	for _, c := range u {
		out = append(out, byte(c), byte(c>>8)) //nolint:gosec // the two bytes of a UTF-16 unit
	}
	return out, nil
}

// armRollback schedules the restore of the old binary. The task runs the OLD binary, so it works even if the new
// one cannot start at all.
func armRollback(dir, exe string) error {
	deleteTask(rollbackTask)
	start := time.Now().Add(RollbackDelay)
	data, err := taskXML(start, prevPath(dir), fmt.Sprintf(`agent rollback-update --data-dir "%s" --binary "%s"`, dir, exe))
	if err != nil {
		return err
	}
	f := filepath.Join(dir, "rollback-task.xml")
	if err := os.WriteFile(f, data, 0o600); err != nil {
		return err
	}
	defer func() { _ = os.Remove(f) }()
	if out, err := exec.Command("schtasks.exe", "/Create", "/TN", rollbackTask, "/XML", f, "/F").CombinedOutput(); err != nil { //nolint:gosec // fixed command, paths we built
		return fmt.Errorf("%w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// stopService stops the agent service and waits until it is stopped.
func stopService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer func() { _ = m.Disconnect() }()
	s, err := m.OpenService(winServiceName)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	st, err := s.Query()
	if err != nil {
		return err
	}
	if st.State == svc.Stopped {
		return nil
	}
	if st.State != svc.StopPending {
		if _, err := s.Control(svc.Stop); err != nil {
			return err
		}
	}
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		if st, err = s.Query(); err == nil && st.State == svc.Stopped {
			return nil
		}
	}
	return errors.New("the service did not stop in 30 seconds")
}

func startService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer func() { _ = m.Disconnect() }()
	s, err := m.OpenService(winServiceName)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	return s.Start()
}

// RestartService stops the agent service and starts it again. It must not run inside the service itself.
func RestartService() error {
	_ = stopService()
	return startService()
}

// restartService restarts the service. Inside the service a helper process does it (it survives the stop); from the
// command line or the rollback task it is done directly.
func restartService() error {
	if !IsService() {
		return RestartService()
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	for _, flags := range []uint32{windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_BREAKAWAY_FROM_JOB, windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP} {
		cmd := exec.Command(exe, "agent", "restart-service") //nolint:gosec // our own binary, fixed arguments
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: true}
		if err := cmd.Start(); err == nil {
			_ = cmd.Process.Release()
			return nil
		}
	}
	return errors.New("cannot start the helper that restarts the service")
}

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
	if st.State == svc.Running {
		return "running"
	}
	_, sub := stateNames(st.State)
	return sub
}
