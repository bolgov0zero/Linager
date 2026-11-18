//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

type termParams struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

func clampSize(v, def int) int {
	if v < 1 || v > 500 {
		return def
	}
	return v
}

// shellCommand is the command line of the shell a terminal opens: PowerShell, or cmd.exe when it is missing.
func shellCommand() string {
	if p := powerShellPath(); filepath.IsAbs(p) {
		return `"` + p + `" -NoLogo`
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		return `"` + filepath.Join(root, "System32", "cmd.exe") + `"`
	}
	if p, err := exec.LookPath("cmd.exe"); err == nil {
		return `"` + p + `"`
	}
	return "cmd.exe"
}

// workDir is where new shells start: the system folder, like the shell of a service.
func workDir() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		return filepath.Join(root, "System32")
	}
	return `C:\`
}

// conPTY is a Windows pseudo console with the shell attached to it.
type conPTY struct {
	hpc  windows.Handle
	in   *os.File // keystrokes to the shell
	out  *os.File // output of the shell
	proc windows.Handle
}

func startConPTY(cmdline string, cols, rows int) (*conPTY, error) {
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, err
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		_ = windows.CloseHandle(inR)
		_ = windows.CloseHandle(inW)
		return nil, err
	}
	closeAll := func() {
		for _, h := range []windows.Handle{inR, inW, outR, outW} {
			if h != 0 {
				_ = windows.CloseHandle(h)
			}
		}
	}
	var hpc windows.Handle
	if err := windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, inR, outW, 0, &hpc); err != nil { //nolint:gosec // bounded by clampSize
		closeAll()
		return nil, err
	}
	// The pseudo console keeps its own copies of these ends.
	_ = windows.CloseHandle(inR)
	_ = windows.CloseHandle(outW)
	inR, outW = 0, 0

	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		closeAll()
		return nil, err
	}
	defer attrs.Delete()
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&hpc)), unsafe.Sizeof(hpc)); err != nil { //nolint:gosec // the handle value itself is the attribute, as the API wants it
		windows.ClosePseudoConsole(hpc)
		closeAll()
		return nil, err
	}

	var si windows.StartupInfoEx
	si.Cb = uint32(unsafe.Sizeof(si))
	// With empty standard handles the pseudo console supplies its own. Without this flag a child of a process whose
	// output is redirected (the agent run under a test or a pipe) inherits that redirection and bypasses the console.
	si.Flags = windows.STARTF_USESTDHANDLES
	si.ProcThreadAttributeList = attrs.List()
	cl, err := windows.UTF16PtrFromString(cmdline)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		closeAll()
		return nil, err
	}
	dir, _ := windows.UTF16PtrFromString(workDir())
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(nil, cl, nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT, nil, dir, &si.StartupInfo, &pi); err != nil {
		windows.ClosePseudoConsole(hpc)
		closeAll()
		return nil, err
	}
	_ = windows.CloseHandle(pi.Thread)
	return &conPTY{hpc: hpc, in: os.NewFile(uintptr(inW), "conin"), out: os.NewFile(uintptr(outR), "conout"), proc: pi.Process}, nil
}

func (c *conPTY) resize(cols, rows int) {
	_ = windows.ResizePseudoConsole(c.hpc, windows.Coord{X: int16(cols), Y: int16(rows)}) //nolint:gosec // bounded by clampSize
}

// close ends the shell and releases the pseudo console. The output keeps being read while the console closes:
// older Windows versions block ClosePseudoConsole until everything written was read.
func (c *conPTY) close() {
	_ = windows.TerminateProcess(c.proc, 0)
	drained := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := c.out.Read(buf); err != nil {
				close(drained)
				return
			}
		}
	}()
	windows.ClosePseudoConsole(c.hpc)
	_ = c.in.Close()
	_ = c.out.Close()
	<-drained
	_ = windows.CloseHandle(c.proc)
}

// termStream runs a shell in a pseudo console and bridges it to the stream. The agent is a service running as
// SYSTEM, so the shell is a SYSTEM shell: the terminal is a full administrator access by design, gated by the
// terminal module and the audit journal, just like the root shell of the Linux agent.
func termStream(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p termParams
	_ = json.Unmarshal(raw, &p)
	pty, err := startConPTY(shellCommand(), clampSize(p.Cols, 80), clampSize(p.Rows, 24))
	if err != nil {
		return err
	}
	var closing atomic.Bool
	defer func() {
		closing.Store(true)
		pty.close()
	}()

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 16<<10)
		for {
			n, err := pty.out.Read(buf)
			if n > 0 && !closing.Load() {
				if oerr := sio.Out(append([]byte(nil), buf[:n]...)); oerr != nil {
					done <- oerr
					return
				}
			}
			if err != nil {
				done <- nil // the shell exited
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-done:
			return err
		case b, ok := <-sio.In:
			if !ok {
				return nil
			}
			if _, err := pty.in.Write(b); err != nil {
				return errors.New("terminal write failed")
			}
		case sz := <-sio.Resize:
			pty.resize(clampSize(sz[0], 80), clampSize(sz[1], 24))
		}
	}
}
