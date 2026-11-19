package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/bolgov0zero/linager/internal/assist"
)

// assistScreen is what the Assistant looks at. On Windows it is the GDI picture of a session; the simulator and the
// tests use a drawn one.
type assistScreen interface {
	// Grab returns the screen as BGRA (4 bytes per pixel, rows without gaps) and its size. The slice is valid until
	// the next call.
	Grab() (pix []byte, w, h int, err error)
}

// assistInput is what the Assistant presses: the mouse and the keys of the browser, on the screen.
type assistInput interface {
	Do(in assist.Input) error
}

// idleAfter is the number of pictures without a change after which the capture slows down until something happens.
const idleAfter = 20

// runAssistLoop is the helper of the Assistant: it takes pictures of the screen, sends what changed as frames to rw
// and carries out the input and the settings that come back. It ends when rw is closed, the other side sends an end
// frame or the screen cannot be read. info is the Hello without the screen size.
func runAssistLoop(ctx context.Context, rw io.ReadWriter, scr assistScreen, in assistInput, info assist.Hello, log *slog.Logger) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	setCh := make(chan assist.Settings, 1)
	wake := make(chan struct{}, 1)

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
				if err := in.Do(ev); err != nil && log != nil {
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
			case assist.TypeEnd:
				return
			}
		}
	}()

	_, w, h, err := scr.Grab()
	if err != nil {
		_ = assist.WriteJSON(rw, assist.TypeEnd, assist.End{Reason: "cannot read the screen: " + err.Error()})
		return err
	}
	hello := info
	hello.Width, hello.Height = w, h
	if err := assist.WriteJSON(rw, assist.TypeHello, hello); err != nil {
		return err
	}
	enc := assist.NewEncoder(assist.DefaultSettings())
	lastW, lastH := w, h
	var frames, sentBytes, idle int
	statAt := time.Now()
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
			idle = 0
		case <-wake:
			t.Stop()
			idle = 0
		case <-t.C:
		}
		if pix, w, h, err = scr.Grab(); err != nil {
			_ = assist.WriteJSON(rw, assist.TypeEnd, assist.End{Reason: "cannot read the screen: " + err.Error()})
			return err
		}
		if w != lastW || h != lastH {
			lastW, lastH = w, h
			hello.Width, hello.Height = w, h
			if err := assist.WriteJSON(rw, assist.TypeHello, hello); err != nil {
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
			if err := assist.WriteFrame(rw, assist.TypeTiles, payload); err != nil {
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
		if now := time.Now(); now.Sub(statAt) >= time.Second {
			secs := now.Sub(statAt).Seconds()
			st := assist.Stats{FPS: float64(int(float64(frames)/secs*10)) / 10, Kbps: int(float64(sentBytes) * 8 / 1000 / secs), Quality: enc.Quality()}
			if err := assist.WriteJSON(rw, assist.TypeStats, st); err != nil {
				return err
			}
			frames, sentBytes, statAt = 0, 0, now
		}
		wait = enc.Interval()
		if idle > idleAfter {
			wait = min(4*wait, 200*time.Millisecond)
		}
	}
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
