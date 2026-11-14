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
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/bolgov0zero/linager/internal/config"
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
	SampleEvery  time.Duration // default 5 s
	// Collector supplies samples; nil uses the /proc collector.
	Collector Collector
	// Config selects the enabled modules; nil enables all of them.
	Config *config.Agent
	// Uninstall performs self-removal; nil uses the real implementation.
	Uninstall func(purge bool, dataDir string) error
}

// Run keeps a signed WebSocket connection to the server until ctx ends,
// the agent is revoked, or it is told to uninstall itself.
func Run(ctx context.Context, o RunOptions) error {
	if o.PingEvery == 0 {
		o.PingEvery = 15 * time.Second
	}
	if o.SampleEvery == 0 {
		o.SampleEvery = 5 * time.Second
	}
	if o.Uninstall == nil {
		o.Uninstall = Uninstall
	}
	if o.Collector == nil {
		o.Collector = NewCollector(ctx)
	}
	if o.Config == nil {
		o.Config = config.DefaultAgent()
	}
	reg := NewRegistry(o.Config, o.Collector, o.Log, o.DataDir)
	buf := &sampleBuffer{}
	go sampler(ctx, o, buf)
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
		err := session(ctx, o, st, cert, key, buf, reg)
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

func session(ctx context.Context, o RunOptions, st *State, cert tls.Certificate, key ed25519.PrivateKey, buf *sampleBuffer, reg *Registry) error {
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
	defer reg.CloseAll()

	var seq uint64
	var sendMu sync.Mutex // seq numbers must hit the wire in order
	send := func(typ string, payload any) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		seq++
		env, err := protocol.Sign(key, typ, seq, payload)
		if err != nil {
			return err
		}
		b, _ := json.Marshal(env)
		return ws.Write(ctx, websocket.MessageText, b)
	}
	if err := send(protocol.TypeHello, protocol.Hello{AgentVersion: o.AgentVersion, OS: osName(), Hostname: hostname(), Modules: reg.Modules()}); err != nil {
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
	reg.SetEmitter(send)
	defer reg.SetEmitter(nil)
	ConfirmUpdate(o.DataDir, func(m string, a ...any) { o.Log.Info(m, a...) }) // the new version is connected: cancel the rollback
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
	go func() {
		t := time.NewTicker(o.SampleEvery)
		defer t.Stop()
		first := true
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pending := buf.snapshot()
				if len(pending) == 0 && !first {
					continue
				}
				b := protocol.MetricsBatch{Samples: pending}
				if first {
					si := o.Collector.System(o.AgentVersion)
					b.System = &si
				}
				if err := send(protocol.TypeMetrics, b); err != nil {
					errc <- err
					return
				}
				buf.ack(len(pending))
				first = false
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
			switch env.Type {
			case protocol.TypeCmd:
				var c protocol.Cmd
				if json.Unmarshal(env.Payload, &c) == nil {
					go func() { _ = send(protocol.TypeCmdResult, reg.HandleCmd(ctx, c)) }()
				}
				continue
			case protocol.TypeStreamOpen:
				var so protocol.StreamOpen
				if json.Unmarshal(env.Payload, &so) == nil {
					reg.OpenStream(ctx, so, send)
				}
				continue
			case protocol.TypeStreamInput:
				var d protocol.StreamData
				if json.Unmarshal(env.Payload, &d) == nil {
					reg.StreamInput(d.SID, d.Data)
				}
				continue
			case protocol.TypeStreamResize:
				var rz protocol.StreamResize
				if json.Unmarshal(env.Payload, &rz) == nil {
					reg.StreamResize(rz.SID, rz.Cols, rz.Rows)
				}
				continue
			case protocol.TypeStreamClose:
				var sc protocol.StreamClose
				if json.Unmarshal(env.Payload, &sc) == nil {
					reg.StreamClose(sc.SID)
				}
				continue
			}
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

// maxBuffered is how many samples are kept while the server is unreachable (one hour at 5 s).
const maxBuffered = 720

// sampleBuffer holds samples not yet delivered to the server.
type sampleBuffer struct {
	mu    sync.Mutex
	items []protocol.Sample
}

func (b *sampleBuffer) add(s protocol.Sample) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = append(b.items, s)
	if len(b.items) > maxBuffered {
		b.items = b.items[len(b.items)-maxBuffered:]
	}
}

func (b *sampleBuffer) snapshot() []protocol.Sample {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]protocol.Sample(nil), b.items...)
}

// ack removes the first n samples after they were written to the connection.
func (b *sampleBuffer) ack(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > len(b.items) {
		n = len(b.items)
	}
	b.items = b.items[n:]
}

// sampler collects a sample every SampleEvery, independent of the connection state.
func sampler(ctx context.Context, o RunOptions, buf *sampleBuffer) {
	o.Collector.Collect(time.Now()) // primes the CPU and network counters
	t := time.NewTicker(o.SampleEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			buf.add(o.Collector.Collect(now))
		}
	}
}
