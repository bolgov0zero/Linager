package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"time"

	"github.com/coder/websocket"

	"github.com/bolgov0zero/linager/server/store"
)

// desktopLimitCode is the WebSocket close code sent when a remote desktop of the server is already open: a Windows
// client edition lets one person in at a time, a second window would push the first one out.
const desktopLimitCode websocket.StatusCode = 4030

var desktopLoginRe = regexp.MustCompile(`^[^\x00-\x1f]{0,64}$`)

// streamConn makes a stream to the agent look like the net.Conn of the remote desktop service, so that the TLS client
// of the proxy can run over it.
type streamConn struct {
	ctx  context.Context
	sh   *StreamHandle
	left []byte
}

func (c *streamConn) Read(p []byte) (int, error) {
	if len(c.left) == 0 {
		select {
		case b := <-c.sh.Data:
			c.left = b
		case <-c.sh.Done():
			select { // what arrived before the end is still read
			case b := <-c.sh.Data:
				c.left = b
			default:
				return 0, io.EOF
			}
		case <-c.ctx.Done():
			return 0, io.EOF
		}
	}
	n := copy(p, c.left)
	c.left = c.left[n:]
	return n, nil
}

func (c *streamConn) Write(p []byte) (int, error) {
	const chunk = 16 << 10
	n := 0
	for len(p) > 0 {
		part := p
		if len(part) > chunk {
			part = part[:chunk]
		}
		if err := c.sh.Input(c.ctx, part); err != nil {
			return n, err
		}
		n += len(part)
		p = p[len(part):]
	}
	return n, nil
}

func (c *streamConn) Close() error                     { c.sh.Close(); return nil }
func (c *streamConn) LocalAddr() net.Addr              { return streamAddr("panel") }
func (c *streamConn) RemoteAddr() net.Addr             { return streamAddr("agent") }
func (c *streamConn) SetDeadline(time.Time) error      { return nil }
func (c *streamConn) SetReadDeadline(time.Time) error  { return nil }
func (c *streamConn) SetWriteDeadline(time.Time) error { return nil }

type streamAddr string

func (a streamAddr) Network() string { return "linager-stream" }
func (a streamAddr) String() string  { return string(a) }

// wsMsg adapts a WebSocket to msgConn.
type wsMsg struct{ ws *websocket.Conn }

func (w wsMsg) Read(ctx context.Context) ([]byte, error) {
	for {
		typ, b, err := w.ws.Read(ctx)
		if err != nil {
			return nil, err
		}
		if typ == websocket.MessageBinary {
			return b, nil
		}
	}
}

func (w wsMsg) Write(ctx context.Context, b []byte) error {
	return w.ws.Write(ctx, websocket.MessageBinary, b)
}

// desktopSlot counts the open remote desktops per server; delta +1 reserves, -1 releases.
func (a *Admin) desktopSlot(serverID string, delta int) bool {
	a.termMu.Lock()
	defer a.termMu.Unlock()
	if a.desks == nil {
		a.desks = map[string]int{}
	}
	if delta > 0 {
		if a.desks[serverID] >= 1 {
			return false
		}
		a.desks[serverID]++
		return true
	}
	if a.desks[serverID] > 0 {
		a.desks[serverID]--
	}
	return true
}

// desktopWS is the remote desktop: the browser client speaks RDCleanPath over this WebSocket, the proxy carries it to
// the Remote Desktop service of the computer through the agent. The password never reaches the panel in the clear:
// the client signs in by itself (CredSSP) inside the connection.
func (a *Admin) desktopWS(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	login := r.URL.Query().Get("login")
	if !desktopLoginRe.MatchString(login) {
		jsonErr(w, http.StatusBadRequest, "invalid login")
		return
	}
	if !a.Hub.Online(s.ID) {
		jsonErr(w, http.StatusServiceUnavailable, "agent is offline")
		return
	}
	if !a.desktopSlot(s.ID, +1) {
		if ws, err := websocket.Accept(w, r, nil); err == nil {
			_ = ws.Close(desktopLimitCode, "a remote desktop of this server is already open")
		}
		return
	}
	defer a.desktopSlot(s.ID, -1)
	ws, err := websocket.Accept(w, r, nil) // same-origin check is on by default
	if err != nil {
		return
	}
	ws.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	sh, err := a.Hub.OpenStream(ctx, s.ID, "desktop", map[string]any{})
	if err != nil {
		_ = ws.Close(websocket.StatusInternalError, "cannot open the remote desktop")
		return
	}
	start := time.Now()
	a.audit(r, u, "desktop.open", "server", s.ID, s.Name, "ok", map[string]any{"login": login, "sid": sh.SID})
	up := &streamConn{ctx: ctx, sh: sh}
	err = serveRDCleanPath(ctx, wsMsg{ws}, up, "127.0.0.1:3389")
	result := "ok"
	reason := ""
	if err != nil && !errors.Is(err, context.Canceled) {
		result, reason = "failed", err.Error()
	}
	if sr := sh.Reason(); sr != "" { // why the agent ended the stream is more useful than the end of the connection
		reason = sr
		result = "failed"
	}
	a.audit(r, u, "desktop.close", "server", s.ID, s.Name, result, map[string]any{"login": login, "sid": sh.SID,
		"seconds": int(time.Since(start).Seconds()), "reason": reason})
	sh.Close()
	if reason != "" {
		_ = ws.Close(websocket.StatusInternalError, truncate(reason, 120))
		return
	}
	_ = ws.Close(websocket.StatusNormalClosure, "")
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// desktopState asks the agent whether the remote desktop of the computer is on.
func (a *Admin) desktopState(w http.ResponseWriter, r *http.Request, _ *store.User) {
	a.proxyCall(w, r, "desktop", "state", map[string]any{}, 15*time.Second)
}

// desktopEnable turns the remote desktop of a Windows computer on (the setting, the firewall rule and the service).
func (a *Admin) desktopEnable(w http.ResponseWriter, r *http.Request, u *store.User) {
	s := a.serverOr404(w, r)
	if s == nil {
		return
	}
	var req confirmReq
	if !decode(w, r, &req) {
		return
	}
	if !req.Confirm {
		needConfirm(w)
		return
	}
	a.secCall(w, r, u, s, "desktop", "enable", "desktop.enable", map[string]any{}, map[string]any{}, 60*time.Second)
}
