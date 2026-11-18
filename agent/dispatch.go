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

	dataDir string
	emitMu  sync.Mutex
	emitFn  func(typ string, payload any) error
}

// SetEmitter lets modules send unsolicited messages (update progress) over the current connection.
func (r *Registry) SetEmitter(f func(typ string, payload any) error) {
	r.emitMu.Lock()
	r.emitFn = f
	r.emitMu.Unlock()
}

func (r *Registry) emitUpdate(e protocol.UpdateEvent) {
	r.emitMu.Lock()
	f := r.emitFn
	r.emitMu.Unlock()
	if f != nil {
		_ = f(protocol.TypeUpdateEvent, e)
	}
}

type liveStream struct {
	ctx    context.Context
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
func NewRegistry(cfg *config.Agent, col Collector, log *slog.Logger, dataDir string) *Registry {
	r := &Registry{cmds: map[string]CmdFunc{}, streams: map[string]StreamFunc{}, log: log, live: map[string]*liveStream{}, nStream: map[string]int{}, dataDir: dataDir}
	r.modules = cfg.List()
	r.registerPlatform(cfg, col, log, dataDir)
	return r
}

// Modules lists the enabled module names (reported in hello).
func (r *Registry) Modules() []string { return r.modules }

// defaultCmdTimeout is the limit of a command; cmdTimeouts lists the ones that may run longer.
const defaultCmdTimeout = 2 * time.Minute

var cmdTimeouts = map[string]time.Duration{
	// the update downloads a release of tens of megabytes: on a slow link two minutes are not enough
	"update.apply": protocol.AgentUpdateTimeout,
}

// HandleCmd executes a command and returns the result message payload.
func (r *Registry) HandleCmd(ctx context.Context, c protocol.Cmd) protocol.CmdResult {
	res := protocol.CmdResult{ID: c.ID}
	fn, ok := r.cmds[c.Module+"."+c.Action]
	if !ok {
		res.Error = protocol.ErrModuleDisabled
		return res
	}
	limit := defaultCmdTimeout
	if d, ok := cmdTimeouts[c.Module+"."+c.Action]; ok {
		limit = d
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	data, err := fn(ctx, c.Params, c.Dangerous)
	if err != nil {
		if r.log != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			r.log.Warn("command timed out", "cmd", c.Module+"."+c.Action, "limit_s", int64(limit.Seconds()))
		}
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
	ls := &liveStream{ctx: sctx, cancel: cancel, in: make(chan []byte, 64), resize: make(chan [2]int, 8)}
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
	// Blocking with a timeout: uploads must not lose chunks. The wait is bounded so a stuck handler
	// cannot stall the whole connection for long.
	select {
	case ls.in <- b:
	case <-ls.ctx.Done():
	case <-time.After(10 * time.Second):
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

func without(list []string, name string) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if v != name {
			out = append(out, v)
		}
	}
	return out
}
