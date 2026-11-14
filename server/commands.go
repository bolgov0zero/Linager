package server

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/internal/protocol"
)

// Errors returned to API handlers.
var (
	ErrOffline        = errors.New("agent is offline")
	ErrCallTimeout    = errors.New("agent did not answer in time")
	ErrAgentDisconect = errors.New("agent disconnected")
)

// AgentError is an error reported by the agent module.
type AgentError struct{ Msg string }

func (e *AgentError) Error() string { return e.Msg }

// connState holds request/stream bookkeeping of one agent connection.
type connState struct {
	mu      sync.Mutex
	pending map[string]chan protocol.CmdResult
	streams map[string]*StreamHandle
	closed  bool
}

func newConnState() *connState {
	return &connState{pending: map[string]chan protocol.CmdResult{}, streams: map[string]*StreamHandle{}}
}

// StreamHandle is the server side of one agent stream.
type StreamHandle struct {
	SID  string
	Data chan []byte
	done chan struct{}
	once sync.Once
	// Reason is set when the stream ended.
	reason string
	c      *conn
}

// Done is closed when the stream has ended.
func (s *StreamHandle) Done() <-chan struct{} { return s.done }

// Reason explains why the stream ended (empty for a normal end).
func (s *StreamHandle) Reason() string { return s.reason }

func (s *StreamHandle) finish(reason string) {
	s.once.Do(func() {
		s.reason = reason
		close(s.done)
	})
}

// Input sends keystrokes to the agent.
func (s *StreamHandle) Input(ctx context.Context, b []byte) error {
	return s.c.send(ctx, protocol.TypeStreamInput, protocol.StreamData{SID: s.SID, Data: b})
}

// Resize changes the remote terminal size.
func (s *StreamHandle) Resize(ctx context.Context, cols, rows int) error {
	return s.c.send(ctx, protocol.TypeStreamResize, protocol.StreamResize{SID: s.SID, Cols: cols, Rows: rows})
}

// Close ends the stream on both sides.
func (s *StreamHandle) Close() {
	s.c.state.mu.Lock()
	_, was := s.c.state.streams[s.SID]
	delete(s.c.state.streams, s.SID)
	s.c.state.mu.Unlock()
	if was {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = s.c.send(ctx, protocol.TypeStreamClose, protocol.StreamClose{SID: s.SID})
		cancel()
	}
	s.finish("")
}

func (h *Hub) conn(id string) *conn {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.conns[id]
}

// Call runs one command on the agent and waits for its result.
func (h *Hub) Call(ctx context.Context, serverID, module, action string, params any, dangerous bool, timeout time.Duration) (json.RawMessage, error) {
	c := h.conn(serverID)
	if c == nil {
		return nil, ErrOffline
	}
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	id := protocol.NewID()
	ch := make(chan protocol.CmdResult, 1)
	c.state.mu.Lock()
	if c.state.closed {
		c.state.mu.Unlock()
		return nil, ErrOffline
	}
	c.state.pending[id] = ch
	c.state.mu.Unlock()
	defer func() {
		c.state.mu.Lock()
		delete(c.state.pending, id)
		c.state.mu.Unlock()
	}()
	if err := c.send(ctx, protocol.TypeCmd, protocol.Cmd{ID: id, Module: module, Action: action, Params: raw, Dangerous: dangerous}); err != nil {
		return nil, err
	}
	select {
	case res, ok := <-ch:
		if !ok {
			return nil, ErrAgentDisconect
		}
		if !res.OK {
			return nil, &AgentError{Msg: res.Error}
		}
		return res.Data, nil
	case <-time.After(timeout):
		return nil, ErrCallTimeout
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// OpenStream starts a stream on the agent.
func (h *Hub) OpenStream(ctx context.Context, serverID, module string, params any) (*StreamHandle, error) {
	c := h.conn(serverID)
	if c == nil {
		return nil, ErrOffline
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	sh := &StreamHandle{SID: protocol.NewID(), Data: make(chan []byte, 128), done: make(chan struct{}), c: c}
	c.state.mu.Lock()
	if c.state.closed {
		c.state.mu.Unlock()
		return nil, ErrOffline
	}
	c.state.streams[sh.SID] = sh
	c.state.mu.Unlock()
	if err := c.send(ctx, protocol.TypeStreamOpen, protocol.StreamOpen{SID: sh.SID, Module: module, Params: raw}); err != nil {
		sh.Close()
		return nil, err
	}
	return sh, nil
}

// deliver routes agent messages to waiting callers and streams.
func (c *conn) deliver(ctx context.Context, env *protocol.Envelope) {
	switch env.Type {
	case protocol.TypeCmdResult:
		var res protocol.CmdResult
		if json.Unmarshal(env.Payload, &res) != nil {
			return
		}
		c.state.mu.Lock()
		ch := c.state.pending[res.ID]
		c.state.mu.Unlock()
		if ch != nil {
			select {
			case ch <- res:
			default:
			}
		}
	case protocol.TypeStreamData:
		var d protocol.StreamData
		if json.Unmarshal(env.Payload, &d) != nil {
			return
		}
		c.state.mu.Lock()
		sh := c.state.streams[d.SID]
		c.state.mu.Unlock()
		if sh != nil {
			select {
			case sh.Data <- d.Data:
			case <-sh.done:
			case <-ctx.Done():
			case <-time.After(10 * time.Second): // a stuck browser must not block the whole agent connection
				sh.finish("consumer too slow")
			}
		}
	case protocol.TypeStreamClose:
		var sc protocol.StreamClose
		if json.Unmarshal(env.Payload, &sc) != nil {
			return
		}
		c.state.mu.Lock()
		sh := c.state.streams[sc.SID]
		delete(c.state.streams, sc.SID)
		c.state.mu.Unlock()
		if sh != nil {
			sh.finish(sc.Reason)
		}
	}
}

// failAll is called when the connection ends: pending calls fail and streams finish.
func (c *conn) failAll() {
	c.state.mu.Lock()
	c.state.closed = true
	pend := c.state.pending
	streams := c.state.streams
	c.state.pending = map[string]chan protocol.CmdResult{}
	c.state.streams = map[string]*StreamHandle{}
	c.state.mu.Unlock()
	for _, ch := range pend {
		close(ch)
	}
	for _, s := range streams {
		s.finish("agent disconnected")
	}
}
