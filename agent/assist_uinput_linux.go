//go:build linux

package agent

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// uinputDev is a virtual keyboard and pointer made through /dev/uinput. The kernel hands its events to whatever reads the
// input devices of the seat (the sign-in screen, the lock screen, the desktop), so it works below the compositor.
type uinputDev struct {
	mu sync.Mutex
	f  *os.File
}

// ioctl request numbers (linux/uinput.h); the size of the argument is in the number.
func uiIO(nr uintptr) uintptr                { return uintptr('U')<<8 | nr }
func uiIOW(nr, size uintptr) uintptr         { return 1<<30 | size<<16 | uintptr('U')<<8 | nr }
func uinputSet(fd int, req, v uintptr) error { return ioctlVal(fd, req, v) }

type uinputSetup struct {
	bustype, vendor, product, version uint16
	name                              [80]byte
	ffEffectsMax                      uint32
}

type inputAbsinfo struct {
	value, minimum, maximum, fuzz, flat, resolution int32
}

type uinputAbsSetup struct {
	code uint16
	_    uint16
	info inputAbsinfo
}

func ioctlVal(fd int, req, v uintptr) error {
	if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, v); e != 0 {
		return e
	}
	return nil
}

func ioctlPtr(fd int, req uintptr, p unsafe.Pointer) error {
	if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(p)); e != 0 { //nolint:gosec // the pointer of a struct of the kernel interface
		return e
	}
	return nil
}

// newUinput makes the device: the keys of the keyboard and the mouse buttons, an absolute pointer over 0..absMax and the
// wheels. The caller must be root (or have the access to /dev/uinput).
func newUinput() (*uinputDev, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("/dev/uinput (the kernel module uinput and root are needed): %w", err)
	}
	fd := int(f.Fd()) //nolint:gosec // a file descriptor
	fail := func(what string, err error) (*uinputDev, error) {
		_ = f.Close()
		return nil, fmt.Errorf("uinput %s: %w", what, err)
	}
	for _, ev := range []uintptr{evKey, evAbs, evRel, evSyn} {
		if err := uinputSet(fd, uiIOW(100, 4), ev); err != nil { // UI_SET_EVBIT
			return fail("event type", err)
		}
	}
	for k := uintptr(1); k <= 255; k++ { // the keys of the keyboard
		_ = uinputSet(fd, uiIOW(101, 4), k) // UI_SET_KEYBIT
	}
	for _, b := range mouseButtonEvdev {
		if err := uinputSet(fd, uiIOW(101, 4), uintptr(b&0xffff)); err != nil {
			return fail("button", err)
		}
	}
	for _, r := range []uintptr{relWheel, relHWheel} {
		if err := uinputSet(fd, uiIOW(102, 4), r); err != nil { // UI_SET_RELBIT
			return fail("wheel", err)
		}
	}
	for _, a := range []uint16{absX, absY} {
		if err := uinputSet(fd, uiIOW(103, 4), uintptr(a)); err != nil { // UI_SET_ABSBIT
			return fail("axis", err)
		}
		s := uinputAbsSetup{code: a, info: inputAbsinfo{maximum: absMax}}
		if err := ioctlPtr(fd, uiIOW(4, unsafe.Sizeof(s)), unsafe.Pointer(&s)); err != nil { //nolint:gosec // UI_ABS_SETUP
			return fail("axis range", err)
		}
	}
	_ = uinputSet(fd, uiIOW(110, 4), 0) // UI_SET_PROPBIT: INPUT_PROP_POINTER
	setup := uinputSetup{bustype: 0x06, vendor: 0x1a0f, product: 0x0001, version: 1}
	copy(setup.name[:], "Linager remote input")
	if err := ioctlPtr(fd, uiIOW(3, unsafe.Sizeof(setup)), unsafe.Pointer(&setup)); err != nil { //nolint:gosec // UI_DEV_SETUP
		return fail("setup", err)
	}
	if err := ioctlVal(fd, uiIO(1), 0); err != nil { // UI_DEV_CREATE
		return fail("create", err)
	}
	time.Sleep(250 * time.Millisecond) // udev and the compositor take the new device
	return &uinputDev{f: f}, nil
}

func (u *uinputDev) write(events ...[]byte) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.f == nil {
		return errors.New("the input device is closed")
	}
	var buf []byte
	for _, e := range events {
		buf = append(buf, e...)
	}
	buf = append(buf, inputEvent(evSyn, synReport, 0)...)
	_, err := u.f.Write(buf)
	return err
}

func (u *uinputDev) moveTo(x, y, w, h int) error {
	return u.write(inputEvent(evAbs, absX, scaleAbs(x, w)), inputEvent(evAbs, absY, scaleAbs(y, h)))
}

func (u *uinputDev) button(code int32, down bool) error {
	v := int32(0)
	if down {
		v = 1
	}
	return u.write(inputEvent(evKey, uint16(code), v)) //nolint:gosec // an evdev button code
}

func (u *uinputDev) key(code int, down bool) error {
	v := int32(0)
	if down {
		v = 1
	}
	return u.write(inputEvent(evKey, uint16(code), v)) //nolint:gosec // 1..127
}

// wheel turns the wheels; the browser counts notches down and to the right, the kernel up and to the right.
func (u *uinputDev) wheel(dx, dy int) error {
	var ev [][]byte
	if dy != 0 {
		ev = append(ev, inputEvent(evRel, relWheel, int32(-dy))) //nolint:gosec // bounded by ParseInput
	}
	if dx != 0 {
		ev = append(ev, inputEvent(evRel, relHWheel, int32(dx))) //nolint:gosec // bounded by ParseInput
	}
	if len(ev) == 0 {
		return nil
	}
	return u.write(ev...)
}

func (u *uinputDev) close() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.f != nil {
		_ = ioctlVal(int(u.f.Fd()), uiIO(2), 0) //nolint:gosec // UI_DEV_DESTROY
		_ = u.f.Close()
		u.f = nil
	}
}
