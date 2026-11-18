//go:build !windows

package agent

import (
	"context"
	"encoding/json"
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
