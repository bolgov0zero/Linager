package agent

import (
	"strings"
	"testing"
)

func TestTermEnvHasHomeAndHistory(t *testing.T) {
	env := termEnv("/bin/bash", []string{"PATH=/usr/bin"}) // an agent service environment without HOME
	got := map[string]string{}
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			got[k] = v
		}
	}
	if got["HOME"] == "" || got["USER"] == "" || got["SHELL"] != "/bin/bash" || got["TERM"] != "xterm-256color" {
		t.Fatalf("env: %v", got)
	}
	if got["PROMPT_COMMAND"] != "history -a" {
		t.Fatalf("history must be appended after every command, got %q", got["PROMPT_COMMAND"])
	}
	keep := termEnv("/bin/bash", []string{"HOME=/srv/x", "PROMPT_COMMAND=echo hi"})
	for _, e := range keep {
		if e == "HOME=/var" || e == "PROMPT_COMMAND=history -a" {
			t.Fatalf("an existing HOME or PROMPT_COMMAND must be kept: %v", keep)
		}
	}
}
