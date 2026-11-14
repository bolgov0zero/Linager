package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"regexp"
	"strconv"
)

var logUnitRe = regexp.MustCompile(`^[a-zA-Z0-9:_.@*-]{1,100}$`)

type logParams struct {
	Unit     string `json:"unit"`
	Priority string `json:"priority"`
	Kernel   string `json:"kernel"`
	Lines    int    `json:"lines"`
}

// logsStream follows the journal.
func logsStream(ctx context.Context, raw json.RawMessage, sio *StreamIO) error {
	var p logParams
	_ = json.Unmarshal(raw, &p)
	n := p.Lines
	if n < 0 || n > 1000 {
		n = 200
	}
	args := []string{"-f", "-n", strconv.Itoa(n), "-o", "short-iso", "--no-pager"}
	if p.Kernel == "1" {
		args = append(args, "-k")
	}
	if p.Unit != "" && logUnitRe.MatchString(p.Unit) {
		args = append(args, "-u", p.Unit)
	}
	if v, err := strconv.Atoi(p.Priority); err == nil && v >= 0 && v <= 7 {
		args = append(args, "-p", strconv.Itoa(v))
	}
	cmd := exec.CommandContext(ctx, "journalctl", args...) //nolint:gosec // arguments are validated above
	return pipeCommand(cmd, sio)
}

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
