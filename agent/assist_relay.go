package agent

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/bolgov0zero/linager/internal/assist"
)

// assistConnectTimeout is how long the agent waits for the helper to call back.
var assistConnectTimeout = 20 * time.Second

// assistHelper is a helper process the agent started in a Windows session.
type assistHelper interface {
	// Done is closed when the process has ended; Err then tells how.
	Done() <-chan struct{}
	Err() error
	Kill()
}

// assistLauncher starts the helper in a session of Windows (a number, or "console") and tells it where to call back.
type assistLauncher func(ctx context.Context, session, addr, token string) (assistHelper, error)

// runAssistRelay carries the Assistant stream: it starts the helper, takes its call-back on a port of the loopback
// (the helper proves itself with a one-time token that only its environment holds) and moves bytes between the
// helper and the server. The way up it reads the frames only to take out the one thing the helper cannot do: Ctrl+Alt+Del
// (the secure attention sequence may be sent by a service only), which goes to cad; everything else, and everything
// that comes down, is passed on as it is. cad may be nil.
func runAssistRelay(ctx context.Context, sio *StreamIO, session string, launch assistLauncher, cad func() error) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return err
	}
	token := hex.EncodeToString(raw[:])

	h, err := launch(ctx, session, ln.Addr().String(), token)
	if err != nil {
		return err
	}
	defer h.Kill()

	connCh := make(chan net.Conn, 1)
	go func() { // only a caller that knows the token is taken; the others are dropped
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, len(token))
			_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, err = io.ReadFull(c, buf)
			_ = c.SetReadDeadline(time.Time{})
			if err != nil || subtle.ConstantTimeCompare(buf, []byte(token)) != 1 {
				_ = c.Close()
				continue
			}
			connCh <- c
			return
		}
	}()

	var conn net.Conn
	select {
	case conn = <-connCh:
	case <-h.Done():
		if err := h.Err(); err != nil {
			return fmt.Errorf("the helper stopped before it connected: %w", err)
		}
		return errors.New("the helper stopped before it connected")
	case <-time.After(assistConnectTimeout):
		return errors.New("the helper did not connect in time")
	case <-ctx.Done():
		return ctx.Err()
	}
	_ = ln.Close()
	defer func() { _ = conn.Close() }()

	go func() { // the server -> the helper
		var re assist.Reassembler
		for b := range sio.In {
			frames, err := re.Push(b)
			if err != nil {
				break
			}
			for _, f := range frames {
				if f.Type == assist.TypeInput {
					if in, err := assist.ParseInput(f.Data); err == nil && in.T == assist.InCAD {
						if cad != nil {
							_ = cad()
						}
						continue
					}
				}
				if _, err := conn.Write(assist.AppendFrame(nil, f.Type, f.Data)); err != nil {
					return
				}
			}
		}
		_ = conn.Close() // the stream ended
	}()
	go func() { // the helper ends: the connection ends with it, the read below returns
		select {
		case <-h.Done():
		case <-ctx.Done():
		}
		_ = conn.Close()
	}()

	buf := make([]byte, 32<<10)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if oerr := sio.Out(append([]byte(nil), buf[:n]...)); oerr != nil {
				return oerr
			}
		}
		if err != nil {
			select {
			case <-h.Done():
				if herr := h.Err(); herr != nil {
					return fmt.Errorf("the helper stopped: %w", herr)
				}
			default:
			}
			return nil
		}
	}
}
