package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/bolgov0zero/linager/server/store"
)

// Live fans server-sent events out to connected browsers.
type Live struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

// NewLive creates an empty fan-out.
func NewLive() *Live { return &Live{subs: map[chan []byte]struct{}{}} }

// Publish sends one named event to every subscriber; slow subscribers drop events.
func (l *Live) Publish(event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, b))
	l.mu.Lock()
	defer l.mu.Unlock()
	for ch := range l.subs {
		select {
		case ch <- msg:
		default: // subscriber is too slow; the browser reloads state on reconnect
		}
	}
}

func (l *Live) subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 256)
	l.mu.Lock()
	l.subs[ch] = struct{}{}
	l.mu.Unlock()
	return ch, func() {
		l.mu.Lock()
		delete(l.subs, ch)
		l.mu.Unlock()
	}
}

// handler streams events to one authenticated browser.
func (a *Admin) liveHandler(w http.ResponseWriter, r *http.Request, _ *store.User) {
	fl, ok := w.(http.Flusher)
	if !ok {
		jsonErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // nginx: do not buffer the stream
	ch, unsub := a.Live.subscribe()
	defer unsub()
	_, _ = w.Write([]byte("retry: 3000\n\n"))
	fl.Flush()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case m := <-ch:
			if _, err := w.Write(m); err != nil {
				return
			}
			fl.Flush()
		case <-tick.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			fl.Flush()
		}
	}
}
