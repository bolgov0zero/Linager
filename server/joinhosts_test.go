package server

import (
	"testing"

	"github.com/bolgov0zero/linager/internal/config"
)

func TestJoinHostsBareNameIsLocal(t *testing.T) {
	cfg := &config.Server{}
	cfg.Agents.Hosts = []string{"homeSRV-linager", "192.168.1.10", "panel.example.com"}
	ext, loc := joinHosts(cfg, "panel.example.com:443")
	if ext != "panel.example.com" {
		t.Errorf("external = %q, want the site address", ext)
	}
	if loc != "192.168.1.10" {
		t.Errorf("local = %q, want the IP address, not the bare name", loc)
	}
	if hostKind("homeSRV-linager") != "loc" {
		t.Errorf("a bare host name must be local")
	}
}
