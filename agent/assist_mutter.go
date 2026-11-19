package agent

import (
	"sort"

	"github.com/bolgov0zero/linager/internal/assist"
)

// The pure parts of the Linux backend of the Assistant (GNOME Mutter and GStreamer), kept apart so that they are tested
// on every platform.

// logicalMonitor is a monitor of the layout of the compositor (GNOME calls it a logical monitor).
type logicalMonitor struct {
	X, Y      int
	Scale     float64
	Primary   bool
	Connector string // the connector of the first physical monitor in it ("DP-1", "Virtual-1")
	W, H      int    // the size of the current mode in physical pixels
}

// buildMonitors turns the layout into the list the browser shows (the primary monitor first) and the connectors in the
// same order, which is what is asked of the compositor to record one of them.
func buildMonitors(layout []logicalMonitor) ([]assist.Monitor, []string) {
	list := append([]logicalMonitor(nil), layout...)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Primary != list[j].Primary {
			return list[i].Primary
		}
		if list[i].X != list[j].X {
			return list[i].X < list[j].X
		}
		return list[i].Y < list[j].Y
	})
	mons := make([]assist.Monitor, 0, len(list))
	conns := make([]string, 0, len(list))
	for _, m := range list {
		mons = append(mons, assist.Monitor{X: m.X, Y: m.Y, W: m.W, H: m.H, Primary: m.Primary})
		conns = append(conns, m.Connector)
	}
	return mons, conns
}

// pointerScale is the factor from the pixels of the picture to the coordinates of the stream: the picture is in
// physical pixels, the pointer is moved in the logical size of the stream (they differ on a scaled display).
func pointerScale(frameW, frameH int, logicalW, logicalH float64) (sx, sy float64) {
	if frameW <= 0 || frameH <= 0 || logicalW <= 0 || logicalH <= 0 {
		return 1, 1
	}
	return logicalW / float64(frameW), logicalH / float64(frameH)
}

// evdev codes of the mouse buttons for the browser's 0 left, 1 middle, 2 right, 3 back, 4 forward.
var mouseButtonEvdev = [5]int32{0x110, 0x112, 0x111, 0x113, 0x114}

// clipMimes are the text formats of the clipboard, the best first.
var clipMimes = []string{"text/plain;charset=utf-8", "text/plain", "UTF8_STRING", "STRING"}

// pickClipMime chooses the text format to read out of what the owner of the clipboard offers.
func pickClipMime(offered []string) string {
	for _, want := range clipMimes {
		for _, have := range offered {
			if have == want {
				return want
			}
		}
	}
	return ""
}
