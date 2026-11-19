//go:build linux

package agent

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/bolgov0zero/linager/internal/assist"
)

// The capture below the compositor: the framebuffer the display controller shows is read through DRM (the kernel
// interface of the graphics card). A compositor cannot forbid it, so it shows the sign-in screen and the lock screen
// that GNOME does not let a screen cast see. It needs root, and it reads only a plain (linear) framebuffer: that is
// what virtual machines and many cards have; a tiled one is read by the GPU only.

// ioctl numbers of DRM (drm.h, drm_mode.h): _IOWR('d', nr, struct).
func drmIOWR(nr, size uintptr) uintptr { return 3<<30 | size<<16 | uintptr('d')<<8 | nr }

// drmIoctl calls the DRM ioctl nr with a struct of the kernel interface (its size is a part of the request number).
func drmIoctl[T any](fd int, nr uintptr, v *T) error {
	return ioctlPtr(fd, drmIOWR(nr, unsafe.Sizeof(*v)), unsafe.Pointer(v)) //nolint:gosec // a struct of the kernel interface
}

type drmModeCardRes struct {
	fbIDPtr, crtcIDPtr, connectorIDPtr, encoderIDPtr     uint64
	countFBs, countCRTCs, countConnectors, countEncoders uint32
	_                                                    [4]uint32 // the limits of the size of a framebuffer
}

type drmModeModeInfo struct {
	_        uint32 // clock
	hdisplay uint16
	_        [4]uint16 // hsync_start, hsync_end, htotal, hskew
	vdisplay uint16
	_        [4]uint16 // vsync_start, vsync_end, vtotal, vscan
	_        [3]uint32 // vrefresh, flags, type
	_        [32]byte  // name
}

type drmModeCrtc struct {
	_                  uint64 // set_connectors_ptr
	_                  uint32 // count_connectors
	crtcID, fbID, x, y uint32
	_                  uint32 // gamma_size
	mode               uint32 // mode_valid: 1 when the controller shows something
	info               drmModeModeInfo
}

type drmModeFBCmd2 struct {
	fbID, width, height, pixelFormat, flags uint32
	handles, pitches, offsets               [4]uint32
	modifier                                [4]uint64
}

type drmPrimeHandle struct {
	handle, flags uint32
	fd            int32
}

type drmGemClose struct {
	handle uint32
	_      uint32
}

type drmMapDumb struct {
	handle uint32
	_      uint32
	offset uint64
}

// kmsOutput is a controller that shows a picture.
type kmsOutput struct {
	card int // the index of the card in kmsHost.cards
	crtc uint32
	fb   uint32
	x, y int
	w, h int
}

// kmsMap is a framebuffer mapped into this process.
type kmsMap struct {
	data   []byte
	dmaFD  int // the dma-buf, -1 when the buffer was mapped as a dumb buffer
	handle uint32
	card   int
	format uint32
	pitch  int
	width  int
	height int
}

// kmsHost is the screen read below the compositor and the input through uinput.
type kmsHost struct {
	log *slog.Logger
	ui  *uinputDev

	mu    sync.Mutex
	cards []int // the file descriptors of /dev/dri/card*
	outs  []kmsOutput
	sel   int
	out   []byte
	lastW int
	lastH int
}

func newKMSHost(log *slog.Logger) (*kmsHost, error) {
	h := &kmsHost{log: log}
	paths, _ := filepath.Glob("/dev/dri/card[0-9]*")
	sort.Strings(paths)
	for _, p := range paths {
		fd, err := unix.Open(p, unix.O_RDWR|unix.O_CLOEXEC, 0)
		if err != nil {
			log.Debug("assistant drm", "card", p, "err", err)
			continue
		}
		h.cards = append(h.cards, fd)
	}
	if len(h.cards) == 0 {
		return nil, errors.New("no graphics card (/dev/dri/card*) can be opened: the screen cannot be read below the compositor")
	}
	if err := h.scan(); err != nil {
		h.release()
		return nil, err
	}
	if len(h.outs) == 0 {
		h.release()
		return nil, errors.New("no display shows a picture now")
	}
	ui, err := newUinput()
	if err != nil {
		h.release()
		return nil, err
	}
	h.ui = ui
	return h, nil
}

// scan reads which controllers show a picture and in what size.
func (h *kmsHost) scan() error {
	var outs []kmsOutput
	var firstErr error
	for ci, fd := range h.cards {
		var res drmModeCardRes
		if err := drmIoctl(fd, 0xA0, &res); err != nil { //nolint:gosec // a struct of the kernel interface
			firstErr = err
			continue
		}
		if res.countCRTCs == 0 {
			continue
		}
		crtcs := make([]uint32, res.countCRTCs)
		res.crtcIDPtr = uint64(uintptr(unsafe.Pointer(&crtcs[0]))) //nolint:gosec // an array for the kernel to fill
		res.countFBs, res.countConnectors, res.countEncoders = 0, 0, 0
		res.fbIDPtr, res.connectorIDPtr, res.encoderIDPtr = 0, 0, 0
		if err := drmIoctl(fd, 0xA0, &res); err != nil { //nolint:gosec // a struct of the kernel interface
			firstErr = err
			continue
		}
		for _, id := range crtcs[:min(int(res.countCRTCs), len(crtcs))] {
			c := drmModeCrtc{crtcID: id}
			if err := drmIoctl(fd, 0xA1, &c); err != nil { //nolint:gosec // a struct of the kernel interface
				continue
			}
			if c.mode == 0 || c.fbID == 0 || c.info.hdisplay == 0 {
				continue
			}
			outs = append(outs, kmsOutput{card: ci, crtc: id, fb: c.fbID, x: int(c.x), y: int(c.y), w: int(c.info.hdisplay), h: int(c.info.vdisplay)})
		}
	}
	if len(outs) == 0 && firstErr != nil {
		return fmt.Errorf("the graphics card did not answer: %w", firstErr)
	}
	h.outs = outs
	if h.sel >= len(outs) {
		h.sel = 0
	}
	return nil
}

// framebuffer maps the framebuffer an output shows now. It is mapped for every picture and let go after it: the number of a
// framebuffer is given again to another buffer when the compositor changes (the sign-in screen gives way to the desktop),
// and a map kept by that number would show the old buffer.
func (h *kmsHost) framebuffer(o kmsOutput) (*kmsMap, error) {
	fd := h.cards[o.card]
	var f drmModeFBCmd2
	f.fbID = o.fb
	if err := drmIoctl(fd, 0xCE, &f); err != nil {
		return nil, fmt.Errorf("the framebuffer of the screen (a kernel with GETFB2 and root are needed): %w", err)
	}
	if f.handles[0] == 0 {
		return nil, errors.New("the kernel gave no handle of the framebuffer: the agent must run as root")
	}
	if mod := f.modifier[0]; f.flags&2 != 0 /* DRM_MODE_FB_MODIFIERS */ && mod != drmModLinear && mod != drmModInvalid {
		return nil, fmt.Errorf("the framebuffer of the screen is tiled (modifier %#x): only a GPU can read it, the Assistant cannot show this screen below the compositor", mod)
	}
	if !formatSupported(f.pixelFormat) {
		return nil, fmt.Errorf("the pixel format %s of the screen is not supported", fourcc(f.pixelFormat))
	}
	m := &kmsMap{dmaFD: -1, handle: f.handles[0], card: o.card, format: f.pixelFormat, pitch: int(f.pitches[0]), width: int(f.width), height: int(f.height)}
	size := int(f.offsets[0]) + m.pitch*m.height
	// the dma-buf of the buffer is mapped and read; a driver that does not export one maps a dumb buffer
	p := drmPrimeHandle{handle: f.handles[0], flags: unix.O_CLOEXEC}
	if err := drmIoctl(fd, 0x2D, &p); err == nil {
		if data, err := unix.Mmap(int(p.fd), 0, size, unix.PROT_READ, unix.MAP_SHARED); err == nil {
			m.data, m.dmaFD = data[f.offsets[0]:], int(p.fd)
		} else {
			_ = unix.Close(int(p.fd))
		}
	}
	if m.data == nil {
		var d drmMapDumb
		d.handle = f.handles[0]
		if err := drmIoctl(fd, 0xB3, &d); err != nil {
			h.closeHandle(fd, m.handle)
			return nil, fmt.Errorf("the framebuffer cannot be mapped (the driver exports no dma-buf and it is not a dumb buffer): %w", err)
		}
		data, err := unix.Mmap(fd, int64(d.offset), size, unix.PROT_READ, unix.MAP_SHARED) //nolint:gosec // an offset the kernel gave
		if err != nil {
			h.closeHandle(fd, m.handle)
			return nil, fmt.Errorf("map the framebuffer: %w", err)
		}
		m.data = data[f.offsets[0]:]
	} else {
		h.closeHandle(fd, m.handle) // the dma-buf holds the buffer
		m.handle = 0
	}
	return m, nil
}

func (h *kmsHost) closeHandle(fd int, handle uint32) {
	g := drmGemClose{handle: handle}
	_ = drmIoctl(fd, 0x09, &g)
}

// unmap lets go of a mapped framebuffer.
func (h *kmsHost) unmap(m *kmsMap) {
	if m == nil {
		return
	}
	if m.data != nil {
		_ = unix.Munmap(m.data)
	}
	if m.dmaFD >= 0 {
		_ = unix.Close(m.dmaFD)
	}
	if m.handle != 0 {
		h.closeHandle(h.cards[m.card], m.handle)
	}
}

// The flags of DMA_BUF_IOCTL_SYNC.
const (
	dmaSyncRead  = 1
	dmaSyncStart = 0
	dmaSyncEnd   = 4
)

// dmaSync brackets the reading of a dma-buf: the CPU must see what the GPU wrote.
func dmaSync(fd int, flags uint64) {
	if fd < 0 {
		return
	}
	_ = ioctlPtr(fd, 1<<30|8<<16|uintptr('b')<<8, unsafe.Pointer(&flags)) //nolint:gosec // DMA_BUF_IOCTL_SYNC
}

func (h *kmsHost) release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, fd := range h.cards {
		_ = unix.Close(fd)
	}
	h.cards = nil
	if h.ui != nil {
		h.ui.close()
		h.ui = nil
	}
}

// ---- assistHost ----

func (h *kmsHost) Grab() ([]byte, int, int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.scan(); err != nil {
		return nil, 0, 0, err
	}
	if len(h.outs) == 0 {
		return nil, 0, 0, errors.New("no display shows a picture now")
	}
	o := h.outs[h.sel]
	m, err := h.framebuffer(o)
	if err != nil {
		return nil, 0, 0, err
	}
	defer h.unmap(m)
	x, y, w, hh := o.x, o.y, min(o.w, m.width-o.x), min(o.h, m.height-o.y)
	if len(h.out) != w*hh*4 {
		h.out = make([]byte, w*hh*4)
	}
	dmaSync(m.dmaFD, dmaSyncStart|dmaSyncRead)
	err = copyRegion(h.out, m.data, m.pitch, m.format, x, y, w, hh)
	dmaSync(m.dmaFD, dmaSyncEnd|dmaSyncRead)
	if err != nil {
		return nil, 0, 0, err
	}
	h.lastW, h.lastH = w, hh
	return h.out, w, hh, nil
}

func (h *kmsHost) Do(in assist.Input) error {
	h.mu.Lock()
	ui, w, hh := h.ui, h.lastW, h.lastH
	h.mu.Unlock()
	if ui == nil {
		return errors.New("the input device is not open")
	}
	switch in.T {
	case assist.InMove:
		return ui.moveTo(in.X, in.Y, w, hh)
	case assist.InBtn:
		if err := ui.moveTo(in.X, in.Y, w, hh); err != nil {
			return err
		}
		return ui.button(mouseButtonEvdev[in.B], in.Down)
	case assist.InWheel:
		return ui.wheel(in.DX, in.DY)
	case assist.InKey:
		code, ok := scanToEvdev(in.Sc, in.Ext)
		if !ok {
			return nil
		}
		return ui.key(code, in.Down)
	}
	return nil
}

func (h *kmsHost) Monitors() ([]assist.Monitor, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	mons := make([]assist.Monitor, 0, len(h.outs))
	for i, o := range h.outs {
		mons = append(mons, assist.Monitor{X: o.x, Y: o.y, W: o.w, H: o.h, Primary: i == 0})
	}
	return mons, h.sel
}

func (h *kmsHost) SelectMonitor(i int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if i < 0 || i >= len(h.outs) {
		return errors.New("that display cannot be shown")
	}
	h.sel = i
	return nil
}

func (h *kmsHost) ClipboardGet() (string, error) {
	return "", errors.New("the clipboard is not available below the compositor")
}

func (h *kmsHost) ClipboardSet(string) error {
	return errors.New("the clipboard is not available below the compositor")
}

// Desktop is "winlogon" for the panel: this is the screen of the sign-in or the lock, which the badge of the picture names.
func (h *kmsHost) Desktop() string { return "winlogon" }

// SetShowCursor: the pointer is not in the picture of the controller (it is a plane of its own).
func (h *kmsHost) SetShowCursor(bool) {}
