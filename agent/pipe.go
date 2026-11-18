package agent

import (
	"bufio"
	"io"
	"os/exec"
)

// pipeCommand streams stdout and stderr of cmd into the stream until it exits.
func pipeCommand(cmd *exec.Cmd, sio *StreamIO) error {
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait(); _ = pw.Close() }()
	rd := bufio.NewReaderSize(pr, 16<<10)
	buf := make([]byte, 8<<10)
	for {
		n, err := rd.Read(buf)
		if n > 0 {
			if oerr := sio.Out(append([]byte(nil), buf[:n]...)); oerr != nil {
				_ = pr.Close()
				return oerr
			}
		}
		if err != nil {
			return nil
		}
	}
}
