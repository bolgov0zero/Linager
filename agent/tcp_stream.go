package agent

import (
	"context"
	"errors"
	"net"
	"time"
)

// pumpTCP connects to addr and carries the bytes both ways between that connection and the stream until either side
// ends. The remote desktop uses it: the address is always the local RDP port, never one that came from the panel.
func pumpTCP(ctx context.Context, addr string, sio *StreamIO) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return errors.New("cannot reach the remote desktop service on this computer: " + err.Error())
	}
	defer func() { _ = conn.Close() }()
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				if oerr := sio.Out(append([]byte(nil), buf[:n]...)); oerr != nil {
					done <- oerr
					return
				}
			}
			if err != nil {
				done <- nil // the service closed the connection
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-done:
			return err
		case b, ok := <-sio.In:
			if !ok {
				return nil
			}
			if _, err := conn.Write(b); err != nil {
				return errors.New("the remote desktop connection was closed")
			}
		}
	}
}
