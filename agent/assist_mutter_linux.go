//go:build linux

package agent

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/bolgov0zero/linager/internal/assist"
)

// The names of the private D-Bus interfaces of GNOME Mutter (the compositor of GNOME). A process of the person's own
// session may use them without a dialog: they are what the remote desktop of GNOME itself is built on.
const (
	rdDest   = "org.gnome.Mutter.RemoteDesktop"
	rdPath   = "/org/gnome/Mutter/RemoteDesktop"
	rdIface  = "org.gnome.Mutter.RemoteDesktop"
	rdSess   = "org.gnome.Mutter.RemoteDesktop.Session"
	scDest   = "org.gnome.Mutter.ScreenCast"
	scPath   = "/org/gnome/Mutter/ScreenCast"
	scIface  = "org.gnome.Mutter.ScreenCast"
	scSess   = "org.gnome.Mutter.ScreenCast.Session"
	scStream = "org.gnome.Mutter.ScreenCast.Stream"
	dcDest   = "org.gnome.Mutter.DisplayConfig"
	dcPath   = "/org/gnome/Mutter/DisplayConfig"
)

// The cursor modes of a stream.
const (
	cursorHidden   = 0
	cursorEmbedded = 1
)

// assistNodeTimeout is how long the helper waits for the compositor to name the PipeWire node of the stream.
const assistNodeTimeout = 8 * time.Second

// mutterHost is the screen, the keyboard, the mouse and the clipboard of a GNOME session on Wayland. The compositor
// gives a PipeWire stream of a monitor, GStreamer reads the frames of it, and the input goes back through the same
// remote-desktop session of the compositor.
type mutterHost struct {
	log  *slog.Logger
	conn *dbus.Conn

	// sigMu guards what the signal handler reads and writes; it never waits for mu, because starting a stream (with mu
	// held) waits for the handler to bring the node of the stream.
	sigMu     sync.Mutex
	sigStream dbus.ObjectPath
	sigRD     dbus.ObjectPath
	sigSC     dbus.ObjectPath
	closedErr error
	clipMimes []string // what the applications on the computer offer

	mu         sync.RWMutex
	rd         dbus.BusObject
	stream     dbus.ObjectPath
	node       uint32
	logicalW   float64 // the size of the stream in the logical pixels of the compositor
	logicalH   float64
	monitors   []assist.Monitor
	connectors []string
	sel        int
	showCursor bool
	maxFPS     int
	capture    *gstCapture
	clipOn     bool
	clipText   string // what the browser put on the clipboard of the computer
	out        []byte // the frame handed out by Grab

	relayout atomic.Bool   // the monitors changed: the capture starts again from the new layout
	frameCh  chan struct{} // a new frame came

	nodeCh chan uint32
	sigs   chan *dbus.Signal
	stop   chan struct{}
}

// newMutterHost connects to the session bus of the person this process runs as and opens the screen.
func newMutterHost(log *slog.Logger) (*mutterHost, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("the session bus: %w", err)
	}
	h := &mutterHost{log: log, conn: conn, maxFPS: 15, frameCh: make(chan struct{}, 1), nodeCh: make(chan uint32, 4), sigs: make(chan *dbus.Signal, 32), stop: make(chan struct{})}
	conn.Signal(h.sigs)
	for _, m := range []struct{ iface, member string }{
		{scStream, "PipeWireStreamAdded"},
		{rdSess, "SelectionOwnerChanged"},
		{rdSess, "SelectionTransfer"},
		{rdSess, "Closed"},
		{scSess, "Closed"},
		{dcDest, "MonitorsChanged"},
	} {
		if err := conn.AddMatchSignal(dbus.WithMatchInterface(m.iface), dbus.WithMatchMember(m.member)); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("match %s: %w", m.member, err)
		}
	}
	go h.signals()
	h.mu.Lock()
	err = h.startLocked()
	h.mu.Unlock()
	if err != nil {
		h.release()
		return nil, err
	}
	return h, nil
}

// startLocked opens the remote-desktop session and the screen-cast stream of the monitor chosen and starts reading
// frames. h.mu is held.
func (h *mutterHost) startLocked() error {
	h.readMonitors()
	conn := h.conn
	rd := conn.Object(rdDest, rdPath)
	var rdSession dbus.ObjectPath
	if err := rd.Call(rdIface+".CreateSession", 0).Store(&rdSession); err != nil {
		return fmt.Errorf("remote desktop session (GNOME Mutter is needed): %w", err)
	}
	rds := conn.Object(rdDest, rdSession)
	id, err := rds.GetProperty(rdSess + ".SessionId")
	if err != nil {
		return fmt.Errorf("session id: %w", err)
	}
	// the clipboard must be enabled before the session starts; a computer without it still works
	h.clipOn = conn.SupportsUnixFDs() && rds.Call(rdSess+".EnableClipboard", 0, map[string]dbus.Variant{}).Err == nil

	var scSession dbus.ObjectPath
	if err := conn.Object(scDest, scPath).Call(scIface+".CreateSession", 0, map[string]dbus.Variant{"remote-desktop-session-id": id}).Store(&scSession); err != nil {
		return fmt.Errorf("screen cast session: %w", err)
	}
	connector := ""
	if h.sel >= 0 && h.sel < len(h.connectors) {
		connector = h.connectors[h.sel]
	}
	mode := uint32(cursorHidden)
	if h.showCursor {
		mode = cursorEmbedded
	}
	var stream dbus.ObjectPath
	props := map[string]dbus.Variant{"cursor-mode": dbus.MakeVariant(mode)}
	if err := conn.Object(scDest, scSession).Call(scSess+".RecordMonitor", 0, connector, props).Store(&stream); err != nil {
		return fmt.Errorf("record the monitor %q: %w", connector, err)
	}
	for len(h.nodeCh) > 0 { // a node of an older stream
		<-h.nodeCh
	}
	h.rd, h.stream = rds, stream
	h.sigMu.Lock()
	h.sigStream, h.sigRD, h.sigSC = stream, rdSession, scSession
	h.sigMu.Unlock()
	if err := rds.Call(rdSess+".Start", 0).Err; err != nil {
		return fmt.Errorf("start the remote desktop session: %w", err)
	}
	select {
	case h.node = <-h.nodeCh:
	case <-time.After(assistNodeTimeout):
		return errors.New("the compositor gave no stream of the screen")
	}
	h.logicalW, h.logicalH = 0, 0
	if p, err := conn.Object(scDest, stream).GetProperty(scStream + ".Parameters"); err == nil {
		if m, ok := p.Value().(map[string]dbus.Variant); ok {
			if sz, ok := m["size"].Value().([]int32); ok && len(sz) == 2 {
				h.logicalW, h.logicalH = float64(sz[0]), float64(sz[1])
			}
		}
	}
	return h.startCaptureLocked()
}

func (h *mutterHost) startCaptureLocked() error {
	if h.capture != nil {
		h.capture.stop()
		h.capture = nil
	}
	w, hh := h.frameSizeLocked()
	c, err := h.tryGstLocked(w, hh, h.maxFPS)
	if err != nil && h.maxFPS > 0 && !errors.Is(err, errNoGst) {
		// some GStreamer builds have no usable videorate in front of a live stream: try without
		if c, err = h.tryGstLocked(w, hh, 0); err == nil {
			h.maxFPS = 0
		}
	}
	if err != nil {
		return err
	}
	h.capture = c
	return nil
}

// frameSizeLocked is the size of the frames: the current mode of the monitor, or the logical size of the stream.
func (h *mutterHost) frameSizeLocked() (int, int) {
	if h.sel >= 0 && h.sel < len(h.monitors) && h.monitors[h.sel].W > 0 {
		return h.monitors[h.sel].W, h.monitors[h.sel].H
	}
	return int(h.logicalW), int(h.logicalH)
}

// tryGstLocked starts a pipeline and waits for its first frame. A screen that does not change sends none, so after a
// moment the pointer is moved by a pixel and back: that is a change.
func (h *mutterHost) tryGstLocked(w, hh, fps int) (*gstCapture, error) {
	c, err := startGst(h.node, fps, w, hh, h.frameReady)
	if err != nil {
		return nil, err
	}
	err = c.waitFirst(2 * time.Second)
	if errors.Is(err, errNoPicture) && h.rd != nil {
		_ = h.rd.Call(rdSess+".NotifyPointerMotionRelative", 0, 1.0, 0.0).Err
		time.Sleep(60 * time.Millisecond)
		_ = h.rd.Call(rdSess+".NotifyPointerMotionRelative", 0, -1.0, 0.0).Err
		err = c.waitFirst(5 * time.Second)
	}
	if err != nil {
		c.stop()
		return nil, err
	}
	return c, nil
}

// stopLocked ends the sessions and the capture.
func (h *mutterHost) stopLocked() {
	if h.capture != nil {
		h.capture.stop()
		h.capture = nil
	}
	if h.rd != nil {
		_ = h.rd.Call(rdSess+".Stop", 0).Err
		h.rd = nil
	}
}

// restart opens the sessions again (another monitor, the pointer shown or hidden).
func (h *mutterHost) restart() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopLocked()
	h.sigMu.Lock()
	h.closedErr = nil
	h.sigMu.Unlock()
	return h.startLocked()
}

func (h *mutterHost) release() {
	select {
	case <-h.stop:
	default:
		close(h.stop)
	}
	h.mu.Lock()
	h.stopLocked()
	h.mu.Unlock()
	_ = h.conn.Close()
}

// readMonitors reads the layout of the monitors from the compositor.
func (h *mutterHost) readMonitors() {
	type spec struct{ Connector, Vendor, Product, Serial string }
	type mode struct {
		ID        string
		W, H      int32
		Refresh   float64
		PrefScale float64
		Scales    []float64
		Props     map[string]dbus.Variant
	}
	type monitor struct {
		Spec  spec
		Modes []mode
		Props map[string]dbus.Variant
	}
	type logical struct {
		X, Y      int32
		Scale     float64
		Transform uint32
		Primary   bool
		Monitors  []spec
		Props     map[string]dbus.Variant
	}
	var serial uint32
	var mons []monitor
	var logs []logical
	var props map[string]dbus.Variant
	if err := h.conn.Object(dcDest, dcPath).Call(dcDest+".GetCurrentState", 0).Store(&serial, &mons, &logs, &props); err != nil {
		h.log.Debug("assistant monitors", "err", err)
		h.monitors, h.connectors = nil, nil
		return
	}
	current := map[string][2]int{}
	for _, m := range mons {
		for _, md := range m.Modes {
			if v, ok := md.Props["is-current"]; ok && v.Value() == true {
				current[m.Spec.Connector] = [2]int{int(md.W), int(md.H)}
			}
		}
	}
	var layout []logicalMonitor
	for _, l := range logs {
		if len(l.Monitors) == 0 {
			continue
		}
		c := l.Monitors[0].Connector
		sz := current[c]
		layout = append(layout, logicalMonitor{X: int(l.X), Y: int(l.Y), Scale: l.Scale, Primary: l.Primary, Connector: c, W: sz[0], H: sz[1]})
	}
	h.monitors, h.connectors = buildMonitors(layout)
	if h.sel >= len(h.monitors) {
		h.sel = 0
	}
}

// signals handles what the compositor tells: the node of the stream, the end of the session, the clipboard.
func (h *mutterHost) signals() {
	for {
		select {
		case <-h.stop:
			return
		case s := <-h.sigs:
			if s == nil {
				return
			}
			switch {
			case strings.HasSuffix(s.Name, ".PipeWireStreamAdded"):
				h.sigMu.Lock()
				mine := s.Path == h.sigStream || h.sigStream == ""
				h.sigMu.Unlock()
				if n, ok := firstUint32(s.Body); ok && mine {
					select {
					case h.nodeCh <- n:
					default:
					}
				}
			case strings.HasSuffix(s.Name, ".MonitorsChanged"):
				h.relayout.Store(true)
			case strings.HasSuffix(s.Name, ".Closed"):
				h.sigMu.Lock()
				if s.Path == h.sigRD || s.Path == h.sigSC {
					h.closedErr = errors.New("the session of GNOME was closed by the compositor")
				}
				h.sigMu.Unlock()
			case strings.HasSuffix(s.Name, ".SelectionOwnerChanged"):
				h.onOwnerChanged(s)
			case strings.HasSuffix(s.Name, ".SelectionTransfer"):
				if len(s.Body) >= 2 {
					mime, _ := s.Body[0].(string)
					serial, _ := s.Body[1].(uint32)
					go h.serveClipboard(mime, serial)
				}
			}
		}
	}
}

func firstUint32(body []any) (uint32, bool) {
	if len(body) == 0 {
		return 0, false
	}
	n, ok := body[0].(uint32)
	return n, ok
}

func (h *mutterHost) onOwnerChanged(s *dbus.Signal) {
	if len(s.Body) == 0 {
		return
	}
	opts, ok := s.Body[0].(map[string]dbus.Variant)
	if !ok {
		return
	}
	if v, ok := opts["session-is-owner"]; ok && v.Value() == true {
		return // our own text
	}
	var mimes []string
	if v, ok := opts["mime-types"]; ok {
		mimes, _ = v.Value().([]string)
	}
	h.sigMu.Lock()
	h.clipMimes = mimes
	h.sigMu.Unlock()
}

// serveClipboard gives an application that pastes the text the browser put on the clipboard.
func (h *mutterHost) serveClipboard(_ string, serial uint32) {
	h.mu.RLock()
	rd, text := h.rd, h.clipText
	h.mu.RUnlock()
	if rd == nil {
		return
	}
	var fd dbus.UnixFD
	if err := rd.Call(rdSess+".SelectionWrite", 0, serial).Store(&fd); err != nil {
		h.log.Debug("assistant clipboard write", "err", err)
		return
	}
	f := os.NewFile(uintptr(fd), "clipboard") //nolint:gosec // a file descriptor the bus gave us
	_, werr := io.WriteString(f, text)
	_ = f.Close()
	_ = rd.Call(rdSess+".SelectionWriteDone", 0, serial, werr == nil).Err
}

// ---- assistHost ----

func (h *mutterHost) Grab() ([]byte, int, int, error) {
	if h.relayout.Swap(false) { // a monitor was plugged in or taken out, or the mode changed
		if err := h.restart(); err != nil {
			return nil, 0, 0, err
		}
	}
	h.sigMu.Lock()
	cerr := h.closedErr
	h.sigMu.Unlock()
	if cerr != nil {
		return nil, 0, 0, cerr
	}
	h.mu.RLock()
	c := h.capture
	h.mu.RUnlock()
	if c == nil {
		return nil, 0, 0, errors.New("the screen is not open")
	}
	if err := c.waitFirst(10 * time.Second); err != nil {
		return nil, 0, 0, err
	}
	if err := c.Err(); err != nil {
		return nil, 0, 0, err
	}
	h.out = c.copyFrame(h.out)
	return h.out, c.w, c.h, nil
}

func (h *mutterHost) Do(in assist.Input) error {
	h.mu.RLock()
	rd, stream, lw, lh, c := h.rd, h.stream, h.logicalW, h.logicalH, h.capture
	h.mu.RUnlock()
	if rd == nil {
		return errors.New("the session is not open")
	}
	// no answer is waited for: the events follow one another on the bus, and a round trip each would queue them up
	call := func(method string, args ...any) error {
		return rd.Call(rdSess+"."+method, dbus.FlagNoReplyExpected, args...).Err
	}
	move := func() error {
		fw, fh := 0, 0
		if c != nil {
			fw, fh = c.w, c.h
		}
		sx, sy := pointerScale(fw, fh, lw, lh)
		return call("NotifyPointerMotionAbsolute", string(stream), float64(in.X)*sx, float64(in.Y)*sy)
	}
	switch in.T {
	case assist.InMove:
		return move()
	case assist.InBtn:
		if err := move(); err != nil {
			return err
		}
		return call("NotifyPointerButton", mouseButtonEvdev[in.B], in.Down)
	case assist.InWheel:
		var err error
		if in.DY != 0 {
			err = call("NotifyPointerAxisDiscrete", uint32(0), int32(in.DY)) //nolint:gosec // bounded by ParseInput
		}
		if in.DX != 0 {
			if e := call("NotifyPointerAxisDiscrete", uint32(1), int32(in.DX)); e != nil { //nolint:gosec // bounded by ParseInput
				err = e
			}
		}
		return err
	case assist.InKey:
		code, ok := scanToEvdev(in.Sc, in.Ext)
		if !ok {
			return nil
		}
		return call("NotifyKeyboardKeycode", uint32(code), in.Down) //nolint:gosec // 1..127
	}
	return nil // Ctrl+Alt+Del has no meaning here
}

func (h *mutterHost) Monitors() ([]assist.Monitor, int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]assist.Monitor(nil), h.monitors...), h.sel
}

// SelectMonitor shows another monitor; -1 (all of them as one picture) is not offered by the compositor.
func (h *mutterHost) SelectMonitor(i int) error {
	h.mu.Lock()
	if i < 0 || i >= len(h.monitors) {
		h.mu.Unlock()
		return errors.New("that monitor cannot be shown")
	}
	if i == h.sel {
		h.mu.Unlock()
		return nil
	}
	h.sel = i
	h.mu.Unlock()
	return h.restart()
}

func (h *mutterHost) ClipboardGet() (string, error) {
	h.mu.RLock()
	rd, on := h.rd, h.clipOn
	h.mu.RUnlock()
	h.sigMu.Lock()
	mimes := h.clipMimes
	h.sigMu.Unlock()
	if rd == nil || !on {
		return "", errors.New("the clipboard of this computer cannot be read")
	}
	mime := pickClipMime(mimes)
	if mime == "" {
		return "", errors.New("there is no text on the clipboard")
	}
	var fd dbus.UnixFD
	if err := rd.Call(rdSess+".SelectionRead", 0, mime).Store(&fd); err != nil {
		return "", err
	}
	f := os.NewFile(uintptr(fd), "clipboard") //nolint:gosec // a file descriptor the bus gave us
	defer func() { _ = f.Close() }()
	_ = f.SetReadDeadline(time.Now().Add(3 * time.Second))
	b, err := io.ReadAll(io.LimitReader(f, assist.MaxClip+1))
	if err != nil && len(b) == 0 {
		return "", err
	}
	if len(b) > assist.MaxClip {
		return "", errors.New("the clipboard is too long")
	}
	return string(b), nil
}

func (h *mutterHost) ClipboardSet(text string) error {
	h.mu.Lock()
	rd, on := h.rd, h.clipOn
	h.clipText = text
	h.mu.Unlock()
	if rd == nil || !on {
		return errors.New("the clipboard of this computer cannot be written")
	}
	return rd.Call(rdSess+".SetSelection", 0, map[string]dbus.Variant{"mime-types": dbus.MakeVariant(clipMimes[:3])}).Err
}

func (h *mutterHost) Desktop() string { return "default" }

// frameReady tells the loop that a new frame came, so that a change on the screen is sent at once and not at the next tick.
func (h *mutterHost) frameReady() {
	select {
	case h.frameCh <- struct{}{}:
	default:
	}
}

// Frames is what the loop waits on besides its timer.
func (h *mutterHost) Frames() <-chan struct{} { return h.frameCh }

// SetShowCursor draws the pointer of the computer into the picture or not; the compositor decides it when the stream
// is made, so the sessions are opened again.
func (h *mutterHost) SetShowCursor(show bool) {
	h.mu.Lock()
	if h.showCursor == show {
		h.mu.Unlock()
		return
	}
	h.showCursor = show
	h.mu.Unlock()
	if err := h.restart(); err != nil {
		h.log.Warn("assistant cursor", "err", err)
		h.sigMu.Lock()
		h.closedErr = err
		h.sigMu.Unlock()
	}
}

// SetMaxFPS limits the frames the pipeline produces (the loop does not need more than the setting allows).
func (h *mutterHost) SetMaxFPS(n int) {
	n = max(1, min(n, 30))
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.maxFPS == 0 || h.maxFPS == n || h.capture == nil {
		return // 0: this GStreamer has no rate limit; the pipeline already has this rate
	}
	h.maxFPS = n
	if err := h.startCaptureLocked(); err != nil {
		h.sigMu.Lock()
		h.closedErr = err
		h.sigMu.Unlock()
	}
}
