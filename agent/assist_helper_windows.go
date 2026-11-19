//go:build windows

package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/bolgov0zero/linager/internal/assist"
)

// Exit codes of the helper; the agent turns them into the reason shown to the administrator.
const (
	helperExitConnect = 10 // could not call the agent back
	helperExitScreen  = 11 // could not open the screen
	helperExitLoop    = 12 // the picture failed while running
)

func helperExitText(code uint32) string {
	switch code {
	case helperExitConnect:
		return "the helper could not call the agent back"
	case helperExitScreen:
		return "the helper could not open the screen of the session"
	case helperExitLoop:
		return "the helper stopped: the screen could not be read"
	}
	return fmt.Sprintf("the helper exited with code %d", code)
}

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")
	modGdi32  = windows.NewLazySystemDLL("gdi32.dll")
	modSas    = windows.NewLazySystemDLL("sas.dll")

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

// gdiScreen takes the picture of the primary monitor of the session this process runs in. GDI is used on purpose: it
// works in remote desktop sessions (where the desktop duplication API does not) and on every desktop of a session.
type gdiScreen struct {
	w, h     int
	memDC    uintptr
	bmp      uintptr
	old      uintptr
	bits     unsafe.Pointer
	failures int
	hot      map[uintptr][2]int // the hot spots of the cursors seen
}

func newGDIScreen() (*gdiScreen, error) {
	s := &gdiScreen{hot: map[uintptr][2]int{}}
	if err := s.alloc(metric(smCxScreen), metric(smCyScreen)); err != nil {
		return nil, err
	}
	return s, nil
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

func (s *gdiScreen) alloc(w, h int) error {
	if w <= 0 || h <= 0 {
		return errors.New("the session has no screen")
	}
	dc, _, _ := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		return errors.New("CreateCompatibleDC failed")
	}
	bi := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(w), Height: -int32(h), Planes: 1, Bits: 32} //nolint:gosec // screen sizes
	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		_, _, _ = procDeleteDC.Call(dc)
		return errors.New("CreateDIBSection failed")
	}
	old, _, _ := procSelectObject.Call(dc, bmp)
	s.w, s.h, s.memDC, s.bmp, s.old, s.bits = w, h, dc, bmp, old, bits
	return nil
}

func (s *gdiScreen) release() {
	if s.memDC != 0 {
		_, _, _ = procSelectObject.Call(s.memDC, s.old)
		_, _, _ = procDeleteObject.Call(s.bmp)
		_, _, _ = procDeleteDC.Call(s.memDC)
	}
	s.memDC, s.bmp, s.bits = 0, 0, nil
}

// Grab copies the screen into the bitmap, draws the pointer on it and returns the bits.
func (s *gdiScreen) Grab() ([]byte, int, int, error) {
	if w, h := metric(smCxScreen), metric(smCyScreen); (w != s.w || h != s.h) && w > 0 && h > 0 { // a change of the resolution
		s.release()
		if err := s.alloc(w, h); err != nil {
			return nil, 0, 0, err
		}
	}
	dc, _, _ := procGetDC.Call(0)
	if dc == 0 {
		return s.fail(errors.New("GetDC failed"))
	}
	r, _, _ := procBitBlt.Call(s.memDC, 0, 0, uintptr(s.w), uintptr(s.h), dc, 0, 0, srcCopy|captureBlt)
	_, _, _ = procReleaseDC.Call(0, dc)
	if r == 0 {
		return s.fail(errors.New("BitBlt failed"))
	}
	s.failures = 0
	s.drawCursor()
	return unsafe.Slice((*byte)(s.bits), s.w*s.h*4), s.w, s.h, nil
}

// fail tolerates a short time without a picture (a switch of the desktop) and gives the old picture meanwhile.
func (s *gdiScreen) fail(err error) ([]byte, int, int, error) {
	s.failures++
	if s.failures > 50 || s.bits == nil {
		return nil, 0, 0, err
	}
	time.Sleep(50 * time.Millisecond)
	return unsafe.Slice((*byte)(s.bits), s.w*s.h*4), s.w, s.h, nil
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

// drawCursor puts the pointer of the session on the picture: the viewer sees where the mouse is without a second
// picture of it.
func (s *gdiScreen) drawCursor() {
	ci := cursorInfo{Size: uint32(unsafe.Sizeof(cursorInfo{}))}
	if r, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci))); r == 0 || ci.Flags&1 == 0 || ci.Cursor == 0 {
		return
	}
	hot, ok := s.hot[ci.Cursor]
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
		s.hot[ci.Cursor] = hot
	}
	_, _, _ = procDrawIconEx.Call(s.memDC, uintptr(int(ci.X)-hot[0]), uintptr(int(ci.Y)-hot[1]), ci.Cursor, 0, 0, 0, 0, diNormal)
}

// winInput carries out the events of the browser with SendInput.
type winInput struct{}

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
)

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

// absolute turns a pixel of the screen into the 0..65535 range of an absolute mouse move.
func absolute(v, size int) int32 {
	if size <= 1 {
		return 0
	}
	v = max(0, min(v, size-1))
	return int32((int64(v)*65535 + int64(size-1)/2) / int64(size-1)) //nolint:gosec // within 0..65535
}

func (winInput) Do(in assist.Input) error {
	w, h := metric(smCxScreen), metric(smCyScreen)
	switch in.T {
	case assist.InMove:
		return sendMouse(absolute(in.X, w), absolute(in.Y, h), 0, mevMove|mevAbsolute)
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
		return sendMouse(absolute(in.X, w), absolute(in.Y, h), data, mevMove|mevAbsolute|f)
	case assist.InWheel:
		if in.DY != 0 {
			if err := sendMouse(0, 0, uint32(int32(-in.DY*120)), mevWheel); err != nil { //nolint:gosec // the notches are bounded
				return err
			}
		}
		if in.DX != 0 {
			return sendMouse(0, 0, uint32(int32(in.DX*120)), mevHWheel) //nolint:gosec // same
		}
		return nil
	case assist.InKey:
		f := uint32(kevScancode)
		if !in.Down {
			f |= kevKeyUp
		}
		if in.Ext {
			f |= kevExtended
		}
		return sendKey(uint16(in.Sc), f) //nolint:gosec // 1..255
	case assist.InCAD:
		// The secure attention sequence. Windows lets only a service (or the policy "Software SAS generation") send it.
		if r, _, err := procSendSAS.Call(0); r == 0 && err != nil && !errors.Is(err, windows.ERROR_SUCCESS) {
			return fmt.Errorf("SendSAS: %w", err)
		}
	}
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
	_, _, _ = procSetDPIAware.Call()
	scr, err := newGDIScreen()
	if err != nil {
		_ = assist.WriteJSON(conn, assist.TypeEnd, assist.End{Reason: "cannot open the screen: " + err.Error()})
		return helperExitScreen, err
	}
	defer scr.release()
	info := assist.Hello{Session: session, User: strings.TrimSpace(sessionUser(uint32(session)))} //nolint:gosec // a session number
	if err := runAssistLoop(ctx, conn, scr, winInput{}, info, nil); err != nil {
		return helperExitLoop, err
	}
	return 0, nil
}
