package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"syscall"
	"time"

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

// termGrace is how long a shell may take to exit after the hangup before it is killed.
const termGrace = 3 * time.Second

// termEnv builds the environment of a terminal shell. The agent runs as a service whose environment has no HOME,
// so the shell would not find its history file or dotfiles and "cd" would fail with "HOME not set". The values come
// from the user the agent runs as; PROMPT_COMMAND appends every command to the history at once, so it survives a
// session that is cut off.
func termEnv(shell string, base []string) []string {
	env := append([]string{}, base...)
	set := func(k, v string) { env = append(env, k+"="+v) }
	has := func(k string) bool {
		for _, e := range base {
			if len(e) > len(k) && e[:len(k)+1] == k+"=" {
				return true
			}
		}
		return false
	}
	if u, err := user.Current(); err == nil {
		if !has("HOME") && u.HomeDir != "" {
			set("HOME", u.HomeDir)
		}
		if !has("USER") {
			set("USER", u.Username)
		}
		if !has("LOGNAME") {
			set("LOGNAME", u.Username)
		}
	}
	set("SHELL", shell)
	if !has("PROMPT_COMMAND") {
		set("PROMPT_COMMAND", "history -a")
	}
	set("TERM", "xterm-256color")
	set("LANG", "C.UTF-8")
	return env
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
	cmd.Env = termEnv(shell, os.Environ())
	cmd.Dir = homeDir()
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(clampSize(p.Cols, 80)), Rows: uint16(clampSize(p.Rows, 24))}) //nolint:gosec // bounded by clampSize
	if err != nil {
		return err
	}
	defer func() {
		_ = ptmx.Close()
		if cmd.Process == nil {
			_ = cmd.Wait()
			return
		}
		// Hang up the shell and give it a moment to exit by itself: bash writes its history only on exit.
		// A shell that ignores the hangup is killed after the grace period.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP) // process group, set by pty.Start (Setsid)
		exited := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(exited)
		}()
		select {
		case <-exited:
		case <-time.After(termGrace):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Process.Kill()
			<-exited
		}
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

// homeDir is the working directory of new shells: the home of the user running the agent
// (root in production), or "/" when it is unknown or inaccessible.
func homeDir() string {
	dir := os.Getenv("HOME")
	if dir == "" {
		if u, err := user.Current(); err == nil {
			dir = u.HomeDir
		}
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() { //nolint:gosec // HOME of the agent user, only probed
		return "/"
	}
	if f, err := os.Open(dir); err != nil { //nolint:gosec // probing accessibility of the home directory
		return "/"
	} else {
		_ = f.Close()
	}
	return dir
}
