package agent

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// The pure parts of the capture below the compositor (DRM/KMS) and of the input through uinput: the formats of the
// framebuffer, the copy of a region of it, the events of the input device. They are tested on every platform.

// Pixel formats of a framebuffer (DRM fourcc codes, little endian). The memory order of XRGB8888 is B, G, R, X: it is the
// BGRA the encoder wants.
const (
	drmFormatXRGB8888 = 0x34325258 // XR24
	drmFormatARGB8888 = 0x34325241 // AR24
	drmFormatXBGR8888 = 0x34324258 // XB24
	drmFormatABGR8888 = 0x34324241 // AB24
	drmFormatRGB565   = 0x36314752 // RG16
)

// The modifiers that tell a plain (linear) framebuffer: a tiled or compressed one is read by the GPU only.
const (
	drmModLinear  = 0
	drmModInvalid = 0x00ffffffffffffff // the driver does not say: the old drivers, whose buffers are linear
)

// fourcc spells a format code the way it is written in the headers.
func fourcc(f uint32) string {
	b := []byte{byte(f & 0xff), byte(f >> 8 & 0xff), byte(f >> 16 & 0xff), byte(f >> 24 & 0xff)}
	for i, c := range b {
		if c < 0x20 || c > 0x7e {
			b[i] = '?'
		}
	}
	return string(b)
}

// formatSupported tells whether copyRegion knows the format.
func formatSupported(f uint32) bool {
	switch f {
	case drmFormatXRGB8888, drmFormatARGB8888, drmFormatXBGR8888, drmFormatABGR8888, drmFormatRGB565:
		return true
	}
	return false
}

// copyRegion copies the rectangle (x, y, w, h) of a framebuffer (src, pitch bytes a row, the format) into dst as BGRA
// without gaps between the rows. dst must hold w*h*4 bytes.
func copyRegion(dst, src []byte, pitch int, format uint32, x, y, w, h int) error {
	if !formatSupported(format) {
		return fmt.Errorf("the pixel format %s of the screen is not supported", fourcc(format))
	}
	bpp := 4
	if format == drmFormatRGB565 {
		bpp = 2
	}
	if w <= 0 || h <= 0 || x < 0 || y < 0 || pitch < (x+w)*bpp || len(dst) < w*h*4 || len(src) < (y+h-1)*pitch+(x+w)*bpp {
		return fmt.Errorf("the screen %dx%d at %d,%d does not fit its framebuffer", w, h, x, y)
	}
	for row := 0; row < h; row++ {
		s := src[(y+row)*pitch+x*bpp:]
		d := dst[row*w*4 : (row+1)*w*4]
		switch format {
		case drmFormatXRGB8888, drmFormatARGB8888:
			copy(d, s[:w*4])
			if format == drmFormatXRGB8888 {
				for i := 3; i < len(d); i += 4 {
					d[i] = 255
				}
			}
		case drmFormatXBGR8888, drmFormatABGR8888:
			for i := 0; i < w*4; i += 4 {
				d[i], d[i+1], d[i+2], d[i+3] = s[i+2], s[i+1], s[i], 255 //nolint:gosec // i+3 < w*4 and s holds the row
			}
		case drmFormatRGB565:
			for i := 0; i < w; i++ {
				v := binary.LittleEndian.Uint16(s[i*2:])
				r, g, b := byte(v>>11&0x1f), byte(v>>5&0x3f), byte(v&0x1f)
				d[i*4], d[i*4+1], d[i*4+2], d[i*4+3] = b<<3|b>>2, g<<2|g>>4, r<<3|r>>2, 255
			}
		}
	}
	return nil
}

// Event types and codes of the input subsystem (linux/input-event-codes.h).
const (
	evSyn      = 0
	evKey      = 1
	evRel      = 2
	evAbs      = 3
	absX       = 0
	absY       = 1
	relHWheel  = 6
	relWheel   = 8
	synReport  = 0
	absMax     = 65535 // the range of the absolute pointer; the browser's pixels are scaled to it
	inputEvLen = 24    // struct input_event on a 64-bit system: the time (two int64), the type, the code, the value
)

// inputEvent is the struct input_event written to /dev/uinput (the time is left zero: the kernel stamps it).
func inputEvent(typ, code uint16, value int32) []byte {
	b := make([]byte, inputEvLen)
	binary.LittleEndian.PutUint16(b[16:], typ)
	binary.LittleEndian.PutUint16(b[18:], code)
	binary.LittleEndian.PutUint32(b[20:], uint32(value)) //nolint:gosec // the bits of a signed value
	return b
}

// scaleAbs turns a pixel of the picture into the range of the absolute pointer.
func scaleAbs(v, size int) int32 {
	if size <= 1 {
		return 0
	}
	v = max(0, min(v, size-1))
	return int32(int64(v) * absMax / int64(size-1)) //nolint:gosec // 0..65535
}

// wakeTimeout is how long a dark screen is given to come back after the input that wakes it (a monitor needs a few seconds
// to find its signal); a nudge is repeated every wakeNudge.
const (
	wakeTimeout = 10 * time.Second
	wakeNudge   = 2 * time.Second
	wakeStep    = 200 * time.Millisecond
)

var errScreenStaysOff = errors.New("the screen is off and did not wake up (the monitor may be switched off, or the computer is asleep)")

// waitForScreen wakes a dark screen: it tells the browser (say), sends an input that wakes the monitor (nudge) and looks
// (lit) every step for the picture, nudging again every `every`. It returns when the screen shows a picture, or an error
// when it stays dark for `timeout`.
func waitForScreen(lit func() bool, nudge func(), say func(on bool), timeout, every, step time.Duration) error {
	if say != nil {
		say(true)
	}
	nudge()
	start := time.Now()
	last := start
	for time.Since(start) < timeout {
		time.Sleep(step)
		if lit() {
			if say != nil {
				say(false)
			}
			return nil
		}
		if time.Since(last) >= every {
			nudge()
			last = time.Now()
		}
	}
	return errScreenStaysOff
}
