package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/internal/assist"
)

// fakeScreen is a drawn desktop for the simulator and the tests: a window-like picture, a clock that changes every
// second, a square that moves by itself and the pointer of the viewer.
type fakeScreen struct {
	w, h int
	pix  []byte
	base []byte
	in   *fakeInput
}

func newFakeScreen(in *fakeInput) *fakeScreen {
	const w, h = 960, 600
	s := &fakeScreen{w: w, h: h, pix: make([]byte, w*h*4), base: make([]byte, w*h*4), in: in}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			s.base[i], s.base[i+1], s.base[i+2], s.base[i+3] = byte(120+y/6), byte(70+x/12), byte(30+x/20), 255 // a blue-ish gradient (BGRA)
		}
	}
	rect(s.base, w, 60, 50, 520, 330, 244, 244, 244) // a window
	rect(s.base, w, 60, 50, 520, 24, 217, 85, 43)    // its title bar
	for r := 0; r < 8; r++ {
		rect(s.base, w, 80, 100+r*34, 480, 1, 200, 200, 200)
	}
	rect(s.base, w, 0, h-32, w, 32, 39, 24, 16) // the taskbar
	rect(s.base, w, 8, h-26, 20, 20, 255, 140, 79)
	return s
}

func rect(p []byte, w, x, y, rw, rh int, b, g, r byte) {
	for yy := y; yy < y+rh; yy++ {
		for xx := x; xx < x+rw; xx++ {
			i := (yy*w + xx) * 4
			p[i], p[i+1], p[i+2], p[i+3] = b, g, r, 255
		}
	}
}

// Grab draws the picture of this moment.
func (s *fakeScreen) Grab() ([]byte, int, int, error) {
	copy(s.pix, s.base)
	now := time.Now()
	sec := now.Second()
	rect(s.pix, s.w, 90, 110, 6*sec+4, 18, 217, 85, 43) // the clock: a bar that grows during a minute
	t := float64(now.UnixMilli()%4000) / 4000
	x := 100 + int(t*400)
	rect(s.pix, s.w, x, 250, 40, 40, 80, 200, 80) // a square that moves
	px, py, keys := s.in.state()
	rect(s.pix, s.w, px, py, 10, 10, 255, 255, 255)
	rect(s.pix, s.w, 600, 60, 3*keys%300+3, 12, 40, 40, 220) // grows with every key press
	return s.pix, s.w, s.h, nil
}

// fakeInput keeps the pointer and the number of key presses.
type fakeInput struct {
	mu   sync.Mutex
	x, y int
	keys int
}

func (f *fakeInput) Do(in assist.Input) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch in.T {
	case assist.InMove, assist.InBtn:
		f.x, f.y = min(in.X, 940), min(in.Y, 570)
	case assist.InKey:
		if in.Down {
			f.keys++
		}
	}
	return nil
}

func (f *fakeInput) state() (x, y, keys int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.x, f.y, f.keys
}

// fakeAssistStream is the Assistant of the simulator: the same loop as the real helper, over the stream directly.
func fakeAssistStream(log *slog.Logger) StreamFunc {
	return func(ctx context.Context, params json.RawMessage, sio *StreamIO) error {
		var p struct {
			Session string `json:"session"`
		}
		_ = json.Unmarshal(params, &p)
		in := &fakeInput{x: 300, y: 200}
		id := 1
		if p.Session != "console" && p.Session != "" {
			id = 2
		}
		return runAssistLoop(ctx, &sioConn{sio: sio}, newFakeScreen(in), in, assist.Hello{Session: id, User: "i.petrova"}, log)
	}
}

// FakeAssistStream is the Assistant of a drawn desktop, for the tests of the panel and for the simulator.
func FakeAssistStream() StreamFunc { return fakeAssistStream(nil) }
