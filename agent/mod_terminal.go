package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
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

// termStream runs a login shell in a PTY and bridges it to the stream.
func termStream(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p termParams
	_ = json.Unmarshal(raw, &p)
	shell := "/bin/bash"
	if _, err := os.Stat(shell); err != nil {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-l") //nolint:gosec // the terminal is a root shell by design; gated by the terminal module and audit
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "LANG=C.UTF-8")
	cmd.Dir = "/root"
	if _, err := os.Stat(cmd.Dir); err != nil {
		cmd.Dir = "/"
	}
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(clampSize(p.Cols, 80)), Rows: uint16(clampSize(p.Rows, 24))}) //nolint:gosec // bounded by clampSize
	if err != nil {
		return err
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP) // process group, set by pty.Start (Setsid)
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 16<<10)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if oerr := sio.Out(append([]byte(nil), buf[:n]...)); oerr != nil {
					done <- oerr
					return
				}
			}
			if err != nil {
				done <- nil // shell exited
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
			if _, err := ptmx.Write(b); err != nil {
				return errors.New("terminal write failed")
			}
		case sz := <-sio.Resize:
			_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(clampSize(sz[0], 80)), Rows: uint16(clampSize(sz[1], 24))}) //nolint:gosec // bounded by clampSize
		}
	}
}
