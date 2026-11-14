package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "linager ") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestUnknownCommandFails(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"nonexistent-command"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error from unimplemented command")
	}
}
