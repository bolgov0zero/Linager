package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/internal/config"
	"github.com/bolgov0zero/linager/internal/protocol"
)

// errors produced by modules.
var (
	errDangerous = errors.New(protocol.ErrDangerous)
	errBusy      = errors.New(protocol.ErrBusy)
)

// CmdFunc handles one request/response action.
type CmdFunc func(ctx context.Context, params json.RawMessage, dangerous bool) (any, error)

// StreamIO connects a stream handler with the server connection.
type StreamIO struct {
	// Out sends output bytes to the server (chunked by the dispatcher).
	Out func(b []byte) error
	// In receives input bytes; Resize receives {cols, rows}. Both close when the stream ends.
	In     <-chan []byte
	Resize <-chan [2]int
}

// StreamFunc runs a stream until it finishes or ctx is cancelled.
type StreamFunc func(ctx context.Context, params json.RawMessage, io *StreamIO) error

// Registry holds the actions of all enabled modules.
type Registry struct {
	cmds    map[string]CmdFunc
	streams map[string]StreamFunc
	modules []string
	log     *slog.Logger

	mu      sync.Mutex
	live    map[string]*liveStream
	nStream map[string]int // active streams per module
}

type liveStream struct {
	cancel context.CancelFunc
	in     chan []byte
	resize chan [2]int
}

const (
	maxChunk        = 24 << 10
	maxTerminals    = 5
	maxOtherStreams = 3
)

// NewRegistry registers every enabled module.
func NewRegistry(cfg *config.Agent, col Collector, log *slog.Logger) *Registry {
	r := &Registry{cmds: map[string]CmdFunc{}, streams: map[string]StreamFunc{}, log: log, live: map[string]*liveStream{}, nStream: map[string]int{}}
	r.modules = cfg.List()
	if cfg.Enabled("terminal") {
		r.streams["terminal"] = termStream
	}
	if cfg.Enabled("services") {
		r.cmds["services.list"] = svcList
		r.cmds["services.action"] = svcAction
	}
	if cfg.Enabled("apt") {
		r.cmds["apt.list"] = aptList
		r.streams["apt"] = func(ctx context.Context, p json.RawMessage, io *StreamIO) error {
			defer refreshUpdates(col)
			return aptStream(ctx, p, io)
		}
	}
	if cfg.Enabled("processes") {
		r.cmds["processes.list"] = procList
		r.cmds["processes.kill"] = procKill
	}
	if cfg.Enabled("logs") {
		r.streams["logs"] = logsStream
	}
	r.cmds["system.reboot"] = sysReboot // always available: rebooting is not a toggleable module
	return r
}

// Modules lists the enabled module names (reported in hello).
func (r *Registry) Modules() []string { return r.modules }

// HandleCmd executes a command and returns the result message payload.
func (r *Registry) HandleCmd(ctx context.Context, c protocol.Cmd) protocol.CmdResult {
	res := protocol.CmdResult{ID: c.ID}
	fn, ok := r.cmds[c.Module+"."+c.Action]
	if !ok {
		res.Error = protocol.ErrModuleDisabled
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	data, err := fn(ctx, c.Params, c.Dangerous)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	b, err := json.Marshal(data)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.OK, res.Data = true, b
	return res
}

// OpenStream starts a stream handler in the background. send emits protocol messages.
func (r *Registry) OpenStream(ctx context.Context, o protocol.StreamOpen, send func(typ string, payload any) error) {
	fn, ok := r.streams[o.Module]
	if !ok {
		_ = send(protocol.TypeStreamClose, protocol.StreamClose{SID: o.SID, Reason: protocol.ErrModuleDisabled})
		return
	}
	limit := maxOtherStreams
	if o.Module == "terminal" {
		limit = maxTerminals
	}
	r.mu.Lock()
	if r.nStream[o.Module] >= limit || r.live[o.SID] != nil {
		r.mu.Unlock()
		_ = send(protocol.TypeStreamClose, protocol.StreamClose{SID: o.SID, Reason: protocol.ErrBusy})
		return
	}
	sctx, cancel := context.WithCancel(ctx)
	ls := &liveStream{cancel: cancel, in: make(chan []byte, 64), resize: make(chan [2]int, 8)}
	r.live[o.SID] = ls
	r.nStream[o.Module]++
	r.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			r.mu.Lock()
			delete(r.live, o.SID)
			r.nStream[o.Module]--
			r.mu.Unlock()
		}()
		sio := &StreamIO{In: ls.in, Resize: ls.resize, Out: func(b []byte) error {
			for len(b) > 0 {
				n := min(len(b), maxChunk)
				if err := send(protocol.TypeStreamData, protocol.StreamData{SID: o.SID, Data: b[:n]}); err != nil {
					return err
				}
				b = b[n:]
			}
			return nil
		}}
		reason := ""
		if err := fn(sctx, o.Params, sio); err != nil && sctx.Err() == nil {
			reason = err.Error()
		}
		_ = send(protocol.TypeStreamClose, protocol.StreamClose{SID: o.SID, Reason: reason})
	}()
}

// StreamInput feeds keystrokes to a running stream.
func (r *Registry) StreamInput(sid string, b []byte) {
	r.mu.Lock()
	ls := r.live[sid]
	r.mu.Unlock()
	if ls == nil {
		return
	}
	select {
	case ls.in <- b:
	default: // input is dropped if the handler cannot keep up
	}
}

// StreamResize changes the size of a running terminal.
func (r *Registry) StreamResize(sid string, cols, rows int) {
	r.mu.Lock()
	ls := r.live[sid]
	r.mu.Unlock()
	if ls != nil {
		select {
		case ls.resize <- [2]int{cols, rows}:
		default:
		}
	}
}

// StreamClose cancels a running stream (server asked to close it).
func (r *Registry) StreamClose(sid string) {
	r.mu.Lock()
	ls := r.live[sid]
	r.mu.Unlock()
	if ls != nil {
		ls.cancel()
	}
}

// CloseAll cancels every running stream (connection lost).
func (r *Registry) CloseAll() {
	r.mu.Lock()
	for _, ls := range r.live {
		ls.cancel()
	}
	r.mu.Unlock()
}

func refreshUpdates(col Collector) {
	if rc, ok := col.(interface{ RefreshUpdates() }); ok {
		go rc.RefreshUpdates()
	}
}
