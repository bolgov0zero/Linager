package server

import (
	"testing"

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
