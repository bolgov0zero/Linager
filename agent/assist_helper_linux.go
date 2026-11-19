//go:build linux

package agent

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/bolgov0zero/linager/internal/assist"
)

// RunAssistHelper is `linager agent assistant-helper` on Linux: it runs as the person of a graphical session, calls the
// agent back on the loopback with the token of its environment, opens the screen of the session through the compositor
// and serves it until the stream ends. The return value is the exit code.
func RunAssistHelper(ctx context.Context, addr string, session int) (int, error) {
	token := os.Getenv(assistTokenEnv)
	_ = os.Unsetenv(assistTokenEnv)
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" || len(token) != 64 {
		return helperExitConnect, errors.New("bad address or token")
	}
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return helperExitConnect, err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(token)); err != nil {
		return helperExitConnect, err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	h, err := newMutterHost(log)
	if err != nil {
		_ = assist.WriteJSON(conn, assist.TypeEnd, assist.End{Reason: "cannot open the screen: " + err.Error()})
		return helperExitScreen, err
	}
	defer h.release()
	info := assist.Hello{Session: session, User: os.Getenv("USER"), Platform: "linux"}
	if err := runAssistLoop(ctx, conn, h, info, log); err != nil {
		return helperExitLoop, err
	}
	return 0, nil
}
