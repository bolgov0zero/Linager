package server

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bolgov0zero/linager/internal/release"
)

func TestReleaseBinaryByArch(t *testing.T) {
	sum := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	legacy := release.Manifest{Version: "v0.5.0", SHA256: sum, Size: 1}
	multi := legacy
	multi.Assets = map[string]release.Asset{
		"amd64": {Name: "linager_linux_amd64", SHA256: sum, Size: 1},
		"arm64": {Name: "linager_linux_arm64", SHA256: sum, Size: 1},
	}
	cases := []struct {
		name string
		m    release.Manifest
		arch string
		want string
		ok   bool
	}{
		{"old agent, no arch", legacy, "", "linager_linux_amd64", true},
		{"old agent, multi manifest", multi, "", "linager_linux_amd64", true},
		{"amd64", multi, "amd64", "linager_linux_amd64", true},
		{"arm64", multi, "arm64", "linager_linux_arm64", true},
		{"arm64 from a release without it", legacy, "arm64", "", false},
		{"unknown arch", multi, "riscv64", "", false},
		{"path-like arch", multi, "../../etc/passwd", "", false},
	}
	for _, c := range cases {
		got, ok := releaseBinary(c.m, c.arch)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got %q %v, want %q %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestReleaseBinaryServesTheWindowsFile(t *testing.T) {
	m := release.Manifest{Assets: map[string]release.Asset{
		"amd64":            {Name: "linager_linux_amd64", SHA256: "a"},
		release.WindowsKey: {Name: release.WindowsBinaryAsset, SHA256: "b"},
	}}
	if got, ok := releaseBinary(m, release.WindowsKey); !ok || got != release.WindowsBinaryAsset {
		t.Fatalf("windows: %q %v", got, ok)
	}
	if got, ok := releaseBinary(m, ""); !ok || got != "linager_linux_amd64" {
		t.Fatalf("an agent that names nothing is amd64: %q %v", got, ok)
	}
	if _, ok := releaseBinary(release.Manifest{Assets: map[string]release.Asset{"amd64": {Name: "x", SHA256: "a"}}}, release.WindowsKey); ok {
		t.Fatal("a release without the Windows file has none to give")
	}
}

func TestCountingWriterCountsWhatWasWritten(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := &countingWriter{ResponseWriter: rec}
	_, _ = cw.Write([]byte("hello"))
	_, _ = cw.Write([]byte(" world"))
	if cw.n != 11 || cw.err != nil || rec.Body.String() != "hello world" {
		t.Fatalf("n=%d err=%v body=%q", cw.n, cw.err, rec.Body.String())
	}
	if kibPerSec(2048, time.Second) != 2 || kibPerSec(10, 0) != 0 {
		t.Fatal("kibPerSec")
	}
}
