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

func TestStubReturnsError(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"server"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error from unimplemented command")
	}
}
