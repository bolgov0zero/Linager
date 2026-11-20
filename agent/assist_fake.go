package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/internal/assist"
)

// fakeHost is a drawn computer for the simulator and the tests: two displays (960x600 and 640x480 next to it), a window,
// a clock that grows during a minute, a square that moves by itself, the pointer of the viewer, a clipboard, and a
// lock screen that Ctrl+Alt+Del switches on and off.
type fakeHost struct {
	mu         sync.Mutex
	full, out  []byte // the whole desktop and the part that is shown
	sel        int    // the display shown, -1 for all
	px, py     int    // the pointer, in pixels of the whole desktop
	keys       int
	locked     bool
	clip       string
	showCursor bool
	typed      []string // what TypeText was asked to type, "text|after"
}

const (
	fakeW, fakeH = 1600, 600
	fakeM1X      = 960
)

var fakeMonitors = []assist.Monitor{{X: 0, Y: 0, W: 960, H: 600, Primary: true}, {X: fakeM1X, Y: 0, W: 640, H: 480}}

func newFakeHost() *fakeHost {
	h := &fakeHost{full: make([]byte, fakeW*fakeH*4), px: 300, py: 200}
	return h
}

func fakeRect(p []byte, w, x, y, rw, rh int, b, g, r byte) {
	for yy := y; yy < y+rh; yy++ {
		for xx := x; xx < x+rw; xx++ {
			i := (yy*w + xx) * 4
			p[i], p[i+1], p[i+2], p[i+3] = b, g, r, 255
		}
	}
}

// area is the rectangle of the desktop that is shown.
func (f *fakeHost) area() assist.Monitor {
	if f.sel >= 0 && f.sel < len(fakeMonitors) {
		return fakeMonitors[f.sel]
	}
	return assist.Monitor{W: fakeW, H: fakeH}
}

// Grab draws the picture of this moment.
func (f *fakeHost) Grab() ([]byte, int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.full
	for y := 0; y < fakeH; y++ {
		for x := 0; x < fakeW; x++ {
			i := (y*fakeW + x) * 4
			if x >= fakeM1X && y < 480 { // the second display
				p[i], p[i+1], p[i+2], p[i+3] = byte(60+y/10), 90, byte(150+x/20), 255
			} else if x < 960 { // the first display
				p[i], p[i+1], p[i+2], p[i+3] = byte(120+y/6), byte(70+x/12), byte(30+x/20), 255
			} else { // outside every display
				p[i], p[i+1], p[i+2], p[i+3] = 0, 0, 0, 255
			}
		}
	}
	if f.locked {
		fakeRect(p, fakeW, 0, 0, fakeW, fakeH, 58, 27, 14)
		fakeRect(p, fakeW, 380, 220, 200, 60, 255, 255, 255) // the clock of the lock screen
	} else {
		fakeRect(p, fakeW, 60, 50, 520, 330, 244, 244, 244) // a window
		fakeRect(p, fakeW, 60, 50, 520, 24, 217, 85, 43)    // its title bar
		for r := 0; r < 8; r++ {
			fakeRect(p, fakeW, 80, 100+r*34, 480, 1, 200, 200, 200)
		}
		fakeRect(p, fakeW, 0, 568, 960, 32, 39, 24, 16) // the taskbar
		fakeRect(p, fakeW, 8, 574, 20, 20, 255, 140, 79)
		fakeRect(p, fakeW, fakeM1X+40, 40, 300, 200, 244, 244, 244) // a window on the second display
		now := time.Now()
		fakeRect(p, fakeW, 90, 110, 6*now.Second()+4, 18, 217, 85, 43) // the clock: a bar that grows during a minute
		t := float64(now.UnixMilli()%4000) / 4000
		fakeRect(p, fakeW, 100+int(t*400), 250, 40, 40, 80, 200, 80) // a square that moves
		fakeRect(p, fakeW, 600, 60, 3*f.keys%300+3, 12, 40, 40, 220) // grows with every key press
	}
	if f.showCursor {
		fakeRect(p, fakeW, min(f.px, fakeW-10), min(f.py, fakeH-10), 10, 10, 255, 255, 255)
	}
	a := f.area()
	if cap(f.out) < a.W*a.H*4 {
		f.out = make([]byte, a.W*a.H*4)
	}
	f.out = f.out[:a.W*a.H*4]
	for y := 0; y < a.H; y++ {
		copy(f.out[y*a.W*4:(y+1)*a.W*4], p[((a.Y+y)*fakeW+a.X)*4:((a.Y+y)*fakeW+a.X+a.W)*4])
	}
	return f.out, a.W, a.H, nil
}

// Do keeps the pointer and counts key presses; Ctrl+Alt+Del switches the lock screen.
func (f *fakeHost) Do(in assist.Input) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.area()
	switch in.T {
	case assist.InMove, assist.InBtn:
		f.px, f.py = a.X+min(in.X, a.W-1), a.Y+min(in.Y, a.H-1)
	case assist.InKey:
		if in.Down {
			f.keys++
		}
	case assist.InCAD:
		f.locked = !f.locked
	}
	return nil
}

func (f *fakeHost) Monitors() ([]assist.Monitor, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fakeMonitors, f.sel
}

func (f *fakeHost) SelectMonitor(i int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i < -1 || i >= len(fakeMonitors) {
		return errors.New("no such monitor")
	}
	f.sel = i
	return nil
}

func (f *fakeHost) ClipboardGet() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clip, nil
}

// TypeText records what was typed; it is refused unless the drawn screen is locked (the secure desktop).
func (f *fakeHost) TypeText(text, after string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.locked { // the drawn lock screen is the secure desktop
		return errTypeDesktop
	}
	f.typed = append(f.typed, text+"|"+after)
	return nil
}

func (f *fakeHost) ClipboardSet(text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clip = text
	return nil
}

func (f *fakeHost) Desktop() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.locked {
		return "winlogon"
	}
	return "default"
}

func (f *fakeHost) SetShowCursor(show bool) {
	f.mu.Lock()
	f.showCursor = show
	f.mu.Unlock()
}

// state is what the tests look at: the pointer on the desktop, the key presses and the lock.
func (f *fakeHost) state() (x, y, keys int, locked bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.px, f.py, f.keys, f.locked
}

// fakeAssistStream is the Assistant of the simulator: the same loop as the real helper, over the stream directly.
func fakeAssistStream(log *slog.Logger) StreamFunc {
	return func(ctx context.Context, params json.RawMessage, sio *StreamIO) error {
		var p struct {
			Session string `json:"session"`
		}
		_ = json.Unmarshal(params, &p)
		id := 1
		if p.Session != "console" && p.Session != "" {
			id = 2
		}
		return runAssistLoop(ctx, &sioConn{sio: sio}, newFakeHost(), assist.Hello{Session: id, User: "i.petrova"}, log)
	}
}

// FakeAssistStream is the Assistant of a drawn desktop, for the tests of the panel and for the simulator.
func FakeAssistStream() StreamFunc { return fakeAssistStream(nil) }
