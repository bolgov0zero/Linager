package agent

import (
	"reflect"
	"testing"
)

func TestBuildMonitors(t *testing.T) {
	mons, conns := buildMonitors([]logicalMonitor{
		{X: 1920, Y: 0, Connector: "HDMI-1", W: 1280, H: 1024},
		{X: 0, Y: 0, Connector: "DP-1", W: 1920, H: 1080, Primary: true},
		{X: 0, Y: 1080, Connector: "eDP-1", W: 1366, H: 768},
	})
	if !reflect.DeepEqual(conns, []string{"DP-1", "eDP-1", "HDMI-1"}) {
		t.Errorf("connectors %v: the primary first, then left to right, top to bottom", conns)
	}
	if len(mons) != 3 || !mons[0].Primary || mons[0].W != 1920 || mons[2].X != 1920 {
		t.Errorf("monitors %+v", mons)
	}
	if m, c := buildMonitors(nil); len(m) != 0 || len(c) != 0 {
		t.Error("no layout, no monitors")
	}
}

func TestPointerScale(t *testing.T) {
	if sx, sy := pointerScale(3840, 2160, 1920, 1080); sx != 0.5 || sy != 0.5 {
		t.Errorf("a 2x display: %v %v", sx, sy)
	}
	if sx, sy := pointerScale(1920, 1080, 1920, 1080); sx != 1 || sy != 1 {
		t.Errorf("a plain display: %v %v", sx, sy)
	}
	if sx, sy := pointerScale(0, 0, 100, 100); sx != 1 || sy != 1 {
		t.Errorf("no picture yet: %v %v", sx, sy)
	}
}

func TestPickClipMime(t *testing.T) {
	if got := pickClipMime([]string{"image/png", "text/plain", "text/plain;charset=utf-8"}); got != "text/plain;charset=utf-8" {
		t.Errorf("got %q", got)
	}
	if got := pickClipMime([]string{"STRING", "UTF8_STRING"}); got != "UTF8_STRING" {
		t.Errorf("got %q", got)
	}
	if got := pickClipMime([]string{"image/png"}); got != "" {
		t.Errorf("an image is not text: %q", got)
	}
}
