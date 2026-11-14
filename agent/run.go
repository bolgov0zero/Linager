package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/bolgov0zero/linager/internal/protocol"
)

// ErrRevoked means the server no longer knows this agent (it was deleted in the admin panel).
var ErrRevoked = errors.New("this agent was removed from the server")

// ErrUninstalled is returned after the server asked the agent to remove itself.
var ErrUninstalled = errors.New("agent was uninstalled by the server")

// RunOptions configure the connection loop.
type RunOptions struct {
	DataDir      string
	AgentVersion string
	Log          *slog.Logger
	PingEvery    time.Duration // default 15 s
	// Uninstall performs self-removal; nil uses the real implementation.
	Uninstall func(purge bool, dataDir string) error
}

// Run keeps a signed WebSocket connection to the server until ctx ends,
// the agent is revoked, or it is told to uninstall itself.
func Run(ctx context.Context, o RunOptions) error {
	if o.PingEvery == 0 {
		o.PingEvery = 15 * time.Second
	}
	if o.Uninstall == nil {
		o.Uninstall = Uninstall
	}
	st, cert, err := loadState(o.DataDir)
	if err != nil {
		return err
	}
	key, err := loadOrCreateKey(o.DataDir)
	if err != nil {
		return err
	}
	backoff := time.Second
	for {
		err := session(ctx, o, st, cert, key)
		switch {
		case errors.Is(err, ErrRevoked), errors.Is(err, ErrUninstalled), ctx.Err() != nil:
			if ctx.Err() != nil && err == nil {
				return ctx.Err()
			}
			return err
		}
		o.Log.Warn("disconnected", "err", err, "retry_in", backoff.String())
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff + time.Duration(rand.Int64N(int64(backoff/2)+1))): //nolint:gosec // jitter, not security
		}
		if backoff < time.Minute {
			backoff *= 2
		}
	}
}

func session(ctx context.Context, o RunOptions, st *State, cert tls.Certificate, key ed25519.PrivateKey) error {
	hc := &http.Client{Transport: &http.Transport{TLSClientConfig: pinnedTLS(st.ServerFP, &cert)}}
	ws, resp, err := websocket.Dial(ctx, "wss://"+st.Server+"/v1/ws", &websocket.DialOptions{HTTPClient: hc})
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized && resp.Header.Get("X-Linager-Reason") == "revoked" {
			return ErrRevoked
		}
		return err
	}
	ws.SetReadLimit(1 << 20)
	defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var seq uint64
	send := func(typ string, payload any) error {
		seq++
		env, err := protocol.Sign(key, typ, seq, payload)
		if err != nil {
			return err
		}
		b, _ := json.Marshal(env)
		return ws.Write(ctx, websocket.MessageText, b)
	}
	if err := send(protocol.TypeHello, protocol.Hello{AgentVersion: o.AgentVersion, OS: osName(), Hostname: hostname()}); err != nil {
		return err
	}

	var guard protocol.SeqGuard
	read := func() (*protocol.Envelope, error) {
		_, b, err := ws.Read(ctx)
		if err != nil {
			return nil, err
		}
		var env protocol.Envelope
		if err := json.Unmarshal(b, &env); err != nil {
			return nil, err
		}
		if err := protocol.Verify(ed25519.PublicKey(st.CmdPubkey), &env, time.Now()); err != nil {
			return nil, err
		}
		return &env, guard.Accept(env.Seq)
	}
	hello, err := read()
	if err != nil || hello.Type != protocol.TypeHello {
		return errors.New("bad hello from server")
	}
	o.Log.Info("connected", "server", st.Server)

	errc := make(chan error, 1)
	go func() {
		t := time.NewTicker(o.PingEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := send(protocol.TypePing, nil); err != nil {
					errc <- err
					return
				}
			}
		}
	}()
	msgs := make(chan *protocol.Envelope)
	go func() {
		for {
			env, err := read()
			if err != nil {
				errc <- err
				return
			}
			select {
			case msgs <- env:
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errc:
			return err
		case env := <-msgs:
			if env.Type == protocol.TypeAgentRemove {
				var u protocol.Uninstall
				_ = json.Unmarshal(env.Payload, &u)
				o.Log.Warn("server requested agent removal", "purge", u.Purge)
				if err := o.Uninstall(u.Purge, o.DataDir); err != nil {
					o.Log.Error("uninstall failed", "err", err)
					continue
				}
				return ErrUninstalled
			}
		}
	}
}
