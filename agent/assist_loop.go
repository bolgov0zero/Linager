package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bolgov0zero/linager/internal/assist"
)

// assistHost is the computer the Assistant looks at and presses keys on. On Windows it is a session (the GDI picture
// of its desktop, SendInput, the clipboard of its window station); the simulator and the tests use a drawn one.
type assistHost interface {
	// Grab returns the screen as BGRA (4 bytes per pixel, rows without gaps) and its size. The slice is valid until
	// the next call.
	Grab() (pix []byte, w, h int, err error)
	// Do carries out the mouse and the keys of the browser; the coordinates are pixels of the picture.
	Do(in assist.Input) error
	// Monitors lists the displays (the primary one first) and the index shown, -1 for all of them as one picture.
	Monitors() ([]assist.Monitor, int)
	SelectMonitor(i int) error
	ClipboardGet() (string, error)
	ClipboardSet(text string) error
	// Desktop is the name of the desktop the picture is taken from: "default", or "winlogon" while the screen is
	// locked, a sign-in or a UAC prompt is shown.
	Desktop() string
	// SetShowCursor says whether the pointer of the computer is drawn into the picture.
	SetShowCursor(show bool)
	// TypeText types a text as characters and then presses Tab or Enter (after is "", "tab" or "enter"). It is done only
	// while the screen of the computer is a sign-in or a UAC prompt; the host checks it at the moment of typing and
	// returns errTypeDesktop when it is not. A host that cannot type returns errTypeUnsupported.
	TypeText(text, after string) error
}

var (
	// errTypeDesktop: the screen is not a sign-in or a UAC prompt (any more), so nothing is typed.
	errTypeDesktop = errors.New("the screen is not a sign-in or a UAC prompt")
	// errTypeUnsupported: this computer cannot type text.
	errTypeUnsupported = errors.New("typing is not supported here")
)

// idleAfter is the number of pictures without a change after which the capture slows down until something happens.
const idleAfter = 20

// runAssistLoop is the helper of the Assistant: it takes pictures of the screen, sends what changed as frames to rw
// and carries out the input and the settings that come back. It ends when rw is closed, the other side sends an end
// frame or the screen cannot be read. info is the Hello without the screen size and the displays.
func runAssistLoop(ctx context.Context, rw io.ReadWriter, host assistHost, info assist.Hello, log *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	setCh := make(chan assist.Settings, 1)
	wake := make(chan struct{}, 1)
	var wmu sync.Mutex // the frames of the main loop and of the answers do not interleave
	send := func(typ byte, data []byte) error {
		wmu.Lock()
		defer wmu.Unlock()
		return assist.WriteFrame(rw, typ, data)
	}
	sendJSON := func(typ byte, v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return send(typ, b)
	}
	reHello := make(chan struct{}, 1)
	var paused atomic.Bool // the tab is hidden: no pictures are taken or sent

	go func() { // the other side: input and settings
		defer cancel()
		for {
			f, err := assist.ReadFrame(rw)
			if err != nil {
				return
			}
			switch f.Type {
			case assist.TypeInput:
				ev, err := assist.ParseInput(f.Data)
				if err != nil {
					continue
				}
				if err := host.Do(ev); err != nil && log != nil {
					log.Debug("assistant input", "err", err)
				}
				select {
				case wake <- struct{}{}:
				default:
				}
			case assist.TypeSettings:
				var s assist.Settings
				if json.Unmarshal(f.Data, &s) != nil || s.Validate() != nil {
					continue
				}
				select { // the newest wins
				case <-setCh:
				default:
				}
				setCh <- s
			case assist.TypeClipGet:
				text, err := host.ClipboardGet()
				if err != nil {
					if log != nil {
						log.Debug("assistant clipboard", "err", err)
					}
					continue
				}
				if err := sendJSON(assist.TypeClip, assist.Clip{Text: text}); err != nil {
					return
				}
			case assist.TypeClipSet:
				c, err := assist.ParseClip(f.Data)
				if err != nil {
					continue
				}
				if err := host.ClipboardSet(c.Text); err != nil && log != nil {
					log.Debug("assistant clipboard", "err", err)
				}
			case assist.TypeType:
				t, err := assist.ParseTyping(f.Data)
				if err != nil {
					continue
				}
				res := assist.Typed{OK: true}
				if err := host.TypeText(t.Text, t.After); err != nil {
					res = assist.Typed{Reason: "failed"}
					switch {
					case errors.Is(err, errTypeDesktop):
						res.Reason = "desktop"
					case errors.Is(err, errTypeUnsupported):
						res.Reason = "unsupported"
					case log != nil:
						log.Debug("assistant typing", "err", err) // never the text
					}
				}
				if err := sendJSON(assist.TypeTyped, res); err != nil {
					return
				}
				select {
				case wake <- struct{}{}:
				default:
				}
			case assist.TypePause:
				if p, err := assist.ParsePause(f.Data); err == nil {
					paused.Store(p.On)
					select {
					case wake <- struct{}{}:
					default:
					}
				}
			case assist.TypeMonitor:
				m, err := assist.ParseMonitor(f.Data)
				if err != nil {
					continue
				}
				if err := host.SelectMonitor(m.I); err != nil {
					if log != nil {
						log.Debug("assistant monitor", "err", err)
					}
					continue
				}
				select {
				case reHello <- struct{}{}:
				default:
				}
			case assist.TypeEnd:
				return
			}
		}
	}()

	_, w, h, err := host.Grab()
	if err != nil {
		_ = send2(send, assist.TypeEnd, endFor(err))
		return err
	}
	var lastMons []assist.Monitor
	var lastSel int
	lastW, lastH := w, h
	hello := func() error {
		h2 := info
		h2.Width, h2.Height = lastW, lastH
		h2.Monitors, h2.Monitor = host.Monitors()
		lastMons, lastSel = h2.Monitors, h2.Monitor
		return sendJSON(assist.TypeHello, h2)
	}
	if err := hello(); err != nil {
		return err
	}
	// a host that has frames of its own (a compositor that sends a frame when the screen changes) wakes the loop with them
	var hostFrames <-chan struct{}
	if f, ok := host.(interface{ Frames() <-chan struct{} }); ok {
		hostFrames = f.Frames()
	}
	enc := assist.NewEncoder(assist.DefaultSettings())
	var frames, sentBytes, idle int
	statAt := time.Now()
	lastDesk := host.Desktop()
	var pix []byte
	wait := time.Duration(0) // the first picture at once

	for {
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case s := <-setCh:
			t.Stop()
			enc.SetSettings(s)
			host.SetShowCursor(s.ShowCursor)
			if l, ok := host.(interface{ SetMaxFPS(int) }); ok {
				l.SetMaxFPS(s.FPS)
			}
			idle = 0
		case <-wake:
			t.Stop()
			idle = 0
		case <-hostFrames:
			t.Stop()
			idle = 0
		case <-reHello:
			t.Stop()
			enc.Reset()
			idle = 0
		case <-t.C:
		}
		if paused.Load() {
			wait = 250 * time.Millisecond // a hidden tab costs nothing but this look; the picture goes on from where it was
			continue
		}
		if pix, w, h, err = host.Grab(); err != nil {
			_ = send2(send, assist.TypeEnd, endFor(err))
			return err
		}
		mons, sel := host.Monitors()
		if w != lastW || h != lastH || sel != lastSel || !reflect.DeepEqual(mons, lastMons) {
			lastW, lastH = w, h
			if err := hello(); err != nil {
				return err
			}
			enc.Reset()
		}
		tiles, err := enc.Encode(pix, w, h, time.Now())
		if err != nil {
			return err
		}
		if len(tiles) > 0 {
			payload := assist.PackTiles(tiles)
			if err := send(assist.TypeTiles, payload); err != nil {
				if errors.Is(err, assist.ErrFrameTooBig) {
					enc.Reset() // a picture too large for one frame: start again with a smaller one
					continue
				}
				return err
			}
			frames++
			sentBytes += len(payload)
			idle = 0
		} else {
			idle++
		}
		now := time.Now()
		if desk := host.Desktop(); now.Sub(statAt) >= time.Second || desk != lastDesk {
			secs := max(now.Sub(statAt).Seconds(), 0.2)
			st := assist.Stats{FPS: float64(int(float64(frames)/secs*10)) / 10, Kbps: int(float64(sentBytes) * 8 / 1000 / secs), Quality: enc.Quality(), Desktop: desk}
			if err := sendJSON(assist.TypeStats, st); err != nil {
				return err
			}
			if now.Sub(statAt) >= time.Second {
				frames, sentBytes, statAt = 0, 0, now
			}
			lastDesk = desk
		}
		wait = enc.Interval()
		if idle > idleAfter {
			wait = min(4*wait, 200*time.Millisecond)
		}
	}
}

// send2 sends a JSON frame through the serialised sender (used for the last frame before the loop ends).
func send2(send func(byte, []byte) error, typ byte, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return send(typ, b)
}

// sioConn makes the stream to the server look like a connection, so the loop can run on it directly (the simulator
// and the tests have no helper process).
type sioConn struct {
	sio *StreamIO
	buf []byte
}

func (c *sioConn) Read(p []byte) (int, error) {
	if len(c.buf) == 0 {
		b, ok := <-c.sio.In
		if !ok {
			return 0, io.EOF
		}
		c.buf = b
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}

func (c *sioConn) Write(p []byte) (int, error) {
	if err := c.sio.Out(p); err != nil {
		return 0, err
	}
	return len(p), nil
}
