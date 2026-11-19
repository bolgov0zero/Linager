package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
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

func startLoop(t *testing.T) (net.Conn, *fakeHost, chan error) {
	t.Helper()
	a, b := net.Pipe()
	h := newFakeHost()
	done := make(chan error, 1)
	go func() {
		done <- runAssistLoop(context.Background(), b, h, assist.Hello{Session: 1, User: "u"}, nil)
		_ = b.Close()
	}()
	t.Cleanup(func() { _ = a.Close() })
	return a, h, done
}

func TestAssistLoop(t *testing.T) {
	a, in, done := startLoop(t)

	var hello assist.Hello
	if err := json.Unmarshal(readUntil(t, a, assist.TypeHello).Data, &hello); err != nil {
		t.Fatal(err)
	}
	if hello.Width != 960 || hello.Height != 600 || hello.User != "u" || len(hello.Monitors) != 2 || hello.Monitor != 0 || !hello.Monitors[0].Primary {
		t.Fatalf("hello: %+v", hello)
	}
	tiles, err := assist.UnpackTiles(readUntil(t, a, assist.TypeTiles).Data)
	if err != nil || len(tiles) < 5 {
		t.Fatalf("the first picture covers the whole screen: %d tiles, %v", len(tiles), err)
	}

	// input reaches the host and shows on the next picture
	if err := assist.WriteJSON(a, assist.TypeInput, assist.Input{T: assist.InMove, X: 400, Y: 300}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pointer moved", func() bool { x, y, _, _ := in.state(); return x == 400 && y == 300 })
	if err := assist.WriteJSON(a, assist.TypeInput, assist.Input{T: assist.InKey, Sc: 30, Down: true}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the key counted", func() bool { _, _, k, _ := in.state(); return k == 1 })

	// bad input and bad settings are ignored, good settings are taken
	_ = assist.WriteFrame(a, assist.TypeInput, []byte(`{"t":"zzz"}`))
	_ = assist.WriteFrame(a, assist.TypeSettings, []byte(`{"colors":"nope","fps":15,"mbit":4}`))
	if err := assist.WriteJSON(a, assist.TypeSettings, assist.Settings{Colors: assist.ColorsGray, FPS: 30, NoLimit: true, Mbit: 4}); err != nil {
		t.Fatal(err)
	}
	var st assist.Stats
	if err := json.Unmarshal(readUntil(t, a, assist.TypeStats).Data, &st); err != nil || st.Desktop != "default" {
		t.Fatalf("stats: %+v %v", st, err)
	}

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

// The choice of a display gives a new hello with the new size and a whole new picture; the pointer is in the pixels of
// the picture that is shown.
func TestAssistLoopMonitorsClipboardAndDesktop(t *testing.T) {
	a, host, _ := startLoop(t)
	readUntil(t, a, assist.TypeHello)
	readUntil(t, a, assist.TypeTiles)

	if err := assist.WriteJSON(a, assist.TypeMonitor, assist.MonitorSel{I: 1}); err != nil {
		t.Fatal(err)
	}
	var hello assist.Hello
	for hello.Monitor != 1 {
		if err := json.Unmarshal(readUntil(t, a, assist.TypeHello).Data, &hello); err != nil {
			t.Fatal(err)
		}
	}
	if hello.Width != 640 || hello.Height != 480 {
		t.Fatalf("the second display: %+v", hello)
	}
	if tl, _ := assist.UnpackTiles(readUntil(t, a, assist.TypeTiles).Data); len(tl) < 5 {
		t.Fatalf("a whole picture after the change: %d tiles", len(tl))
	}
	_ = assist.WriteJSON(a, assist.TypeInput, assist.Input{T: assist.InMove, X: 10, Y: 20})
	waitFor(t, "the pointer in desktop pixels", func() bool { x, y, _, _ := host.state(); return x == 970 && y == 20 })
	_ = assist.WriteJSON(a, assist.TypeMonitor, assist.MonitorSel{I: 9}) // a display that is not there changes nothing

	// the clipboard goes both ways
	if err := assist.WriteJSON(a, assist.TypeClipSet, assist.Clip{Text: "привет\nмир"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "clipboard set", func() bool { s, _ := host.ClipboardGet(); return s == "привет\nмир" })
	if err := assist.WriteFrame(a, assist.TypeClipGet, nil); err != nil {
		t.Fatal(err)
	}
	var clip assist.Clip
	if err := json.Unmarshal(readUntil(t, a, assist.TypeClip).Data, &clip); err != nil || clip.Text != "привет\nмир" {
		t.Fatalf("clipboard back: %+v %v", clip, err)
	}

	// Ctrl+Alt+Del on the fake host locks the screen, and the stats say so at once
	_ = assist.WriteJSON(a, assist.TypeInput, assist.Input{T: assist.InCAD})
	for {
		var st assist.Stats
		if err := json.Unmarshal(readUntil(t, a, assist.TypeStats).Data, &st); err != nil {
			t.Fatal(err)
		}
		if st.Desktop == "winlogon" {
			break
		}
	}

	// the pointer can be left out of the picture (the pipe has no buffer: the frames are read meanwhile)
	go func() {
		for {
			if _, err := assist.ReadFrame(a); err != nil {
				return
			}
		}
	}()
	_ = assist.WriteJSON(a, assist.TypeSettings, assist.Settings{Colors: assist.Colors24, FPS: 15, NoLimit: true, Mbit: 4, HideCursor: true})
	waitFor(t, "the pointer is hidden", func() bool { host.mu.Lock(); defer host.mu.Unlock(); return !host.showCursor })
}

type fakeHelper struct {
	done chan struct{}
	err  error
	once sync.Once
}

func (h *fakeHelper) Done() <-chan struct{} { return h.done }
func (h *fakeHelper) Err() error            { return h.err }
func (h *fakeHelper) Kill()                 { h.once.Do(func() { close(h.done) }) }

// A helper that calls back with the wrong token first and then with the right one: only the second is served. Ctrl+Alt+Del
// is taken out on the way up and handed to the service, the rest reaches the helper.
func TestAssistRelay(t *testing.T) {
	var host *fakeHost
	launch := func(ctx context.Context, session, addr, token string) (assistHelper, error) {
		h := &fakeHelper{done: make(chan struct{})}
		host = newFakeHost()
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
			_ = runAssistLoop(ctx, c, host, assist.Hello{Session: 1}, nil)
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
	var sas atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runAssistRelay(ctx, sio, "console", launch, func() error { sas.Add(1); return nil })
	}()

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
	_ = assist.WriteJSON(&buf, assist.TypeInput, assist.Input{T: assist.InCAD})
	b := buf.Bytes()
	inCh <- b[:7] // the chunks of the stream do not follow the frames
	inCh <- b[7:]
	waitFor(t, "input through the relay", func() bool { x, y, _, _ := host.state(); return x == 77 && y == 88 })
	waitFor(t, "Ctrl+Alt+Del goes to the service", func() bool { return sas.Load() == 1 })
	if _, _, _, locked := host.state(); locked {
		t.Fatal("Ctrl+Alt+Del must not reach the helper")
	}

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
	if err := runAssistRelay(context.Background(), sio, "9", func(context.Context, string, string, string) (assistHelper, error) { return nil, boom }, nil); !errors.Is(err, boom) {
		t.Fatalf("a failed launch: %v", err)
	}
	// the helper dies before it calls back
	err := runAssistRelay(context.Background(), sio, "console", func(context.Context, string, string, string) (assistHelper, error) {
		h := &fakeHelper{done: make(chan struct{}), err: errors.New("exit code 3")}
		h.Kill()
		return h, nil
	}, nil)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("exit code 3")) {
		t.Fatalf("a helper that stops: %v", err)
	}
	// the helper never calls back
	old := assistConnectTimeout
	assistConnectTimeout = 200 * time.Millisecond
	defer func() { assistConnectTimeout = old }()
	err = runAssistRelay(context.Background(), sio, "console", func(context.Context, string, string, string) (assistHelper, error) {
		return &fakeHelper{done: make(chan struct{})}, nil
	}, nil)
	if err == nil {
		t.Fatal("a silent helper must time out")
	}
}
