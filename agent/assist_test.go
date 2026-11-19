package agent

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/bolgov0zero/linager/internal/assist"
)

// readUntil reads frames from c until one of the wanted type arrives (or the time is out).
func readUntil(t *testing.T, c net.Conn, typ byte) assist.Frame {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		f, err := assist.ReadFrame(c)
		if err != nil {
			t.Fatalf("waiting for frame %d: %v", typ, err)
		}
		if f.Type == typ {
			return f
		}
	}
}

func TestAssistLoop(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close() }()
	in := &fakeInput{x: 10, y: 10}
	done := make(chan error, 1)
	go func() {
		done <- runAssistLoop(context.Background(), b, newFakeScreen(in), in, assist.Hello{Session: 1, User: "u"}, nil)
		_ = b.Close()
	}()

	hello := readUntil(t, a, assist.TypeHello)
	if !bytes.Contains(hello.Data, []byte(`"w":960`)) || !bytes.Contains(hello.Data, []byte(`"user":"u"`)) {
		t.Fatalf("hello: %s", hello.Data)
	}
	tiles, err := assist.UnpackTiles(readUntil(t, a, assist.TypeTiles).Data)
	if err != nil || len(tiles) < 5 {
		t.Fatalf("the first picture covers the whole screen: %d tiles, %v", len(tiles), err)
	}

	// input reaches the sink and shows on the next picture
	if err := assist.WriteJSON(a, assist.TypeInput, assist.Input{T: assist.InMove, X: 400, Y: 300}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pointer moved", func() bool { x, y, _ := in.state(); return x == 400 && y == 300 })
	if err := assist.WriteJSON(a, assist.TypeInput, assist.Input{T: assist.InKey, Sc: 30, Down: true}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the key counted", func() bool { _, _, k := in.state(); return k == 1 })

	// bad input and bad settings are ignored, good settings are taken (a grey picture is a grey JPEG)
	_ = assist.WriteFrame(a, assist.TypeInput, []byte(`{"t":"zzz"}`))
	_ = assist.WriteFrame(a, assist.TypeSettings, []byte(`{"colors":"nope","fps":15,"mbit":4}`))
	if err := assist.WriteJSON(a, assist.TypeSettings, assist.Settings{Colors: assist.ColorsGray, FPS: 30, NoLimit: true, Mbit: 4}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stats", func() bool { readUntil(t, a, assist.TypeStats); return true })

	// an end frame stops the loop
	if err := assist.WriteJSON(a, assist.TypeEnd, assist.End{Reason: "bye"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("loop: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the loop did not stop")
	}
}

func waitFor(t *testing.T, what string, f func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if f() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", what)
}

type fakeHelper struct {
	done chan struct{}
	err  error
	once sync.Once
}

func (h *fakeHelper) Done() <-chan struct{} { return h.done }
func (h *fakeHelper) Err() error            { return h.err }
func (h *fakeHelper) Kill()                 { h.once.Do(func() { close(h.done) }) }

// A helper that calls back with the wrong token first and then with the right one: only the second is served.
func TestAssistRelay(t *testing.T) {
	in := &fakeInput{x: 1, y: 1}
	launch := func(ctx context.Context, session, addr, token string) (assistHelper, error) {
		h := &fakeHelper{done: make(chan struct{})}
		go func() {
			if bad, err := net.Dial("tcp", addr); err == nil { // an intruder
				_, _ = bad.Write(bytes.Repeat([]byte("x"), len(token)))
				_ = bad.Close()
			}
			c, err := net.Dial("tcp", addr)
			if err != nil {
				return
			}
			_, _ = c.Write([]byte(token))
			_ = runAssistLoop(ctx, c, newFakeScreen(in), in, assist.Hello{Session: 1}, nil)
			_ = c.Close()
			h.Kill()
		}()
		return h, nil
	}
	inCh := make(chan []byte, 16)
	var mu sync.Mutex
	var re assist.Reassembler
	var got []assist.Frame
	sio := &StreamIO{In: inCh, Out: func(b []byte) error {
		mu.Lock()
		defer mu.Unlock()
		fs, err := re.Push(b)
		got = append(got, fs...)
		return err
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runAssistRelay(ctx, sio, "console", launch) }()

	has := func(typ byte) bool {
		mu.Lock()
		defer mu.Unlock()
		for _, f := range got {
			if f.Type == typ {
				return true
			}
		}
		return false
	}
	waitFor(t, "hello through the relay", func() bool { return has(assist.TypeHello) })
	waitFor(t, "tiles through the relay", func() bool { return has(assist.TypeTiles) })
	var buf bytes.Buffer
	_ = assist.WriteJSON(&buf, assist.TypeInput, assist.Input{T: assist.InMove, X: 77, Y: 88})
	inCh <- buf.Bytes()
	waitFor(t, "input through the relay", func() bool { x, y, _ := in.state(); return x == 77 && y == 88 })

	close(inCh) // the stream ends
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("relay: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the relay did not end with the stream")
	}
	cancel()
}

func TestAssistRelayHelperFailures(t *testing.T) {
	sio := &StreamIO{In: make(chan []byte), Out: func([]byte) error { return nil }}
	// the launcher fails
	boom := errors.New("no such session")
	if err := runAssistRelay(context.Background(), sio, "9", func(context.Context, string, string, string) (assistHelper, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("a failed launch: %v", err)
	}
	// the helper dies before it calls back
	err := runAssistRelay(context.Background(), sio, "console", func(context.Context, string, string, string) (assistHelper, error) {
		h := &fakeHelper{done: make(chan struct{}), err: errors.New("exit code 3")}
		h.Kill()
		return h, nil
	})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("exit code 3")) {
		t.Fatalf("a helper that stops: %v", err)
	}
	// the helper never calls back
	old := assistConnectTimeout
	assistConnectTimeout = 200 * time.Millisecond
	defer func() { assistConnectTimeout = old }()
	err = runAssistRelay(context.Background(), sio, "console", func(context.Context, string, string, string) (assistHelper, error) {
		return &fakeHelper{done: make(chan struct{})}, nil
	})
	if err == nil {
		t.Fatal("a silent helper must time out")
	}
}
