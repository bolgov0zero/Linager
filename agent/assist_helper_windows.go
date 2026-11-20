//go:build windows

package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/bolgov0zero/linager/internal/assist"
)

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modGdi32    = windows.NewLazySystemDLL("gdi32.dll")
	modSas      = windows.NewLazySystemDLL("sas.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procGetDC            = modUser32.NewProc("GetDC")
	procReleaseDC        = modUser32.NewProc("ReleaseDC")
	procGetSystemMetrics = modUser32.NewProc("GetSystemMetrics")
	procSetDPIAware      = modUser32.NewProc("SetProcessDPIAware")
	procSendInput        = modUser32.NewProc("SendInput")
	procGetCursorInfo    = modUser32.NewProc("GetCursorInfo")
	procGetIconInfo      = modUser32.NewProc("GetIconInfo")
	procDrawIconEx       = modUser32.NewProc("DrawIconEx")

	procCreateCompatibleDC = modGdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = modGdi32.NewProc("CreateDIBSection")
	procSelectObject       = modGdi32.NewProc("SelectObject")
	procBitBlt             = modGdi32.NewProc("BitBlt")
	procDeleteObject       = modGdi32.NewProc("DeleteObject")
	procDeleteDC           = modGdi32.NewProc("DeleteDC")

	procSendSAS = modSas.NewProc("SendSAS")
)

const (
	smCxScreen = 0
	smCyScreen = 1
	srcCopy    = 0x00CC0020
	captureBlt = 0x40000000
	diNormal   = 3
)

func metric(i uintptr) int {
	r, _, _ := procGetSystemMetrics.Call(i)
	return int(int32(r)) //nolint:gosec // a small number
}

// uiThread runs functions on one operating system thread. A desktop belongs to a thread (SetThreadDesktop), so every
// call that touches the screen, the input or the clipboard goes through the same thread, and the thread follows the
// desktop the person works on.
type uiThread struct{ ch chan func() }

func newUIThread() *uiThread {
	u := &uiThread{ch: make(chan func())}
	go func() {
		runtime.LockOSThread()
		for f := range u.ch {
			f()
		}
	}()
	return u
}

func (u *uiThread) do(f func()) {
	done := make(chan struct{})
	u.ch <- func() {
		defer close(done)
		f()
	}
	<-done
}

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes, Bits  uint16
	Compression   uint32
	SizeImage     uint32
	XPels, YPels  int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type cursorInfo struct {
	Size   uint32
	Flags  uint32
	Cursor uintptr
	X, Y   int32
}

type iconInfo struct {
	Icon  int32
	XHot  uint32
	YHot  uint32
	Mask  uintptr
	Color uintptr
}

type mouseInput struct {
	DX, DY    int32
	Data      uint32
	Flags     uint32
	Time      uint32
	_         uint32
	ExtraInfo uintptr
}

type keybdInput struct {
	VK        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	_         uint32
	ExtraInfo uintptr
}

// inputRec is the INPUT structure of Windows: a type and a union of the size of the largest member (40 bytes in all).
type inputRec struct {
	Type uint32
	_    uint32
	U    [32]byte
}

const (
	inMouse    = 0
	inKeyboard = 1

	mevMove       = 0x0001
	mevLeftDown   = 0x0002
	mevLeftUp     = 0x0004
	mevRightDown  = 0x0008
	mevRightUp    = 0x0010
	mevMiddleDown = 0x0020
	mevMiddleUp   = 0x0040
	mevXDown      = 0x0080
	mevXUp        = 0x0100
	mevWheel      = 0x0800
	mevHWheel     = 0x1000
	mevAbsolute   = 0x8000

	kevExtended = 0x0001
	kevKeyUp    = 0x0002
	kevScancode = 0x0008
	kevUnicode  = 0x0004
)

var (
	procOpenInputDesktop    = modUser32.NewProc("OpenInputDesktop")
	procCloseDesktop        = modUser32.NewProc("CloseDesktop")
	procSetThreadDesktop    = modUser32.NewProc("SetThreadDesktop")
	procGetUserObjectInfo   = modUser32.NewProc("GetUserObjectInformationW")
	procEnumDisplayMonitors = modUser32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfo      = modUser32.NewProc("GetMonitorInfoW")
	procOpenClipboard       = modUser32.NewProc("OpenClipboard")
	procCloseClipboard      = modUser32.NewProc("CloseClipboard")
	procEmptyClipboard      = modUser32.NewProc("EmptyClipboard")
	procGetClipboardData    = modUser32.NewProc("GetClipboardData")
	procSetClipboardData    = modUser32.NewProc("SetClipboardData")
	procSetDpiAwarenessCtx  = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procGlobalAlloc         = modKernel32.NewProc("GlobalAlloc")
	procGlobalFree          = modKernel32.NewProc("GlobalFree")
	procGlobalLock          = modKernel32.NewProc("GlobalLock")
	procGlobalUnlock        = modKernel32.NewProc("GlobalUnlock")
	procGlobalSize          = modKernel32.NewProc("GlobalSize")
)

const (
	genericAll      = 0x10000000
	uoiName         = 2
	smXVirtual      = 76
	smYVirtual      = 77
	smCxVirtual     = 78
	smCyVirtual     = 79
	cfUnicodeText   = 13
	gmemMoveable    = 0x0002
	mevVirtualDesk  = 0x4000
	monitorPrimary  = 1
	monitorRefresh  = 2 * time.Second
	dpiPerMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 is -4
)

type monitorInfo struct {
	Size          uint32
	Monitor, Work [4]int32 // left, top, right, bottom
	Flags         uint32
}

// monitorSink collects the displays of one EnumDisplayMonitors call; the call and the callback run on the UI thread,
// one at a time.
var monitorSink *[]assist.Monitor

var monitorEnumCb = windows.NewCallback(func(hmon, _, _, _ uintptr) uintptr {
	mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if r, _, _ := procGetMonitorInfo.Call(hmon, uintptr(unsafe.Pointer(&mi))); r != 0 && monitorSink != nil {
		*monitorSink = append(*monitorSink, assist.Monitor{X: int(mi.Monitor[0]), Y: int(mi.Monitor[1]),
			W: int(mi.Monitor[2] - mi.Monitor[0]), H: int(mi.Monitor[3] - mi.Monitor[1]), Primary: mi.Flags&monitorPrimary != 0})
	}
	return 1
})

// winHost is the session this process runs in, as the Assistant sees it: the picture of the desktop the person works
// on (GDI: it works in remote desktop sessions, where the desktop duplication API does not, and on every desktop of a
// session), the mouse and the keys (SendInput) and the text clipboard of the window station.
type winHost struct {
	ui *uiThread

	// the picture (UI thread only)
	w, h       int
	memDC      uintptr
	bmp, old   uintptr
	bits       unsafe.Pointer
	failures   int
	hot        map[uintptr][2]int // the hot spots of the cursors seen
	showCursor bool

	// the desktop the thread is attached to (UI thread only)
	curDesk  uintptr
	deskName string

	// the displays (UI thread only)
	mons  []assist.Monitor // the primary one first
	virt  assist.Monitor   // the whole desktop
	sel   int              // the index shown, -1 for all
	monAt time.Time
	area  assist.Monitor // what the last picture shows, in pixels of the desktop
}

func newWinHost() (*winHost, error) {
	h := &winHost{ui: newUIThread(), hot: map[uintptr][2]int{}}
	var err error
	h.ui.do(func() {
		h.syncDesktop()
		h.refreshMonitors(true)
		a := h.currentArea()
		err = h.alloc(a.W, a.H)
	})
	return h, err
}

// syncDesktop attaches the thread to the desktop that receives the input now: the lock screen, the sign-in screen and
// the UAC prompt are on another desktop than the programs of the person.
func (h *winHost) syncDesktop() {
	hd, _, _ := procOpenInputDesktop.Call(0, 0, genericAll)
	if hd == 0 {
		return // a switch is in progress or the desktop is not ours to open: keep the one we have
	}
	var buf [64]uint16
	var need uint32
	name := ""
	if r, _, _ := procGetUserObjectInfo.Call(hd, uoiName, uintptr(unsafe.Pointer(&buf[0])), unsafe.Sizeof(buf), uintptr(unsafe.Pointer(&need))); r != 0 {
		name = strings.ToLower(windows.UTF16ToString(buf[:]))
	}
	if h.curDesk != 0 && name == h.deskName {
		_, _, _ = procCloseDesktop.Call(hd)
		return
	}
	if r, _, _ := procSetThreadDesktop.Call(hd); r == 0 {
		_, _, _ = procCloseDesktop.Call(hd)
		return
	}
	if h.curDesk != 0 {
		_, _, _ = procCloseDesktop.Call(h.curDesk)
	}
	h.curDesk, h.deskName = hd, name
}

// refreshMonitors reads the list of displays (at most every two seconds unless forced).
func (h *winHost) refreshMonitors(force bool) {
	if !force && time.Since(h.monAt) < monitorRefresh {
		return
	}
	h.monAt = time.Now()
	var list []assist.Monitor
	monitorSink = &list
	_, _, _ = procEnumDisplayMonitors.Call(0, 0, monitorEnumCb, 0)
	monitorSink = nil
	sort.SliceStable(list, func(i, j int) bool { return list[i].Primary && !list[j].Primary })
	h.mons = list
	h.virt = assist.Monitor{X: metric(smXVirtual), Y: metric(smYVirtual), W: metric(smCxVirtual), H: metric(smCyVirtual)}
	if h.virt.W <= 0 || h.virt.H <= 0 {
		h.virt = assist.Monitor{W: metric(smCxScreen), H: metric(smCyScreen)}
	}
	if len(h.mons) == 0 {
		h.mons = []assist.Monitor{{X: 0, Y: 0, W: metric(smCxScreen), H: metric(smCyScreen), Primary: true}}
	}
	if h.sel >= len(h.mons) {
		h.sel = 0
	}
}

// currentArea is the rectangle of the desktop that is shown.
func (h *winHost) currentArea() assist.Monitor {
	if h.sel < 0 {
		return h.virt
	}
	if h.sel < len(h.mons) {
		return h.mons[h.sel]
	}
	return h.mons[0]
}

func (h *winHost) alloc(w, ht int) error {
	if w <= 0 || ht <= 0 {
		return errors.New("the session has no screen")
	}
	dc, _, _ := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		return errors.New("CreateCompatibleDC failed")
	}
	bi := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(w), Height: -int32(ht), Planes: 1, Bits: 32} //nolint:gosec // screen sizes
	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		_, _, _ = procDeleteDC.Call(dc)
		return errors.New("CreateDIBSection failed")
	}
	old, _, _ := procSelectObject.Call(dc, bmp)
	h.w, h.h, h.memDC, h.bmp, h.old, h.bits = w, ht, dc, bmp, old, bits
	return nil
}

func (h *winHost) release() {
	h.ui.do(func() {
		if h.memDC != 0 {
			_, _, _ = procSelectObject.Call(h.memDC, h.old)
			_, _, _ = procDeleteObject.Call(h.bmp)
			_, _, _ = procDeleteDC.Call(h.memDC)
		}
		h.memDC, h.bmp, h.bits = 0, 0, nil
		if h.curDesk != 0 {
			_, _, _ = procCloseDesktop.Call(h.curDesk)
			h.curDesk = 0
		}
	})
}

// Grab copies the shown part of the desktop into the bitmap, draws the pointer on it and returns the bits.
func (h *winHost) Grab() (pix []byte, w, ht int, err error) {
	h.ui.do(func() { pix, w, ht, err = h.grab() })
	return
}

func (h *winHost) grab() ([]byte, int, int, error) {
	h.syncDesktop()
	h.refreshMonitors(false)
	a := h.currentArea()
	if a.W != h.w || a.H != h.h { // another display or another resolution
		if h.memDC != 0 {
			_, _, _ = procSelectObject.Call(h.memDC, h.old)
			_, _, _ = procDeleteObject.Call(h.bmp)
			_, _, _ = procDeleteDC.Call(h.memDC)
			h.memDC = 0
		}
		if err := h.alloc(a.W, a.H); err != nil {
			return nil, 0, 0, err
		}
	}
	h.area = a
	dc, _, _ := procGetDC.Call(0)
	if dc == 0 {
		return h.fail(errors.New("GetDC failed"))
	}
	r, _, _ := procBitBlt.Call(h.memDC, 0, 0, uintptr(h.w), uintptr(h.h), dc, uintptr(a.X), uintptr(a.Y), srcCopy|captureBlt)
	_, _, _ = procReleaseDC.Call(0, dc)
	if r == 0 {
		return h.fail(errors.New("BitBlt failed"))
	}
	h.failures = 0
	if h.showCursor {
		h.drawCursor()
	}
	return unsafe.Slice((*byte)(h.bits), h.w*h.h*4), h.w, h.h, nil
}

// fail tolerates a short time without a picture (a switch of the desktop) and gives the old picture meanwhile.
func (h *winHost) fail(err error) ([]byte, int, int, error) {
	h.failures++
	if h.failures > 50 || h.bits == nil {
		return nil, 0, 0, err
	}
	time.Sleep(50 * time.Millisecond)
	return unsafe.Slice((*byte)(h.bits), h.w*h.h*4), h.w, h.h, nil
}

// drawCursor puts the pointer of the session on the picture.
func (h *winHost) drawCursor() {
	ci := cursorInfo{Size: uint32(unsafe.Sizeof(cursorInfo{}))}
	if r, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci))); r == 0 || ci.Flags&1 == 0 || ci.Cursor == 0 {
		return
	}
	hot, ok := h.hot[ci.Cursor]
	if !ok {
		var ii iconInfo
		if r, _, _ := procGetIconInfo.Call(ci.Cursor, uintptr(unsafe.Pointer(&ii))); r != 0 {
			hot = [2]int{int(ii.XHot), int(ii.YHot)}
			if ii.Mask != 0 {
				_, _, _ = procDeleteObject.Call(ii.Mask)
			}
			if ii.Color != 0 {
				_, _, _ = procDeleteObject.Call(ii.Color)
			}
		}
		h.hot[ci.Cursor] = hot
	}
	_, _, _ = procDrawIconEx.Call(h.memDC, uintptr(int(ci.X)-h.area.X-hot[0]), uintptr(int(ci.Y)-h.area.Y-hot[1]), ci.Cursor, 0, 0, 0, 0, diNormal)
}

func (h *winHost) Monitors() (list []assist.Monitor, sel int) {
	h.ui.do(func() {
		h.refreshMonitors(false)
		list, sel = append([]assist.Monitor(nil), h.mons...), h.sel
	})
	return
}

func (h *winHost) SelectMonitor(i int) (err error) {
	h.ui.do(func() {
		h.refreshMonitors(true)
		if i < -1 || i >= len(h.mons) {
			err = errors.New("no such monitor")
			return
		}
		h.sel = i
	})
	return
}

func (h *winHost) Desktop() (name string) {
	h.ui.do(func() { name = h.deskName })
	return
}

func (h *winHost) SetShowCursor(show bool) { h.ui.do(func() { h.showCursor = show }) }

func (h *winHost) ClipboardGet() (text string, err error) {
	h.ui.do(func() {
		h.syncDesktop()
		text, err = clipboardGet()
	})
	return
}

func (h *winHost) ClipboardSet(text string) (err error) {
	h.ui.do(func() {
		h.syncDesktop()
		err = clipboardSet(text)
	})
	return
}

// ptrOf turns an address that Windows returned (a block of global memory) into a pointer.
func ptrOf(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

func openClipboard() error {
	for i := 0; i < 10; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			return nil
		}
		time.Sleep(20 * time.Millisecond) // another program holds it for a moment
	}
	return errors.New("the clipboard is busy")
}

func clipboardGet() (string, error) {
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer func() { _, _, _ = procCloseClipboard.Call() }()
	hm, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if hm == 0 {
		return "", nil // no text on the clipboard
	}
	size, _, _ := procGlobalSize.Call(hm)
	p, _, _ := procGlobalLock.Call(hm)
	if p == 0 {
		return "", errors.New("cannot lock the clipboard data")
	}
	defer func() { _, _, _ = procGlobalUnlock.Call(hm) }()
	units := unsafe.Slice((*uint16)(ptrOf(p)), int(size)/2)
	n := 0
	for n < len(units) && units[n] != 0 {
		n++
	}
	text := strings.ReplaceAll(windows.UTF16ToString(units[:n]), "\r\n", "\n")
	if len(text) > assist.MaxClip {
		return "", errors.New("the clipboard is too long to carry")
	}
	return text, nil
}

func clipboardSet(text string) error {
	if len(text) > assist.MaxClip {
		return errors.New("the clipboard text is too long")
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	u, err := windows.UTF16FromString(text)
	if err != nil {
		return errors.New("the text has a zero character")
	}
	hm, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(u)*2))
	if hm == 0 {
		return errors.New("out of memory")
	}
	p, _, _ := procGlobalLock.Call(hm)
	if p == 0 {
		_, _, _ = procGlobalFree.Call(hm)
		return errors.New("cannot lock the memory")
	}
	copy(unsafe.Slice((*uint16)(ptrOf(p)), len(u)), u)
	_, _, _ = procGlobalUnlock.Call(hm)
	if err := openClipboard(); err != nil {
		_, _, _ = procGlobalFree.Call(hm)
		return err
	}
	defer func() { _, _, _ = procCloseClipboard.Call() }()
	_, _, _ = procEmptyClipboard.Call()
	if r, _, _ := procSetClipboardData.Call(cfUnicodeText, hm); r == 0 {
		_, _, _ = procGlobalFree.Call(hm)
		return errors.New("SetClipboardData failed")
	}
	return nil // the clipboard owns the memory now
}

func sendMouse(dx, dy int32, data, flags uint32) error {
	rec := inputRec{Type: inMouse}
	*(*mouseInput)(unsafe.Pointer(&rec.U[0])) = mouseInput{DX: dx, DY: dy, Data: data, Flags: flags}
	return sendRec(&rec)
}

func sendKey(scan uint16, flags uint32) error {
	rec := inputRec{Type: inKeyboard}
	*(*keybdInput)(unsafe.Pointer(&rec.U[0])) = keybdInput{Scan: scan, Flags: flags}
	return sendRec(&rec)
}

func sendRec(rec *inputRec) error {
	r, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(rec)), unsafe.Sizeof(*rec))
	if r == 0 {
		return fmt.Errorf("SendInput: %w", err)
	}
	return nil
}

// absolute turns a pixel of the desktop into the 0..65535 range of an absolute mouse move over the virtual desktop
// (origin is the left edge of it, size its width or height).
func absolute(v, origin, size int) int32 {
	if size <= 1 {
		return 0
	}
	v = max(0, min(v-origin, size-1))
	return int32((int64(v)*65535 + int64(size-1)/2) / int64(size-1)) //nolint:gosec // within 0..65535
}

// Do carries out an event of the browser; x and y are pixels of the picture that is shown.
func (h *winHost) Do(in assist.Input) (err error) {
	h.ui.do(func() {
		h.syncDesktop()
		a, v := h.area, h.virt
		if a.W == 0 {
			a = h.currentArea()
		}
		pos := func() (int32, int32) { return absolute(a.X+in.X, v.X, v.W), absolute(a.Y+in.Y, v.Y, v.H) }
		switch in.T {
		case assist.InMove:
			x, y := pos()
			err = sendMouse(x, y, 0, mevMove|mevAbsolute|mevVirtualDesk)
		case assist.InBtn:
			var f, data uint32
			switch in.B {
			case 0:
				f = map[bool]uint32{true: mevLeftDown, false: mevLeftUp}[in.Down]
			case 1:
				f = map[bool]uint32{true: mevMiddleDown, false: mevMiddleUp}[in.Down]
			case 2:
				f = map[bool]uint32{true: mevRightDown, false: mevRightUp}[in.Down]
			default: // the side buttons
				f = map[bool]uint32{true: mevXDown, false: mevXUp}[in.Down]
				data = uint32(in.B - 2) //nolint:gosec // 1 or 2: XBUTTON1, XBUTTON2
			}
			x, y := pos()
			err = sendMouse(x, y, data, mevMove|mevAbsolute|mevVirtualDesk|f)
		case assist.InWheel:
			if in.DY != 0 {
				if err = sendMouse(0, 0, uint32(int32(-in.DY*120)), mevWheel); err != nil { //nolint:gosec // the notches are bounded
					return
				}
			}
			if in.DX != 0 {
				err = sendMouse(0, 0, uint32(int32(in.DX*120)), mevHWheel) //nolint:gosec // same
			}
		case assist.InKey:
			f := uint32(kevScancode)
			if !in.Down {
				f |= kevKeyUp
			}
			if in.Ext {
				f |= kevExtended
			}
			err = sendKey(uint16(in.Sc), f) //nolint:gosec // 1..255
		}
		// Ctrl+Alt+Del is not here: only a service may send it, the agent takes it out of the stream.
	})
	return
}

// TypeText types a text as characters (SendInput with KEYEVENTF_UNICODE: no key codes, so the layout of the computer does not
// matter and the clipboard is not used) and then presses Tab or Enter. Only while the input desktop is Winlogon (the sign-in,
// the lock screen or a UAC prompt): this is checked here, on the thread that sends the input, so a prompt that went away a
// moment ago cannot get a password typed into the window of the person.
func (h *winHost) TypeText(text, after string) (err error) {
	h.ui.do(func() {
		h.syncDesktop()
		if h.deskName != "winlogon" {
			err = errTypeDesktop
			return
		}
		for _, u := range utf16.Encode([]rune(text)) {
			if err = sendKey(u, kevUnicode); err != nil {
				return
			}
			if err = sendKey(u, kevUnicode|kevKeyUp); err != nil {
				return
			}
			time.Sleep(4 * time.Millisecond) // a field of a sign-in screen drops characters that come too fast
		}
		var sc uint16
		switch after {
		case "tab":
			sc = 0x0F
		case "enter":
			sc = 0x1C
		}
		if sc != 0 {
			time.Sleep(40 * time.Millisecond)
			if err = sendKey(sc, kevScancode); err != nil {
				return
			}
			err = sendKey(sc, kevScancode|kevKeyUp)
		}
	})
	return
}

// sendSAS sends the secure attention sequence (Ctrl+Alt+Del) to the console session. Only a service may do it, so it
// runs in the agent, not in the helper; Windows also has to allow it (see assistSasPolicy).
func sendSAS() error {
	if err := procSendSAS.Find(); err != nil {
		return err
	}
	_, _, _ = procSendSAS.Call(0)
	return nil
}

// RunAssistHelper is the process the service starts in a session of Windows (`linager agent assistant-helper`). It
// calls the agent back with the one-time token, then serves the screen of its session until the connection ends. The
// returned code is the exit code of the process.
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
	// the pixels of the picture are the pixels of the screen, whatever the scaling of the display is
	if procSetDpiAwarenessCtx.Find() == nil {
		_, _, _ = procSetDpiAwarenessCtx.Call(dpiPerMonitorV2)
	} else {
		_, _, _ = procSetDPIAware.Call()
	}
	wh, err := newWinHost()
	if err != nil {
		_ = assist.WriteJSON(conn, assist.TypeEnd, assist.End{Reason: "cannot open the screen: " + err.Error()})
		return helperExitScreen, err
	}
	defer wh.release()
	info := assist.Hello{Session: session, User: strings.TrimSpace(sessionUser(uint32(session)))} //nolint:gosec // a session number
	if err := runAssistLoop(ctx, conn, wh, info, nil); err != nil {
		return helperExitLoop, err
	}
	return 0, nil
}
